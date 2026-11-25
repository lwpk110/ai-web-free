PORT ?= 18080
WEB_PORT ?= 5173
IMAGE ?= ai-web-free:latest
CGO_ENABLED ?= 0
# 避免宿主机环境变量中残留旧版本 GOROOT 导致 GOTOOLCHAIN=auto 切换后出现 compile version mismatch
unexport GOROOT

ifeq ($(OS),Windows_NT)
  BIN ?= ai-web-free.exe
  # Windows 下自动定位 Git Bash，避免误唤起 WSL (C:\Windows\System32\bash.exe)
  GIT_PATHS := $(wildcard D:/programs/Git/bin/bash.exe "C:/Program Files/Git/bin/bash.exe" "C:/Program Files (x86)/Git/bin/bash.exe")
  ifeq ($(GIT_PATHS),)
    GIT_WHERE := $(shell where.exe git 2>NUL)
    ifneq ($(GIT_WHERE),)
      GIT_PATHS := $(subst \cmd\git.exe,/bin/bash.exe,$(subst \,/,$(firstword $(GIT_WHERE))))
    endif
  endif
  BASH_EXE := $(if $(GIT_PATHS),"$(firstword $(GIT_PATHS))",bash)
else
  BIN ?= ai-web-free
  BASH_EXE := bash
endif

.DEFAULT_GOAL := help

.PHONY: help web-install web-check web-build build build-go test test-go test-py adr-sync run dev dev-backend dev-frontend stop docker-build docker-up docker-down clean

help: ## 显示可用命令列表
	@$(BASH_EXE) -c 'awk "BEGIN {FS = \":.*##\"; printf \"\n用法: make <target> [PORT=18080] [WEB_PORT=5173]\n\n\"} /^[a-zA-Z0-9_-]+:.*?##/ {printf \"  \033[36m%-14s\033[0m %s\n\", \$$1, \$$2}" $(MAKEFILE_LIST)'

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

run: stop build-go ## 停止旧进程、编译并启动本地生产模式单二进制服务 (默认 PORT=18080)
	@$(BASH_EXE) -c 'SERVER_PORT=$(PORT) ./$(BIN)'

dev-backend: stop ## 仅启动 Go 后端开发服务 (默认 PORT=18080)
	@$(BASH_EXE) -c 'SERVER_PORT=$(PORT) go run .'

dev-frontend: ## 仅启动 Vite 前端热更新 (默认 WEB_PORT=5173，代理至 PORT=18080)
	@$(BASH_EXE) -c '\
		if [ ! -d "web/node_modules" ]; then \
			echo ">> 未检测到 web/node_modules，正在自动执行 npm install..."; \
			(cd web && npm install); \
		fi; \
		cd web && VITE_API_TARGET="http://localhost:$(PORT)" npm run dev -- --port $(WEB_PORT) \
	'

dev: ## 本地联调模式：自动停止已有进程，并同时启动 Go 后端与 Vite 前端热更新 (默认 PORT=18080, WEB_PORT=5173)
ifeq ($(OS),Windows_NT)
	@powershell.exe -ExecutionPolicy Bypass -File scripts/dev/dev.ps1 -Port $(PORT) -WebPort $(WEB_PORT)
else
	@PORT=$(PORT) WEB_PORT=$(WEB_PORT) bash scripts/dev/dev.sh
endif

stop: ## 停止本项目的本地运行进程
ifeq ($(OS),Windows_NT)
	@powershell.exe -ExecutionPolicy Bypass -File scripts/dev/stop.ps1 -Port $(PORT) -WebPort $(WEB_PORT)
else
	@PORT=$(PORT) WEB_PORT=$(WEB_PORT) bash scripts/dev/stop.sh
endif

clean: ## 清理编译产物与临时日志
ifeq ($(OS),Windows_NT)
	@$(BASH_EXE) -c 'rm -f $(BIN) ai-web-free.exe *.log'
else
	@rm -f $(BIN) ai-web-free *.log
endif

sync-push: ## 备份 SQLite 数据库、.env 与多媒体缓存全量同步到七牛云
	@$(BASH_EXE) -c 'go run scripts/ops/sync_qiniu.go push'

sync-pull: ## 跨机一键恢复：从七牛云全量拉取最新 SQLite 数据库、.env 与全部关联多媒体物料
	@$(BASH_EXE) -c 'go run scripts/ops/sync_qiniu.go pull'

backup: sync-push ## 别名：一键全量备份到云存储（数据库、.env 与多媒体物料）
restore: sync-pull ## 别名：一键全量恢复（SQLite 数据库与关联物料一体化恢复）
restore-all: restore ## 兼容别名

docker-build: ## 构建 Docker 镜像
	docker build -t $(IMAGE) .

docker-up: ## 使用 Docker Compose 构建并后台启动
	docker-compose up -d --build

docker-down: ## 停止并移除 Docker Compose 容器
	docker-compose down
