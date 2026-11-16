//go:build ignore

package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/joho/godotenv"
)

type QiniuSyncer struct {
	ak        string
	sk        string
	bucket    string
	domain    string
	uploadURL string
	client    *http.Client
}

func loadDotEnv() {
	content, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	envMap, err := godotenv.Unmarshal(string(content))
	if err != nil {
		return
	}
	for k, v := range envMap {
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

func newSyncer() (*QiniuSyncer, error) {
	loadDotEnv()
	ak := os.Getenv("QINIU_ACCESS_KEY")
	sk := os.Getenv("QINIU_SECRET_KEY")
	bucket := os.Getenv("QINIU_BUCKET")
	if bucket == "" {
		bucket = "ai-web-free-storage"
	}
	domain := os.Getenv("QINIU_DOMAIN")
	uploadURL := os.Getenv("QINIU_UPLOAD_URL")
	if uploadURL == "" {
		uploadURL = "https://upload.qiniup.com"
	}

	if ak == "" || sk == "" {
		return nil, fmt.Errorf("缺少 QINIU_ACCESS_KEY 或 QINIU_SECRET_KEY，请检查 .env 配置")
	}

	return &QiniuSyncer{
		ak:        ak,
		sk:        sk,
		bucket:    bucket,
		domain:    strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(domain), "http://"), "https://"),
		uploadURL: uploadURL,
		client:    &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (s *QiniuSyncer) signQBox(data string) string {
	mac := hmac.New(sha1.New, []byte(s.sk))
	mac.Write([]byte(data))
	sign := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("QBox %s:%s", s.ak, sign)
}

func (s *QiniuSyncer) makeUploadToken(key string) string {
	type PutPolicy struct {
		Scope    string `json:"scope"`
		Deadline int64  `json:"deadline"`
	}
	scope := s.bucket
	if key != "" {
		scope = fmt.Sprintf("%s:%s", s.bucket, key)
	}
	policy := PutPolicy{
		Scope:    scope,
		Deadline: time.Now().Add(2 * time.Hour).Unix(),
	}
	data, _ := json.Marshal(policy)
	encodedPolicy := base64.URLEncoding.EncodeToString(data)

	mac := hmac.New(sha1.New, []byte(s.sk))
	mac.Write([]byte(encodedPolicy))
	encodedSign := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("%s:%s:%s", s.ak, encodedSign, encodedPolicy)
}

func (s *QiniuSyncer) makePrivateDownloadURL(rawURL string) string {
	deadline := time.Now().Add(2 * time.Hour).Unix()
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	target := fmt.Sprintf("%s%se=%d", rawURL, sep, deadline)
	mac := hmac.New(sha1.New, []byte(s.sk))
	mac.Write([]byte(target))
	sign := base64.URLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s&token=%s:%s", target, s.ak, sign)
}

func (s *QiniuSyncer) Upload(ctx context.Context, key string, data []byte) error {
	cleanKey := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(key), "/"), "\\", "/")
	upToken := s.makeUploadToken(cleanKey)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("token", upToken)
	_ = writer.WriteField("key", cleanKey)

	filename := cleanKey
	if idx := strings.LastIndex(cleanKey, "/"); idx >= 0 {
		filename = cleanKey[idx+1:]
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.uploadURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}
	return nil
}

func (s *QiniuSyncer) Download(ctx context.Context, key string) ([]byte, error) {
	if s.domain == "" {
		return nil, fmt.Errorf("未配置 QINIU_DOMAIN")
	}
	cleanKey := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(key), "/"), "\\", "/")
	rawURL := fmt.Sprintf("http://%s/%s", s.domain, cleanKey)
	downloadURL := s.makePrivateDownloadURL(rawURL)

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBytes))
	}
	return io.ReadAll(resp.Body)
}

