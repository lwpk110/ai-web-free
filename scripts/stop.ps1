param(
    [int]$Port = 18080,
    [int]$WebPort = 5173
)

$targetPorts = @($Port, $WebPort, 5174)
foreach ($p in $targetPorts) {
    try {
        $conns = Get-NetTCPConnection -LocalPort $p -ErrorAction SilentlyContinue
        if ($conns) {
            $pids = $conns | Select-Object -ExpandProperty OwningProcess -Unique
            foreach ($processId in $pids) {
                if ($processId -gt 4) {
                    Stop-Process -Id $processId -Force -ErrorAction SilentlyContinue
                }
            }
        }
    } catch {}
}

# 兜底清理进程名
taskkill /F /IM ai-web-free.exe 2>$null
taskkill /F /IM ai-web-free 2>$null

Write-Host "已停止本地项目进程"
