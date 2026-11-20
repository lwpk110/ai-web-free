package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/db"

	_ "github.com/glebarez/go-sqlite"
)

var tokenCredentialFiles = []string{
	"access_tokens.txt",
	"kami_accounts.txt",
	"refresh_tokens.txt",
	"session_tokens.txt",
	"free_tokens.txt",
	"proxies.txt",
	"account_proxies.txt",
	"banned_accounts.txt",
	"disabled_accounts.txt",
	"grok_accounts.txt",
}

// AutoRestoreDBIfMissing 检查本地 SQLite 数据库是否存在，若不存在且云端存在备份，则自动从七牛云拉取恢复冷启动
func AutoRestoreDBIfMissing(dbPath string, client *QiniuClient) {
	if client == nil {
		return
	}
	if dbPath == "" || dbPath == "default" {
		dbPath = "data/ai-web-free.db"
	}
	// 忽略内存库或非 SQLite 连接串
	if dbPath == ":memory:" || strings.Contains(dbPath, "://") || strings.Contains(dbPath, "@tcp(") {
		return
	}

	if _, err := os.Stat(dbPath); err == nil {
		// 本地数据库已存在，直接使用
		return
	}

	log.Printf("[Storage] 本地 SQLite 数据库 %q 不存在，正在检查七牛云端备份...", dbPath)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if !client.Exists(ctx, "_backups/ai-web-free.db") {
		log.Printf("[Storage] 七牛云端未发现历史数据库快照，将自动新建初始化数据库")
		return
	}

	data, err := client.Download(ctx, "_backups/ai-web-free.db")
	if err != nil {
		log.Printf("[Storage] 从七牛云拉取数据库快照失败: %v", err)
		return
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		log.Printf("[Storage] 创建数据库目录失败: %v", err)
		return
	}

	if err := os.WriteFile(dbPath, data, 0644); err != nil {
		log.Printf("[Storage] 写入拉取的数据库失败: %v", err)
		return
	}

	log.Printf("[Storage] 成功从七牛云恢复 SQLite 数据库快照至 %s (大小: %.2f MB)", dbPath, float64(len(data))/(1024*1024))
	syncCloudCredentialsIntoDB(ctx, client, dbPath)
}

// CheckpointSQLite 触发 SQLite 执行 WAL checkpoint 将日志刷入主数据库文件
func CheckpointSQLite(dbPath string) error {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	return err
}

