#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

TUNNEL_TOKEN="${CF_TUNNEL_TOKEN:-}"
PORT=0
API_KEY=""
ADMIN_TOKEN=""
PROTOCOL="http2"
REBUILD=0
KEEP_SERVER=0
ALLOW_EMPTY_AUTH=0

usage() {
    cat <<'EOF'
用法: ./scripts/cf-tunnel.sh [选项]

选项:
  --tunnel-token <TOKEN>  Cloudflare Tunnel Token（传入则使用固定域名模式；省略则使用临时 Quick Tunnel）
  --port <PORT>           指定本地后端监听端口（默认读取 .env 中的 SERVER_PORT/PORT 或 18080）
  --api-key <KEY>         指定并写入 .env 的 Authorization API Key
  --admin-token <TOKEN>   指定并写入 .env 的 ADMIN_TOKEN 管理台密码
  --protocol <PROTO>      cloudflared 传输协议: http2 | quic | auto（默认 http2）
  --rebuild               强制重新编译 ai-web-free 二进制
  --keep-server           按 Ctrl+C 退出隧道后保留本脚本启动的后端进程继续运行
  --allow-empty-auth      允许跳过自动生成 Authorization / ADMIN_TOKEN（不推荐）
  -h, --help              显示此帮助信息
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --tunnel-token)
            TUNNEL_TOKEN="${2:-}"
            shift 2
            ;;
        --port)
            PORT="${2:-0}"
            shift 2
            ;;
        --api-key)
            API_KEY="${2:-}"
            shift 2
            ;;
        --admin-token)
            ADMIN_TOKEN="${2:-}"
            shift 2
            ;;
        --protocol)
            PROTOCOL="${2:-http2}"
            shift 2
            ;;
        --rebuild)
            REBUILD=1
            shift
            ;;
        --keep-server)
            KEEP_SERVER=1
            shift
            ;;
        --allow-empty-auth)
            ALLOW_EMPTY_AUTH=1
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            printf '未知参数: %s\n' "$1" >&2
            usage >&2
            exit 1
            ;;
    esac
done

case "$PROTOCOL" in
    http2|quic|auto) ;;
    *)
        printf '无效的 --protocol 值: %s（仅支持 http2 / quic / auto）\n' "$PROTOCOL" >&2
        exit 1
        ;;
esac

STARTED_PID=""
CLEANED_UP=0

cleanup() {
    if [[ "$CLEANED_UP" -eq 1 ]]; then
        return
    fi
    CLEANED_UP=1
    if [[ -n "$STARTED_PID" && "$KEEP_SERVER" -eq 0 ]]; then
        if kill -0 "$STARTED_PID" 2>/dev/null; then
            printf '\n正在关闭本次脚本启动的 ai-web-free（PID=%s）...\n' "$STARTED_PID"
            kill -TERM "$STARTED_PID" 2>/dev/null || true
            for _ in $(seq 1 20); do
                if ! kill -0 "$STARTED_PID" 2>/dev/null; then
                    break
                fi
                sleep 0.2
            done
            if kill -0 "$STARTED_PID" 2>/dev/null; then
                kill -KILL "$STARTED_PID" 2>/dev/null || true
            fi
        fi
    fi
}
trap cleanup EXIT INT TERM

