# 启动或连接本地 Kokoro-FastAPI TTS，并将常用音色目录与试听样音预拉取到本地 data/storage
param(
    [string]$GatewayUrl = "http://127.0.0.1:18080",
    [switch]$StartDocker,
    [int]$Limit = 24
)

$ErrorActionPreference = "Stop"

if ($StartDocker) {
    Write-Host ">>> 启动本地 Kokoro-FastAPI 容器 (profile: local-tts)..." -ForegroundColor Cyan
    docker compose --profile local-tts up -d kokoro-tts
    Write-Host ">>> 等待 Kokoro TTS 健康检查就绪 (http://127.0.0.1:8880/v1/audio/voices)..." -ForegroundColor Cyan
    for ($i = 0; $i -lt 45; $i++) {
        try {
            $res = Invoke-RestMethod -Uri "http://127.0.0.1:8880/v1/audio/voices" -TimeoutSec 3
            if ($res) {
                Write-Host ">>> Kokoro TTS 已就绪！" -ForegroundColor Green
                break
            }
        } catch {
            Start-Sleep -Seconds 2
        }
    }
}

$pullEndpoint = "$($GatewayUrl.TrimEnd('/'))/v1/audio/voices/pull-local"
Write-Host ">>> 正在向网关触发音色本地拉取: $pullEndpoint (limit=$Limit)..." -ForegroundColor Cyan
$body = @{
    include_samples = $true
    refresh_live    = $true
    limit           = $Limit
} | ConvertTo-Json

$resp = Invoke-RestMethod -Method Post -Uri $pullEndpoint -ContentType "application/json" -Body $body -TimeoutSec 120
Write-Host ">>> 拉取完成:" -ForegroundColor Green
$resp | ConvertTo-Json -Depth 4