// PushBackup 将本地 SQLite 数据库、.env 配置与多媒体缓存全量同步推送到七牛云
func PushBackup(client *QiniuClient, dbPath, envPath, storageDir, videosDir string) error {
	if client == nil {
		return fmt.Errorf("qiniu client is nil")
	}
	if dbPath == "" {
		dbPath = "data/ai-web-free.db"
	}
	if envPath == "" {
		envPath = ".env"
	}
	if storageDir == "" {
		storageDir = "data/storage"
	}
	if videosDir == "" {
		videosDir = "data/videos"
	}

	ctx := context.Background()

	// 1. 同步 SQLite 数据库快照（若已配置云端 PostgreSQL 则物化同步后多端归档）
	if strings.HasPrefix(dbPath, "postgres://") || strings.HasPrefix(dbPath, "postgresql://") {
		fmt.Printf("📦 [1/3] 检测到已连接云端托管 PostgreSQL (Neon)，正在将云端数据物化写入本地 SQLite 快照并归档...\n")
		if pgDB, err := db.InitDB(dbPath); err == nil {
			_ = db.MaterializePostgresToSQLite(pgDB, "data/ai-web-free.db")
		}
		_ = CheckpointSQLite("data/ai-web-free.db")
		if data, err := os.ReadFile("data/ai-web-free.db"); err == nil {
			if _, err := client.Upload(ctx, "_backups/ai-web-free.db", data, "application/x-sqlite3"); err == nil {
				fmt.Printf("✅ 已同步最新物化 SQLite 数据库快照至七牛云: _backups/ai-web-free.db (大小: %.2f MB)\n", float64(len(data))/(1024*1024))
			}
		}
	} else {
		fmt.Printf("📦 [1/3] 正在对 SQLite 数据库进行快照同步...\n")
		_ = CheckpointSQLite(dbPath)
		if data, err := os.ReadFile(dbPath); err == nil {
			if _, err := client.Upload(ctx, "_backups/ai-web-free.db", data, "application/x-sqlite3"); err != nil {
				fmt.Printf("❌ 上传数据库快照失败: %v\n", err)
			} else {
				fmt.Printf("✅ 已更新云端最新数据库快照: _backups/ai-web-free.db (大小: %.2f MB)\n", float64(len(data))/(1024*1024))
			}
		} else {
			fmt.Printf("⚠️ 未找到本地 SQLite 数据库文件 (%s)，跳过数据库备份\n", dbPath)
		}
	}

	// 2. 同步 .env 与号池凭据
	fmt.Printf("\n🔑 [2/3] 正在同步 .env 与号池凭据...\n")
	if envBytes, err := os.ReadFile(envPath); err == nil {
		if _, err := client.Upload(ctx, "_backups/env.snapshot", envBytes, "text/plain"); err != nil {
			fmt.Printf("❌ 上传 .env 凭据配置失败: %v\n", err)
		} else {
			fmt.Printf("✅ 已更新云端环境配置: _backups/env.snapshot\n")
		}
	} else {
		fmt.Printf("⚠️ 未找到本地 .env 文件，跳过环境配置备份\n")
	}
	for _, tf := range tokenCredentialFiles {
		if data, err := os.ReadFile(tf); err == nil && len(data) > 0 {
			cloudKey := "_backups/" + tf
			if _, err := client.Upload(ctx, cloudKey, data, "text/plain"); err != nil {
				fmt.Printf("❌ 上传号池凭据 [%s] 失败: %v\n", tf, err)
			} else {
				fmt.Printf("✅ 已备份号池凭据至云端: %s (大小: %d 字节)\n", cloudKey, len(data))
			}
		}
	}

	// 3. 增量同步多媒体
	fmt.Printf("\n🎨 [3/3] 正在扫描并增量同步多媒体文件...\n")
	totalUploaded := 0
	totalSkipped := 0
	var totalBytesUploaded int64

	syncDir := func(baseDir, prefix string) {
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			return
		}
		_ = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			relPath, err := filepath.Rel(baseDir, path)
			if err != nil {
				return nil
			}
			cloudKey := prefix + filepath.ToSlash(relPath)
			if client.Exists(ctx, cloudKey) {
				totalSkipped++
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			contentType := "application/octet-stream"
			switch strings.ToLower(filepath.Ext(path)) {
			case ".png":
				contentType = "image/png"
			case ".jpg", ".jpeg":
				contentType = "image/jpeg"
			case ".webp":
				contentType = "image/webp"
			case ".mp3":
				contentType = "audio/mpeg"
			case ".mp4":
				contentType = "video/mp4"
			case ".json":
				contentType = "application/json"
			}
			if _, err := client.Upload(ctx, cloudKey, data, contentType); err == nil {
				totalUploaded++
				totalBytesUploaded += int64(len(data))
				fmt.Printf("  ⬆️ 上传成功: %s (%.2f KB)\n", cloudKey, float64(len(data))/1024)
			}
			return nil
		})
	}

	syncDir(storageDir, "storage/")
	syncDir(videosDir, "videos/")

	fmt.Printf("\n🎉 推送完成！\n")
	fmt.Printf("   - 新增上传多媒体: %d 个文件 (共 %.2f MB)\n", totalUploaded, float64(totalBytesUploaded)/(1024*1024))
	fmt.Printf("   - 云端已有跳过:   %d 个文件\n", totalSkipped)
	fmt.Printf("   - 数据库与凭据已成功归档到七牛云 [_backups/] 目录\n")
	return nil
}

