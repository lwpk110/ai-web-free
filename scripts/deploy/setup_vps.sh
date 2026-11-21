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

echo "[setup_vps] VPS 基础环境与 systemd 服务就绪。"
