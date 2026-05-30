// Package singleinstance enforces that only one widget runs per user session,
// using a named Win32 mutex. The handle is intentionally never closed: the OS
// releases it when the process exits, which is exactly the lifetime we want.
package singleinstance

import "golang.org/x/sys/windows"

// mutexName lives in the session-local namespace, so the limit is one instance
// per logged-in user (the normal desktop case).
const mutexName = `Local\ClaudeUsageWidget_Singleton`

var held windows.Handle

// Acquire attempts to become the sole instance. It returns already=true when
// another instance already holds the mutex.
func Acquire() (already bool, err error) {
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return false, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if h != 0 {
		held = h // keep the handle alive for the process lifetime
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		return true, nil
	}
	if err != nil && h == 0 {
		return false, err
	}
	return false, nil
}