// PullRestore 从七牛云拉取恢复 SQLite 数据库、.env 配置与多媒体
func PullRestore(client *QiniuClient, dbPath, envPath string, pullAllMedia bool) error {
	if client == nil {
		return fmt.Errorf("qiniu client is nil")
	}
	if dbPath == "" {
		dbPath = "data/ai-web-free.db"
	}
	if envPath == "" {
		envPath = ".env"
	}

	ctx := context.Background()

	// 1. 恢复 .env（智能合并保护本地云数据库连接串）
	fmt.Printf("🔑 [1/3] 正在检查云端 .env 配置...\n")
	envBytes, err := client.Download(ctx, "_backups/env.snapshot")
	if err != nil {
		fmt.Printf("⚠️ 未在云端找到 _backups/env.snapshot 或下载失败: %v\n", err)
	} else {
		var preservedLines []string
		if oldBytes, err := os.ReadFile(envPath); err == nil {
			for _, line := range strings.Split(string(oldBytes), "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "DATABASE_URL=") ||
					strings.HasPrefix(trimmed, "DATABASE_URL_UNPOOLED=") ||
					strings.HasPrefix(trimmed, "NEON_") {
					preservedLines = append(preservedLines, line)
				}
			}
			backupOld := envPath + ".bak." + time.Now().Format("20060102150405")
			_ = os.Rename(envPath, backupOld)
			fmt.Printf("ℹ️ 检测到本地已有 %s，已备份为 %s\n", envPath, backupOld)
		}
		mergedBytes := envBytes
		if len(preservedLines) > 0 && !strings.Contains(string(envBytes), "DATABASE_URL=") {
			mergedStr := strings.TrimRight(string(envBytes), "\r\n") + "\n" + strings.Join(preservedLines, "\n") + "\n"
			mergedBytes = []byte(mergedStr)
			fmt.Printf("🔒 已自动保留并合并本地云端 PostgreSQL (Neon) 数据库配置，防止连接被旧快照抹除\n")
		}
		if err := os.WriteFile(envPath, mergedBytes, 0644); err != nil {
			fmt.Printf("❌ 写入 %s 失败: %v\n", envPath, err)
		} else {
			fmt.Printf("✅ 成功恢复 %s！\n", envPath)
		}
	}

	// 2. 恢复数据库与本地物化快照
	if strings.HasPrefix(dbPath, "postgres://") || strings.HasPrefix(dbPath, "postgresql://") {
		fmt.Printf("\n📦 [2/3] 检测到已配置云端托管 PostgreSQL (Neon)，多端实时直连共享，正在对齐云端凭据...\n")
		syncCloudCredentialsIntoDB(ctx, client, dbPath)
		if pgDB, err := db.InitDB(dbPath); err == nil {
			fmt.Printf("📦 [2/3] 正在将云端 PostgreSQL 数据全量物化同步至本地 SQLite (data/ai-web-free.db)...\n")
			_ = db.MaterializePostgresToSQLite(pgDB, "data/ai-web-free.db")
		}
	} else {
		fmt.Printf("\n📦 [2/3] 正在从云端拉取 SQLite 数据库快照...\n")
		dbBytes, err := client.Download(ctx, "_backups/ai-web-free.db")
		if err != nil {
			fmt.Printf("⚠️ 未在云端找到 _backups/ai-web-free.db 或下载失败: %v\n", err)
		} else {
			dbDir := filepath.Dir(dbPath)
			_ = os.MkdirAll(dbDir, 0755)
			if _, err := os.Stat(dbPath); err == nil {
				backupOld := dbPath + ".bak." + time.Now().Format("20060102150405")
				_ = os.Rename(dbPath, backupOld)
				fmt.Printf("ℹ️ 检测到本地已有旧数据库，已归档为 %s\n", backupOld)
			}
			_ = os.Remove(filepath.Join(dbDir, "ai-web-free.db-wal"))
			_ = os.Remove(filepath.Join(dbDir, "ai-web-free.db-shm"))
			if err := os.WriteFile(dbPath, dbBytes, 0644); err != nil {
				fmt.Printf("❌ 写入 SQLite 数据库失败: %v\n", err)
			} else {
				fmt.Printf("✅ 成功恢复 SQLite 数据库到 %s (大小: %.2f MB)！\n", dbPath, float64(len(dbBytes))/(1024*1024))
				syncCloudCredentialsIntoDB(ctx, client, dbPath)
			}
		}
	}

	// 3. 恢复关联多媒体与缓存物料（与 SQLite 数据库强关联）
	fmt.Printf("\n🎨 [3/3] 正在同步云端关联多媒体与缓存物料 (storage/ 与 videos/)...\n")
	downloaded := 0
	skipped := 0
	for _, prefix := range []string{"storage/", "videos/"} {
		keys, err := client.List(ctx, prefix, 1000)
		if err != nil {
			fmt.Printf("列举云端文件 [%s] 失败: %v\n", prefix, err)
			continue
		}
		for _, k := range keys {
			localPath := filepath.Join("data", k)
			if _, err := os.Stat(localPath); err == nil {
				skipped++
				continue
			}
			_ = os.MkdirAll(filepath.Dir(localPath), 0755)
			data, err := client.Download(ctx, k)
			if err != nil {
				fmt.Printf("  ⚠️ 下载失败 %s: %v\n", k, err)
			} else {
				_ = os.WriteFile(localPath, data, 0644)
				downloaded++
			}
		}
	}
	fmt.Printf("✅ 多媒体物料同步完成：新下载 %d 个文件，本地已有跳过 %d 个文件！\n", downloaded, skipped)

	fmt.Printf("\n🎉 恢复完成！服务可直接正常启动。\n")
	return nil
}

