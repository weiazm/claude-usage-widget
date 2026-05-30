package usage

import (
	"fmt"
	"strings"
	"time"
)

// Summary 渲染完整的多行用量文本块，供弹窗和命令行自检使用。
//
// Go 提示：“(u *Usage)”是方法接收者——它把这个函数挂到 Usage 类型上，于是你可以用
// u.Summary() 来调用。用指针接收者（*Usage）能避免复制结构体，也允许方法在需要时修改它。
func (u *Usage) Summary() string {
	// Go 提示：[]string{...} 是内联构造的切片（一个可增长的数组视图）。
	// strings.Join 用 "\n" 把各元素拼接起来。
	return strings.Join([]string{
		Line("5 小时: ", u.FiveHour),
		Line("7 天:   ", u.SevenDay),
		Line("Opus 周:", u.SevenDayOpus),
		Line("Sonnet周:", u.SevenDaySonnet),
	}, "\n")
}

// FormatPercent 把一个窗口渲染成 "37%"；窗口缺失（nil）时渲染成 "n/a"。
//
// Go 提示：w 是 *Window 指针，所以读取前要先判空——对 nil 指针解引用会 panic（崩溃）。
func FormatPercent(w *Window) string {
	if w == nil {
		return "n/a"
	}
	// %.0f 打印不带小数的浮点数；%% 打印一个字面的百分号。
	return fmt.Sprintf("%.0f%%", w.Utilization)
}

// FormatReset 渲染重置倒计时，例如 "1h38m 后重置"。
func FormatReset(w *Window) string {
	if w == nil || w.ResetsAt == nil {
		return ""
	}
	// Go 提示：*w.ResetsAt 解引用指针，取出 time.Time 值。
	// time.Until 返回一个 Duration（从现在到那个时间点的间隔）。
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

// Line 渲染一行带标签的用量，例如 "5 小时:  37%  (1h38m 后重置)"。
func Line(label string, w *Window) string {
	pct := FormatPercent(w)
	reset := FormatReset(w)
	if reset == "" {
		return fmt.Sprintf("%s %s", label, pct)
	}
	return fmt.Sprintf("%s %s  (%s)", label, pct, reset)
}
