# Claude Usage Widget (Windows)

常驻 Windows 系统托盘的小部件，实时显示 Claude Code 的用量与余量，效果对标 TUI 里的 `/usage`。

数据来自 Claude Code 登录后写在本地的 OAuth token（`~/.claude/.credentials.json`），
直接调用 `/usage` 背后的接口 `GET https://api.anthropic.com/api/oauth/usage`，无需调用 CLI、无需抓屏。
详见 [DESIGN.md](DESIGN.md)。

## 功能

- 托盘图标按 5 小时窗口用量变色：绿 `<50%` / 黄 `50–85%` / 红 `≥85%`。
- 鼠标点击托盘图标展开菜单，显示：5 小时、7 天、Opus 周、Sonnet 周 用量与重置倒计时。
- 每 60 秒自动刷新，或菜单「立即刷新」手动刷新。
- 悬浮 tooltip 显示一行摘要。
- **单实例**：每个用户会话只允许运行一个。重复启动时，第二个进程不会再创建托盘图标，
  而是弹出一个对话框显示当前用量详情并提醒「已在后台运行」，随后自行退出。

## 前置条件

- 已安装并登录过 Claude Code（本机存在 `~/.claude/.credentials.json`）。
- Go 1.21+（仅构建时需要）。Windows 下 systray 走纯 Win32 syscall，**无需 gcc/cgo**。

## 构建

```powershell
# 生成无控制台窗口、去符号的精简 GUI exe
go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
```

或运行 `build.ps1`。

### exe 图标

exe 的应用图标(资源管理器/任务栏/Alt-Tab 显示)由仓库根目录的 `rsrc_windows_amd64.syso`
提供,`go build` 会自动链接,无需额外步骤。

如需修改图标:编辑/运行 `assets/make_appicon.ps1` 重新生成 `assets/app.ico`,再用
[rsrc](https://github.com/akavel/rsrc) 重新生成资源文件:

```powershell
go install github.com/akavel/rsrc@latest
& "$(go env GOPATH)\bin\rsrc.exe" -ico assets\app.ico -arch amd64 -o rsrc_windows_amd64.syso
go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
```

## 低开销说明

针对「常驻、几乎全程空闲」的场景做了调优：

- `GOMAXPROCS(1)` —— 托盘小部件无需并行，减少 OS 线程与调度开销。
- `SetGCPercent(20)` + `SetMemoryLimit(32MiB)` —— 堆保持很小、回收更积极。
- 每次轮询后 `FreeOSMemory()` —— 把一次性的拉取/解析内存归还系统。
- HTTP 客户端 `DisableKeepAlives` —— 每分钟一次请求，不保留空闲连接/协程。
- 构建 `-s -w -trimpath` —— exe 从 9.5MB 降到 6.6MB。

实测稳态：**工作集 ~20MB，线程 ~9，跨轮询无增长（20.5→20.7MB）**。

## 运行

双击 `claude-usage-widget.exe`，托盘区即出现图标。

### 开机自启（可选）

把快捷方式放进启动文件夹即可：

```powershell
$exe = (Resolve-Path .\claude-usage-widget.exe).Path
$startup = [Environment]::GetFolderPath('Startup')
$ws = New-Object -ComObject WScript.Shell
$lnk = $ws.CreateShortcut("$startup\Claude Usage Widget.lnk")
$lnk.TargetPath = $exe
$lnk.Save()
```

## 快速自检（不弹托盘，直接打印用量）

```powershell
go run ./cmd/usagecheck
```

## 已知边界

- token 数小时过期，但只要你用过 Claude Code 它会自动刷新；过期时托盘提示「打开一次 Claude Code」。
- `Opus 周 / Sonnet 周` 在 Pro 档为 `n/a`（接口返回 null），更高档才有值。
- macOS 菜单栏版后续：复用 `internal/auth`、`internal/usage`，仅替换托盘 UI 层（macOS token 在 Keychain）。
