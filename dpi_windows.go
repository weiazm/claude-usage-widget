// DPI 感知（Windows 专属）。
//
// 为什么需要：默认情况下进程是“DPI 不感知”的，Windows 会先按 96 DPI 渲染我们的窗口、
// 再按系统缩放比例（125%/150%/200%）整体位图拉伸——文字和图形于是发虚发糊。把进程标记为
// “每显示器 DPI 感知 V2”后，Windows 不再替我们拉伸，而是把真实 DPI 交给我们，由我们按比例
// 自己绘制清晰像素（见 panel 包里的 scale 逻辑）。
//
// Go 提示：放在 *_windows.go 文件里的 init() 会在 main() 之前自动执行，且只在 Windows
// 构建时编译。这样既保证“在创建任何窗口之前就设好 DPI 感知”，又不污染跨平台的 main.go。
package main

import "golang.org/x/sys/windows"

func init() {
	user32 := windows.NewLazySystemDLL("user32.dll")

	// 首选：SetProcessDpiAwarenessContext(PER_MONITOR_AWARE_V2)，Win10 1703+。
	// DPI_AWARENESS_CONTEXT 是一个“伪句柄”常量，V2 的值为 -4。
	const perMonitorAwareV2 = ^uintptr(3) // (DPI_AWARENESS_CONTEXT)-4
	if p := user32.NewProc("SetProcessDpiAwarenessContext"); p.Find() == nil {
		if r, _, _ := p.Call(perMonitorAwareV2); r != 0 {
			return
		}
	}

	// 退路 1：SetProcessDpiAwareness(PROCESS_PER_MONITOR_DPI_AWARE=2)，Win8.1+（shcore.dll）。
	shcore := windows.NewLazySystemDLL("shcore.dll")
	if p := shcore.NewProc("SetProcessDpiAwareness"); p.Find() == nil {
		const processPerMonitorDpiAware = 2
		if r, _, _ := p.Call(processPerMonitorDpiAware); r == 0 { // S_OK
			return
		}
	}

	// 退路 2：SetProcessDPIAware()，Vista+。只有系统级（非每显示器）感知，但足以避免位图拉伸糊化。
	if p := user32.NewProc("SetProcessDPIAware"); p.Find() == nil {
		p.Call()
	}
}
