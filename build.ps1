# 把托盘 widget 构建成一个 GUI exe（无控制台窗口）。
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    # -s -w 去掉符号表/DWARF 调试信息（exe 更小）；-trimpath 去掉本地路径；
    # -H windowsgui 隐藏控制台窗口。
    go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
    Write-Host "Built claude-usage-widget.exe" -ForegroundColor Green
} finally {
    Pop-Location
}
