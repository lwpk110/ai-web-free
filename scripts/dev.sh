#!/usr/bin/env bash
set -e

PORT="${PORT:-18080}"
WEB_PORT="${WEB_PORT:-5173}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$ROOT_DIR"

# 1. 确保释放端口与历史残留进程
if [ -f "$ROOT_DIR/scripts/stop.sh" ]; then
    bash "$ROOT_DIR/scripts/stop.sh"
fi

# 2. 检查并安装前端依赖
if [ ! -d "web/node_modules" ]; then
    echo ">> 未检测到 web/node_modules，正在执行 npm install..."
    (cd web && npm install)
fi

# 3. 进程退出清理钩子
BACKEND_PID=""
cleanup() {
    echo ""
    echo ">> 正在关闭 Dev 开发服务..."
    if [ -n "$BACKEND_PID" ]; then
        kill "$BACKEND_PID" 2>/dev/null || true
        kill -9 "$BACKEND_PID" 2>/dev/null || true
    fi
    # 再次清理端口，确保子进程彻底退出
    if [ -f "$ROOT_DIR/scripts/stop.sh" ]; then
        bash "$ROOT_DIR/scripts/stop.sh" >/dev/null 2>&1 || true
    fi
    echo ">> Dev 服务已停止"
}
trap cleanup EXIT INT TERM

# 4. 启动 Go 后端
echo ">> 启动 Go 后端: http://localhost:${PORT}"
SERVER_PORT="${PORT}" go run . &
BACKEND_PID=$!

# 5. 启动 Vite 前端
echo ">> 启动 Vite 前端: http://localhost:${WEB_PORT} (代理至 http://localhost:${PORT})"
cd web && VITE_API_TARGET="http://localhost:${PORT}" npm run dev -- --port "${WEB_PORT}"
