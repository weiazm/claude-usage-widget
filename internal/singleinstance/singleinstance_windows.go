// Package singleinstance 用一个命名的 Win32 互斥量，保证每个用户会话只运行一个 widget。
// 这个句柄故意不关闭：进程退出时操作系统会自动释放它，而这正是我们想要的生命周期。
//
// Go 提示：文件名后缀 “_windows.go” 是一个编译约束——Go 只在编译目标为 Windows 时才
// 编译这个文件。一个工程就是这样容纳平台相关代码的（macOS 版本会放在 *_darwin.go 里）。
package singleinstance

// golang.org/x/sys/windows 封装了原始的 Win32 API，让我们能直接调用像 CreateMutex
// 这样的系统函数。
import "golang.org/x/sys/windows"

// mutexName 位于会话本地（session-local）命名空间，所以限制是“每个已登录用户一个实例”
// （也就是常见的桌面场景）。
const mutexName = `Local\ClaudeUsageWidget_Singleton`

// held 在整个进程生命周期内持有互斥量句柄。如果它是局部变量，就可能被垃圾回收，导致
// 互斥量被提前释放。
var held windows.Handle

// Acquire 尝试成为唯一实例。当已有另一个实例持有互斥量时，返回 already=true。
//
// Go 提示：“(already bool, err error)”给返回值命了名。这里它们只是起文档作用——
// 下面我们仍然显式地把它们返回。
func Acquire() (already bool, err error) {
	// Win32 需要一个 UTF-16 字符串指针；先把我们的 Go 字符串转换过去。
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		return false, err
	}
	// 即使互斥量已存在，CreateMutex 也会返回一个句柄；此时 err 是 ERROR_ALREADY_EXISTS，
	// 我们正是借此判断已经有一个副本在运行。
	h, err := windows.CreateMutex(nil, false, name)
	if h != 0 {
		held = h // 在进程生命周期内持有该句柄
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		return true, nil
	}
	if err != nil && h == 0 {
		return false, err
	}
	return false, nil
}
