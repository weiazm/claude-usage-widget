# AGENTS.md

面向 AI 编码代理（agentic coding）的工程指南。人类阅读请优先看 [README.md](README.md)。

## 这是什么

常驻 Windows 系统托盘的小部件，实时显示 Claude Code 用量，效果对标 TUI 的 `/usage`。
直接读本机 `~/.claude/.credentials.json` 里的 OAuth token，调
`GET https://api.anthropic.com/api/oauth/usage` 拿数据，在托盘画一个动态仪表盘图标。

- **纯 Go，无 cgo**：Windows 下 systray 走纯 Win32 syscall，不需要 gcc。Go 1.21+。
- **当前仅 Windows**；macOS 菜单栏版是后续方向（见 README）。

## ⚠️ 环境关键约定（务必遵守）

1. **构建/验证 exe 一律用 PowerShell，不要用 Bash 工具。**
   Bash 工具是沙箱化的，`go build` 写出的 exe **不保证落到真实磁盘**（曾出现 bash 里
   `ls` 看得到、PowerShell 与资源管理器里却没有的情况）。统一用 `.\build.ps1`。
   - 只读分析（grep/读文件/`go vet`/`go build ./...` 编译检查）用任何工具都行；
   - 一旦涉及“产出可运行的 exe 并验证”，必须走 PowerShell。

2. **改了代码要看效果，必须重启 widget。** `SetIcon` 等只在新进程生效。`.\build.ps1`
   会先结束正在运行的实例再构建，用它即可。

3. **图标改了在资源管理器/任务栏没变 ≈ Windows 图标缓存。** 不是 bug。
   用 `.\build.ps1 -PurgeCache` 彻底清缓存（会重启 explorer）。验证 exe 真实图标用
   `[System.Drawing.Icon]::ExtractAssociatedIcon($path)`（直读 PE 资源，绕过缓存）。

## 常用命令（PowerShell）

```powershell
.\build.ps1                 # 结束旧进程 → 构建 GUI exe → 轻量刷新图标缓存
.\build.ps1 -PurgeCache     # 额外彻底清空图标缓存并重启 explorer（图标没刷新时）
.\build.ps1 -Run            # 构建后顺带启动新 exe
go run .\cmd\usagecheck     # 命令行自检：不弹托盘，直接打印当前用量
go vet ./...                # 静态检查
go build ./...              # 全包编译检查（不产出 exe，纯校验用 Bash 也行）
```

修改图标后重新嵌入 exe 应用图标（托盘图标是运行时画的，无需此步）：

```powershell
go run .\cmd\makeappicon                                      # 重新生成 assets\app.ico
rsrc -ico assets\app.ico -arch amd64 -o rsrc_windows_amd64.syso  # 嵌入资源（go build 自动链接）
.\build.ps1
```

## 结构与职责

```
main.go                      入口：单实例判断 → 运行时调优 → tray.Run()
dpi_windows.go               init() 里把进程标记为“每显示器 DPI 感知 V2”（早于任何窗口创建），杜绝位图拉伸糊化
internal/
  auth/auth.go               读 ~/.claude/.credentials.json，取 token、判过期（只读，从不写）
  usage/usage.go             调 /api/oauth/usage、解析 JSON（Window 用 *指针 表达 null）
  usage/format.go            把用量格式化为可读文本（纯函数，最适合补单测）
  config/config.go           读 ~/.claude/claude-usage-widget.json（轮询间隔/图标取色窗口）
  panel/panel_windows.go     左键弹出的独立 Win32+GDI 用量面板（进度条/额外用量/「立即刷新·退出」按钮；惰性创建、自带 UI 线程、按 DPI 缩放）
  tray/tray.go               托盘图标+轮询循环；左键→面板、右键→仅「退出」兜底；onExit=os.Exit(0)
  dialog/dialog_windows.go   Win32 MessageBoxW 弹窗
  singleinstance/*_windows.go  Win32 命名互斥量实现单实例
assets/gauge.go              纯 Go 绘制动态仪表图标（托盘 + exe 图标 + 面板进度条配色的唯一来源）
cmd/usagecheck/              命令行自检
cmd/makeappicon/             生成 exe 应用图标 app.ico（复用 assets.RenderICO）
rsrc_windows_amd64.syso      由 app.ico 生成的资源，go build 自动链接出 exe 图标（已提交）
build.ps1                    构建脚本（见上）
```

### 点击面板线程模型（panel 包，易踩坑）

