// Package singleinstance enforces that only one widget runs per user session,
// using a named Win32 mutex. The handle is intentionally never closed: the OS
// releases it when the process exits, which is exactly the lifetime we want.
//
// Go note: the "_windows.go" filename suffix is a build constraint — Go compiles
// this file only when targeting Windows. That is how one project can hold
// platform-specific code (a macOS version would live in *_darwin.go).
package singleinstance

// golang.org/x/sys/windows wraps the raw Win32 API so we can call OS functions
// like CreateMutex directly.
import "golang.org/x/sys/windows"

// mutexName lives in the session-local namespace, so the limit is one instance
// per logged-in user (the normal desktop case).
const mutexName = `Local\ClaudeUsageWidget_Singleton`

// held keeps the mutex handle alive for the whole process. If it were a local
// variable it could be garbage-collected and the mutex released early.
var held windows.Handle

// Acquire attempts to become the sole instance. It returns already=true when
// another instance already holds the mutex.
//
// Go note: "(already bool, err error)" names the return values. They are just
// documentation here — we still return them explicitly below.
func Acquire() (already bool, err error) {
	// Win32 wants a UTF-16 string pointer; convert our Go string first.
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return false, err
	}
	// CreateMutex returns a handle even when the mutex already exists; in that
	// case err is ERROR_ALREADY_EXISTS, which is how we detect a running copy.
	h, err := windows.CreateMutex(nil, false, name)
	if h != 0 {
		held = h // hold the handle for the process lifetime
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		return true, nil
	}
	if err != nil && h == 0 {
		return false, err
	}
	return false, nil
}
