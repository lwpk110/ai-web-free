#!/usr/bin/env bash
set -euo pipefail

# ==============================================================================
# setup_vps.sh - 阿里云 / Linux VPS 一键初始化与幂等部署引导脚本
# 职责：
#   1. 初始化 /opt/ai-web-free 与 /etc/mihomo 目录结构
#   2. 自动检测架构并安装 Mihomo (Clash Meta 内核，用于将机场订阅映射为本机多端口代理池)
#   3. 生成安全的初始 /etc/mihomo/config.yaml（仅监听 127.0.0.1，防止公网暴露被盗刷）
#   4. 注册并启用 mihomo.service 与 ai-web-free.service
# ==============================================================================

APP_DIR="${APP_DIR:-/opt/ai-web-free}"
MIHOMO_DIR="${MIHOMO_DIR:-/etc/mihomo}"
MIHOMO_BIN="${MIHOMO_BIN:-/usr/local/bin/mihomo}"
MIHOMO_VERSION="${MIHOMO_VERSION:-v1.18.10}"
CLOUDFLARED_BIN="${CLOUDFLARED_BIN:-/usr/local/bin/cloudflared}"
CF_TUNNEL_TOKEN="${CF_TUNNEL_TOKEN:-}"
CF_TUNNEL_PROTOCOL="${CF_TUNNEL_PROTOCOL:-http2}"

while [ $# -gt 0 ]; do
  case "$1" in
    --cf-token)
      CF_TUNNEL_TOKEN="${2:-}"
      shift 2
      ;;
    --cf-protocol)
      CF_TUNNEL_PROTOCOL="${2:-http2}"
      shift 2
      ;;
    --app-dir)
      APP_DIR="${2:-/opt/ai-web-free}"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done

# 若未显式传参，尝试从 .env 自动读取 CF_TUNNEL_TOKEN / CF_TUNNEL_PROTOCOL
if [ -z "${CF_TUNNEL_TOKEN}" ] && [ -f "${APP_DIR}/.env" ]; then
  CF_TUNNEL_TOKEN="$(grep -E '^CF_TUNNEL_TOKEN=' "${APP_DIR}/.env" | tail -n 1 | cut -d'=' -f2- | tr -d ' "\r' || true)"
fi
if [ -f "${APP_DIR}/.env" ]; then
  ENV_CF_PROTO="$(grep -E '^CF_TUNNEL_PROTOCOL=' "${APP_DIR}/.env" | tail -n 1 | cut -d'=' -f2- | tr -d ' "\r' || true)"
  if [ -n "${ENV_CF_PROTO}" ]; then
    CF_TUNNEL_PROTOCOL="${ENV_CF_PROTO}"
  fi
fi

echo "[setup_vps] 1/4 创建运行目录: ${APP_DIR} & ${MIHOMO_DIR}"
mkdir -p "${APP_DIR}/data/storage" "${MIHOMO_DIR}"

install_mihomo_if_needed() {
  if [ -x "${MIHOMO_BIN}" ]; then
    echo "[setup_vps] Mihomo 已安装: $(${MIHOMO_BIN} -v | head -n 1 || true)"
    return 0
  fi

  ARCH="$(uname -m)"
  case "${ARCH}" in
    x86_64|amd64)
      MIHOMO_ARCH="amd64"
      ;;
    aarch64|arm64)
      MIHOMO_ARCH="arm64"
      ;;
    *)
      echo "[setup_vps] 警告: 未知 CPU 架构 ${ARCH}，跳过自动下载 Mihomo"
      return 0
      ;;
  esac

  echo "[setup_vps] 2/4 正在下载 Mihomo (${MIHOMO_VERSION}, linux-${MIHOMO_ARCH})..."
  TMP_GZ="$(mktemp /tmp/mihomo.XXXXXX.gz)"
  # 优先尝试 GitHub Mirror (适合阿里云国内 VPS) 与官方 GitHub Release
  DOWNLOAD_URLS=(
    "https://ghfast.top/https://github.com/MetaCubeX/mihomo/releases/download/${MIHOMO_VERSION}/mihomo-linux-${MIHOMO_ARCH}-${MIHOMO_VERSION}.gz"
    "https://mirror.ghproxy.com/https://github.com/MetaCubeX/mihomo/releases/download/${MIHOMO_VERSION}/mihomo-linux-${MIHOMO_ARCH}-${MIHOMO_VERSION}.gz"
    "https://github.com/MetaCubeX/mihomo/releases/download/${MIHOMO_VERSION}/mihomo-linux-${MIHOMO_ARCH}-${MIHOMO_VERSION}.gz"
  )

  DOWNLOADED=0
  for u in "${DOWNLOAD_URLS[@]}"; do
    if curl -fsSL --connect-timeout 10 --max-time 60 "${u}" -o "${TMP_GZ}"; then
      DOWNLOADED=1
      break
    fi
  done

  if [ "${DOWNLOADED}" -eq 1 ]; then
    gzip -dc "${TMP_GZ}" > "${MIHOMO_BIN}.tmp"
    chmod +x "${MIHOMO_BIN}.tmp"
    mv -f "${MIHOMO_BIN}.tmp" "${MIHOMO_BIN}"
    rm -f "${TMP_GZ}"
    echo "[setup_vps] Mihomo 安装成功: $(${MIHOMO_BIN} -v | head -n 1 || true)"
  else
    rm -f "${TMP_GZ}"
    echo "[setup_vps] 警告: 自动下载 Mihomo 失败，请稍后手动放置二进制至 ${MIHOMO_BIN}"
  fi
}

