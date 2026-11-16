//go:build ignore

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"

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
	allMedia := flag.Bool("all", false, "pull 时是否全量下载所有多媒体文件")
	flag.Parse()

	args := flag.Args()
	cmd := "push"
	if len(args) > 0 {
		cmd = strings.ToLower(args[0])
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
		fmt.Printf("🚀 开始向七牛云存储桶 [%s] 推送数据...\n\n", cfg.QiniuBucket)
		if err := storage.PushBackup(client, cfg.DatabaseURL, ".env", cfg.StorageDir, "data/videos"); err != nil {
			fmt.Fprintf(os.Stderr, "备份推送失败: %v\n", err)
			os.Exit(1)
		}
	case "pull", "restore":
		fmt.Printf("🚀 开始从七牛云存储桶 [%s] 恢复数据...\n\n", cfg.QiniuBucket)
		if err := storage.PullRestore(client, cfg.DatabaseURL, ".env", *allMedia); err != nil {
			fmt.Fprintf(os.Stderr, "拉取恢复失败: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("未知命令: %s (可用命令: push, pull)\n", cmd)
		os.Exit(1)
	}
}