func (s *QiniuSyncer) Exists(ctx context.Context, key string) bool {
	cleanKey := strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(key), "/"), "\\", "/")
	entryURI := base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", s.bucket, cleanKey)))
	statPath := fmt.Sprintf("/stat/%s\n", entryURI)
	statURL := fmt.Sprintf("https://rs.qbox.me/stat/%s", entryURI)

	req, err := http.NewRequestWithContext(ctx, "GET", statURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", s.signQBox(statPath))

	resp, err := s.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (s *QiniuSyncer) List(ctx context.Context, prefix string) ([]string, error) {
	var allKeys []string
	marker := ""
	for {
		path := fmt.Sprintf("/list?bucket=%s&prefix=%s&limit=1000", s.bucket, prefix)
		if marker != "" {
			path += "&marker=" + marker
		}
		listURL := "https://rsf.qbox.me" + path

		req, err := http.NewRequestWithContext(ctx, "GET", listURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", s.signQBox(path+"\n"))

		resp, err := s.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("list failed: %d %s", resp.StatusCode, string(b))
		}

		var res struct {
			Items []struct {
				Key string `json:"key"`
			} `json:"items"`
			Marker string `json:"marker"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return nil, err
		}
		for _, item := range res.Items {
			allKeys = append(allKeys, item.Key)
		}
		if res.Marker == "" || len(res.Items) == 0 {
			break
		}
		marker = res.Marker
	}
	return allKeys, nil
}

func checkpointSQLite(dbPath string) {
	if _, err := os.Stat(dbPath); err != nil {
		return
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
}

func runPush(s *QiniuSyncer) error {
	ctx := context.Background()
	timestamp := time.Now().Format("20060102_150405")
	fmt.Printf("🚀 开始向七牛云存储桶 [%s] 推送数据...\n\n", s.bucket)

	// 1. 同步 SQLite 数据库
	dbPath := filepath.Join("data", "ai-web-free.db")
	if _, err := os.Stat(dbPath); err == nil {
		fmt.Printf("📦 [1/3] 正在对 SQLite 数据库进行快照同步...\n")
		checkpointSQLite(dbPath)
		dbBytes, err := os.ReadFile(dbPath)
		if err != nil {
			fmt.Printf("⚠️ 读取数据库失败: %v\n", err)
		} else {
			// 上传最新主快照
			if err := s.Upload(ctx, "_backups/ai-web-free.db", dbBytes); err != nil {
				fmt.Printf("❌ 上传数据库主快照失败: %v\n", err)
			} else {
				fmt.Printf("✅ 已更新云端最新数据库快照: _backups/ai-web-free.db (大小: %.2f MB)\n", float64(len(dbBytes))/(1024*1024))
			}
			// 上传带时间戳的历史快照
			historyKey := fmt.Sprintf("_backups/snapshots/ai-web-free_%s.db", timestamp)
			_ = s.Upload(ctx, historyKey, dbBytes)
		}
	} else {
		fmt.Printf("ℹ️ 未发现本地 SQLite 数据库 (%s)，跳过数据库备份\n", dbPath)
	}

	// 2. 同步 .env 敏感凭据
	envPath := ".env"
	if _, err := os.Stat(envPath); err == nil {
		fmt.Printf("\n🔑 [2/3] 正在同步 .env 凭据配置...\n")
		envBytes, err := os.ReadFile(envPath)
		if err == nil {
			if err := s.Upload(ctx, "_backups/env.snapshot", envBytes); err != nil {
				fmt.Printf("❌ 上传 .env 失败: %v\n", err)
			} else {
				fmt.Printf("✅ 已更新云端环境配置: _backups/env.snapshot\n")
			}
		}
	}

	// 3. 增量同步多媒体缓存
	fmt.Printf("\n🎨 [3/3] 正在扫描并增量同步多媒体文件...\n")
	scanDirs := []string{
		filepath.Join("data", "storage"),
		filepath.Join("data", "videos"),
	}

	uploadedCount := 0
	skippedCount := 0
	var uploadedBytes int64

	for _, baseDir := range scanDirs {
		if _, err := os.Stat(baseDir); err != nil {
			continue
		}
		_ = filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			// 计算云端 Key
			rel, err := filepath.Rel("data", path)
			if err != nil {
				return nil
			}
			cloudKey := strings.ReplaceAll(rel, "\\", "/")

			// 探测是否已存在，增量跳过
			if s.Exists(ctx, cloudKey) {
				skippedCount++
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if err := s.Upload(ctx, cloudKey, data); err != nil {
				fmt.Printf("  ⚠️ 上传失败 %s: %v\n", cloudKey, err)
			} else {
				uploadedCount++
				uploadedBytes += int64(len(data))
				fmt.Printf("  ⬆️ 上传成功: %s (%.2f KB)\n", cloudKey, float64(len(data))/1024)
			}
			return nil
		})
	}

	fmt.Printf("\n🎉 推送完成！\n")
	fmt.Printf("   - 新增上传多媒体: %d 个文件 (共 %.2f MB)\n", uploadedCount, float64(uploadedBytes)/(1024*1024))
	fmt.Printf("   - 云端已有跳过:   %d 个文件\n", skippedCount)
	fmt.Printf("   - 数据库与凭据已成功归档到七牛云 [_backups/] 目录\n")
	return nil
}

func runPull(s *QiniuSyncer, pullAllMedia bool) error {
	ctx := context.Background()
	fmt.Printf("📥 正在从七牛云存储桶 [%s] 恢复数据...\n\n", s.bucket)

	// 1. 恢复 .env（如果本地不存在）
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		fmt.Printf("🔑 [1/3] 本地未找到 .env，正在从云端拉取恢复...\n")
		envBytes, err := s.Download(ctx, "_backups/env.snapshot")
		if err != nil {
			fmt.Printf("⚠️ 云端未找到 _backups/env.snapshot: %v\n", err)
		} else {
			if err := os.WriteFile(".env", envBytes, 0644); err != nil {
				fmt.Printf("❌ 写入 .env 失败: %v\n", err)
			} else {
				fmt.Printf("✅ 成功恢复 .env 配置文件！\n")
			}
		}
	} else {
		fmt.Printf("ℹ️ [1/3] 本地已存在 .env，为避免覆盖本地现有 Key，跳过自动覆盖（若需强制覆盖请先备份或删除本地 .env）\n")
	}

	// 2. 恢复 SQLite 数据库
	dbDir := "data"
	_ = os.MkdirAll(dbDir, 0755)
	dbPath := filepath.Join(dbDir, "ai-web-free.db")

	fmt.Printf("\n📦 [2/3] 正在拉取 SQLite 数据库快照...\n")
	dbBytes, err := s.Download(ctx, "_backups/ai-web-free.db")
	if err != nil {
		fmt.Printf("⚠️ 云端未找到 _backups/ai-web-free.db: %v\n", err)
	} else {
		// 清理旧 WAL 避免锁冲突
		_ = os.Remove(filepath.Join(dbDir, "ai-web-free.db-wal"))
		_ = os.Remove(filepath.Join(dbDir, "ai-web-free.db-shm"))
		if err := os.WriteFile(dbPath, dbBytes, 0644); err != nil {
			fmt.Printf("❌ 写入 SQLite 数据库失败: %v\n", err)
		} else {
			fmt.Printf("✅ 成功恢复 SQLite 数据库到 %s (大小: %.2f MB)！\n", dbPath, float64(len(dbBytes))/(1024*1024))
		}
	}

	// 3. 多媒体文件恢复
	fmt.Printf("\n🎨 [3/3] 多媒体文件机制：\n")
	if !pullAllMedia {
		fmt.Printf("✨ 当前已启用【两级分层缓存引擎 (TieredStorage)】：\n")
		fmt.Printf("   所有多媒体资源已在七牛云持久保存；当页面加载任何图片、短剧视频或音频时，\n")
		fmt.Printf("   系统会自动触发按需回填并缓存在本地磁盘，换机无需等待冗长全量下载！\n")
		fmt.Printf("   (若需要一次性全量预下载全部多媒体，请运行: go run scripts/sync_qiniu.go pull --all)\n")
	} else {
		fmt.Printf("正在全量拉取云端多媒体文件...\n")
		keys, err := s.List(ctx, "storage/")
		if err != nil {
			fmt.Printf("列举云端文件失败: %v\n", err)
		} else {
			downloaded := 0
			for _, k := range keys {
				localPath := filepath.Join("data", k)
				if _, err := os.Stat(localPath); err == nil {
					continue
				}
				_ = os.MkdirAll(filepath.Dir(localPath), 0755)
				data, err := s.Download(ctx, k)
				if err != nil {
					fmt.Printf("  ⚠️ 下载失败 %s: %v\n", k, err)
				} else {
					_ = os.WriteFile(localPath, data, 0644)
					downloaded++
				}
			}
			fmt.Printf("✅ 全量下载完成，新拉取 %d 个多媒体文件！\n", downloaded)
		}
	}

	fmt.Printf("\n🎉 恢复完成！服务可直接正常启动。\n")
	return nil
}

func main() {
	allMedia := flag.Bool("all", false, "pull 时是否全量下载所有多媒体文件")
	flag.Parse()

	args := flag.Args()
	cmd := "push"
	if len(args) > 0 {
		cmd = strings.ToLower(args[0])
	}

	syncer, err := newSyncer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化失败: %v\n", err)
		os.Exit(1)
	}

	switch cmd {
	case "push", "backup":
		if err := runPush(syncer); err != nil {
			fmt.Fprintf(os.Stderr, "备份推送失败: %v\n", err)
			os.Exit(1)
		}
	case "pull", "restore":
		if err := runPull(syncer, *allMedia); err != nil {
			fmt.Fprintf(os.Stderr, "拉取恢复失败: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("未知命令: %s (可用命令: push, pull)\n", cmd)
		os.Exit(1)
	}
}
