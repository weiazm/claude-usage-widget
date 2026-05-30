// Package dialog 弹出一个原生的 Windows 消息框，用于在第二次启动时告知用户 widget
// 已在运行（并顺带显示当前用量）。
package dialog

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Go 提示：NewLazySystemDLL 在首次使用时加载 user32.dll，NewProc 在其中找到 MessageBoxW
// 函数。对于 x/sys/windows 包没有现成封装的 Win32 函数，就是这样调用的。
var procMessageBox = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

// 下面这些是 Win32 API 的 MessageBox 标志位（在下方用 | 组合起来）。
const (
	mbOK            = 0x00000000
	mbIconInfo      = 0x00000040
	mbSetForeground = 0x00010000
	mbTopMost       = 0x00040000
)

// Info 弹出一个模态信息框，并阻塞直到用户关闭它。
func Info(title, text string) {
	// Win32 需要以 null 结尾的 UTF-16 字符串；转换失败就直接返回。
	t, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	c, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	// Go 提示：.Call 把参数当作 uintptr（机器字）传递。unsafe.Pointer 把我们的字符串
	// 指针转换成那种原始形式。“unsafe”是做底层系统调用的逃生通道——这里用没问题，但在
	// 普通代码里要避免使用。
	procMessageBox.Call(
		0, // 没有属主窗口
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(c)),
		uintptr(mbOK|mbIconInfo|mbSetForeground|mbTopMost),
	)
}