install_mihomo_if_needed

if [ ! -f "${MIHOMO_DIR}/config.yaml" ]; then
  echo "[setup_vps] 3/4 初始化默认 ${MIHOMO_DIR}/config.yaml"
  cat > "${MIHOMO_DIR}/config.yaml" <<'EOF'
mixed-port: 7890
allow-lan: false
bind-address: 127.0.0.1
mode: rule
log-level: warning
ipv6: false
external-controller: 127.0.0.1:9090
proxies: []
listeners: []
EOF
fi

echo "[setup_vps] 4/4 注册 systemd 服务单元"
if [ -f "${APP_DIR}/deploy/systemd/mihomo.service" ]; then
  cp -f "${APP_DIR}/deploy/systemd/mihomo.service" /etc/systemd/system/mihomo.service
fi
if [ -f "${APP_DIR}/deploy/systemd/ai-web-free.service" ]; then
  cp -f "${APP_DIR}/deploy/systemd/ai-web-free.service" /etc/systemd/system/ai-web-free.service
fi

systemctl daemon-reload
if [ -x "${MIHOMO_BIN}" ] && [ -f /etc/systemd/system/mihomo.service ]; then
  systemctl enable mihomo.service >/dev/null 2>&1 || true
  systemctl restart mihomo.service || true
fi
if [ -f /etc/systemd/system/ai-web-free.service ]; then
  systemctl enable ai-web-free.service >/dev/null 2>&1 || true
fi

setup_cloudflared_if_configured() {
  if [ -z "${CF_TUNNEL_TOKEN}" ]; then
    return 0
  fi

  if [ ! -x "${CLOUDFLARED_BIN}" ]; then
    ARCH="$(uname -m)"
    case "${ARCH}" in
      x86_64|amd64) CF_ASSET="cloudflared-linux-amd64" ;;
      aarch64|arm64) CF_ASSET="cloudflared-linux-arm64" ;;
      *)
        echo "[setup_vps] 警告: 未知架构 ${ARCH}，跳过自动下载 cloudflared"
        return 0
        ;;
    esac
    echo "[setup_vps] 检测到 CF_TUNNEL_TOKEN，正在下载安装 cloudflared (${CF_ASSET})..."
    TMP_CF="$(mktemp /tmp/cloudflared.XXXXXX)"
    CF_URLS=(
      "https://ghfast.top/https://github.com/cloudflare/cloudflared/releases/latest/download/${CF_ASSET}"
      "https://mirror.ghproxy.com/https://github.com/cloudflare/cloudflared/releases/latest/download/${CF_ASSET}"
      "https://github.com/cloudflare/cloudflared/releases/latest/download/${CF_ASSET}"
    )
    CF_OK=0
    for u in "${CF_URLS[@]}"; do
      if curl -fsSL --connect-timeout 10 --max-time 60 "${u}" -o "${TMP_CF}"; then
        CF_OK=1
        break
      fi
    done
    if [ "${CF_OK}" -eq 1 ]; then
      chmod 0755 "${TMP_CF}"
      mv -f "${TMP_CF}" "${CLOUDFLARED_BIN}"
      echo "[setup_vps] cloudflared 安装成功: $(${CLOUDFLARED_BIN} --version || true)"
    else
      rm -f "${TMP_CF}"
      echo "[setup_vps] 警告: 下载 cloudflared 失败，跳过配置隧道服务"
      return 0
    fi
  fi

  cat > /etc/systemd/system/cloudflared.service <<EOF
[Unit]
Description=Cloudflare Tunnel for AI-Web-Free
After=network-online.target ai-web-free.service
Wants=network-online.target

[Service]
Type=simple
User=root
ExecStart=${CLOUDFLARED_BIN} tunnel --protocol ${CF_TUNNEL_PROTOCOL} --no-autoupdate run --token ${CF_TUNNEL_TOKEN}
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=cloudflared

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable cloudflared.service >/dev/null 2>&1 || true
  systemctl restart cloudflared.service || true
  echo "[setup_vps] Cloudflare Tunnel (protocol=${CF_TUNNEL_PROTOCOL}) 已注册并启动！"
}

setup_cloudflared_if_configured

echo "[setup_vps] VPS 基础环境与 systemd 服务就绪。"
