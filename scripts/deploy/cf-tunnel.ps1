[CmdletBinding()]
param(
    [string]$TunnelToken = $env:CF_TUNNEL_TOKEN,
    [int]$Port = 0,
    [string]$ApiKey = "",
    [string]$AdminToken = "",
    [ValidateSet("http2", "quic", "auto")]
    [string]$Protocol = "http2",
    [switch]$Rebuild,
    [switch]$KeepServer,
    [switch]$AllowEmptyAuth
)

$ErrorActionPreference = "Stop"

function New-RandomSecret {
    param(
        [string]$Prefix,
        [int]$ByteCount = 18
    )
    $bytes = New-Object byte[] $ByteCount
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($bytes)
    }
    finally {
        $rng.Dispose()
    }
    $hex = -join ($bytes | ForEach-Object { $_.ToString("x2") })
    return "$Prefix$hex"
}

function Read-EnvMap {
    param([string]$Path)
    $map = [ordered]@{}
    if (-not (Test-Path $Path)) {
        return $map
    }
    foreach ($line in (Get-Content -Path $Path -Encoding UTF8)) {
        $trimmed = $line.Trim()
        if ([string]::IsNullOrWhiteSpace($trimmed) -or $trimmed.StartsWith("#")) {
            continue
        }
        $idx = $line.IndexOf("=")
        if ($idx -lt 1) {
            continue
        }
        $k = $line.Substring(0, $idx).Trim()
        $v = $line.Substring($idx + 1).Trim()
        if (($v.StartsWith('"') -and $v.EndsWith('"')) -or ($v.StartsWith("'") -and $v.EndsWith("'"))) {
            if ($v.Length -ge 2) {
                $v = $v.Substring(1, $v.Length - 2)
            }
        }
        $map[$k] = $v
    }
    return $map
}

function Set-EnvValue {
    param(
        [string]$Path,
        [string]$Key,
        [string]$Value
    )
    $lines = @()
    if (Test-Path $Path) {
        $lines = @(Get-Content -Path $Path -Encoding UTF8)
    }
    $updated = $false
    $pattern = "^\s*" + [regex]::Escape($Key) + "\s*="
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match $pattern) {
            $lines[$i] = "$Key=$Value"
            $updated = $true
            break
        }
    }
    if (-not $updated) {
        $lines += "$Key=$Value"
    }
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllLines($Path, [string[]]$lines, $utf8NoBom)
}

function Find-Cloudflared {
    $cmd = Get-Command cloudflared -ErrorAction SilentlyContinue
    if ($cmd -and $cmd.Source -and (Test-Path $cmd.Source)) {
        return $cmd.Source
    }

    $candidates = @(
        (Join-Path $env:ProgramFiles "cloudflared\cloudflared.exe"),
        (Join-Path ${env:ProgramFiles(x86)} "cloudflared\cloudflared.exe"),
        (Join-Path $env:LOCALAPPDATA "Microsoft\WinGet\Links\cloudflared.exe")
    )
    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path $candidate)) {
            return $candidate
        }
    }

    $wingetPkgDir = Join-Path $env:LOCALAPPDATA "Microsoft\WinGet\Packages"
    if (Test-Path $wingetPkgDir) {
        $found = Get-ChildItem -Path $wingetPkgDir -Filter "cloudflared*.exe" -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($found) {
            return $found.FullName
        }
    }

    return $null
}

function Ensure-Cloudflared {
    $exe = Find-Cloudflared
    if ($exe) {
        return $exe
    }

    Write-Host "[1/4] 未检测到 cloudflared，尝试使用 winget 自动安装..." -ForegroundColor Yellow
    $winget = Get-Command winget -ErrorAction SilentlyContinue
    if (-not $winget) {
        throw "未找到 cloudflared 且当前系统缺少 winget。请手动下载安装 Cloudflare cloudflared：https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/"
    }

    & $winget.Source install --id Cloudflare.cloudflared --accept-package-agreements --accept-source-agreements
    if ($LASTEXITCODE -ne 0) {
        throw "winget 安装 cloudflared 失败（退出码 $LASTEXITCODE）。请手动安装后重试：https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/"
    }

    $exe = Find-Cloudflared
    if (-not $exe) {
        throw "cloudflared 安装完成但未找到可执行文件，请重新打开终端或将其加入 PATH 后重试。"
    }
    return $exe
}

