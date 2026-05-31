// Package tray 渲染系统托盘 widget：一个按 5 小时用量着色的图标，外加一个显示用量明细
// 的菜单，并按定时器和用户手动操作刷新。
package tray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"fyne.io/systray"

	"claude-usage-widget/assets"
	"claude-usage-widget/internal/auth"
	"claude-usage-widget/internal/config"
	"claude-usage-widget/internal/panel"
	"claude-usage-widget/internal/usage"
)

// menu 持有托盘相关状态。用量明细已移到左键弹出的面板里，右键只保留一个「退出」兜底，
// 所以这里很简洁。
//
// Go 提示：*systray.MenuItem 是指针；systray 库返回指针，这样调用 it.SetTitle(...)
// 就能更新屏幕上真正的那一项。
type menu struct {
	quit *systray.MenuItem // 右键菜单里的「退出」兜底项

	// cfg 是本次会话加载的配置（轮询间隔、图标取色窗口）。
	cfg config.Config
}

// Run 启动 systray 事件循环。它会一直阻塞，直到用户退出。
//
// Go 提示：systray.Run 接收两个函数作为参数（函数在 Go 里是值）。onReady 在托盘就绪后
// 运行一次；第二个是 onExit 回调。
//
// onExit 里直接 os.Exit(0) 是为了“退干净”：systray 的退出只拆掉托盘 UI 和消息循环，但我们
// 还有后台的 60s 轮询 goroutine（可能正卡在一次网络请求里）。光让消息循环结束，进程不一定会
// 立刻终止。systray 在删除托盘图标后、消息循环收尾前会调用 onExit，正好是强制退出的时机——
// 此时图标已经移除，os.Exit(0) 立刻结束整个进程（含所有 goroutine），不留残留进程。
func Run() {
	systray.Run(onReady, func() { os.Exit(0) })
	// 兜底：万一消息循环是正常返回（没走 onExit），也确保进程退出。
	os.Exit(0)
}

func onReady() {
	systray.SetIcon(assets.Gauge(0))
	systray.SetTitle("")
	systray.SetTooltip("Claude Usage — 加载中…")

	// Go 提示：channel（通道）是一根带类型的管道，用于在 goroutine 之间传值。
	// chan struct{} 不携带数据——它是纯信号。后面的 "1" 给它一个长度为 1 的缓冲，这样
	// 即使暂时没人接收，发送也不会阻塞。
	refreshNow := make(chan struct{}, 1)

	// triggerRefresh 请求轮询 goroutine 立即刷新。内层非阻塞 select 在已有信号排队时
	//（缓冲已满）丢弃本次，这样连续快速触发会被合并，而不是堆积。可从任意线程安全调用。
	triggerRefresh := func() {
		select {
		case refreshNow <- struct{}{}:
		default:
		}
	}

	// 把“左键点击托盘”接到用量面板的显隐切换上；面板上的「立即刷新 / 退出」按钮回调到这里。
	// 面板是惰性创建的——用户不点就不会有任何窗口/线程开销（见 panel 包）。
	panel.SetActions(triggerRefresh, systray.Quit)
	systray.SetOnTapped(func() { panel.Toggle() })

	// 用量明细已全部移到左键面板；右键只保留一个「退出」兜底，万一面板出问题也能退出。
	//
	// Go 提示：&menu{...} 构造一个 menu 结构体并取它的地址。
	m := &menu{cfg: config.Load()}
	m.quit = systray.AddMenuItem("退出", "退出小部件")

	// Go 提示："go func() { ... }()" 启动一个 goroutine——一种轻量级线程。
	// 这个 goroutine 监听右键菜单的「退出」点击。
	go func() {
		<-m.quit.ClickedCh
		systray.Quit()
	}()

	// 第二个 goroutine：负责刷新数据的轮询循环。
	go func() {
		// Ticker 每隔配置的间隔就在它的通道（ticker.C）上触发一次。
		ticker := time.NewTicker(m.cfg.PollInterval())
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

// iconWindow 按配置选出“给托盘图标着色”的那个用量窗口。
func iconWindow(cfg config.Config, u *usage.Usage) *usage.Window {
	if cfg.IconSource == config.IconSourceSevenDay {
		return u.SevenDay
	}
	return u.FiveHour
}

// update 拉取当前用量，并把它推送到面板、图标和 tooltip。
func update(m *menu) {
	tok, err := auth.Read()
	if err != nil {
		showError(err)
		return // 出错就提前返回——这是常见的 Go 写法
	}
	ctx := context.Background()
	u, err := usage.Fetch(ctx, tok.AccessToken)
	if err != nil {
		showError(err)
		return
	}

	// 把这份快照推给点击面板（它在可见时会据此重绘进度条），并清掉之前的错误态。
	panel.Update(u)

	// 图标颜色跟随哪个窗口由配置决定（默认 5 小时）。
	// Go 提示：var 声明 pct 并赋零值（0.0），窗口缺失时安全保持空仪表（绿色 0%）。
	var pct float64
	if w := iconWindow(m.cfg, u); w != nil {
		pct = w.Utilization
	}
	systray.SetIcon(assets.Gauge(pct))
	systray.SetTooltip(fmt.Sprintf("Claude — 5小时 %s / 7天 %s",
		usage.FormatPercent(u.FiveHour), usage.FormatPercent(u.SevenDay)))

	// 把这次拉取/解析产生的临时内存归还操作系统，让常驻内存在每分钟一次的轮询之间
	// 重新沉降下来。
	debug.FreeOSMemory()
}

// showError 把错误映射成友好的中文状态文案，推给面板脚注和 tooltip。
func showError(err error) {
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
	panel.SetError(msg)
	systray.SetTooltip("Claude Usage — " + msg)
}
