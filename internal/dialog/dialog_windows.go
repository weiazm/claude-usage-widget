// Package dialog shows a native Windows message box, used to tell a second
// launch that the widget is already running (and show current usage).
package dialog

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Go note: NewLazySystemDLL loads user32.dll on first use, and NewProc finds the
// MessageBoxW function inside it. This is how Go calls Win32 functions that the
// x/sys/windows package does not already wrap.
var procMessageBox = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

// These are the MessageBox flag bits from the Win32 API (combined with | below).
const (
	mbOK            = 0x00000000
	mbIconInfo      = 0x00000040
	mbSetForeground = 0x00010000
	mbTopMost       = 0x00040000
)

// Info shows a modal informational message box and blocks until dismissed.
func Info(title, text string) {
	// Win32 expects null-terminated UTF-16 strings; convert and bail on error.
	t, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	c, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	// Go note: .Call passes arguments as uintptr (machine words). unsafe.Pointer
	// converts our string pointers into that raw form. "unsafe" is the escape
	// hatch for low-level OS calls — fine here, but avoid it in ordinary code.
	procMessageBox.Call(
		0, // no owner window
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(c)),
		uintptr(mbOK|mbIconInfo|mbSetForeground|mbTopMost),
	)
}
