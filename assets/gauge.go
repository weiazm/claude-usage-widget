// Package assets 负责渲染托盘图标。我们不预制几张图片，而是在运行时用纯 Go 把仪表盘
// 画出来，让弧长精确跟随用量百分比（例如 37% 就填 37% 的弧），而不是绿/黄/红三档的粗略
// 近似。纯标准库实现：无 cgo、无外部图像库。
package assets

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
)

// Gauge 返回 pct（0..100）对应的 Windows .ico 字节，可直接交给 systray.SetIcon。
// 图标里打包了多个像素尺寸，让 Windows 能按当前 DPI 挑一张清晰的。
//
// Go 提示：渲染要做一点工作，所以我们做了缓存。导出的这个函数是其它包唯一接触到的入口；
// 下面所有小写名字都是私有的。
func Gauge(pct float64) []byte {
	// 四舍五入到整数百分比：在约 16px 的托盘图标里，眼睛也只能分辨到这个精度，而且这意味
	// 着整个程序生命周期里最多只画约 100 张不同的图标（轮询之间通常还能复用缓存的那张）。
	key := int(math.Round(clampF(pct, 0, 100)))

	gaugeMu.Lock()
	defer gaugeMu.Unlock()
	if key == gaugeKey && gaugeBytes != nil {
		return gaugeBytes
	}

	// 关键：fyne/systray 在 Windows 上用 LoadImage(LR_DEFAULTSIZE, cx=cy=0) 加载托盘图标，
	// 这会按“大图标”指标 SM_CXICON 取帧（100% DPI=32px，随 DPI 放大：125%→40、150%→48、
	// 200%→64），随后由系统外壳把它缩放进约 16px 的托盘槽位。也就是说：被选用的是这些“大”帧，
	// 16/20/24 那种小帧根本不会被取用。所以这里打包 SM_CXICON 在各 DPI 下的原生尺寸，让系统
	// 总能拿到精确匹配的大帧、无需软缩放，最终下采样到托盘时最锐利。
	gaugeBytes = RenderICO(float64(key), []int{32, 40, 48, 64})
	gaugeKey = key
	return gaugeBytes
}

// RenderICO 用给定百分比把仪表盘渲染成一张包含若干尺寸的 Windows .ico。
//
// 托盘图标（Gauge）和 exe 应用图标（cmd/makeappicon）都复用它，保证两者风格完全一致——
// 同一套绘制代码就是“唯一事实来源”。颜色按用量档位（绿/黄/红）自动选取。
func RenderICO(pct float64, sizes []int) []byte {
	c1, c2 := bandColors(pct)
	imgs := make([]image.Image, len(sizes))
	for i, s := range sizes {
		imgs[i] = renderGauge(s, pct, c1, c2)
	}
	return encodeICO(imgs)
}

// Go 提示：包级的互斥量 + 缓存。sync.Mutex 保护这两个缓存变量，让并发调用方（虽然我们
// 只有一个，但这样更安全）不会发生竞态。
var (
	gaugeMu    sync.Mutex
	gaugeKey   = -1 // 上次渲染的百分比；-1 表示“还没有缓存”
	gaugeBytes []byte
)

// --- 几何常量（以图标尺寸的比例表示）------------------------------------------
//
// 仪表是一段在底部留 90° 缺口的圆弧：从左下角（135°）开始，顺时针扫到满刻度时为 270°。
// 角度采用屏幕坐标（y 轴向下），所以 0°=右、90°=下、180°=左、270°=上。
const (
	arcStart = 135.0 // 轨道起点（左下角）
	arcFull  = 270.0 // 满刻度时的总扫过角度

	padFrac    = 0.02 // 圆角矩形背景前的外边距
	radiusFrac = 0.22 // 背景圆角半径
	insetFrac  = 0.15 // 边缘到仪表圆环的距离（越小仪表越大、越占满图标）
	strokeFrac = 0.15 // 仪表描边粗细
	ss         = 8    // 抗锯齿用的超采样倍数（越大边缘越平滑）
)