// syncCloudCredentialsIntoDB 从云端凭据备份（_backups/*.txt）直接无缝解析并对齐至 SQLite 数据库，避免污染根目录且保证换机完整恢复
func syncCloudCredentialsIntoDB(ctx context.Context, client *QiniuClient, dbPath string) {
	if client == nil {
		return
	}
	database, err := db.InitDB(dbPath)
	if err != nil {
		return
	}
	profiles := accounts.DefaultProfiles
	accs, _ := accounts.LoadAccountsFromDB(database, profiles)

	findExisting := func(email, chatGPTID, token string) *accounts.Account {
		emailClean := strings.ToLower(strings.TrimSpace(email))
		idClean := strings.TrimSpace(chatGPTID)
		tokenClean := strings.TrimSpace(token)
		for _, a := range accs {
			if emailClean != "" && strings.ToLower(strings.TrimSpace(a.Email)) == emailClean {
				return a
			}
			if idClean != "" && strings.TrimSpace(a.ChatGPTAccountID) == idClean {
				return a
			}
			if tokenClean != "" && (a.Token == tokenClean || a.RefreshToken == tokenClean || a.SessionToken == tokenClean) {
				return a
			}
		}
		return nil
	}

	// 1. 同步 access_tokens
	if accessData, err := client.Download(ctx, "_backups/access_tokens.txt"); err == nil {
		lines := strings.Split(string(accessData), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			token := parts[0]
			teamID := ""
			if len(parts) > 1 {
				teamID = parts[1]
			}
			chatGPTID := accounts.ExtractChatGPTAccountID(token)
			email := accounts.ExtractEmail(token)
			if matched := findExisting(email, chatGPTID, token); matched != nil {
				if matched.Token == "" {
					matched.Token = token
				}
				if matched.TeamUserID == "" {
					matched.TeamUserID = teamID
				}
				if matched.ChatGPTAccountID == "" {
					matched.ChatGPTAccountID = chatGPTID
				}
				if matched.Email == "" {
					matched.Email = email
				}
				continue
			}
			acct := accounts.CreateAccount(token, accounts.TypeFree, profiles)
			acct.TeamUserID = teamID
			acct.ChatGPTAccountID = chatGPTID
			if email != "" {
				acct.Email = email
			}
			acct.Status = accounts.StatusActive
			accs = append(accs, acct)
		}
	}

	// 2. 同步 kami_accounts
	if kamiData, err := client.Download(ctx, "_backups/kami_accounts.txt"); err == nil {
		kamiList := accounts.ParseKamiText(string(kamiData))
		for _, k := range kamiList {
			if matched := findExisting(k.Email, "", ""); matched != nil {
				matched.Password = k.Password
				matched.TOTPSecret = k.TOTPSecret
				continue
			}
			acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
			acct.Email = k.Email
			acct.Password = k.Password
			acct.TOTPSecret = k.TOTPSecret
			acct.Status = accounts.StatusPending
			acct.LastError = "待浏览器登录提取 Token"
			accs = append(accs, acct)
		}
	}

	// 3. 同步 account_proxies
	if proxyData, err := client.Download(ctx, "_backups/account_proxies.txt"); err == nil {
		lines := strings.Split(string(proxyData), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "----", 2)
			if len(parts) == 2 {
				key := strings.ToLower(strings.TrimSpace(parts[0]))
				pURL := strings.TrimSpace(parts[1])
				for _, a := range accs {
					if strings.ToLower(strings.TrimSpace(a.Email)) == key || strings.ToLower(strings.TrimSpace(a.ChatGPTAccountID)) == key {
						a.Proxy = pURL
					}
				}
			}
		}
	}

	// 4. 同步 banned_accounts
	if bannedData, err := client.Download(ctx, "_backups/banned_accounts.txt"); err == nil {
		lines := strings.Split(string(bannedData), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "----", 2)
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			reason := "账号已被官方封禁"
			if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
				reason = strings.TrimSpace(parts[1])
			}
			_ = db.SaveRestrictionToDB(database, key, "banned", reason)
			for _, a := range accs {
				if strings.ToLower(strings.TrimSpace(a.Email)) == key || strings.ToLower(strings.TrimSpace(a.ChatGPTAccountID)) == key {
					a.Status = accounts.StatusBanned
					a.LastError = reason
				}
			}
		}
	}

	// 5. 同步 proxies
	if proxiesData, err := client.Download(ctx, "_backups/proxies.txt"); err == nil {
		lines := strings.Split(string(proxiesData), "\n")
		var validProxies []string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "#") {
				validProxies = append(validProxies, l)
			}
		}
		if len(validProxies) > 0 {
			_ = db.SaveProxiesToDB(database, validProxies)
		}
	}

	if len(accs) > 0 {
		if err := accounts.BatchSaveAccountsToDB(database, accs); err == nil {
			fmt.Printf("📦 已对齐云端号池凭据：%d 个账号已同步至 SQLite 数据库！\n", len(accs))
		}
	}
}
