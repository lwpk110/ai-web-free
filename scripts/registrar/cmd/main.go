package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"ai-web-free/scripts/registrar"
)

func main() {
	providerName := flag.String("provider", "grok", "目标供应商 (grok 或 chatgpt)")
	count := flag.Int("count", 1, "批量注册账号数量")
	gatewayURL := flag.String("gateway", "http://localhost:18080", "ai-web-free 网关管理地址")
	adminToken := flag.String("admin-token", "", "网关 ADMIN_TOKEN (若已配置)")
	capsolverKey := flag.String("capsolver-key", "", "CapSolver API 密钥 (留空使用模拟模式)")
	mockMail := flag.Bool("mock-mail", false, "使用本地模拟临时邮箱服务 (无需外网依赖)")
	dryRun := flag.Bool("dry-run", false, "模拟演练模式 (不向网关提交)")
	flag.Parse()

	log.Printf("[Registrar] 启动 %s 免费号自动注册机, 目标数量: %d, 网关: %s", *providerName, *count, *gatewayURL)

	var solver registrar.CaptchaSolver
	if *capsolverKey != "" {
		solver = registrar.NewCapSolverClient(*capsolverKey)
		log.Printf("[Registrar] 使用 CapSolver API 真实打码服务")
	} else {
		solver = registrar.NewMockCaptchaSolver("")
		log.Printf("[Registrar] 未指定 capsolver-key，启用本地模拟/测试打码服务")
	}

	var mailService registrar.MailProvider
	if *mockMail {
		mailService = registrar.NewMockMailProvider("886611")
		log.Printf("[Registrar] 启用本地 Mock 临时邮箱")
	} else {
		mailService = registrar.NewMailTmProvider()
	}
	regConfig := registrar.RegistrarConfig{
		GatewayURL:  *gatewayURL,
		AdminToken:  *adminToken,
		MailService: mailService,
		Solver:      solver,
	}

	grokReg := registrar.NewGrokRegistrar(regConfig)

	successCount := 0
	for i := 0; i < *count; i++ {
		log.Printf("[Registrar] [%d/%d] 正在生成并验证账号...", i+1, *count)
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		res, err := grokReg.RegisterOne(ctx)
		cancel()

		if err != nil {
			log.Printf("[Registrar] [%d/%d] 注册失败: %v", i+1, *count, err)
			continue
		}

		log.Printf("[Registrar] [%d/%d] 注册成功: %s", i+1, *count, res.Email)
		fmt.Printf("ACCOUNT_LINE: %s|%s|%s\n", res.Email, res.Password, res.Cookie)

		if !*dryRun {
			pushCtx, pushCancel := context.WithTimeout(context.Background(), 15*time.Second)
			pushErr := registrar.PushToGateway(pushCtx, *gatewayURL, *adminToken, res)
			pushCancel()
			if pushErr != nil {
				log.Printf("[Registrar] 推送网关失败: %v", pushErr)
			} else {
				log.Printf("[Registrar] 已成功热推入网关号池并在数据库持久化！")
				successCount++
			}
		} else {
			log.Printf("[Registrar] [DryRun] 演练完成，跳过网关推送")
			successCount++
		}
	}

	log.Printf("[Registrar] 全部任务完成，成功数: %d/%d", successCount, *count)
}
