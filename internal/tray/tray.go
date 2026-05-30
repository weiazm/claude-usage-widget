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

// menu holds pointers to every tray menu item so update() can change their text.
//
// Go note: *systray.MenuItem is a pointer; the systray library hands us pointers
// so that calling it.SetTitle(...) updates the real on-screen item.
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
//
// Go note: systray.Run takes two functions as arguments (functions are values in
// Go). onReady runs once the tray is ready; the second is an onExit callback —
// here an empty inline function "func() {}" because we have no cleanup to do.
func Run() {
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetIcon(assets.Gauge(0))
	systray.SetTitle("")
	systray.SetTooltip("Claude Usage — 加载中…")

	// Go note: &menu{...} builds a menu struct and takes its address. We set the
	// header field inline; the rest are assigned below.
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

	// Go note: a channel is a typed pipe for passing values between goroutines.
	// chan struct{} carries no data — it is a pure signal. The "1" gives it a
	// buffer of one, so a send never blocks even if no one is reading yet.
	refreshNow := make(chan struct{}, 1)

	// Go note: "go func() { ... }()" starts a goroutine — a lightweight thread.
	// This one watches for menu clicks forever.
	go func() {
		for {
			// select waits on multiple channels and runs whichever is ready.
			// Each menu item exposes a ClickedCh channel that fires on click.
			select {
			case <-m.refresh.ClickedCh:
				// Ask the polling goroutine to refresh. The inner non-blocking
				// select drops the signal if one is already queued (buffer full),
				// so rapid clicks coalesce instead of piling up.
				select {
				case refreshNow <- struct{}{}:
				default:
				}
			case <-m.quit.ClickedCh:
				systray.Quit()
				return // ends this goroutine
			}
		}
	}()

	// Second goroutine: the polling loop that refreshes the data.
	go func() {
		// A Ticker fires on its channel (ticker.C) every pollInterval.
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		update(m) // fetch immediately on startup
		for {
			select {
			case <-ticker.C: // periodic refresh
				update(m)
			case <-refreshNow: // manual "立即刷新"
				update(m)
			}
		}
	}()
}

// addInfoItem adds a non-clickable (disabled) row used purely to display text.
func addInfoItem(text string) *systray.MenuItem {
	it := systray.AddMenuItem(text, "")
	it.Disable()
	return it
}

// update fetches current usage and pushes it into the menu, icon and tooltip.
func update(m *menu) {
	tok, err := auth.Read()
	if err != nil {
		showError(m, err)
		return // bail out early on error — a common Go pattern
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

	// Go note: var declares pct with its zero value (0.0). We only overwrite it
	// when FiveHour is present, so a missing window safely keeps the green icon.
	var pct float64
	if u.FiveHour != nil {
		pct = u.FiveHour.Utilization
	}
	systray.SetIcon(assets.Gauge(pct))
	systray.SetTooltip(fmt.Sprintf("Claude — 5小时 %s / 7天 %s",
		usage.FormatPercent(u.FiveHour), usage.FormatPercent(u.SevenDay)))

	// Return the transient fetch/parse allocations to the OS so the resident
	// set settles back down between the once-a-minute polls.
	debug.FreeOSMemory()
}

// showError maps an error to a friendly Chinese status line and tooltip.
func showError(m *menu, err error) {
	var msg string
	// Go note: a switch with no expression acts like if/else-if. errors.Is checks
	// whether err is (or wraps) one of our sentinel errors. The second case lists
	// two values — either one matches.
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
