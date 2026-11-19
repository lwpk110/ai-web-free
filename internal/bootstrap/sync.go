package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/db"
	"gorm.io/gorm/clause"
)

// SyncLocalFilesToDB 扫描本地凭据文件并增量写入 SQLite 数据库（用于备份前对齐）
func SyncLocalFilesToDB(cfg *config.Config) (int, error) {
	if cfg == nil {
		loaded := config.Load()
		cfg = &loaded
	}
	database, err := db.InitDB(cfg.DatabaseURL)
	if err != nil {
		return 0, fmt.Errorf("打开数据库失败: %w", err)
	}
	defer func() {
		if sqlDB, err := database.DB(); err == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
	}()

	profiles := accounts.DefaultProfiles
	accs, err := accounts.LoadAccountsFromDB(database, profiles)
	if err != nil {
		accs = []*accounts.Account{}
	}

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

	// 1. 合并 access_tokens.txt
	for _, t := range accounts.LoadTokensFromFile("access_tokens.txt") {
		chatGPTID := accounts.ExtractChatGPTAccountID(t.Token)
		email := accounts.ExtractEmail(t.Token)
		if matched := findExisting(email, chatGPTID, t.Token); matched != nil {
			if matched.Token == "" {
				matched.Token = t.Token
			}
			if matched.TeamUserID == "" {
				matched.TeamUserID = t.TeamID
			}
			if matched.ChatGPTAccountID == "" {
				matched.ChatGPTAccountID = chatGPTID
			}
			if matched.Email == "" {
				matched.Email = email
			}
			continue
		}
		acct := accounts.CreateAccount(t.Token, accounts.TypeFree, profiles)
		acct.TeamUserID = t.TeamID
		acct.ChatGPTAccountID = chatGPTID
		if email != "" {
			acct.Email = email
		}
		acct.Status = accounts.StatusActive
		accs = append(accs, acct)
	}

	// 2. 合并 refresh_tokens.txt
	for _, t := range accounts.LoadTokensFromFile("refresh_tokens.txt") {
		if matched := findExisting("", "", t.Token); matched != nil {
			if matched.RefreshToken == "" {
				matched.RefreshToken = t.Token
			}
			continue
		}
		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.RefreshToken = t.Token
		acct.TeamUserID = t.TeamID
		acct.Status = accounts.StatusExpired
		accs = append(accs, acct)
	}

	// 3. 合并 session_tokens.txt
	for _, t := range accounts.LoadTokensFromFile("session_tokens.txt") {
		if matched := findExisting("", "", t.Token); matched != nil {
			if matched.SessionToken == "" {
				matched.SessionToken = t.Token
			}
			continue
		}
		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.SessionToken = t.Token
		acct.TeamUserID = t.TeamID
		acct.Status = accounts.StatusExpired
		accs = append(accs, acct)
	}

	// 4. 合并 free_tokens.txt
	for _, t := range accounts.LoadTokensFromFile("free_tokens.txt") {
		if matched := findExisting("", "", t.Token); matched != nil {
			continue
		}
		acct := accounts.CreateAccount(t.Token, accounts.TypeNoAuth, profiles)
		acct.Status = accounts.StatusActive
		accs = append(accs, acct)
	}

	// 5. 合并 kami_accounts.txt
	for _, k := range accounts.LoadKamiFromFile("kami_accounts.txt") {
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

	if err := accounts.BatchSaveAccountsToDB(database, accs); err != nil {
		return len(accs), fmt.Errorf("批量保存至数据库失败: %w", err)
	}

	// 6. 同步 proxies.txt 至数据库
	if _, err := os.Stat("proxies.txt"); err == nil {
		if data, err := os.ReadFile("proxies.txt"); err == nil {
			var pLines []string
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					pLines = append(pLines, line)
				}
			}
			if len(pLines) > 0 {
				_ = db.SaveProxiesToDB(database, pLines)
			}
		}
	}

	// 7. 同步 banned_accounts.txt 与 disabled_accounts.txt 至数据库
	for id, reason := range accounts.LoadBannedFromFile("banned_accounts.txt") {
		_ = db.SaveRestrictionToDB(database, id, "banned", reason)
	}
	for id, reason := range accounts.LoadDisabledFromFile("disabled_accounts.txt") {
		_ = db.SaveRestrictionToDB(database, id, "disabled", reason)
	}

	// 8. 同步 grok_accounts.txt 至数据库
	if _, err := os.Stat("grok_accounts.txt"); err == nil {
		if data, err := os.ReadFile("grok_accounts.txt"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				gRec := db.PoolAccountRecord{
					Provider:  "grok",
					Type:      "free",
					Status:    "active",
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				}
				if strings.Contains(line, "|") {
					parts := strings.Split(line, "|")
					if len(parts) > 0 {
						gRec.Email = strings.TrimSpace(parts[0])
					}
					if len(parts) > 1 {
						gRec.Password = strings.TrimSpace(parts[1])
					}
					if len(parts) > 2 {
						third := strings.TrimSpace(parts[2])
						if strings.HasPrefix(third, "sso=") || strings.Contains(third, "sso") {
							gRec.Token = third
						} else if strings.HasPrefix(third, "eyJ") {
							gRec.SessionToken = third
							gRec.Token = fmt.Sprintf("sso=%s; sso-rw=%s", third, third)
						} else {
							gRec.TOTPSecret = third
						}
					}
					if len(parts) > 3 {
						fourth := strings.TrimSpace(parts[3])
						if strings.HasPrefix(fourth, "sso=") || strings.Contains(fourth, "sso") {
							gRec.Token = fourth
						} else if strings.HasPrefix(fourth, "eyJ") {
							gRec.SessionToken = fourth
							if gRec.Token == "" {
								gRec.Token = fmt.Sprintf("sso=%s; sso-rw=%s", fourth, fourth)
							}
						} else {
							gRec.SessionToken = fourth
						}
					}
					if len(parts) > 4 {
						gRec.Token = strings.TrimSpace(parts[4])
					}
				} else if strings.Contains(line, "----") {
					parts := strings.SplitN(line, "----", 2)
					gRec.Email = strings.TrimSpace(parts[0])
					if len(parts) > 1 {
						gRec.Token = strings.TrimSpace(parts[1])
					}
				} else {
					gRec.Token = line
				}
				if gRec.ID == "" {
					gRec.ID = "grok:" + gRec.Email
					if gRec.Email == "" {
						gRec.ID = "grok:" + gRec.Token
					}
				}
				_ = database.Clauses(clause.OnConflict{UpdateAll: true}).Create(&gRec).Error
			}
		}
	}

	return len(accs), nil
}

