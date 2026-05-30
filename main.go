// Command claude-usage-widget 在 Windows 系统托盘里实时显示 Claude Code 的用量。
// 构建一个 GUI（无控制台窗口）exe：
//
//	go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
//
// Go 提示：package main 里带 func main() 的文件就是可执行程序的入口。其它包（auth、
// usage、tray…）是我们在下面 import 的库。
package main

// Go 提示：import 是分组的——标准库在前，然后是我们自己的包。像
// "claude-usage-widget/internal/auth" 这样的路径，是模块路径（在 go.mod 里声明）加上
// 文件夹名。Go 里 "internal/" 很特殊：只有本模块内的代码才能 import 它，所以它永远不会
// 作为公开 API 泄露出去。
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
	// 每个用户会话只允许一个 widget。第二次启动会显示当前用量并提醒已在后台运行，然后退出。
	//
	// Go 提示：":=" 一步完成声明和赋值，类型由 Go 推断。一个函数可以返回多个值——这里
	// Acquire() 返回 (already, err)。我们在 "if" 的条件里同时检查这两个值，再决定是否
	// 进入函数体。
	if already, err := singleinstance.Acquire(); err == nil && already {
		dialog.Info("Claude Usage", alreadyRunningMessage())
		return
	}

	// 托盘 widget 几乎一直空闲，不需要并行。把它固定到单个 P（逻辑处理器）能减少 OS
	// 线程和调度开销。
	runtime.GOMAXPROCS(1)
	// 让堆保持很小、并让运行时更积极地回收。这个 widget 每分钟只分配一小份 JSON 快照。
	debug.SetGCPercent(20)
	debug.SetMemoryLimit(32 << 20) // "32 << 20" 即 32 * 2^20 = 32 MiB

	// tray.Run 会阻塞（运行 OS 的消息循环），直到用户点击“退出”。
	tray.Run()
}

// alreadyRunningMessage 构造重复启动时弹窗里的文本：实时用量明细 + “已在运行”的提醒。
//
// Go 提示：小写名字（alreadyRunningMessage）让这个函数成为包私有——只有 package main
// 里的文件能调用它。像 usage.Fetch 这样首字母大写的名字才是导出（公开）的。
func alreadyRunningMessage() string {
	// Go 提示："const" 是编译期常量。Go 处理错误的惯用法是把它们作为值返回、并立刻用
	// "if err != nil" 检查，而不是抛异常。
	const reminder = "Claude Usage Widget 已在后台运行（任务栏托盘区）。"
	tok, err := auth.Read()
	if err != nil {
		return reminder + "\n\n（" + err.Error() + "）"
	}
	// 让弹窗反应迅速：给这次拉取设一个上限，免得提醒被慢网络拖住（否则会套用 Fetch 自己
	// 的 15 秒超时）。
	//
	// Go 提示：context.Context 把截止时间/取消信号沿调用链向下传递。"defer cancel()"
	// 安排 cancel() 在本函数返回时执行——defer 是 Go 标准的清理机制（即使提前 return
	// 也一定会执行），这样我们绝不会泄露这个计时器。
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	u, err := usage.Fetch(ctx, tok.AccessToken)
	if err != nil {
		return reminder + "\n\n（用量获取失败：" + err.Error() + "）"
	}
	return reminder + "\n\n" + u.Summary()
}
