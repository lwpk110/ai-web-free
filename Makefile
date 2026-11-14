BIN ?= ai-web-free
PORT ?= 18080
WEB_PORT ?= 5173
IMAGE ?= ai-web-free:latest
CGO_ENABLED ?= 0

.DEFAULT_GOAL := help

.PHONY: help web-install web-check web-build build build-go test test-go test-py adr-sync run dev dev-backend dev-frontend stop docker-build docker-up docker-down clean

help: ## 显示可用命令列表
	@awk 'BEGIN {FS = ":.*##"; printf "\n用法: make <target> [PORT=18080] [WEB_PORT=5173]\n\n"} /^[a-zA-Z0-9_-]+:.*?##/ {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

web-install: ## 安装前端依赖 (web/node_modules)
	cd web && npm ci

web-check: ## 运行前端 TypeScript 类型检查
	cd web && npx tsc --noEmit

web-build: web-check ## 构建前端静态资源 (web/dist 与 web/system-prompt.txt)
	cd web && npm run build

build-go: ## 仅编译零 CGO 纯静态 Go 二进制 (要求已存在 web/dist)
	CGO_ENABLED=$(CGO_ENABLED) go build -trimpath -ldflags="-s -w" -o $(BIN) .

build: web-build build-go ## 完整构建 (前端静态资源 + 纯静态 Go 二进制)

test-go: ## 运行全部 Go 单元测试
	go test ./...

test-py: ## 运行 Python 辅助脚本单元测试
	python3 -m unittest discover -s scripts -p "test_*.py" -v

test: test-go test-py ## 运行 Go + Python 全部测试

adr-sync: ## 回填并同步 /grill-me 访谈记录到 docs/adr/
	python3 .agents/hooks/grill_me_adr.py --backfill

run: build-go ## 编译并启动本地生产模式单二进制服务 (默认 PORT=18080)
	SERVER_PORT=$(PORT) ./$(BIN)

dev-backend: ## 仅启动 Go 后端开发服务 (默认 PORT=18080)
	SERVER_PORT=$(PORT) go run .

dev-frontend: ## 仅启动 Vite 前端热更新 (默认 WEB_PORT=5173，代理至 PORT=18080)
	cd web && VITE_API_TARGET="http://localhost:$(PORT)" npm run dev -- --port $(WEB_PORT)

dev: ## 本地联调模式：同时启动 Go 后端与 Vite 前端热更新 (默认 PORT=18080, WEB_PORT=5173)
	@bash -c '\
		set -e; \
		trap "kill 0 2>/dev/null || true" EXIT INT TERM; \
		echo ">> 启动 Go 后端: http://localhost:$(PORT)"; \
		SERVER_PORT=$(PORT) go run . & \
		echo ">> 启动 Vite 前端: http://localhost:$(WEB_PORT) (代理至 http://localhost:$(PORT))"; \
		cd web && VITE_API_TARGET="http://localhost:$(PORT)" npm run dev -- --port $(WEB_PORT) \
	'

stop: ## 停止本项目的本地运行进程
	@pkill -x "$(BIN)" 2>/dev/null || true
	@pkill -f "node_modules/.bin/vite" 2>/dev/null || true
	@echo "已停止本地项目进程"

docker-build: ## 构建 Docker 镜像
	docker build -t $(IMAGE) .

docker-up: ## 使用 Docker Compose 构建并后台启动
	docker-compose up -d --build

docker-down: ## 停止并移除 Docker Compose 容器
	docker-compose down

clean: ## 清理编译产物与临时日志
	rm -f $(BIN) *.log
