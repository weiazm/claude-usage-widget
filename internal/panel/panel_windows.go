// Package panel 用一个独立的 Win32 popup 窗口替代“信息全塞在菜单项里”的老做法
// （对应 README 后续优化方向第 5 条）：左键点击托盘图标弹出，里面用进度条画出各用量
// 窗口、显示按量付费的“额外用量”额度，并提供「立即刷新 / 退出」两个按钮。纯 Win32 + GDI，
// 标准库手绘，零新依赖。
//
// 线程模型（关键）：Win32 窗口的消息只会被“创建它的那个线程”的消息循环派发。systray
// 有它自己的线程和循环，所以本面板必须自己起一个锁定的 OS 线程、在上面建窗口并跑
// GetMessage 循环。其它线程（如 systray 的左键回调、轮询 goroutine）只通过 PostMessage
// 往面板窗口投递消息——PostMessage 是跨线程安全的。
//
// DPI（清晰度关键）：进程在启动时已被标记为“每显示器 DPI 感知”（见根目录 dpi_windows.go），
// 于是 Windows 不再位图拉伸我们的窗口；本包据真实 DPI 把所有坐标/字号按 scale 放大，按物理
// 像素绘制，从而在 100%/125%/150%/200% 等常见缩放下都清晰不发糊。
package panel

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"claude-usage-widget/assets"
	"claude-usage-widget/internal/usage"
)

// --- Win32 过程与常量 ---------------------------------------------------------

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procShowWindow            = user32.NewProc("ShowWindow")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procGetClientRect         = user32.NewProc("GetClientRect")
	procInvalidateRect        = user32.NewProc("InvalidateRect")
	procBeginPaint            = user32.NewProc("BeginPaint")
	procEndPaint              = user32.NewProc("EndPaint")
	procFillRect              = user32.NewProc("FillRect")
	procDrawTextW             = user32.NewProc("DrawTextW")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessageW      = user32.NewProc("DispatchMessageW")
	procLoadCursorW           = user32.NewProc("LoadCursorW")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procSetWindowRgn          = user32.NewProc("SetWindowRgn")
	procGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	procGetDC                 = user32.NewProc("GetDC")
	procReleaseDC             = user32.NewProc("ReleaseDC")

	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
	procRoundRect          = gdi32.NewProc("RoundRect")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procCreateFontW        = gdi32.NewProc("CreateFontW")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procGetStockObject     = gdi32.NewProc("GetStockObject")
	procGetDeviceCaps      = gdi32.NewProc("GetDeviceCaps")
)

// 自定义消息：在面板窗口上投递它们来“切换显隐”和“数据已更新、请重绘”。
const (
	wmApp     = 0x8000
	wmToggle  = wmApp + 1
	wmRefresh = wmApp + 2
)

// 一批用到的 Win32 常量。
const (
	wsPopup        = 0x80000000
	wsExToolWindow = 0x00000080 // 不在任务栏/Alt-Tab 里出现
	wsExTopmost    = 0x00000008

	swHide = 0
	swShow = 5

	wmDestroy    = 0x0002
	wmPaint      = 0x000F
	wmActivate   = 0x0006
	wmEraseBkgnd = 0x0014
	wmLButtonUp  = 0x0202

	waInactive = 0

	swpNoActivate = 0x0010

	hwndTopmost = ^uintptr(0) // (HWND)-1

	dtLeft        = 0x0000
	dtCenter      = 0x0001
	dtRight       = 0x0002
	dtVCenter     = 0x0004
	dtSingleLine  = 0x0020
	dtNoPrefix    = 0x0800
	dtEndEllipsis = 0x8000

	transparentBkMode = 1
	nullPen           = 8

	spiGetWorkArea = 0x0030

	idcArrow   = 32512
	logPixelsX = 88 // GetDeviceCaps index：水平 DPI
)

// RECT/POINT/PAINTSTRUCT/MSG/WNDCLASSEX 的 Go 镜像。字段顺序与 Win32 一致，
// 因为我们直接把它们的地址传给系统调用。
type rect struct{ left, top, right, bottom int32 }
type point struct{ x, y int32 }

