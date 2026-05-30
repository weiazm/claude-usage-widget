// Command usagecheck 是一个小型命令行自检工具：读取 token 并把当前 Claude 用量打印到
// 标准输出。运行方式：go run ./cmd/usagecheck
//
// Go 提示：这是同一个模块里的*第二个*程序。cmd/ 下每个带自己 "package main" + func main()
// 的文件夹都会编译成各自独立的二进制，同时共享 internal/ 下的库。
package main

import (
	"context"
	"fmt"
	"os"

	"claude-usage-widget/internal/auth"
	"claude-usage-widget/internal/usage"
)

func main() {
	tok, err := auth.Read()
	if err != nil {
		// Go 提示：os.Stderr 是标准错误流；os.Exit(1) 以非零状态码结束程序，让脚本
		// 知道它失败了。
		fmt.Fprintln(os.Stderr, "auth:", err)
		os.Exit(1)
	}
	// context.Background() 是空的根 context——对一个没有自身截止时间的一次性 CLI 运行
	// 来说足够了（Fetch 会套用它自己的超时）。
	u, err := usage.Fetch(context.Background(), tok.AccessToken)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch:", err)
		os.Exit(1)
	}
	fmt.Println("Claude Usage")
	fmt.Println("────────────────────────────")
	fmt.Println(u.Summary())
	if u.ExtraUsage.IsEnabled {
		fmt.Println("额外用量: 已开启")
	}
}
