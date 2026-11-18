//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"ai-web-free/internal/bootstrap"
	"ai-web-free/internal/config"
	"ai-web-free/internal/storage"

	"github.com/joho/godotenv"
)

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

func main() {
	cmd := "push"
	pullAll := false
	for _, arg := range os.Args[1:] {
		low := strings.ToLower(arg)
		if low == "--all" || low == "-all" || low == "-a" {
			pullAll = true
		} else if !strings.HasPrefix(low, "-") {
			cmd = low
		}
	}

	loadDotEnv()
	cfg := config.Load()
	if cfg.QiniuAccessKey == "" || cfg.QiniuSecretKey == "" {
		fmt.Fprintf(os.Stderr, "初始化失败: 缺少 QINIU_ACCESS_KEY 或 QINIU_SECRET_KEY，请检查 .env 配置\n")
		os.Exit(1)
	}

	client := storage.NewQiniuClient(storage.QiniuConfig{
		AccessKey: cfg.QiniuAccessKey,
		SecretKey: cfg.QiniuSecretKey,
		Bucket:    cfg.QiniuBucket,
		Domain:    cfg.QiniuDomain,
		Region:    cfg.QiniuRegion,
		UploadURL: cfg.QiniuUploadURL,
	})

	switch cmd {
	case "push", "backup":
		// 备份前先对齐：将本地凭据文件中的账号同步增量写入 SQLite 数据库
		if count, err := bootstrap.SyncLocalFilesToDB(&cfg); err == nil {
			fmt.Printf("📦 已对齐 %d 个号池账号至数据库\n", count)
		}
		fmt.Printf("🚀 开始向七牛云存储桶 [%s] 推送数据...\n\n", cfg.QiniuBucket)
		if err := storage.PushBackup(client, cfg.DatabaseURL, ".env", cfg.StorageDir, "data/videos"); err != nil {
			fmt.Fprintf(os.Stderr, "备份推送失败: %v\n", err)
			os.Exit(1)
		}
	case "pull", "restore":
		fmt.Printf("🚀 开始从七牛云存储桶 [%s] 恢复数据...\n\n", cfg.QiniuBucket)
		if err := storage.PullRestore(client, cfg.DatabaseURL, ".env", pullAll); err != nil {
			fmt.Fprintf(os.Stderr, "拉取恢复失败: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("未知命令: %s (可用命令: push, pull)\n", cmd)
		os.Exit(1)
	}
}
