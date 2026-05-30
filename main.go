// Command claude-usage-widget shows live Claude Code usage in the Windows system
// tray. Build a GUI (no console) exe with:
//
//	go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
//
// Go note: a file in "package main" with a func main() is an executable program's
// entry point. Other packages (auth, usage, tray…) are libraries we import below.
package main

// Go note: imports are grouped — the standard library first, then our own
// packages. Each path like "claude-usage-widget/internal/auth" is the module
// path (declared in go.mod) plus the folder. "internal/" is special in Go: only
// code inside this module may import it, so it can never leak as a public API.
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
	//
	// Go note: ":=" declares and assigns in one step; Go infers the types. A
	// function can return several values — here Acquire() returns (already, err).
	// We check both inside the "if" before the body runs.
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
	debug.SetMemoryLimit(32 << 20) // "32 << 20" is 32 * 2^20 = 32 MiB

	// tray.Run blocks (runs the OS message loop) until the user clicks "退出".
	tray.Run()
}

// alreadyRunningMessage builds the popup text for a duplicate launch: the live
// usage breakdown plus the "already running" reminder.
//
// Go note: the lowercase name (alreadyRunningMessage) makes this function
// package-private — only files in "package main" can call it. Capitalized names
// like usage.Fetch are exported (public).
func alreadyRunningMessage() string {
	// Go note: "const" is a compile-time constant. The Go idiom for errors is to
	// return them as values and check "if err != nil" right away, rather than
	// throwing exceptions.
	const reminder = "Claude Usage Widget 已在后台运行（任务栏托盘区）。"
	tok, err := auth.Read()
	if err != nil {
		return reminder + "\n\n（" + err.Error() + "）"
	}
	// Keep the popup snappy: cap the fetch so the reminder never waits on a slow
	// network (Fetch's own 15s timeout would otherwise apply).
	//
	// Go note: a context.Context carries a deadline/cancellation down a call
	// chain. "defer cancel()" schedules cancel() to run when this function
	// returns — defer is Go's standard cleanup mechanism (it always runs, even on
	// early returns), so we never leak the timer.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	u, err := usage.Fetch(ctx, tok.AccessToken)
	if err != nil {
		return reminder + "\n\n（用量获取失败：" + err.Error() + "）"
	}
	return reminder + "\n\n" + u.Summary()
}
