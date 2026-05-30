# Claude Usage Widget — 技术方案

> 一个常驻 Windows 任务栏（系统托盘）的小部件，实时显示 Claude Code 的用量与余量，
> 效果对标 Claude Code TUI 里的 `/usage`。本期只做 Windows，macOS 菜单栏版后续复用核心逻辑。

## 1. 核心结论：不需要驱动 `/usage` 命令

最初设想是「调用本地 CLI 的 `/usage`」，但实测发现：

- `/usage` 是 **TUI 专用** slash command，`echo "/usage" | claude -p` 只会把它当普通 prompt，
  返回的是聊天结果而不是用量数据 —— 走 CLI 抓屏不可行、且脆弱。
- 追踪 TUI 内部行为后发现，它其实只是调了一个带鉴权的 REST 接口。已在本机实测可用：

```http
GET https://api.anthropic.com/api/oauth/usage
Authorization: Bearer <accessToken>
anthropic-beta: oauth-2025-04-20
```

实测返回（即 `/usage` 展示的同一份数据）：

```json
{
  "five_hour":  { "utilization": 37.0, "resets_at": "2026-05-30T16:20:00Z" },
  "seven_day":  { "utilization":  4.0, "resets_at": "2026-06-04T05:59:59Z" },
  "seven_day_opus":   null,
  "seven_day_sonnet": null,
  "extra_usage": { "is_enabled": false, "monthly_limit": null, "used_credits": null }
}
```

字段含义：
- `five_hour` —— 5 小时滚动会话窗口用量（%）+ 重置时间。
- `seven_day` —— 7 天（全模型）用量。
- `seven_day_opus` / `seven_day_sonnet` —— 周度按模型细分（Pro 档为 null，更高档才有值）。
- `extra_usage` —— 是否开启了额外用量（按量付费）及其余额。

**结论：最简方案 = 读本地凭证里的 token → GET 这个接口 → 渲染。无需调用 CLI、无需抓屏。**

## 2. 鉴权与 token

- 凭证文件：`%USERPROFILE%\.claude\.credentials.json`，结构：
  ```json
  { "claudeAiOauth": { "accessToken": "...", "refreshToken": "...",
                        "expiresAt": 1748..., "scopes": [...],
                        "subscriptionType": "pro" } }
  ```
- token 有效期为数小时（本机实测剩余 ~280 分钟），且 **只要你在用 Claude Code，它会自动刷新**。
- 刷新策略（按复杂度递增，本期取「主路径 + 兜底文案」）：
  1. **主路径**：每次轮询都重新读凭证文件 → 直接用 `accessToken`。覆盖绝大多数场景。
  2. **过期/401 兜底**：若 `expiresAt` 已过或接口返回 401，提示「token 已过期，请打开一次
     Claude Code」即可（Claude Code 会刷新文件，下次轮询自动恢复）。
  3. **可选增强**（后续）：自行 `POST https://console.anthropic.com/v1/oauth/token`
     （`grant_type=refresh_token` + `refresh_token` + `client_id`，头带 `anthropic-beta: oauth-2025-04-20`）
     静默刷新并写回凭证文件。

## 3. 技术选型：Go + systray（已对齐）

- 库：`fyne.io/systray`（原 `getlantern/systray`）。Windows 下走纯 Win32 syscall，**无需 cgo / gcc**。
- 产物：单个 `.exe`，无运行时依赖；体积小（~几 MB）。
- 已确认环境：`go1.26.3 windows/amd64`。
- 点击行为：Windows 上点击托盘图标即弹出菜单，用量以菜单项形式展示（见下）。
  > systray 为「菜单式 UI」，本期用菜单项显示用量已满足「点一下看用量」。
  > 若后续要更精致的弹窗面板，可叠加一个小的 Win32 popup 窗口，核心逻辑不变。

## 4. 架构与目录

```
claude-usage-widget/
├── go.mod
├── main.go                 # systray.Run 生命周期、装配
├── internal/
│   ├── auth/auth.go        # 读 .credentials.json、取 token、过期判断
│   ├── usage/usage.go      # 调 /api/oauth/usage、解析为结构体
│   └── tray/tray.go        # 菜单渲染 + 定时刷新循环
├── assets/icon.go          # 内嵌托盘 .ico（go:embed）
├── DESIGN.md
└── README.md
```

数据流：

```
定时器(60s)/点击刷新 ──▶ auth.Token() ──▶ usage.Fetch(token)
        ▲                                        │
        └────────────── tray 更新菜单项 ◀────────┘
```

## 5. 托盘 UI（MVP）

托盘图标：显示 5 小时窗口用量百分比 + 颜色（绿 <50% / 黄 50–85% / 红 >85%）。
Tooltip 悬浮显示一行摘要。点击展开菜单：

```
Claude Usage
─────────────────────────────
5 小时:  37%   (1h38m 后重置)
7 天:     4%   (6/4 重置)
Opus 周: n/a
─────────────────────────────
立即刷新
开机自启      ✓
退出
```

- 用量行用「禁用态菜单项」展示（不可点）。
- 「立即刷新」手动触发一次 fetch。
- 「开机自启」写/删注册表 `HKCU\...\Run`（或启动文件夹快捷方式）。

## 6. 配置（最小）

`%APPDATA%\claude-usage-widget\config.json`：
```json
{ "pollIntervalSec": 60, "iconMetric": "five_hour" }
```

## 7. 错误与边界

| 情况 | 处理 |
|------|------|
| 凭证文件不存在 | 菜单提示「未登录 Claude Code」 |
| token 过期 / 401 | 提示「打开一次 Claude Code 以刷新」 |
| 网络失败 | 保留上次数据 + 标记「离线，HH:MM 更新」|
| 字段为 null（如 Opus 周）| 显示 `n/a` |

## 8. 里程碑

1. M1：CLI 验证 fetch（读 token → 打印用量）——核心已在调研中跑通。
2. M2：systray 托盘 + 菜单展示 + 定时刷新。
3. M3：图标百分比/颜色、tooltip、立即刷新、开机自启。
4. M4：打包 README、构建脚本（`go build -ldflags="-H windowsgui"` 去掉控制台窗口）。
5. （后续）macOS 菜单栏版：复用 auth/usage，UI 层换原生实现。

## 9. 未决/后续

- macOS 凭证在 Keychain（非文件），届时 auth 层加 Keychain 读取分支。
- 是否需要自动静默刷新 token（第 2 节增强项）。
- 是否需要更精致的弹窗面板（替代菜单）。
