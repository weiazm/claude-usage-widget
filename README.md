# Claude Usage Widget (Windows)

常驻 Windows 系统托盘的小部件，**一眼看到 Claude Code 还剩多少额度**——效果对标 TUI 里的
`/usage`，但不用打开终端、不用敲命令，开机挂着就行。

## 效果

托盘区会出现一个**动态仪表盘**图标，**弧长**就是你的 5 小时窗口用量（用多少、弧就填多长），
**底色**随用量在 绿 → 黄绿 → 黄 → 橙 → 红 之间**连续渐变**——越满越红，一眼就知道松紧：

```
0%        25%        50%        75%       100%
🟢 ───── 🟡绿 ───── 🟡 ───── 🟠 ───── 🔴
余量充足              用一半              接近限额
```

- 颜色是**按百分比平滑插值**的，不是绿/黄/红几档硬跳变；弧长也按真实百分比实时绘制
  （37% 就填 37%），不是粗糙的近似。
- **白色填充弧 + 深色凹槽轨道**，对比强，缩到 16px 的托盘里也看得清。
- exe 应用图标和托盘图标**同一套风格**（同一份绘制代码生成）。

**点一下托盘图标**，菜单里展开完整明细：

```
Claude Usage
────────────────
5 小时:  37%  (1h38m 后重置)
7 天:    4%   (3d6h 后重置)
Opus 周: n/a
Sonnet周:12%  (2d1h 后重置)
────────────────
更新于 16:20
立即刷新
退出
```

- **每 60 秒自动刷新**，也可点「立即刷新」手动拉取。
- 鼠标悬浮图标，tooltip 显示一行摘要（`5小时 37% / 7天 4%`）。
- **只跑一个**：重复双击不会开出第二个图标，而是弹窗显示当前用量并提醒「已在后台运行」。

## 用法

### 1. 前置条件

本机已安装并登录过 Claude Code（即存在 `~/.claude/.credentials.json`）。widget 直接读这个
登录凭证，所以**你平时怎么用 Claude Code，它就能显示什么**，无需额外登录或配置。

### 2. 获取 exe

仓库根目录已有构建好的 `claude-usage-widget.exe`，直接用即可。或自行构建（需 Go 1.21+）：

```powershell
go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
# 或直接运行 build.ps1
```

> Windows 下托盘走纯 Win32 syscall，**无需 gcc/cgo**。

### 3. 运行

双击 `claude-usage-widget.exe`，托盘区立刻出现仪表图标。要退出就右键（或左键）图标 → 「退出」。

> 改了代码重新构建后，记得**先退出旧图标再启动新 exe**——否则托盘里还是旧进程，看不到变化。

### 4. 开机自启（可选）

把快捷方式丢进启动文件夹即可：

```powershell
$exe = (Resolve-Path .\claude-usage-widget.exe).Path
$startup = [Environment]::GetFolderPath('Startup')
$ws = New-Object -ComObject WScript.Shell
$lnk = $ws.CreateShortcut("$startup\Claude Usage Widget.lnk")
$lnk.TargetPath = $exe
$lnk.Save()
```

### 5. 命令行自检（可选）

想不弹托盘、直接在终端看一眼用量：

```powershell
go run ./cmd/usagecheck
```

## 已知边界

- token 数小时过期，但只要你平时在用 Claude Code，它会自动刷新；万一过期，托盘会提示
  「打开一次 Claude Code」。
- `Opus 周 / Sonnet 周` 在 Pro 档显示 `n/a`（接口对该档返回 null），更高订阅档才有值。
- 当前仅 Windows；图标资源 `.syso` 为 amd64 专用。

## 后续优化方向

按优先级大致排列：

1. **Token 自动静默刷新** —— 现在 token 过期靠「打开一次 Claude Code」兜底，可改为用
   `refreshToken` 静默续期，让 widget 长时间不开 Claude Code 也持续可用。
