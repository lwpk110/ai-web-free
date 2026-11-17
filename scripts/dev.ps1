param(
    [int]$Port = 18080,
    [int]$WebPort = 5173
)

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$rootDir = Split-Path -Parent $scriptDir
Set-Location $rootDir

# 1. 停止残留进程与端口占用
& "$scriptDir/stop.ps1" -Port $Port -WebPort $WebPort

# 2. 检查并安装前端依赖
if (-not (Test-Path "web/node_modules")) {
    Write-Host ">> 未检测到 web/node_modules，正在执行 npm install..."
    Set-Location "$rootDir/web"
    npm install
    Set-Location $rootDir
}

# 3. 启动 Go 后端
Write-Host ">> 启动 Go 后端: http://localhost:$Port"
$env:SERVER_PORT = "$Port"
$backendProcess = Start-Process -FilePath "go" -ArgumentList "run", "." -WorkingDirectory $rootDir -PassThru

# 4. 启动 Vite 前端
Write-Host ">> 启动 Vite 前端: http://localhost:$WebPort (代理至 http://localhost:$Port)"
$env:VITE_API_TARGET = "http://localhost:$Port"

try {
    Set-Location "$rootDir/web"
    npm run dev -- --port $WebPort
} finally {
    Write-Host "`n>> 正在关闭 Dev 开发服务..."
    if ($backendProcess -and -not $backendProcess.HasExited) {
        Stop-Process -Id $backendProcess.Id -Force -ErrorAction SilentlyContinue
    }
    & "$scriptDir/stop.ps1" -Port $Port -WebPort $WebPort | Out-Null
    Write-Host ">> Dev 服务已停止"
}