type paintStruct struct {
	hdc         uintptr
	erase       int32
	rcPaint     rect
	restore     int32
	incUpdate   int32
	rgbReserved [32]byte
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

// --- 面板尺寸（逻辑像素，96 DPI 基准）-----------------------------------------
//
// 这些都是“逻辑单位”；实际绘制时一律经 sc() 乘以 DPI 缩放比，得到物理像素。
const (
	panelWidth  = 320
	panelHeight = 340

	padX         = 16
	btnHeight    = 32
	btnGap       = 10
	bottomMargin = 14

	// labelSplit 是每行“标签列 / 数值列”的分界（逻辑像素）。标签（5 小时/Sonnet 周…）很短，
	// 所以把大头让给右侧的“百分比 + 重置倒计时”，避免像 "100%  10h59m 后重置" 这种较长文本
	// 被 DT_END_ELLIPSIS 截断。
	labelSplit = 104
)

var (
	// hwnd 在 UI 线程创建窗口后写入、被其它线程读取，所以用 atomic 避免数据竞争。0=尚未创建。
	hwnd  atomic.Uintptr
	ready = make(chan struct{})

	// visible 标记面板当前是否可见。轮询线程据此决定“隐藏时干脆别投递重绘消息”，
	// 免得每分钟无谓地唤醒一次 UI 线程——契合本组件的低开销原则。
	visible atomic.Bool

	// startOnce 保证面板的 UI 线程只惰性启动一次：用户首次左键点击才创建窗口/字体/线程，
	// 不点就零开销。
	startOnce sync.Once

	mu       sync.Mutex
	snapshot *usage.Usage // 最近一次用量快照，绘制时读取
	errText  string       // 最近一次错误文案（拉取失败时显示），成功后清空；均受 mu 保护

	lastHide time.Time // 上次因失焦自动隐藏的时刻，用于吞掉“点击托盘把面板点没”的那一下

	// onRefresh/onQuit 由 tray 在启动时通过 SetActions 注入，对应面板上两个按钮的行为。
	onRefresh func()
	onQuit    func()

	// DPI 缩放状态：curDPI 当前 DPI，scale=curDPI/96。字体随 DPI 变化重建。
	curDPI int32 // 0 表示尚未初始化
	scale  float64

	// 创建一次、按 DPI 复用的 GDI 字体。
	fontTitle  uintptr
	fontNormal uintptr
	fontSmall  uintptr
)

// SetActions 注入「立即刷新 / 退出」按钮的回调（由 tray 在 onReady 里调用一次）。
func SetActions(refresh, quit func()) {
	onRefresh = refresh
	onQuit = quit
}

// ensureStarted 惰性启动面板 UI 线程：在一条锁定的 OS 线程上建窗口并跑消息循环，阻塞到
// 窗口就绪才返回。只有首次调用真正干活，后续是空操作。
func ensureStarted() {
	startOnce.Do(func() {
		go uiThread()
		<-ready
	})
}

// Toggle 请求显示/隐藏面板（左键点击托盘时调用）。首次调用会惰性创建窗口；之后只投递
// 一条消息。该函数仅由 systray 的单一回调线程调用，无需额外加锁。
func Toggle() {
	ensureStarted()
	h := hwnd.Load()
	if h == 0 {
		return
	}
	procPostMessageW.Call(h, wmToggle, 0, 0)
}

// Update 存入最新用量快照并清除错误态；仅当面板正显示时才请求重绘（隐藏/未创建时只更新内存，
// 零唤醒）。可从任意 goroutine 调用。
func Update(u *usage.Usage) {
	mu.Lock()
	snapshot = u
	errText = ""
	mu.Unlock()
	repaintIfVisible()
}

// SetError 记录一行错误文案，供面板在脚注处以告警色显示。隐藏时只存不画。
func SetError(text string) {
	mu.Lock()
	errText = text
	mu.Unlock()
	repaintIfVisible()
}

// repaintIfVisible 仅在面板可见且已创建时投递一次重绘消息。
func repaintIfVisible() {
	if !visible.Load() {
		return
	}
	if h := hwnd.Load(); h != 0 {
		procPostMessageW.Call(h, wmRefresh, 0, 0)
	}
}

// uiThread 是面板专属线程：建窗口、跑 GetMessage 循环，直到进程结束。
func uiThread() {
	// 把这个 goroutine 钉死在一条 OS 线程上：Win32 窗口与它的消息循环必须同线程。
	runtime.LockOSThread()

	className, _ := windows.UTF16PtrFromString("ClaudeUsagePanel")
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)

	wc := wndClassEx{
		style:   0,
		wndProc: windows.NewCallback(wndProc),
		cursor:  windows.Handle(cursor),
		// 背景画刷给 nil：我们在 WM_PAINT 里自己铺底，避免系统先擦一遍造成闪烁。
		background: 0,
		className:  className,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	title, _ := windows.UTF16PtrFromString("Claude Usage")
	h, _, _ := procCreateWindowExW.Call(
		wsExToolWindow|wsExTopmost,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsPopup,
		0, 0, panelWidth, panelHeight, // 真实尺寸在 showPanel 里按 DPI 重设
		0, 0, 0, 0,
	)
	hwnd.Store(h)

	close(ready) // 通知 ensureStarted()：窗口已就绪（字体/尺寸/圆角在首次 showPanel 里按 DPI 设定）

	// 标准 Win32 消息循环。GetMessage 返回 0 表示 WM_QUIT，-1 表示出错。
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// --- DPI 缩放 -----------------------------------------------------------------

// sc 把逻辑像素按当前 DPI 缩放比换算成物理像素（四舍五入，兼顾负值字号）。
func sc(v int32) int32 {
	f := float64(v) * scale
	if f < 0 {
		return int32(f - 0.5)
	}
	return int32(f + 0.5)
}

// applyDPI 在 DPI 变化时更新 scale 并重建字体；DPI 不变则为空操作。
func applyDPI(d int32) {
	if d <= 0 {
		d = 96
	}
	if d == curDPI && fontTitle != 0 {
		return
	}
	curDPI = d
	scale = float64(d) / 96
	rebuildFonts()
}

// dpiForWindow 取窗口所在显示器的 DPI；老系统上 GetDpiForWindow 不存在则退回从 DC 读 LOGPIXELSX。
func dpiForWindow(hWnd windows.Handle) int32 {
	if procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(uintptr(hWnd)); d != 0 {
			return int32(d)
		}
	}
	hdc, _, _ := procGetDC.Call(uintptr(hWnd))
	if hdc != 0 {
		d, _, _ := procGetDeviceCaps.Call(hdc, logPixelsX)
		procReleaseDC.Call(uintptr(hWnd), hdc)
		if d != 0 {
			return int32(d)
		}
	}
	return 96
}

// rebuildFonts 按当前 scale 重建三种字体（先删旧的，避免 GDI 句柄泄漏）。用“微软雅黑”确保中文清晰。
func rebuildFonts() {
	for _, f := range []uintptr{fontTitle, fontNormal, fontSmall} {
		if f != 0 {
			procDeleteObject.Call(f)
		}
	}
	fontTitle = newFont(sc(-19), 700)  // 标题，加粗
	fontNormal = newFont(sc(-15), 400) // 正文
	fontSmall = newFont(sc(-13), 400)  // 脚注/小字
}

// newFont 包一层 CreateFontW：height 取负表示“字符高度”（像素），weight 400/700。
func newFont(height int32, weight int32) uintptr {
	const (
		defaultCharset   = 1
		clearTypeQuality = 5
		defaultPitch     = 0
		ffDontCare       = 0
	)
	face, _ := windows.UTF16PtrFromString("Microsoft YaHei UI")
	h, _, _ := procCreateFontW.Call(
		uintptr(height), 0, 0, 0, uintptr(weight),
		0, 0, 0, // italic, underline, strikeout
		defaultCharset, 0, 0, clearTypeQuality,
		defaultPitch|ffDontCare,
		uintptr(unsafe.Pointer(face)),
	)
	return h
}

// --- 窗口过程 -----------------------------------------------------------------

// wndProc 是窗口过程：系统派发到本窗口的消息都进这里。
func wndProc(hWnd windows.Handle, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmEraseBkgnd:
		return 1 // 自己在 WM_PAINT 铺底，告诉系统“无需擦背景”，减少闪烁

	case wmToggle:
		vis, _, _ := procIsWindowVisible.Call(uintptr(hWnd))
		if vis != 0 {
			visible.Store(false)
			procShowWindow.Call(uintptr(hWnd), swHide)
		} else {
			// 若刚因失焦自动隐藏（<250ms），说明这次点击就是“点掉面板”的那一下，
			// 不要立刻又弹回来。
			if time.Since(lastHide) < 250*time.Millisecond {
				return 0
			}
			showPanel(hWnd)
		}
		return 0

	case wmRefresh:
		vis, _, _ := procIsWindowVisible.Call(uintptr(hWnd))
		if vis != 0 {
			procInvalidateRect.Call(uintptr(hWnd), 0, 0)
		}
		return 0

	case wmLButtonUp:
		// 点击在面板内的两个按钮上→触发对应动作。lParam 低/高 16 位是客户区坐标（物理像素）。
		x := int32(int16(lParam & 0xffff))
		y := int32(int16((lParam >> 16) & 0xffff))
		rRefresh, rQuit := buttonRects()
		p := point{x, y}
		switch {
		case ptInRect(p, rRefresh):
			if onRefresh != nil {
				onRefresh()
			}
		case ptInRect(p, rQuit):
			if onQuit != nil {
				onQuit()
			}
		}
		return 0

	case wmActivate:
		// 低位字 == WA_INACTIVE：失去激活（点了别处）→ 隐藏，做出“点外面就关”的弹层观感。
		if (wParam & 0xffff) == waInactive {
			lastHide = time.Now()
			visible.Store(false)
			procShowWindow.Call(uintptr(hWnd), swHide)
		}
		return 0

	case wmPaint:
		paint(hWnd)
		return 0

	case wmDestroy:
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hWnd), uintptr(message), wParam, lParam)
	return r
}