function Get-ListeningPid {
    param([int]$TargetPort)
    $conn = Get-NetTCPConnection -LocalPort $TargetPort -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($conn) {
        return [int]$conn.OwningProcess
    }
    return $null
}

function Get-HttpStatusCode {
    param([string]$Url)
    try {
        $resp = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 3 -ErrorAction Stop
        return [int]$resp.StatusCode
    }
    catch {
        if ($_.Exception -and $_.Exception.Response) {
            try {
                return [int]$_.Exception.Response.StatusCode
            }
            catch {
                return $null
            }
        }
        return $null
    }
}

function Test-BackendReady {
    param(
        [int]$TargetPort,
        [string]$Scheme,
        [string]$ExpectedApiKey,
        [string]$ExpectedAdminToken
    )
    if ($Scheme -eq "https") {
        return $true
    }
    $pingCode = Get-HttpStatusCode "http://127.0.0.1:$TargetPort/ping"
    if ($pingCode -ne 200) {
        return $false
    }
    if (-not [string]::IsNullOrWhiteSpace($ExpectedApiKey)) {
        $modelsCode = Get-HttpStatusCode "http://127.0.0.1:$TargetPort/v1/models"
        if ($modelsCode -ne 401) {
            return $false
        }
    }
    if (-not [string]::IsNullOrWhiteSpace($ExpectedAdminToken)) {
        $adminCode = Get-HttpStatusCode "http://127.0.0.1:$TargetPort/admin/api/v1/overview"
        if ($adminCode -ne 401) {
            return $false
        }
    }
    return $true
}

function Stop-AiWebFreeOnPort {
    param([int]$TargetPort)
    $listenPid = Get-ListeningPid -TargetPort $TargetPort
    if (-not $listenPid) {
        return
    }
    $proc = Get-Process -Id $listenPid -ErrorAction SilentlyContinue
    if (-not $proc) {
        return
    }
    if ($proc.ProcessName -notlike "ai-web-free*") {
        throw "端口 $TargetPort 当前被其他进程占用（PID=$listenPid, 进程名=$($proc.ProcessName)）。请关闭该进程或通过 -Port 指定其他端口。"
    }
    Write-Host "    正在停止旧的 ai-web-free 进程（PID=$listenPid）以应用最新配置..." -ForegroundColor Yellow
    Stop-Process -Id $listenPid -Force
    for ($i = 0; $i -lt 25; $i++) {
        Start-Sleep -Milliseconds 200
        if (-not (Get-ListeningPid -TargetPort $TargetPort)) {
            return
        }
    }
    throw "停止旧进程 PID=$listenPid 超时，端口 $TargetPort 仍被占用。"
}

