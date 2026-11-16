package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"ai-web-free/internal/bootstrap"
	"ai-web-free/internal/config"
	"ai-web-free/internal/storage"

	"github.com/g-utils/endless"
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
	if len(os.Args) > 1 {
		subcmd := strings.ToLower(os.Args[1])
		switch subcmd {
		case "backup", "sync-push":
			handleBackup()
			return
		case "restore", "sync-pull":
			handleRestore()
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	app, err := bootstrap.Init()
	if err != nil {
		panic(err)
	}
	defer app.Cleanup()

	host := app.Config.ServerHost
	port := app.Config.ServerPort
	if host == "" {
		host = "0.0.0.0"
	}
	if port == "" {
		port = os.Getenv("PORT")
		if port == "" {
			port = "18080"
		}
	}

	if app.Config.TLSCert != "" && app.Config.TLSKey != "" {
		_ = endless.ListenAndServeTLS(host+":"+port, app.Config.TLSCert, app.Config.TLSKey, app.Router)
	} else {
		_ = endless.ListenAndServe(host+":"+port, app.Router)
	}
}

func handleBackup() {
	loadDotEnv()
	cfg := config.Load()
	if cfg.QiniuAccessKey == "" || cfg.QiniuSecretKey == "" {
		fmt.Fprintf(os.Stderr, "❌ 缺少七牛云配置 (QINIU_ACCESS_KEY / QINIU_SECRET_KEY)，请在 .env 或环境变量中配置\n")
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
	fmt.Printf("🚀 开始向七牛云存储桶 [%s] 备份数据...\n\n", cfg.QiniuBucket)
	if err := storage.PushBackup(client, cfg.DatabaseURL, ".env", cfg.StorageDir, "data/videos"); err != nil {
		fmt.Fprintf(os.Stderr, "备份失败: %v\n", err)
		os.Exit(1)
	}
}

func handleRestore() {
	loadDotEnv()
	cfg := config.Load()
	if cfg.QiniuAccessKey == "" || cfg.QiniuSecretKey == "" {
		fmt.Fprintf(os.Stderr, "❌ 缺少七牛云配置 (QINIU_ACCESS_KEY / QINIU_SECRET_KEY)，请在 .env 或环境变量中配置\n")
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
	pullAll := false
	for _, arg := range os.Args[2:] {
		if arg == "--all" || arg == "-a" {
			pullAll = true
		}
	}
	fmt.Printf("🚀 开始从七牛云存储桶 [%s] 恢复数据...\n\n", cfg.QiniuBucket)
	if err := storage.PullRestore(client, cfg.DatabaseURL, ".env", pullAll); err != nil {
		fmt.Fprintf(os.Stderr, "恢复失败: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("ai-web-free - OpenAI 兼容的多供应商免费号池与逆向网关")
	fmt.Println()
	fmt.Println("用法:")
	fmt.Println("  ai-web-free             启动网关与管理端 Web 服务 (默认端口 18080)")
	fmt.Println("  ai-web-free backup      执行数据全量/增量备份到七牛云 (SQLite + .env + 多媒体)")
	fmt.Println("  ai-web-free restore     从七牛云恢复环境凭据与 SQLite 数据库快照")
	fmt.Println("  ai-web-free restore --all 从七牛云恢复全量数据 (包含所有历史多媒体文件)")
	fmt.Println()
}