// renderGauge 渲染某一个尺寸：先以 ss 倍分辨率用硬边判定绘制，再做盒式降采样到目标尺寸，
// 从而得到平滑（抗锯齿）的边缘——比解析式矢量光栅化更简单也更省。
func renderGauge(size int, pct float64, c1, c2 color.RGBA) *image.RGBA {
	big := drawBig(size*ss, pct, c1, c2)
	return downsample(big, size, ss)
}

// drawBig 绘制全分辨率的仪表。S 是超采样后的边长。
func drawBig(S int, pct float64, c1, c2 color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, S, S))
	fs := float64(S)

	// 背景圆角矩形（用状态渐变色填充）。
	pad := math.Max(1, fs*padFrac)
	rectX, rectY := pad, pad
	rectW, rectH := fs-2*pad, fs-2*pad
	radius := fs * radiusFrac

	// 仪表圆环。略微下移（×1.04），让它在有底部缺口的情况下看起来居中，和 exe 应用图标
	// 的观感保持一致。
	inset := fs * insetFrac
	ringW := fs - 2*inset
	cx := inset + ringW/2
	cy := inset*1.04 + ringW/2
	R := ringW / 2
	stroke := math.Max(2*ss, fs*strokeFrac)
	half := stroke / 2

	sweepVal := arcFull * clampF(pct, 0, 100) / 100

	// 各端点的圆心，用来给弧的端帽做圆角。
	tsX, tsY := arcPoint(cx, cy, R, arcStart)          // 轨道与数值弧的起点
	teX, teY := arcPoint(cx, cy, R, arcStart+arcFull)  // 轨道终点
	veX, veY := arcPoint(cx, cy, R, arcStart+sweepVal) // 数值弧终点

	// 颜色。白色数值弧叠在深色“凹槽”轨道上，对比强烈。
	track := color.RGBA{0, 0, 0, 120} // 半透明黑，叠加在背景上
	white := color.RGBA{255, 255, 255, 255}

	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5

			var c color.RGBA
			// 1) 背景。
			if roundRectSDF(px, py, rectX, rectY, rectW, rectH, radius) <= 0 {
				t := (py - rectY) / rectH
				c = lerp(c1, c2, t)
			}
			if c.A == 0 {
				continue // 在圆角矩形之外：保持透明
			}

			d := math.Hypot(px-cx, py-cy)
			onRing := math.Abs(d-R) <= half
			ang := angleOf(px-cx, py-cy)

			// 2) 轨道（未填充的凹槽）：完整的 270° 扫过 + 圆角端帽。
			if (onRing && arcDelta(ang, arcStart) <= arcFull) ||
				within(px, py, tsX, tsY, half) ||
				within(px, py, teX, teY, half) {
				c = over(c, track)
			}

			// 3) 数值弧（已填充部分）：扫过角度与 pct 成正比。
			if sweepVal > 0 {
				if (onRing && arcDelta(ang, arcStart) <= sweepVal) ||
					within(px, py, tsX, tsY, half) ||
					within(px, py, veX, veY, half) {
					c = white
				}
			}

			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// --- 几何 / 颜色小工具 ---------------------------------------------------------

// arcPoint 返回圆（圆心 cx,cy，半径 R）上角度为 angleDeg 处的点。
func arcPoint(cx, cy, R, angleDeg float64) (float64, float64) {
	r := angleDeg * math.Pi / 180
	return cx + R*math.Cos(r), cy + R*math.Sin(r)
}

// angleOf 返回向量 (dx,dy) 的角度，范围 [0,360) 度，从 +x 轴起顺时针测量
// （因为屏幕上 y 轴向下）。
func angleOf(dx, dy float64) float64 {
	a := math.Atan2(dy, dx) * 180 / math.Pi
	if a < 0 {
		a += 360
	}
	return a
}

// arcDelta 表示 ang 沿顺时针方向超出 start 多少，范围 [0,360)。一个点落在扫过角度为 s
// 的弧内，当且仅当 arcDelta(ang,start) <= s。
func arcDelta(ang, start float64) float64 {
	return math.Mod(ang-start+360, 360)
}

