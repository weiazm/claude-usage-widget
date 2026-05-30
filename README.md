# Claude Usage Widget (Windows)

常驻 Windows 系统托盘的小部件，实时显示 Claude Code 的用量与余量，效果对标 TUI 里的 `/usage`。

## 工作原理

- `/usage` 是 Claude Code 的 **TUI 专用命令**，无法通过 `claude -p` 等方式拿到结构化输出，
  抓屏也不可靠。
- 但它背后只是调了一个带鉴权的 REST 接口。本 widget 直接复用该接口：

  ```http
  GET https://api.anthropic.com/api/oauth/usage
  Authorization: Bearer <accessToken>
  anthropic-beta: oauth-2025-04-20
  ```

- `accessToken` 直接读本机 Claude Code 登录后写的凭证文件
  `~/.claude/.credentials.json`（字段 `claudeAiOauth.accessToken`）。**无需调用 CLI、无需抓屏、不入库**。

返回示例（即 `/usage` 展示的同一份数据）：

```json
{
  "five_hour":  { "utilization": 37.0, "resets_at": "2026-05-30T16:20:00Z" },
  "seven_day":  { "utilization":  4.0, "resets_at": "2026-06-04T05:59:59Z" },
  "seven_day_opus":   null,
  "seven_day_sonnet": null,
  "extra_usage": { "is_enabled": false, "monthly_limit": null }
}
```

字段含义：`five_hour` = 5 小时滚动会话窗口；`seven_day` = 7 天全模型；
`seven_day_opus/sonnet` = 周度按模型细分（Pro 档为 null，更高档才有值）；
`extra_usage` = 是否开启按量付费及余额。

## 功能

- 托盘图标是**动态仪表盘**：弧长按 5 小时窗口用量实时绘制（37% 就填 37% 的弧，而非三档近似），
  并按用量变色：绿 `<50%` / 黄 `50–85%` / 红 `≥85%`。白色填充弧 + 深色凹槽轨道，对比强、托盘里也清晰。
  图标在运行时用纯 Go（标准库 `image`）绘制，按整数百分比缓存，无需任何外部图像库或预制资源。
- 鼠标点击托盘图标展开菜单，显示：5 小时、7 天、Opus 周、Sonnet 周 用量与重置倒计时。
- 每 60 秒自动刷新，或菜单「立即刷新」手动刷新。
- 悬浮 tooltip 显示一行摘要。
- **单实例**：每个用户会话只允许运行一个。重复启动时，第二个进程不会再创建托盘图标，
  而是弹出一个对话框显示当前用量详情并提醒「已在后台运行」，随后自行退出。

## 项目结构

```
claude-usage-widget/
├── main.go                              # 程序入口：单实例判断 + 运行时调优 + 启动托盘
├── internal/                            # 内部库（仅本项目可 import）
│   ├── auth/auth.go                     # 读 .credentials.json，取 token、判断过期
│   ├── usage/
│   │   ├── usage.go                     # 调 /api/oauth/usage、解析 JSON 为结构体
│   │   └── format.go                    # 把用量格式化成可读文本（百分比/倒计时）
│   ├── tray/tray.go                     # 托盘图标 + 菜单 + 60s 轮询循环（goroutine/channel）
│   ├── dialog/dialog_windows.go         # 调 Win32 MessageBoxW 弹窗
│   └── singleinstance/singleinstance_windows.go  # Win32 命名互斥量实现单实例
├── assets/
│   ├── gauge.go                         # 运行时用纯 Go 绘制动态仪表托盘图标（弧长=百分比）
│   ├── app.ico                          # exe 应用图标源
│   └── make_appicon.ps1                 # exe 图标生成脚本
├── cmd/usagecheck/main.go               # 命令行自检：直接打印用量（不弹托盘）
├── rsrc_windows_amd64.syso              # 由 app.ico 生成的资源文件，go build 自动链接出 exe 图标
└── build.ps1                            # 构建脚本
```

> 代码里每个文件都加了面向 Go 初学者的注释，解释指针、goroutine、channel、defer、
> 错误处理、struct tag、go:embed 等 Go 特有写法。

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

exe 的应用图标（资源管理器/任务栏/Alt-Tab 显示）由仓库根目录的 `rsrc_windows_amd64.syso`
提供，`go build` 会自动链接，无需额外步骤。

如需修改图标：编辑/运行 `assets/make_appicon.ps1` 重新生成 `assets/app.ico`，再用
[rsrc](https://github.com/akavel/rsrc) 重新生成资源文件：

```powershell
go install github.com/akavel/rsrc@latest
& "$(go env GOPATH)\bin\rsrc.exe" -ico assets\app.ico -arch amd64 -o rsrc_windows_amd64.syso
go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
```

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

## 低开销说明

针对「常驻、几乎全程空闲」的场景做了调优：

- `GOMAXPROCS(1)` —— 托盘小部件无需并行，减少 OS 线程与调度开销。
- `SetGCPercent(20)` + `SetMemoryLimit(32MiB)` —— 堆保持很小、回收更积极。
- 每次轮询后 `FreeOSMemory()` —— 把一次性的拉取/解析内存归还系统。
- HTTP 客户端 `DisableKeepAlives` —— 每分钟一次请求，不保留空闲连接/协程。
- 构建 `-s -w -trimpath` —— exe 从 9.5MB 降到 6.6MB。

实测稳态：**工作集 ~20MB，线程 ~9，跨轮询无增长（20.5→20.7MB）**。

## 已知边界

- token 数小时过期，但只要你用过 Claude Code 它会自动刷新；过期时托盘提示「打开一次 Claude Code」。
- `Opus 周 / Sonnet 周` 在 Pro 档为 `n/a`（接口返回 null），更高档才有值。
- 当前仅 Windows；图标资源 `.syso` 为 amd64 专用。

## 后续优化方向

按优先级大致排列：

1. **Token 自动静默刷新** —— 现在 token 过期靠「打开一次 Claude Code」兜底。可改为用凭证里的
   `refreshToken` 调 `POST /v1/oauth/token`（`grant_type=refresh_token`）静默续期并写回文件，
   让 widget 长时间不开 Claude Code 也持续可用。
2. **配置文件** —— `%APPDATA%\claude-usage-widget\config.json` 支持自定义轮询间隔、
   用哪个窗口驱动图标颜色等，避免改代码重编。
3. **应用内「开机自启」开关** —— 菜单加勾选项，自动写/删启动文件夹快捷方式（替代手动脚本）。
4. **阈值桌面通知** —— 用量超过阈值（如 90%）时弹一条 Windows toast 提醒。
5. **更精致的点击面板** —— 用独立 Win32 popup 窗口替代菜单项，展示进度条、额外用量额度等。
6. **macOS 菜单栏版** —— 复用 `internal/auth`、`internal/usage` 核心逻辑，仅替换 UI 层；
   注意 macOS 的 token 存在 Keychain，需要新增读取分支（`*_darwin.go`）。
7. **单元测试 + CI** —— 为 `format.go` 等纯函数补测试，并用 GitHub Actions 自动构建/发布
   多架构（amd64/arm64）release。

> ✅ 已完成：**动态仪表图标**（弧长按真实百分比绘制，见 [`assets/gauge.go`](assets/gauge.go)）。