function Ensure-BackendBinary {
    param(
        [string]$RepoRoot,
        [string]$ExePath,
        [bool]$ForceRebuild
    )
    $needFrontend = (-not (Test-Path (Join-Path $RepoRoot "web\dist\index.html"))) -or (-not (Test-Path (Join-Path $RepoRoot "web\system-prompt.txt")))
    $needBuild = $ForceRebuild -or (-not (Test-Path $ExePath)) -or $needFrontend
    if (-not $needBuild) {
        return
    }

    if ($needFrontend) {
        Write-Host "    检测到缺少前端构建产物，正在构建 web/dist 与 system-prompt.txt..." -ForegroundColor Cyan
        $npmCmd = Get-Command npm -ErrorAction SilentlyContinue
        if (-not $npmCmd) {
            throw "缺少 web/dist 构建产物且未找到 npm，请先安装 Node.js 或执行 cd web && npm ci && npm run build。"
        }
        Push-Location (Join-Path $RepoRoot "web")
        try {
            if (-not (Test-Path "node_modules")) {
                & $npmCmd.Source ci
                if ($LASTEXITCODE -ne 0) { throw "npm ci 失败（退出码 $LASTEXITCODE）" }
            }
            & npx tsc --noEmit
            if ($LASTEXITCODE -ne 0) { throw "前端类型检查失败（退出码 $LASTEXITCODE）" }
            & $npmCmd.Source run build
            if ($LASTEXITCODE -ne 0) { throw "前端构建失败（退出码 $LASTEXITCODE）" }
        }
        finally {
            Pop-Location
        }
    }

    $goCmd = Get-Command go -ErrorAction SilentlyContinue
    if (-not $goCmd) {
        throw "未找到 Go 编译器，且根目录缺少 ai-web-free.exe。"
    }
    Write-Host "    正在编译静态后端二进制 ai-web-free.exe..." -ForegroundColor Cyan
    $prevCgo = $env:CGO_ENABLED
    $env:CGO_ENABLED = "0"
    try {
        & $goCmd.Source build -trimpath -ldflags="-s -w" -o $ExePath .
        if ($LASTEXITCODE -ne 0) {
            throw "go build 失败（退出码 $LASTEXITCODE）"
        }
    }
    finally {
        if ($null -eq $prevCgo) {
            Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
        }
        else {
            $env:CGO_ENABLED = $prevCgo
        }
    }
}

$RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
Push-Location $RepoRoot
$startedProcess = $null

