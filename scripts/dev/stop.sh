#!/usr/bin/env bash
PORT="${PORT:-18080}"
WEB_PORT="${WEB_PORT:-5173}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# 1. 优先通过端口查找并杀死占用进程 (Linux & macOS 通用)
PORTS="${PORT} ${WEB_PORT} 5174"
KILLED_ANY=false

for p in $PORTS; do
    if command -v lsof >/dev/null 2>&1; then
        PIDS=$(lsof -ti :"$p" 2>/dev/null || true)
        if [ -n "$PIDS" ]; then
            echo "$PIDS" | xargs kill -9 2>/dev/null || true
            KILLED_ANY=true
        fi
    elif command -v fuser >/dev/null 2>&1; then
        fuser -k -9 "$p/tcp" 2>/dev/null || true
        KILLED_ANY=true
    fi
done

# 2. 补漏：精确按进程名杀死二进制
pkill -9 -x "ai-web-free" 2>/dev/null || true
pkill -9 -x "ai-web-free.exe" 2>/dev/null || true

# 3. 补漏：杀死带特征的后台进程，使用括号防自身命令行匹配
pkill -9 -f "[n]ode_modules/.bin/vite" 2>/dev/null || true
pkill -9 -f "[g]o run \." 2>/dev/null || true

echo "已停止本地项目进程"
