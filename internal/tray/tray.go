// Package tray renders the system-tray widget: an icon coloured by 5-hour usage
// plus a menu showing the usage breakdown, refreshed on a timer and on demand.
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

// Run starts the systray event loop. It blocks until the user quits.
func Run() {
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetIcon(assets.IconGreen)
	systray.SetTitle("")
	systray.SetTooltip("Claude Usage — 加载中…")

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

	refreshNow := make(chan struct{}, 1)

	// Event loop for clicks.
	go func() {
		for {
			select {
			case <-m.refresh.ClickedCh:
				select {
				case refreshNow <- struct{}{}:
				default:
				}
			case <-m.quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	// Polling loop.
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		update(m)
		for {
			select {
			case <-ticker.C:
				update(m)
			case <-refreshNow:
				update(m)
			}
		}
	}()
}

func addInfoItem(text string) *systray.MenuItem {
	it := systray.AddMenuItem(text, "")
	it.Disable()
	return it
}

func update(m *menu) {
	tok, err := auth.Read()
	if err != nil {
		showError(m, err)
		return
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

	var pct float64
	if u.FiveHour != nil {
		pct = u.FiveHour.Utilization
	}
	systray.SetIcon(assets.IconFor(pct))
	systray.SetTooltip(fmt.Sprintf("Claude — 5小时 %s / 7天 %s",
		usage.FormatPercent(u.FiveHour), usage.FormatPercent(u.SevenDay)))

	// Return the transient fetch/parse allocations to the OS so the resident
	// set settles back down between the once-a-minute polls.
	debug.FreeOSMemory()
}

func showError(m *menu, err error) {
	var msg string
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
