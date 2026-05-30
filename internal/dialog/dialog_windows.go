// Package dialog shows a native Windows message box, used to tell a second
// launch that the widget is already running (and show current usage).
package dialog

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procMessageBox = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

const (
	mbOK            = 0x00000000
	mbIconInfo      = 0x00000040
	mbSetForeground = 0x00010000
	mbTopMost       = 0x00040000
)

// Info shows a modal informational message box and blocks until dismissed.
func Info(title, text string) {
	t, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	c, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	procMessageBox.Call(
		0,
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(c)),
		uintptr(mbOK|mbIconInfo|mbSetForeground|mbTopMost),
	)
}
