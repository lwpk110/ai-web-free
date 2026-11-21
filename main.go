package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ai-web-free/internal/bootstrap"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
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
		case "grok-login":
			handleGrokLogin()
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

	// 备份前先对齐：将本地凭据文件中的账号同步增量写入 SQLite 数据库
	if count, err := bootstrap.SyncLocalFilesToDB(&cfg); err == nil {
		fmt.Printf("📦 已对齐 %d 个号池账号至数据库\n", count)
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

	fmt.Printf("✅ 数据恢复成功！SQLite 数据库已对齐为单一事实源。\n")
}

func handleGrokLogin() {
	flags := flag.NewFlagSet("grok-login", flag.ContinueOnError)
	input := flags.String("input", "", "Grok 卡密文件路径 (邮箱|密码|2FA密钥)")
	output := flags.String("output", "", "SSO 输出文件路径")
	proxyURL := flags.String("proxy", "", "代理地址，例如 http://127.0.0.1:7890")
	browserPath := flags.String("browser-path", "", "Chrome/Chromium 可执行文件路径")
	account := flags.String("account", "", "仅处理邮箱包含该字符串的账号")
	limit := flags.Int("limit", 0, "最多处理账号数量，0 表示不限")
	timeout := flags.Duration("timeout", 180*time.Second, "单个账号登录超时")
	headless := flags.Bool("headless", true, "无头模式运行 (默认开启；遇到人机验证可使用 --headless=false)")
	merge := flags.Bool("merge", false, "保留输出文件中已存在的成功凭据")
	if err := flags.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	if *input == "" {
		fmt.Fprintln(os.Stderr, "缺少 --input 卡密文件路径")
		os.Exit(2)
	}

	cards, err := provider.ParseGrokCardFile(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析卡密失败: %v\n", err)
		os.Exit(1)
	}
	selected := provider.FilterGrokCardAccounts(cards, *account, *limit)
	if len(selected) == 0 {
		fmt.Fprintln(os.Stderr, "没有匹配的卡密账号")
		os.Exit(1)
	}
	if *output == "" {
		*output = strings.TrimSuffix(*input, filepath.Ext(*input)) + "_sso.txt"
	}
	if *proxyURL == "" {
		*proxyURL = provider.AutoDetectProxy()
	}

	fmt.Printf("开始刷新 %d 个 Grok 账号，浏览器将逐个执行（无头: %t）；如改用有头模式遇到人机验证可手动完成\n", len(selected), *headless)
	results, err := provider.RunGrokCardLogin(context.Background(), selected, provider.GrokLoginOptions{
		ProxyURL:    *proxyURL,
		BrowserPath: *browserPath,
		Headless:    *headless,
		Timeout:     *timeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Grok 登录执行失败: %v\n", err)
		os.Exit(1)
	}
	written, err := provider.WriteGrokCookieOutput(*output, results, *merge)
	if err != nil {
		fmt.Fprintf(os.Stderr, "写入 SSO 输出失败: %v\n", err)
		os.Exit(1)
	}

	succeeded := 0
	for _, result := range results {
		if result.OK {
			succeeded++
			continue
		}
		fmt.Printf("失败 %s: %s\n", result.Email, result.Error)
	}
	fmt.Printf("完成: %d/%d，已写入 %d 条到 %s\n", succeeded, len(results), written, *output)
	if succeeded != len(results) {
		os.Exit(2)
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
	fmt.Println("  ai-web-free grok-login  使用卡密批量登录 xAI 并刷新 sso/sso-rw Cookie")
	fmt.Println()
}