// showPanel 按当前 DPI 调整尺寸/圆角，把面板摆到工作区右下角（任务栏上方），显示并置前后重绘。
func showPanel(hWnd windows.Handle) {
	applyDPI(dpiForWindow(hWnd))
	w := sc(panelWidth)
	h := sc(panelHeight)
	setRoundRegion(hWnd, w, h)

	var wa rect
	procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
	margin := sc(8)
	x := wa.right - w - margin
	y := wa.bottom - h - margin

	procSetWindowPos.Call(uintptr(hWnd), hwndTopmost,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		swpNoActivate)
	procShowWindow.Call(uintptr(hWnd), swShow)
	procSetForegroundWindow.Call(uintptr(hWnd))
	visible.Store(true)
	procInvalidateRect.Call(uintptr(hWnd), 0, 0)
}

// setRoundRegion 按当前尺寸给窗口套圆角区域（SetWindowRgn 接管该区域，无需我们删除）。
func setRoundRegion(hWnd windows.Handle, w, h int32) {
	r := sc(16)
	rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(h+1), uintptr(r), uintptr(r))
	procSetWindowRgn.Call(uintptr(hWnd), rgn, 1)
}

// buttonRects 返回「立即刷新 / 退出」两个按钮的物理像素矩形（与绘制、命中测试同一套算法）。
func buttonRects() (refresh, quit rect) {
	btnW := int32((panelWidth - 2*padX - btnGap) / 2)
	btnY := int32(panelHeight - bottomMargin - btnHeight)
	refresh = rect{sc(padX), sc(btnY), sc(padX) + sc(btnW), sc(btnY) + sc(btnHeight)}
	quit = rect{sc(padX) + sc(btnW) + sc(btnGap), sc(btnY), sc(panelWidth - padX), sc(btnY) + sc(btnHeight)}
	return
}