// within 判断 (px,py) 是否在以 (cx,cy) 为圆心、半径 r 的圆内——用于弧的圆角端帽。
func within(px, py, cx, cy, r float64) bool {
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

// roundRectSDF 是到圆角矩形的有符号距离：<=0 表示在内部。
func roundRectSDF(px, py, x, y, w, h, r float64) float64 {
	qx := math.Abs(px-(x+w/2)) - (w/2 - r)
	qy := math.Abs(py-(y+h/2)) - (h/2 - r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

// over 把 src 用标准的“source over”方式叠加到不透明的 dst 之上。
func over(dst, src color.RGBA) color.RGBA {
	a := float64(src.A) / 255
	return color.RGBA{
		R: uint8(float64(dst.R)*(1-a) + float64(src.R)*a),
		G: uint8(float64(dst.G)*(1-a) + float64(src.G)*a),
		B: uint8(float64(dst.B)*(1-a) + float64(src.B)*a),
		A: 255,
	}
}

// lerp 在两个颜色之间做线性插值（t 取 0..1）——用于背景渐变。
func lerp(a, b color.RGBA, t float64) color.RGBA {
	t = clampF(t, 0, 1)
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 255,
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// bandColors 按用量档位挑选状态渐变色：弧长已经编码了精确数值，颜色只是给一个
// 一眼可辨的信号。
func bandColors(pct float64) (color.RGBA, color.RGBA) {
	switch {
	case pct >= 85: // 红
		return color.RGBA{248, 81, 73, 255}, color.RGBA{218, 54, 51, 255}
	case pct >= 50: // 黄
		return color.RGBA{227, 179, 65, 255}, color.RGBA{210, 153, 34, 255}
	default: // 绿
		return color.RGBA{63, 185, 80, 255}, color.RGBA{46, 160, 67, 255}
	}
}

// downsample 把 ss 倍的图像盒式滤波降到 size×size。求平均在预乘 alpha 空间进行，
// 这样透明的边缘像素不会渗出暗色描边。
func downsample(big *image.RGBA, size, ss int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	n := float64(ss * ss)
	for oy := 0; oy < size; oy++ {
		for ox := 0; ox < size; ox++ {
			var pr, pg, pb, pa float64
			for j := 0; j < ss; j++ {
				for i := 0; i < ss; i++ {
					c := big.RGBAAt(ox*ss+i, oy*ss+j)
					af := float64(c.A) / 255
					pr += float64(c.R) * af
					pg += float64(c.G) * af
					pb += float64(c.B) * af
					pa += float64(c.A)
				}
			}
			a := pa / n
			var r, g, b uint8
			if a > 0 {
				scale := a / 255 // 反预乘
				r = clampByte(pr / n / scale)
				g = clampByte(pg / n / scale)
				b = clampByte(pb / n / scale)
			}
			out.SetRGBA(ox, oy, color.RGBA{r, g, b, uint8(a + 0.5)})
		}
	}
	return out
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// encodeICO 把若干张 PNG 压缩的图像打包成 Windows .ico 容器。Windows（Vista 及以上）
// 和 systray 的加载器都支持 PNG-in-ICO 的条目。
func encodeICO(imgs []image.Image) []byte {
	type entry struct {
		data []byte
		dim  int
	}
	entries := make([]entry, 0, len(imgs))
	for _, im := range imgs {
		var buf bytes.Buffer
		_ = png.Encode(&buf, im)
		entries = append(entries, entry{buf.Bytes(), im.Bounds().Dx()})
	}

	var out bytes.Buffer
	// ICONDIR 头：保留位(0)、类型(1=图标)、图像数量。
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries) // 各条目的头部排在像素数据之前
	for _, e := range entries {
		// 宽/高字节里写 0 表示“256”；我们所有尺寸都 < 256。
		dim := byte(0)
		if e.dim < 256 {
			dim = byte(e.dim)
		}
		out.WriteByte(dim)                                      // 宽
		out.WriteByte(dim)                                      // 高
		out.WriteByte(0)                                        // 调色板颜色数（0 = 无）
		out.WriteByte(0)                                        // 保留位
		_ = binary.Write(&out, binary.LittleEndian, uint16(1))  // 颜色平面数
		_ = binary.Write(&out, binary.LittleEndian, uint16(32)) // 每像素位数
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(e.data)))
		_ = binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		out.Write(e.data)
	}
	return out.Bytes()
}