Win32 窗口的消息只会被「创建它的那个线程」的消息循环派发。systray 有自己的线程/循环，所以
`panel` 自起一条 `runtime.LockOSThread()` 的 OS 线程，在上面建窗口并跑 `GetMessage` 循环。
外部（systray 左键回调、轮询 goroutine）**只能用 `PostMessage` 跨线程投递**自定义消息
（`wmToggle`/`wmRefresh`），绝不要从别的线程直接调窗口的 GDI/显隐函数。面板配色用
`assets.BarColor(pct)`，与图标同源。面板底部「立即刷新/退出」两个按钮在 `WM_LBUTTONUP` 里按
`buttonRects()` 做命中测试，回调 `onRefresh/onQuit`（由 tray 经 `panel.SetActions` 注入，避免
panel↔tray 循环 import）。

DPI / 清晰度（务必保持）：进程在 `dpi_windows.go` 的 `init()` 里就被设为“每显示器 DPI 感知”，
所以面板**必须自己按 DPI 缩放**——所有坐标/字号都过 `sc()`（= 逻辑像素 × `scale`），字体随
DPI 在 `applyDPI`→`rebuildFonts` 重建。布局常量一律写“逻辑像素（96 DPI 基准）”，绘制时再 `sc()`。
别写死物理像素，否则高缩放下会糊或错位。

低开销要求（务必保持）：面板**惰性创建**——首次左键点击才经 `startOnce` 起线程/建窗口/造
字体，用户不点就零开销。`Update`/`SetError` 在面板**隐藏时只更新内存、不投递重绘消息**（靠
`visible` 原子标记判断），避免每分钟无谓唤醒 UI 线程。`GetMessage` 空闲时阻塞、不占 CPU。改面板
别破坏这几点。

## 代码约定

- **注释一律中文**，并保留面向 Go 初学者的「Go 提示：…」讲解（指针、goroutine、channel、
  defer、struct tag、错误处理等）。新增代码沿用同一风格。
- **不引入新依赖**，尤其图像库——图标用标准库 `image` 手绘。当前依赖仅 `fyne.io/systray`
  和 `golang.org/x/sys`。
- **低开销优先**：`GOMAXPROCS(1)`、`SetGCPercent(20)`、`SetMemoryLimit(32MiB)`、轮询后
  `FreeOSMemory()`、HTTP `DisableKeepAlives`。改动别破坏这些。
- **平台相关代码用文件名后缀约束**：Windows 专属放 `*_windows.go`，将来 macOS 放
  `*_darwin.go`。`internal/` 下的包仅本模块可 import。
- **错误用值返回 + 哨兵错误**（`auth.ErrNoCredentials` 等），调用方用 `errors.Is` 分支。

## 图标渲染要点（最容易踩坑）

- 托盘和 exe 图标共用 [assets/gauge.go](assets/gauge.go) 的 `RenderICO(pct, sizes)`——
  改绘制只改这一处，两者风格自动一致。
- **托盘帧尺寸必须是“大图标”尺寸 `{32,40,48,64}`**：fyne/systray 用
  `LoadImage(LR_DEFAULTSIZE, cx=cy=0)` 按 `SM_CXICON`（100% DPI=32px，随 DPI 放大）取帧，
  再由外壳缩到约 16px 托盘槽位。**16/20/24 这类小帧根本不会被取用**，别加。
- 几何参数（`insetFrac`/`strokeFrac`/`padFrac`/`ss`）在 gauge.go 顶部的 const 块，调它们
  控制仪表大小、粗细、抗锯齿。底色随用量在 绿→黄→橙→红 之间连续插值，锚点见
  `gaugeStops`、插值函数 `gradientColors`（改色阶改这里）。

## 数据与隐私

- token 从 `~/.claude/.credentials.json` **只读**取出，运行时使用，**不写文件、不落库、
  不抓屏、不调 CLI**。修改 `auth` 时保持只读。
- token 数小时过期，靠用户用一次 Claude Code 自动刷新；过期时托盘提示，不崩溃。
- 仓库**绝不能**提交任何凭证/token。

## 验证清单（提交前）

1. `go vet ./...` 通过。
2. 涉及 exe/图标：`.\build.ps1` 成功，并用 `ExtractAssociatedIcon` 确认嵌入图标符合预期。
3. 涉及托盘行为：实际重启 widget 手动验证（点击、刷新、退出干净、单实例弹窗）。
4. 未引入新依赖、未破坏低开销设定、注释为中文。
