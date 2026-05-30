// Command claude-usage-widget shows live Claude Code usage in the Windows system
// tray. Build a GUI (no console) exe with:
//
//	go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
package main

import (
	"context"
	"runtime"
	"runtime/debug"
	"time"

	"claude-usage-widget/internal/auth"
	"claude-usage-widget/internal/dialog"
	"claude-usage-widget/internal/singleinstance"
	"claude-usage-widget/internal/tray"
	"claude-usage-widget/internal/usage"
)

func main() {
	// Only one widget per user session. A second launch shows the current usage
	// and a reminder that it is already running in the background, then exits.
	if already, err := singleinstance.Acquire(); err == nil && already {
		dialog.Info("Claude Usage", alreadyRunningMessage())
		return
	}

	// A tray widget is almost always idle; it has no need for parallelism.
	// Pinning to a single P cuts OS threads and scheduler overhead.
	runtime.GOMAXPROCS(1)
	// Keep the heap small and let the runtime reclaim aggressively. The widget
	// allocates only a tiny JSON snapshot once a minute.
	debug.SetGCPercent(20)
	debug.SetMemoryLimit(32 << 20) // 32 MiB soft cap

	tray.Run()
}

// alreadyRunningMessage builds the popup text for a duplicate launch: the live
// usage breakdown plus the "already running" reminder.
func alreadyRunningMessage() string {
	const reminder = "Claude Usage Widget 已在后台运行（任务栏托盘区）。"
	tok, err := auth.Read()
	if err != nil {
		return reminder + "\n\n（" + err.Error() + "）"
	}
	// Keep the popup snappy: cap the fetch so the reminder never waits on a slow
	// network (Fetch's own 15s timeout would otherwise apply).
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	u, err := usage.Fetch(ctx, tok.AccessToken)
	if err != nil {
		return reminder + "\n\n（用量获取失败：" + err.Error() + "）"
	}
	return reminder + "\n\n" + u.Summary()
}