try {
    Write-Host "============================================================" -ForegroundColor Cyan
    Write-Host "  ai-web-free · Cloudflare Tunnel 一键内网穿透脚本" -ForegroundColor Cyan
    Write-Host "============================================================" -ForegroundColor Cyan

    # 1. 初始化并加固 .env
    Write-Host "[1/4] 检查 .env 与鉴权配置..." -ForegroundColor Cyan
    $envPath = Join-Path $RepoRoot ".env"
    if (-not (Test-Path $envPath)) {
        $templatePath = Join-Path $RepoRoot "env.template"
        if (Test-Path $templatePath) {
            Copy-Item -Path $templatePath -Destination $envPath
            Write-Host "    已从 env.template 初始化 .env" -ForegroundColor Gray
        }
        else {
            New-Item -Path $envPath -ItemType File -Force | Out-Null
        }
    }

    $envMap = Read-EnvMap -Path $envPath
    $envUpdated = $false
    $apiKeyStatus = "已有 (.env)"
    $adminTokenStatus = "已有 (.env)"

    if (-not [string]::IsNullOrWhiteSpace($ApiKey)) {
        Set-EnvValue -Path $envPath -Key "Authorization" -Value $ApiKey.Trim()
        $envMap["Authorization"] = $ApiKey.Trim()
        $envUpdated = $true
        $apiKeyStatus = "已按参数更新"
    }
    elseif ([string]::IsNullOrWhiteSpace($envMap["Authorization"])) {
        if ($AllowEmptyAuth) {
            $apiKeyStatus = "未设置（已跳过，公网裸奔风险！）"
        }
        else {
            $generatedKey = New-RandomSecret -Prefix "sk-awf-" -ByteCount 18
            Set-EnvValue -Path $envPath -Key "Authorization" -Value $generatedKey
            $envMap["Authorization"] = $generatedKey
            $envUpdated = $true
            $apiKeyStatus = "本次自动生成并写入 .env"
        }
    }

    if (-not [string]::IsNullOrWhiteSpace($AdminToken)) {
        Set-EnvValue -Path $envPath -Key "ADMIN_TOKEN" -Value $AdminToken.Trim()
        $envMap["ADMIN_TOKEN"] = $AdminToken.Trim()
        $envUpdated = $true
        $adminTokenStatus = "已按参数更新"
    }
    elseif ([string]::IsNullOrWhiteSpace($envMap["ADMIN_TOKEN"])) {
        if ($AllowEmptyAuth) {
            $adminTokenStatus = "未设置（已跳过，管理后台可免登访问！）"
        }
        else {
            $generatedAdmin = New-RandomSecret -Prefix "adm-" -ByteCount 16
            Set-EnvValue -Path $envPath -Key "ADMIN_TOKEN" -Value $generatedAdmin
            $envMap["ADMIN_TOKEN"] = $generatedAdmin
            $envUpdated = $true
            $adminTokenStatus = "本次自动生成并写入 .env"
        }
    }

    if ($Port -le 0) {
        $portStr = $envMap["SERVER_PORT"]
        if ([string]::IsNullOrWhiteSpace($portStr)) {
            $portStr = $envMap["PORT"]
        }
        $parsedPort = 0
        if ([int]::TryParse($portStr, [ref]$parsedPort) -and $parsedPort -gt 0) {
            $Port = $parsedPort
        }
        else {
            $Port = 18080
        }
    }

    $originScheme = "http"
    $useLocalTls = (-not [string]::IsNullOrWhiteSpace($envMap["TLS_CERT"])) -and (-not [string]::IsNullOrWhiteSpace($envMap["TLS_KEY"]))
    if ($useLocalTls) {
        $originScheme = "https"
        Write-Host "    [提示] 检测到 .env 配置了 TLS_CERT/TLS_KEY，本地源站将按 HTTPS 对接；Cloudflare 隧道已自带公网 HTTPS，通常建议留空。" -ForegroundColor Yellow
    }

    # 2. 检查/安装 cloudflared
    Write-Host "[2/4] 检查 cloudflared 客户端..." -ForegroundColor Cyan
    $cloudflaredExe = Ensure-Cloudflared
    Write-Host "    已就绪: $cloudflaredExe" -ForegroundColor Green

    # 3. 检查并启动本地后端
    Write-Host "[3/4] 检查本地后端服务 (127.0.0.1:$Port)..." -ForegroundColor Cyan
    $exePath = Join-Path $RepoRoot "ai-web-free.exe"
    $existingPid = Get-ListeningPid -TargetPort $Port
    $needStartBackend = $true

    if ($existingPid) {
        $authMatch = $false
        if (-not $envUpdated -and -not $Rebuild) {
            $authMatch = Test-BackendReady -TargetPort $Port -Scheme $originScheme -ExpectedApiKey $envMap["Authorization"] -ExpectedAdminToken $envMap["ADMIN_TOKEN"]
        }

        if ($authMatch) {
            Write-Host "    检测到后端已在运行且鉴权配置生效（PID=$existingPid），直接复用。" -ForegroundColor Green
            $needStartBackend = $false
        }
        else {
            Stop-AiWebFreeOnPort -TargetPort $Port
        }
    }

    if ($needStartBackend) {
        Ensure-BackendBinary -RepoRoot $RepoRoot -ExePath $exePath -ForceRebuild:$Rebuild

        $outLog = Join-Path $RepoRoot "cf-tunnel-server.log"
        $errLog = Join-Path $RepoRoot "cf-tunnel-server.err.log"
        $prevServerPort = $env:SERVER_PORT
        $env:SERVER_PORT = "$Port"
        try {
            Write-Host "    正在启动 ai-web-free.exe (端口 $Port)..." -ForegroundColor Cyan
            $startedProcess = Start-Process -FilePath $exePath -WorkingDirectory $RepoRoot -WindowStyle Hidden -RedirectStandardOutput $outLog -RedirectStandardError $errLog -PassThru
        }
        finally {
            if ($null -eq $prevServerPort) {
                Remove-Item Env:SERVER_PORT -ErrorAction SilentlyContinue
            }
            else {
                $env:SERVER_PORT = $prevServerPort
            }
        }

        $ready = $false
        for ($i = 0; $i -lt 120; $i++) {
            Start-Sleep -Milliseconds 500
            if ($startedProcess.HasExited) {
                $errTail = if (Test-Path $errLog) { (Get-Content $errLog -Tail 20 -ErrorAction SilentlyContinue) -join "`n" } else { "" }
                $outTail = if (Test-Path $outLog) { (Get-Content $outLog -Tail 20 -ErrorAction SilentlyContinue) -join "`n" } else { "" }
                throw "ai-web-free.exe 启动后提前退出（退出码 $($startedProcess.ExitCode)）。`n$errTail`n$outTail"
            }
            if ($originScheme -eq "https") {
                if (Get-ListeningPid -TargetPort $Port) {
                    $ready = $true
                    break
                }
            }
            else {
                $code = Get-HttpStatusCode "http://127.0.0.1:$Port/ping"
                if ($code -eq 200) {
                    $ready = $true
                    break
                }
            }
        }
        if (-not $ready) {
            throw "等待本地服务监听 127.0.0.1:$Port 超时，请查看 $outLog / $errLog。"
        }
        if (-not (Test-BackendReady -TargetPort $Port -Scheme $originScheme -ExpectedApiKey $envMap["Authorization"] -ExpectedAdminToken $envMap["ADMIN_TOKEN"])) {
            throw "本地服务已启动，但 Authorization / ADMIN_TOKEN 鉴权校验未生效，请检查 .env 后重试。"
        }
        Write-Host "    后端服务启动成功（PID=$($startedProcess.Id)），日志写入 cf-tunnel-server.log" -ForegroundColor Green
    }

    # 4. 打印关键接入信息并启动 Cloudflare Tunnel
    $activeApiKey = $envMap["Authorization"]
    $activeAdminToken = $envMap["ADMIN_TOKEN"]
    $modeLabel = if ([string]::IsNullOrWhiteSpace($TunnelToken)) {
        "Quick Tunnel 临时公网地址（无需 Cloudflare API Key / 账号 / 域名）"
    }
    else {
        "Named Tunnel 固定域名模式（使用你提供的 Cloudflare Tunnel Token）"
    }

    Write-Host ""
    Write-Host "-------------------- 接入与安全信息 --------------------" -ForegroundColor Yellow
    Write-Host "  穿透模式          : $modeLabel"
    Write-Host "  本地源站          : ${originScheme}://127.0.0.1:$Port"
    Write-Host "  API Key (发给用户): $activeApiKey  [$apiKeyStatus]" -ForegroundColor Green
    Write-Host "  ADMIN_TOKEN (后台): $activeAdminToken  [$adminTokenStatus]" -ForegroundColor Green
    Write-Host "  客户端 Base URL   : <下方生成的 https://...>/v1"
    Write-Host "  退出提示          : 按 Ctrl+C 关闭隧道"
    Write-Host "--------------------------------------------------------" -ForegroundColor Yellow
    Write-Host ""

    Write-Host "[4/4] 正在启动 Cloudflare Tunnel，请等待下方输出公网 URL..." -ForegroundColor Cyan
    if ([string]::IsNullOrWhiteSpace($TunnelToken)) {
        $cfArgs = @("tunnel", "--url", "${originScheme}://127.0.0.1:$Port", "--protocol", $Protocol)
        if ($useLocalTls) {
            $cfArgs += "--no-tls-verify"
        }
    }
    else {
        $cfArgs = @("tunnel", "--protocol", $Protocol, "--no-autoupdate", "run", "--token", $TunnelToken.Trim())
    }

    & $cloudflaredExe @cfArgs
}
finally {
    if ($startedProcess -and -not $startedProcess.HasExited -and -not $KeepServer) {
        Write-Host "`n正在关闭本次脚本启动的 ai-web-free.exe（PID=$($startedProcess.Id)）..." -ForegroundColor Yellow
        Stop-Process -Id $startedProcess.Id -Force -ErrorAction SilentlyContinue
    }
    Pop-Location
}
