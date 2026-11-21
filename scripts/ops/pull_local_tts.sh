#!/usr/bin/env bash
set -euo pipefail

GATEWAY_URL="${GATEWAY_URL:-http://127.0.0.1:18080}"
START_DOCKER="${START_DOCKER:-0}"
LIMIT="${LIMIT:-24}"

if [[ "${START_DOCKER}" == "1" ]]; then
  echo ">>> 启动本地 Kokoro-FastAPI 容器 (profile: local-tts)..."
  docker compose --profile local-tts up -d kokoro-tts
  echo ">>> 等待 Kokoro TTS 就绪..."
  for _ in $(seq 1 45); do
    if curl -fsS "http://127.0.0.1:8880/v1/audio/voices" >/dev/null 2>&1; then
      echo ">>> Kokoro TTS 已就绪"
      break
    fi
    sleep 2
  done
fi

echo ">>> 正在触发音色本地缓存拉取: ${GATEWAY_URL%/}/v1/audio/voices/pull-local (limit=${LIMIT})"
curl -fsS -X POST "${GATEWAY_URL%/}/v1/audio/voices/pull-local" \
  -H "Content-Type: application/json" \
  -d "{\"include_samples\":true,\"refresh_live\":true,\"limit\":${LIMIT}}"
echo ""