func ptInRect(p point, r rect) bool {
	return p.x >= r.left && p.x < r.right && p.y >= r.top && p.y < r.bottom
}

// --- 绘制 ---------------------------------------------------------------------

// rgb 把 color.RGBA 转成 GDI 的 COLORREF（0x00BBGGRR）。
func rgb(r, g, b uint8) uintptr {
	return uintptr(uint32(r) | uint32(g)<<8 | uint32(b)<<16)
}

// 面板配色（深色卡片）。
var (
	bgColor    = rgb(32, 34, 40)    // 卡片底
	trackColor = rgb(58, 62, 70)    // 进度条凹槽 / 分隔线
	textColor  = rgb(232, 234, 238) // 主文字
	subColor   = rgb(150, 156, 166) // 次要文字（重置时间/脚注）
	warnColor  = rgb(235, 165, 90)  // 错误/告警文字
	btnColor   = rgb(52, 56, 66)    // 按钮底色
)

// paint 在 WM_PAINT 里完成整块面板的绘制。
func paint(hWnd windows.Handle) {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(uintptr(hWnd), uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(uintptr(hWnd), uintptr(unsafe.Pointer(&ps)))

	var rc rect
	procGetClientRect.Call(uintptr(hWnd), uintptr(unsafe.Pointer(&rc)))

	// 铺底。
	fillRect(hdc, rc.left, rc.top, rc.right, rc.bottom, bgColor)
	procSetBkMode.Call(hdc, transparentBkMode)

	mu.Lock()
	u := snapshot
	errMsg := errText
	mu.Unlock()

	y := int32(14)

	// 标题。
	selectFont(hdc, fontTitle)
	setTextColor(hdc, textColor)
	drawText(hdc, "Claude 用量", sc(padX), sc(y), sc(panelWidth-padX), sc(y+24), dtLeft)
	y += 32

	// 分隔线。
	fillRect(hdc, sc(padX), sc(y), sc(panelWidth-padX), sc(y)+sc(1), trackColor)
	y += 12

	if u == nil {
		// 还没数据：要么显示错误，要么“加载中…”。按钮照常绘制（退出/刷新随时可用）。
		body := "加载中…"
		col := subColor
		if errMsg != "" {
			body = "⚠ " + errMsg
			col = warnColor
		}
		selectFont(hdc, fontNormal)
		setTextColor(hdc, col)
		drawText(hdc, body, sc(padX), sc(y), sc(panelWidth-padX), sc(y+20), dtLeft)
		drawButtons(hdc)
		return
	}

	// 四个用量窗口，各画一行标签+百分比+进度条+重置时间。
	y = drawMetric(hdc, y, "5 小时", u.FiveHour)
	y = drawMetric(hdc, y, "7 天", u.SevenDay)
	y = drawMetric(hdc, y, "Opus 周", u.SevenDayOpus)
	y = drawMetric(hdc, y, "Sonnet 周", u.SevenDaySonnet)

	// 额外用量（按量付费溢出额度）。
	y += 2
	fillRect(hdc, sc(padX), sc(y), sc(panelWidth-padX), sc(y)+sc(1), trackColor)
	y += 10
	selectFont(hdc, fontSmall)
	setTextColor(hdc, subColor)
	drawText(hdc, extraUsageText(u.ExtraUsage), sc(padX), sc(y), sc(panelWidth-padX), sc(y+18), dtLeft)
	y += 22

	// 脚注：有错误就显示错误（告警色），否则显示更新时间。
	if errMsg != "" {
		setTextColor(hdc, warnColor)
		drawText(hdc, "⚠ "+errMsg, sc(padX), sc(y), sc(panelWidth-padX), sc(y+18), dtLeft)
	} else {
		setTextColor(hdc, subColor)
		drawText(hdc, "更新于 "+u.FetchedAt.Format("15:04:05"), sc(padX), sc(y), sc(panelWidth-padX), sc(y+18), dtLeft)
	}

	drawButtons(hdc)
}

// drawButtons 画底部的「立即刷新 / 退出」两个按钮。
func drawButtons(hdc uintptr) {
	rRefresh, rQuit := buttonRects()
	selectFont(hdc, fontNormal)
	drawButton(hdc, rRefresh, "立即刷新")
	drawButton(hdc, rQuit, "退出")
}

// drawButton 画一个圆角按钮：底色 + 居中文字。
func drawButton(hdc uintptr, r rect, label string) {
	roundFilledRect(hdc, r.left, r.top, r.right, r.bottom, sc(8), btnColor)
	setTextColor(hdc, textColor)
	drawText(hdc, label, r.left, r.top, r.right, r.bottom, dtCenter)
}

// drawMetric 画一行用量：标签（左）+ 百分比/重置（右），下面是一条进度条。返回下一行的 y（逻辑像素）。
func drawMetric(hdc uintptr, y int32, label string, w *usage.Window) int32 {
	selectFont(hdc, fontNormal)

	// 标签（左对齐）。
	setTextColor(hdc, textColor)
	drawText(hdc, label, sc(padX), sc(y), sc(labelSplit), sc(y+18), dtLeft)

	// 右侧：百分比 +（可选）重置倒计时，整体右对齐。
	right := usage.FormatPercent(w)
	if reset := usage.FormatReset(w); reset != "" {
		right += "  " + reset
	}
	setTextColor(hdc, subColor)
	drawText(hdc, right, sc(labelSplit), sc(y), sc(panelWidth-padX), sc(y+18), dtRight)
	y += 22

	// 进度条：凹槽 + 按百分比填充（颜色与图标同源）。
	barW := int32(panelWidth - 2*padX)
	barH := int32(8)
	roundFilledRect(hdc, sc(padX), sc(y), sc(padX)+sc(barW), sc(y)+sc(barH), sc(barH), trackColor)
	if w != nil {
		pct := clampPct(w.Utilization)
		fw := int32(float64(barW) * pct / 100)
		if fw > 0 {
			c := assets.BarColor(pct)
			roundFilledRect(hdc, sc(padX), sc(y), sc(padX)+sc(fw), sc(y)+sc(barH), sc(barH), rgb(c.R, c.G, c.B))
		}
	}
	y += barH + 12
	return y
}

func clampPct(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// extraUsageText 把按量付费的额外用量额度渲染成一行可读文本。
func extraUsageText(e usage.ExtraUsage) string {
	if !e.IsEnabled {
		return "额外用量：未开启"
	}
	cur := "$"
	if e.Currency != nil && *e.Currency != "" {
		cur = *e.Currency + " "
	}
	used := 0.0
	if e.UsedCredits != nil {
		used = *e.UsedCredits
	}
	if e.MonthlyLimit != nil {
		return fmt.Sprintf("额外用量：%s%.2f / %s%.2f", cur, used, cur, *e.MonthlyLimit)
	}
	return fmt.Sprintf("额外用量：已用 %s%.2f", cur, used)
}

// --- GDI 小工具 ---------------------------------------------------------------

func fillRect(hdc uintptr, l, t, r, b int32, color uintptr) {
	rc := rect{l, t, r, b}
	brush, _, _ := procCreateSolidBrush.Call(color)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	procDeleteObject.Call(brush)
}

// roundFilledRect 画一个无描边的圆角实心矩形（进度条/按钮）。radius 为物理像素圆角半径。
func roundFilledRect(hdc uintptr, l, t, r, b, radius int32, color uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)
	pen, _, _ := procGetStockObject.Call(nullPen) // 无边框
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procRoundRect.Call(hdc, uintptr(l), uintptr(t), uintptr(r+1), uintptr(b+1), uintptr(radius), uintptr(radius))
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
}

func selectFont(hdc, font uintptr) {
	procSelectObject.Call(hdc, font)
}

func setTextColor(hdc, color uintptr) {
	procSetTextColor.Call(hdc, color)
}

// drawText 用 DrawTextW 在给定矩形内绘制一行文本，flags 控制对齐。
func drawText(hdc uintptr, text string, l, t, r, b int32, flags uintptr) {
	s, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	rc := rect{l, t, r, b}
	procDrawTextW.Call(hdc,
		uintptr(unsafe.Pointer(s)), ^uintptr(0), // -1：以 null 结尾，自动算长度
		uintptr(unsafe.Pointer(&rc)),
		flags|dtSingleLine|dtVCenter|dtNoPrefix|dtEndEllipsis,
	)
}