// ExportDBToLocalFiles 从 SQLite 数据库读取全部账号、代理与黑白名单并回写至本地文本文件（用于恢复后对齐）
func ExportDBToLocalFiles(cfg *config.Config) (int, error) {
	if cfg == nil {
		loaded := config.Load()
		cfg = &loaded
	}
	database, err := db.InitDB(cfg.DatabaseURL)
	if err != nil {
		return 0, fmt.Errorf("打开数据库失败: %w", err)
	}
	defer func() {
		if sqlDB, err := database.DB(); err == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
	}()

	profiles := accounts.DefaultProfiles
	accs, err := accounts.LoadAccountsFromDB(database, profiles)
	if err != nil {
		return 0, err
	}

	// 1. 导出 access_tokens.txt
	var accessLines []string
	for _, a := range accs {
		if a.Type != accounts.TypeNoAuth && a.Token != "" {
			line := a.Token
			if a.TeamUserID != "" {
				line += ":" + a.TeamUserID
			}
			accessLines = append(accessLines, line)
		}
	}
	if len(accessLines) > 0 {
		_ = writeAtomicFile("access_tokens.txt", []byte(strings.Join(accessLines, "\n")+"\n"))
	}

	// 2. 导出 kami_accounts.txt
	var kamiList []accounts.KamiAccount
	for _, a := range accs {
		if a.Email != "" && a.Password != "" {
			kamiList = append(kamiList, accounts.KamiAccount{
				Email:      a.Email,
				Password:   a.Password,
				TOTPSecret: a.TOTPSecret,
			})
		}
	}
	if len(kamiList) > 0 {
		_ = writeAtomicFile("kami_accounts.txt", []byte(accounts.FormatKamiLines(kamiList)))
	}

	// 3. 导出 proxies.txt
	if proxies, err := db.LoadProxiesFromDB(database); err == nil && len(proxies) > 0 {
		_ = writeAtomicFile("proxies.txt", []byte(strings.Join(proxies, "\n")+"\n"))
	}

	// 4. 导出 banned_accounts.txt
	bannedMap := db.LoadRestrictionsFromDB(database, "banned")
	if len(bannedMap) > 0 {
		var bLines []string
		for id, reason := range bannedMap {
			bLines = append(bLines, fmt.Sprintf("%s----%s", id, reason))
		}
		_ = writeAtomicFile("banned_accounts.txt", []byte(strings.Join(bLines, "\n")+"\n"))
	}

	// 5. 导出 disabled_accounts.txt
	disabledMap := db.LoadRestrictionsFromDB(database, "disabled")
	if len(disabledMap) > 0 {
		var dLines []string
		for id, reason := range disabledMap {
			dLines = append(dLines, fmt.Sprintf("%s----%s", id, reason))
		}
		_ = writeAtomicFile("disabled_accounts.txt", []byte(strings.Join(dLines, "\n")+"\n"))
	}

	// 6. 导出 grok_accounts.txt
	var grokRecords []db.PoolAccountRecord
	if err := database.Where("provider = ?", "grok").Find(&grokRecords).Error; err == nil && len(grokRecords) > 0 {
		var gLines []string
		for _, g := range grokRecords {
			if g.Password != "" {
				parts := []string{g.Email, g.Password}
				if g.TOTPSecret != "" {
					parts = append(parts, g.TOTPSecret)
				}
				if g.SessionToken != "" {
					parts = append(parts, g.SessionToken)
				}
				if g.Token != "" {
					parts = append(parts, g.Token)
				}
				gLines = append(gLines, strings.Join(parts, "|"))
			} else if g.Email != "" && g.Token != "" {
				gLines = append(gLines, fmt.Sprintf("%s----%s", g.Email, g.Token))
			} else if g.Token != "" {
				gLines = append(gLines, g.Token)
			}
		}
		if len(gLines) > 0 {
			_ = writeAtomicFile("grok_accounts.txt", []byte(strings.Join(gLines, "\n")+"\n"))
		}
	}

	return len(accs), nil
}

func writeAtomicFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return os.WriteFile(path, data, 0644)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
