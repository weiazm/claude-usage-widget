package usage

import (
	"fmt"
	"strings"
	"time"
)

// Summary renders the full multi-line usage block used by the dialog and the
// CLI self-check.
func (u *Usage) Summary() string {
	return strings.Join([]string{
		Line("5 小时: ", u.FiveHour),
		Line("7 天:   ", u.SevenDay),
		Line("Opus 周:", u.SevenDayOpus),
		Line("Sonnet周:", u.SevenDaySonnet),
	}, "\n")
}

// FormatPercent renders a window as "37%" or "n/a" when the window is absent.
func FormatPercent(w *Window) string {
	if w == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%%", w.Utilization)
}

// FormatReset renders the reset countdown, e.g. "1h38m 后重置".
func FormatReset(w *Window) string {
	if w == nil || w.ResetsAt == nil {
		return ""
	}
	d := time.Until(*w.ResetsAt)
	if d <= 0 {
		return "即将重置"
	}
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h >= 24 {
		days := h / 24
		return fmt.Sprintf("%dd%dh 后重置", days, h%24)
	}
	if h > 0 {
		return fmt.Sprintf("%dh%dm 后重置", h, m)
	}
	return fmt.Sprintf("%dm 后重置", m)
}

// Line renders one labelled usage line, e.g. "5 小时:  37%  (1h38m 后重置)".
func Line(label string, w *Window) string {
	pct := FormatPercent(w)
	reset := FormatReset(w)
	if reset == "" {
		return fmt.Sprintf("%s %s", label, pct)
	}
	return fmt.Sprintf("%s %s  (%s)", label, pct, reset)
}
