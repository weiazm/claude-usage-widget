// Package tray 渲染系统托盘 widget：一个按 5 小时用量着色的图标，外加一个显示用量明细
// 的菜单，并按定时器和用户手动操作刷新。
package tray

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"fyne.io/systray"

	"claude-usage-widget/assets"
	"claude-usage-widget/internal/auth"
	"claude-usage-widget/internal/usage"
)

const pollInterval = 60 * time.Second

// menu 持有每个托盘菜单项的指针，以便 update() 修改它们的文本。
//
// Go 提示：*systray.MenuItem 是指针；systray 库返回指针，这样调用 it.SetTitle(...)
// 就能更新屏幕上真正的那一项。
type menu struct {
	header  *systray.MenuItem
	fiveHr  *systray.MenuItem
	sevenD  *systray.MenuItem
	opus    *systray.MenuItem
	sonnet  *systray.MenuItem
	status  *systray.MenuItem
	refresh *systray.MenuItem
	quit    *systray.MenuItem
}

// Run 启动 systray 事件循环。它会一直阻塞，直到用户退出。
//
// Go 提示：systray.Run 接收两个函数作为参数（函数在 Go 里是值）。onReady 在托盘就绪后
// 运行一次；第二个是 onExit 回调——这里用一个空的内联函数 "func() {}"，因为我们没有需要
// 清理的东西。
func Run() {
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetIcon(assets.Gauge(0))
	systray.SetTitle("")
	systray.SetTooltip("Claude Usage — 加载中…")

	// Go 提示：&menu{...} 构造一个 menu 结构体并取它的地址。这里内联设置 header 字段，
	// 其余字段在下面赋值。
	m := &menu{
		header: systray.AddMenuItem("Claude Usage", ""),
	}
	m.header.Disable()
	systray.AddSeparator()
	m.fiveHr = addInfoItem("5 小时:  …")
	m.sevenD = addInfoItem("7 天:    …")
	m.opus = addInfoItem("Opus 周: …")
	m.sonnet = addInfoItem("Sonnet周:…")
	systray.AddSeparator()
	m.status = addInfoItem("")
	m.refresh = systray.AddMenuItem("立即刷新", "重新拉取用量")
	m.quit = systray.AddMenuItem("退出", "退出小部件")

	// Go 提示：channel（通道）是一根带类型的管道，用于在 goroutine 之间传值。
	// chan struct{} 不携带数据——它是纯信号。后面的 "1" 给它一个长度为 1 的缓冲，这样
	// 即使暂时没人接收，发送也不会阻塞。
	refreshNow := make(chan struct{}, 1)

	// Go 提示："go func() { ... }()" 启动一个 goroutine——一种轻量级线程。
	// 这个 goroutine 永远监听菜单点击。
	go func() {
		for {
			// select 同时等待多个 channel，哪个就绪就执行哪个分支。
			// 每个菜单项都暴露一个 ClickedCh 通道，点击时会触发。
			select {
			case <-m.refresh.ClickedCh:
				// 请求轮询 goroutine 刷新。内层的非阻塞 select 在已有一个信号排队时
				//（缓冲已满）丢弃本次信号，这样连续快速点击会被合并，而不是堆积。
				select {
				case refreshNow <- struct{}{}:
				default:
				}
			case <-m.quit.ClickedCh:
				systray.Quit()
				return // 结束这个 goroutine
			}
		}
	}()

	// 第二个 goroutine：负责刷新数据的轮询循环。
	go func() {
		// Ticker 每隔 pollInterval 就在它的通道（ticker.C）上触发一次。
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		update(m) // 启动时立即拉取一次
		for {
			select {
			case <-ticker.C: // 周期性刷新
				update(m)
			case <-refreshNow: // 手动“立即刷新”
				update(m)
			}
		}
	}()
}

// addInfoItem 添加一个不可点击（禁用）的行，纯粹用来显示文本。
func addInfoItem(text string) *systray.MenuItem {
	it := systray.AddMenuItem(text, "")
	it.Disable()
	return it
}

// update 拉取当前用量，并把它推送到菜单、图标和 tooltip。
func update(m *menu) {
	tok, err := auth.Read()
	if err != nil {
		showError(m, err)
		return // 出错就提前返回——这是常见的 Go 写法
	}
	ctx := context.Background()
	u, err := usage.Fetch(ctx, tok.AccessToken)
	if err != nil {
		showError(m, err)
		return
	}

	m.fiveHr.SetTitle(usage.Line("5 小时: ", u.FiveHour))
	m.sevenD.SetTitle(usage.Line("7 天:   ", u.SevenDay))
	m.opus.SetTitle(usage.Line("Opus 周:", u.SevenDayOpus))
	m.sonnet.SetTitle(usage.Line("Sonnet周:", u.SevenDaySonnet))
	m.status.SetTitle("更新于 " + u.FetchedAt.Format("15:04"))

	// Go 提示：var 声明 pct 并赋零值（0.0）。只有当 FiveHour 存在时我们才覆盖它，
	// 所以窗口缺失时会安全地保持空仪表（绿色 0%）。
	var pct float64
	if u.FiveHour != nil {
		pct = u.FiveHour.Utilization
	}
	systray.SetIcon(assets.Gauge(pct))
	systray.SetTooltip(fmt.Sprintf("Claude — 5小时 %s / 7天 %s",
		usage.FormatPercent(u.FiveHour), usage.FormatPercent(u.SevenDay)))

	// 把这次拉取/解析产生的临时内存归还操作系统，让常驻内存在每分钟一次的轮询之间
	// 重新沉降下来。
	debug.FreeOSMemory()
}

// showError 把错误映射成友好的中文状态行和 tooltip。
func showError(m *menu, err error) {
	var msg string
	// Go 提示：不带表达式的 switch 相当于 if/else-if。errors.Is 检查 err 是否就是
	//（或包裹了）我们某个哨兵错误。第二个 case 列了两个值——任意一个匹配即可。
	switch {
	case errors.Is(err, auth.ErrNoCredentials):
		msg = "未登录 Claude Code"
	case errors.Is(err, auth.ErrTokenExpired), errors.Is(err, usage.ErrUnauthorized):
		msg = "token 过期，请打开一次 Claude Code"
	default:
		msg = "离线：" + err.Error()
	}
	m.status.SetTitle("⚠ " + msg + "（" + time.Now().Format("15:04") + "）")
	systray.SetTooltip("Claude Usage — " + msg)
}
