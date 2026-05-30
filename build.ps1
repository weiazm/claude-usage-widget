# 把托盘 widget 构建成一个 GUI exe（无控制台窗口）。
#
# 执行前会先结束正在运行的同名进程（释放文件占用、让旧托盘实例退出）；构建后刷新 Windows
# 图标缓存，确保改过的图标能立即在资源管理器/任务栏显示。
#
# 用法：
#   .\build.ps1              # 结束旧进程 → 构建 → 轻量刷新图标缓存
#   .\build.ps1 -PurgeCache  # 额外彻底清空图标缓存并重启 explorer（改了图标却没刷新时用）
#   .\build.ps1 -Run         # 构建完成后顺带启动新 exe
[CmdletBinding()]
param(
    [switch]$PurgeCache,
    [switch]$Run
)

$ErrorActionPreference = 'Stop'
$exeName = 'claude-usage-widget'
Push-Location $PSScriptRoot
try {
    # 1) 结束正在运行的实例。否则 Windows 会锁定 exe，go 链接器只能写出 name.exe~ 备份，
    #    而且你看到的还是旧进程的托盘图标。
    $procs = Get-Process -Name $exeName -ErrorAction SilentlyContinue
    if ($procs) {
        Write-Host "结束运行中的 $exeName（$($procs.Count) 个进程）…" -ForegroundColor Yellow
        $procs | Stop-Process -Force
        Start-Sleep -Milliseconds 400  # 等句柄释放、托盘图标移除
    }

    # 2) 构建：-s -w 去符号表/DWARF（更小）；-trimpath 去本地路径；-H windowsgui 隐藏控制台。
    go build -trimpath -ldflags="-s -w -H windowsgui" -o "$exeName.exe" .
    if ($LASTEXITCODE -ne 0) { throw "go build 失败（exit $LASTEXITCODE）" }
    $info = Get-Item ".\$exeName.exe"
    Write-Host ("已构建 {0}.exe（{1:N0} 字节）" -f $exeName, $info.Length) -ForegroundColor Green

    # 3) 刷新图标缓存。Windows 按文件路径缓存 exe 图标，改了图标常常不自动更新。
    if ($PurgeCache) {
        # 彻底清空：必须先结束 explorer 才能删除被它占用的缓存库，删完再拉起来。
        Write-Host "彻底清空图标缓存并重启 explorer…" -ForegroundColor Yellow
        Stop-Process -Name explorer -Force -ErrorAction SilentlyContinue
        Start-Sleep -Milliseconds 600
        $cache = @(
            "$env:LocalAppData\IconCache.db",
            "$env:LocalAppData\Microsoft\Windows\Explorer\iconcache_*.db",
            "$env:LocalAppData\Microsoft\Windows\Explorer\thumbcache_*.db"
        )
        foreach ($c in $cache) {
            Get-ChildItem $c -Force -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue
        }
        Start-Process explorer.exe
        Write-Host "图标缓存已清空" -ForegroundColor Green
    }
    else {
        # 轻量刷新：不重启 explorer，触发外壳重建图标关联即可应付多数情况。
        & ie4uinit.exe -show 2>$null
        Write-Host "已刷新图标缓存（如仍是旧图标，用 .\build.ps1 -PurgeCache）" -ForegroundColor DarkGray
    }

    # 4) 可选：构建后直接启动新 exe。
    if ($Run) {
        Write-Host "启动 $exeName.exe…" -ForegroundColor Green
        Start-Process ".\$exeName.exe"
    }
}
finally {
    Pop-Location
}
