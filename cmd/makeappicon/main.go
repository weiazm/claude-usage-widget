// Command makeappicon 生成 exe 的应用图标 assets/app.ico。
//
// 它复用 assets.RenderICO——和系统托盘图标完全相同的那套 Go 绘制代码——所以 exe 图标
// 和托盘图标天然风格一致。改动绘制只需改 assets/gauge.go 一处。
//
// 用法：
//
//	go run ./cmd/makeappicon
//	rsrc -ico assets/app.ico -arch amd64 -o rsrc_windows_amd64.syso   # 重新嵌入到 exe
//	go build -trimpath -ldflags="-s -w -H windowsgui" -o claude-usage-widget.exe .
package main

import (
	"fmt"
	"os"

	"claude-usage-widget/assets"
)

func main() {
	// 代表性用量：用绿色档（健康状态，和 widget 启动时一致），填一段较饱满的弧让它更像 logo。
	const pct = 45

	// exe 图标会出现在资源管理器/任务栏/Alt-Tab，需要从小到大一整套尺寸（含 256 高清）。
	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	ico := assets.RenderICO(pct, sizes)

	const out = "assets/app.ico"
	if err := os.WriteFile(out, ico, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes, sizes: %v, pct=%d)\n", out, len(ico), sizes, pct)
}