2. **配置文件** —— 自定义轮询间隔、用哪个窗口驱动图标颜色等，避免改代码重编。
3. **应用内「开机自启」开关** —— 菜单加勾选项，自动写/删启动快捷方式（替代手动脚本）。
4. **阈值桌面通知** —— 用量超过阈值（如 90%）时弹一条 Windows toast。
5. **更精致的点击面板** —— 用独立 Win32 popup 窗口替代菜单项，展示进度条、额外用量额度等。
6. **macOS 菜单栏版** —— 复用 `internal/auth`、`internal/usage` 核心逻辑，仅换 UI 层。
7. **单元测试 + CI** —— 补纯函数测试，GitHub Actions 自动构建/发布多架构 release。

> ✅ 已完成：**动态仪表图标**（弧长按真实百分比绘制）、exe 与托盘图标风格统一、低开销调优。

---

<details>
<summary><b>实现原理（点开了解，使用上不必关心）</b></summary>

### 数据从哪来

`/usage` 是 Claude Code 的 TUI 专用命令，拿不到结构化输出。但它背后只是调了一个带鉴权的
REST 接口，本 widget 直接复用：

```http
GET https://api.anthropic.com/api/oauth/usage
Authorization: Bearer <accessToken>
anthropic-beta: oauth-2025-04-20
```

`accessToken` 读自本机 `~/.claude/.credentials.json`（字段 `claudeAiOauth.accessToken`）。
**只读、不写、不入库、不抓屏、不调 CLI。** 返回的就是 `/usage` 展示的同一份数据：

```json
{
  "five_hour":  { "utilization": 37.0, "resets_at": "2026-05-30T16:20:00Z" },
  "seven_day":  { "utilization":  4.0, "resets_at": "2026-06-04T05:59:59Z" },
  "seven_day_opus":   null,
  "seven_day_sonnet": null,
  "extra_usage": { "is_enabled": false, "monthly_limit": null }
}
```

### 图标怎么画

托盘图标和 exe 应用图标都由 [`assets/gauge.go`](assets/gauge.go) 用纯 Go（标准库 `image`）在
运行时绘制——同一份代码、同一套风格。按整数百分比缓存，无外部图像库、无预制资源。
exe 图标由 `cmd/makeappicon` 生成 `assets/app.ico`，再用
[rsrc](https://github.com/akavel/rsrc) 嵌入：

```powershell
go run ./cmd/makeappicon
rsrc -ico assets\app.ico -arch amd64 -o rsrc_windows_amd64.syso
go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
```

### 项目结构

```
claude-usage-widget/
├── main.go                              # 入口：单实例判断 + 运行时调优 + 启动托盘
├── internal/
│   ├── auth/auth.go                     # 读 .credentials.json，取 token、判断过期
│   ├── usage/usage.go,format.go         # 调接口、解析 JSON、格式化为可读文本
│   ├── tray/tray.go                     # 托盘图标 + 菜单 + 60s 轮询循环
│   ├── dialog/dialog_windows.go         # Win32 MessageBoxW 弹窗
│   └── singleinstance/…_windows.go      # Win32 命名互斥量实现单实例
├── assets/gauge.go                      # 纯 Go 绘制动态仪表图标（托盘 + exe 共用）
├── cmd/usagecheck/                      # 命令行自检
├── cmd/makeappicon/                     # 生成 exe 应用图标 app.ico
├── rsrc_windows_amd64.syso              # 由 app.ico 生成，go build 自动链接出 exe 图标
└── build.ps1
```

> 代码里每个文件都加了面向 Go 初学者的中文注释，解释指针、goroutine、channel、defer、
> 错误处理、struct tag 等 Go 特有写法。

### 低开销

针对「常驻、几乎全程空闲」做了调优：`GOMAXPROCS(1)`、`SetGCPercent(20)` +
`SetMemoryLimit(32MiB)`、每次轮询后 `FreeOSMemory()`、HTTP `DisableKeepAlives`、构建
`-s -w -trimpath`。实测稳态 **工作集 ~20MB、跨轮询无增长**。

</details>