trim_ws() {
    local s="$1"
    s="${s#"${s%%[![:space:]]*}"}"
    s="${s%"${s##*[![:space:]]}"}"
    printf '%s' "$s"
}

gen_secret() {
    local prefix="$1"
    local bytes="$2"
    local hex=""
    if command -v openssl >/dev/null 2>&1; then
        hex="$(openssl rand -hex "$bytes")"
    elif [[ -r /dev/urandom ]] && command -v od >/dev/null 2>&1; then
        hex="$(od -An -tx1 -N "$bytes" /dev/urandom | tr -d ' \t\r\n')"
    elif command -v python3 >/dev/null 2>&1; then
        hex="$(python3 -c "import secrets; print(secrets.token_hex($bytes))")"
    else
        printf '无法生成随机密钥：缺少 openssl、od(/dev/urandom) 或 python3\n' >&2
        exit 1
    fi
    printf '%s%s' "$prefix" "$hex"
}

read_env_val() {
    local file="$1"
    local target_key="$2"
    local val=""
    [[ -f "$file" ]] || return 0

    while IFS= read -r line || [[ -n "$line" ]]; do
        line="${line%$'\r'}"
        local trimmed
        trimmed="$(trim_ws "$line")"
        [[ -z "$trimmed" || "$trimmed" == \#* ]] && continue
        [[ "$line" != *"="* ]] && continue

        local k v
        k="$(trim_ws "${line%%=*}")"
        if [[ "$k" == "$target_key" ]]; then
            v="$(trim_ws "${line#*=}")"
            if [[ ${#v} -ge 2 ]]; then
                if [[ ("$v" == \"*\" && "$v" == *\") || ("$v" == \'*\' && "$v" == *\') ]]; then
                    v="${v:1:${#v}-2}"
                fi
            fi
            val="$v"
        fi
    done < "$file"
    printf '%s' "$val"
}

set_env_val() {
    local file="$1"
    local target_key="$2"
    local new_val="$3"
    local tmp_file
    tmp_file="$(mktemp)"
    local updated=0

    if [[ -f "$file" ]]; then
        while IFS= read -r line || [[ -n "$line" ]]; do
            line="${line%$'\r'}"
            local trimmed
            trimmed="$(trim_ws "$line")"
            if [[ -n "$trimmed" && "$trimmed" != \#* && "$line" == *"="* ]]; then
                local k
                k="$(trim_ws "${line%%=*}")"
                if [[ "$k" == "$target_key" && "$updated" -eq 0 ]]; then
                    printf '%s=%s\n' "$target_key" "$new_val" >> "$tmp_file"
                    updated=1
                    continue
                fi
            fi
            printf '%s\n' "$line" >> "$tmp_file"
        done < "$file"
    fi

    if [[ "$updated" -eq 0 ]]; then
        printf '%s=%s\n' "$target_key" "$new_val" >> "$tmp_file"
    fi

    mv "$tmp_file" "$file"
}

find_cloudflared() {
    if command -v cloudflared >/dev/null 2>&1; then
        command -v cloudflared
        return 0
    fi
    local candidate
    for candidate in /usr/local/bin/cloudflared /usr/bin/cloudflared "$HOME/.local/bin/cloudflared"; do
        if [[ -x "$candidate" ]]; then
            printf '%s' "$candidate"
            return 0
        fi
    done
    return 1
}

download_file() {
    local url="$1"
    local dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fL --retry 3 --connect-timeout 15 -o "$dest" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -O "$dest" "$url"
    else
        printf '自动下载失败：系统缺少 curl 或 wget\n' >&2
        return 1
    fi
}

ensure_cloudflared() {
    local bin=""
    if bin="$(find_cloudflared)"; then
        printf '%s' "$bin"
        return 0
    fi

    printf '    未检测到 cloudflared，正在从 Cloudflare 官方 GitHub Releases 自动下载...\n' >&2
    local arch asset url target_dir target_bin tmp_bin
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)   asset="cloudflared-linux-amd64" ;;
        aarch64|arm64)  asset="cloudflared-linux-arm64" ;;
        armv7l|armhf)   asset="cloudflared-linux-armhf" ;;
        arm*)           asset="cloudflared-linux-arm" ;;
        i386|i686)      asset="cloudflared-linux-386" ;;
        *)
            printf '不支持的 CPU 架构: %s，请手动安装 cloudflared：https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/\n' "$arch" >&2
            exit 1
            ;;
    esac

    url="https://github.com/cloudflare/cloudflared/releases/latest/download/${asset}"
    if [[ "$(id -u)" -eq 0 ]]; then
        target_dir="/usr/local/bin"
    else
        target_dir="$HOME/.local/bin"
    fi
    mkdir -p "$target_dir"
    target_bin="${target_dir}/cloudflared"
    tmp_bin="$(mktemp)"

    if ! download_file "$url" "$tmp_bin"; then
        rm -f "$tmp_bin"
        printf '下载 cloudflared 失败（%s）。请检查网络或手动安装后重试。\n' "$url" >&2
        exit 1
    fi
    chmod 0755 "$tmp_bin"
    mv "$tmp_bin" "$target_bin"
    printf '%s' "$target_bin"
}

get_http_status() {
    local url="$1"
    if command -v curl >/dev/null 2>&1; then
        curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$url" 2>/dev/null || true
        return 0
    fi
    if command -v python3 >/dev/null 2>&1; then
        python3 - "$url" <<'PY' 2>/dev/null || true
import sys, urllib.request, urllib.error
try:
    with urllib.request.urlopen(sys.argv[1], timeout=3) as r:
        print(r.status)
except urllib.error.HTTPError as e:
    print(e.code)
except Exception:
    print("000")
PY
        return 0
    fi
    if command -v wget >/dev/null 2>&1; then
        local out
        out="$(wget -q --server-response --spider --timeout=3 "$url" 2>&1 || true)"
        printf '%s' "$out" | awk '/HTTP\//{code=$2} END{if (code=="") print "000"; else print code}'
        return 0
    fi
    printf '000'
}

get_listening_pids() {
    local target_port="$1"
    if command -v ss >/dev/null 2>&1; then
        ss -ltnp "sport = :${target_port}" 2>/dev/null \
            | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' \
            | sort -u
        return 0
    fi
    if command -v lsof >/dev/null 2>&1; then
        lsof -t -iTCP:"$target_port" -sTCP:LISTEN 2>/dev/null | sort -u || true
        return 0
    fi
    if command -v fuser >/dev/null 2>&1; then
        fuser "${target_port}/tcp" 2>/dev/null | tr -s ' ' '\n' | grep -E '^[0-9]+$' | sort -u || true
        return 0
    fi
}

test_backend_ready() {
    local target_port="$1"
    local scheme="$2"
    local expected_api_key="$3"
    local expected_admin_token="$4"

    if [[ "$scheme" == "https" ]]; then
        return 0
    fi

    local ping_code
    ping_code="$(get_http_status "http://127.0.0.1:${target_port}/ping")"
    [[ "$ping_code" == "200" ]] || return 1

    if [[ -n "$expected_api_key" ]]; then
        local models_code
        models_code="$(get_http_status "http://127.0.0.1:${target_port}/v1/models")"
        [[ "$models_code" == "401" ]] || return 1
    fi

    if [[ -n "$expected_admin_token" ]]; then
        local admin_code
        admin_code="$(get_http_status "http://127.0.0.1:${target_port}/admin/api/v1/overview")"
        [[ "$admin_code" == "401" ]] || return 1
    fi

    return 0
}

stop_ai_web_free_on_port() {
    local target_port="$1"
    local pids
    pids="$(get_listening_pids "$target_port")"
    if [[ -z "$pids" ]]; then
        if [[ "$(get_http_status "http://127.0.0.1:${target_port}/ping")" != "000" ]]; then
            printf '端口 %s 已被占用了服务，但当前权限无法识别或停止其 PID；请手动关闭旧服务或通过 --port 指定其他端口。\n' "$target_port" >&2
            exit 1
        fi
        return 0
    fi

    local pid proc_comm proc_args
    for pid in $pids; do
        proc_comm="$(ps -p "$pid" -o comm= 2>/dev/null || true)"
        proc_args="$(ps -p "$pid" -o args= 2>/dev/null || true)"
        if [[ "$proc_comm" != *ai-web-free* && "$proc_args" != *ai-web-free* ]]; then
            printf '端口 %s 当前被其他进程占用（PID=%s, 进程=%s）。请关闭该进程或通过 --port 指定其他端口。\n' "$target_port" "$pid" "$proc_comm" >&2
            exit 1
        fi
        printf '    正在停止旧的 ai-web-free 进程（PID=%s）以应用最新配置...\n' "$pid"
        kill -TERM "$pid" 2>/dev/null || true
    done

    for _ in $(seq 1 25); do
        sleep 0.2
        if [[ -z "$(get_listening_pids "$target_port")" ]]; then
            return 0
        fi
    done

    for pid in $pids; do
        kill -KILL "$pid" 2>/dev/null || true
    done
    sleep 0.3
    if [[ -n "$(get_listening_pids "$target_port")" ]]; then
        printf '停止端口 %s 上的旧进程超时，请手动检查。\n' "$target_port" >&2
        exit 1
    fi
}

ensure_backend_binary() {
    local bin_path="$1"
    local force_rebuild="$2"
    local need_frontend=0

    if [[ ! -f "$REPO_ROOT/web/dist/index.html" || ! -f "$REPO_ROOT/web/system-prompt.txt" ]]; then
        need_frontend=1
    fi

    if [[ "$force_rebuild" -eq 0 && -x "$bin_path" && "$need_frontend" -eq 0 ]]; then
        return 0
    fi

    if [[ "$need_frontend" -eq 1 ]]; then
        printf '    检测到缺少前端构建产物，正在构建 web/dist 与 system-prompt.txt...\n'
        if ! command -v npm >/dev/null 2>&1; then
            printf '缺少 web/dist 构建产物且未找到 npm，请先安装 Node.js 或执行 cd web && npm ci && npm run build。\n' >&2
            exit 1
        fi
        (
            cd "$REPO_ROOT/web"
            if [[ ! -d node_modules ]]; then
                npm ci
            fi
            npx tsc --noEmit
            npm run build
        )
    fi

    if ! command -v go >/dev/null 2>&1; then
        printf '未找到 Go 编译器，且项目根目录缺少可执行文件 %s。\n' "$bin_path" >&2
        exit 1
    fi

    printf '    正在编译纯静态后端二进制 ai-web-free...\n'
    (
        cd "$REPO_ROOT"
        CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$bin_path" .
    )
}

cd "$REPO_ROOT"

printf '============================================================\n'
printf '  ai-web-free · Cloudflare Tunnel 一键内网穿透脚本 (Linux)\n'
printf '============================================================\n'

# 1. 初始化并加固 .env
printf '[1/4] 检查 .env 与鉴权配置...\n'
ENV_FILE="$REPO_ROOT/.env"
if [[ ! -f "$ENV_FILE" ]]; then
    if [[ -f "$REPO_ROOT/env.template" ]]; then
        cp "$REPO_ROOT/env.template" "$ENV_FILE"
        printf '    已从 env.template 初始化 .env\n'
    else
        : > "$ENV_FILE"
    fi
fi

ENV_UPDATED=0
API_KEY_STATUS="已有 (.env)"
ADMIN_TOKEN_STATUS="已有 (.env)"

CURRENT_API_KEY="$(read_env_val "$ENV_FILE" "Authorization")"
CURRENT_ADMIN_TOKEN="$(read_env_val "$ENV_FILE" "ADMIN_TOKEN")"

if [[ -n "$(trim_ws "$API_KEY")" ]]; then
    CURRENT_API_KEY="$(trim_ws "$API_KEY")"
    set_env_val "$ENV_FILE" "Authorization" "$CURRENT_API_KEY"
    ENV_UPDATED=1
    API_KEY_STATUS="已按参数更新"
elif [[ -z "$(trim_ws "$CURRENT_API_KEY")" ]]; then
    if [[ "$ALLOW_EMPTY_AUTH" -eq 1 ]]; then
        API_KEY_STATUS="未设置（已跳过，公网裸奔风险！）"
    else
        CURRENT_API_KEY="$(gen_secret "sk-awf-" 18)"
        set_env_val "$ENV_FILE" "Authorization" "$CURRENT_API_KEY"
        ENV_UPDATED=1
        API_KEY_STATUS="本次自动生成并写入 .env"
    fi
fi

if [[ -n "$(trim_ws "$ADMIN_TOKEN")" ]]; then
    CURRENT_ADMIN_TOKEN="$(trim_ws "$ADMIN_TOKEN")"
    set_env_val "$ENV_FILE" "ADMIN_TOKEN" "$CURRENT_ADMIN_TOKEN"
    ENV_UPDATED=1
    ADMIN_TOKEN_STATUS="已按参数更新"
elif [[ -z "$(trim_ws "$CURRENT_ADMIN_TOKEN")" ]]; then
    if [[ "$ALLOW_EMPTY_AUTH" -eq 1 ]]; then
        ADMIN_TOKEN_STATUS="未设置（已跳过，管理后台可免登访问！）"
    else
        CURRENT_ADMIN_TOKEN="$(gen_secret "adm-" 16)"
        set_env_val "$ENV_FILE" "ADMIN_TOKEN" "$CURRENT_ADMIN_TOKEN"
        ENV_UPDATED=1
        ADMIN_TOKEN_STATUS="本次自动生成并写入 .env"
    fi
fi

if [[ "$PORT" -le 0 ]]; then
    PORT_STR="$(read_env_val "$ENV_FILE" "SERVER_PORT")"
    if [[ -z "$PORT_STR" ]]; then
        PORT_STR="$(read_env_val "$ENV_FILE" "PORT")"
    fi
    if [[ "$PORT_STR" =~ ^[0-9]+$ && "$PORT_STR" -gt 0 ]]; then
        PORT="$PORT_STR"
    else
        PORT=18080
    fi
fi

ORIGIN_SCHEME="http"
USE_LOCAL_TLS=0
TLS_CERT_VAL="$(read_env_val "$ENV_FILE" "TLS_CERT")"
TLS_KEY_VAL="$(read_env_val "$ENV_FILE" "TLS_KEY")"
if [[ -n "$(trim_ws "$TLS_CERT_VAL")" && -n "$(trim_ws "$TLS_KEY_VAL")" ]]; then
    ORIGIN_SCHEME="https"
    USE_LOCAL_TLS=1
    printf '    [提示] 检测到 .env 配置了 TLS_CERT/TLS_KEY，本地源站将按 HTTPS 对接；Cloudflare 隧道已自带公网 HTTPS，通常建议留空。\n'
fi

# 2. 检查/安装 cloudflared
printf '[2/4] 检查 cloudflared 客户端...\n'
CLOUDFLARED_BIN="$(ensure_cloudflared)"
printf '    已就绪: %s\n' "$CLOUDFLARED_BIN"

# 3. 检查并启动本地后端
printf '[3/4] 检查本地后端服务 (127.0.0.1:%s)...\n' "$PORT"
BIN_PATH="$REPO_ROOT/ai-web-free"
NEED_START_BACKEND=1
EXISTING_PIDS="$(get_listening_pids "$PORT")"
PORT_PING_UP=0
if [[ "$ORIGIN_SCHEME" == "http" && "$(get_http_status "http://127.0.0.1:${PORT}/ping")" == "200" ]]; then
    PORT_PING_UP=1
fi

if [[ -n "$EXISTING_PIDS" || "$PORT_PING_UP" -eq 1 ]]; then
    AUTH_MATCH=0
    if [[ "$ENV_UPDATED" -eq 0 && "$REBUILD" -eq 0 ]]; then
        if test_backend_ready "$PORT" "$ORIGIN_SCHEME" "$CURRENT_API_KEY" "$CURRENT_ADMIN_TOKEN"; then
            AUTH_MATCH=1
        fi
    fi

    if [[ "$AUTH_MATCH" -eq 1 ]]; then
        printf '    检测到后端已在运行且鉴权配置生效，直接复用。\n'
        NEED_START_BACKEND=0
    else
        stop_ai_web_free_on_port "$PORT"
    fi
fi

if [[ "$NEED_START_BACKEND" -eq 1 ]]; then
    ensure_backend_binary "$BIN_PATH" "$REBUILD"
    LOG_FILE="$REPO_ROOT/cf-tunnel-server.log"
    printf '    正在启动 ai-web-free (端口 %s)...\n' "$PORT"
    SERVER_PORT="$PORT" nohup "$BIN_PATH" > "$LOG_FILE" 2>&1 &
    STARTED_PID=$!

    READY=0
    for _ in $(seq 1 120); do
        sleep 0.5
        if ! kill -0 "$STARTED_PID" 2>/dev/null; then
            printf 'ai-web-free 启动后提前退出，最近日志如下：\n' >&2
            tail -n 30 "$LOG_FILE" >&2 || true
            exit 1
        fi
        if [[ "$ORIGIN_SCHEME" == "https" ]]; then
            if [[ -n "$(get_listening_pids "$PORT")" ]]; then
                READY=1
                break
            fi
        else
            if [[ "$(get_http_status "http://127.0.0.1:${PORT}/ping")" == "200" ]]; then
                READY=1
                break
            fi
        fi
    done

    if [[ "$READY" -eq 0 ]]; then
        printf '等待本地服务监听 127.0.0.1:%s 超时，请查看 %s。\n' "$PORT" "$LOG_FILE" >&2
        exit 1
    fi

    if ! test_backend_ready "$PORT" "$ORIGIN_SCHEME" "$CURRENT_API_KEY" "$CURRENT_ADMIN_TOKEN"; then
        printf '本地服务已启动，但 Authorization / ADMIN_TOKEN 鉴权校验未生效，请检查 .env 与 %s。\n' "$LOG_FILE" >&2
        exit 1
    fi
    printf '    后端服务启动成功（PID=%s），日志写入 cf-tunnel-server.log\n' "$STARTED_PID"
fi

# 4. 打印接入信息并启动 Cloudflare Tunnel
if [[ -z "$(trim_ws "$TUNNEL_TOKEN")" ]]; then
    MODE_LABEL="Quick Tunnel 临时公网地址（无需 Cloudflare API Key / 账号 / 域名）"
else
    MODE_LABEL="Named Tunnel 固定域名模式（使用你提供的 Cloudflare Tunnel Token）"
fi

printf '\n'
printf '-------------------- 接入与安全信息 --------------------\n'
printf '  穿透模式          : %s\n' "$MODE_LABEL"
printf '  本地源站          : %s://127.0.0.1:%s\n' "$ORIGIN_SCHEME" "$PORT"
printf '  API Key (发给用户): %s  [%s]\n' "$CURRENT_API_KEY" "$API_KEY_STATUS"
printf '  ADMIN_TOKEN (后台): %s  [%s]\n' "$CURRENT_ADMIN_TOKEN" "$ADMIN_TOKEN_STATUS"
printf '  客户端 Base URL   : <下方生成的 https://...>/v1\n'
printf '  退出提示          : 按 Ctrl+C 关闭隧道\n'
printf '--------------------------------------------------------\n\n'

printf '[4/4] 正在启动 Cloudflare Tunnel，请等待下方输出公网 URL...\n'
if [[ -z "$(trim_ws "$TUNNEL_TOKEN")" ]]; then
    CF_ARGS=(tunnel --url "${ORIGIN_SCHEME}://127.0.0.1:${PORT}" --protocol "$PROTOCOL")
    if [[ "$USE_LOCAL_TLS" -eq 1 ]]; then
        CF_ARGS+=(--no-tls-verify)
    fi
else
    CF_ARGS=(tunnel --protocol "$PROTOCOL" --no-autoupdate run --token "$(trim_ws "$TUNNEL_TOKEN")")
fi

"$CLOUDFLARED_BIN" "${CF_ARGS[@]}"
