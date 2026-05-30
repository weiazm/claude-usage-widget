// Package assets renders the tray icon. Instead of shipping a few pre-baked
// images, we draw the gauge in pure Go at runtime so the arc length tracks the
// *exact* usage percentage (e.g. 37% -> 37% of the arc filled), not a coarse
// green/amber/red band. Pure stdlib: no cgo, no external image libraries.
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

// Gauge returns Windows .ico bytes for a gauge filled to pct (0..100), ready to
// hand to systray.SetIcon. The icon packs several pixel sizes so Windows can
// pick a crisp one for the current DPI.
//
// Go note: rendering is a little work, so we memoise. The exported function is
// the only thing other packages touch; everything below is lowercase = private.
func Gauge(pct float64) []byte {
	// Round to a whole percent: that is all the eye can resolve in a ~16px tray
	// icon, and it means we redraw at most ~100 distinct icons over the app's
	// life (and usually reuse the cached one between 60s polls).
	key := int(math.Round(clampF(pct, 0, 100)))

	gaugeMu.Lock()
	defer gaugeMu.Unlock()
	if key == gaugeKey && gaugeBytes != nil {
		return gaugeBytes
	}

	c1, c2 := bandColors(float64(key))
	// Sizes Windows commonly requests for the tray across DPI settings.
	sizes := []int{16, 20, 24, 32}
	imgs := make([]image.Image, len(sizes))
	for i, s := range sizes {
		imgs[i] = renderGauge(s, float64(key), c1, c2)
	}
	gaugeBytes = encodeICO(imgs)
	gaugeKey = key
	return gaugeBytes
}

// Go note: a package-level mutex + cache. sync.Mutex guards the two cache vars
// so concurrent callers (we only have one, but this keeps it safe) don't race.
var (
	gaugeMu    sync.Mutex
	gaugeKey   = -1 // last rendered percent; -1 means "nothing cached yet"
	gaugeBytes []byte
)

// --- geometry constants (as fractions of the icon size) ------------------------
//
// The gauge is an arc with a 90° gap at the bottom: it starts at the lower-left
// (135°) and sweeps clockwise up to 270° at full. Angles use screen coordinates
// (y grows downward), so 0°=right, 90°=down, 180°=left, 270°=up.
const (
	arcStart = 135.0 // where the track begins (lower-left)
	arcFull  = 270.0 // total sweep of a full gauge

	padFrac    = 0.03 // outer padding before the rounded-rect background
	radiusFrac = 0.22 // background corner radius
	insetFrac  = 0.24 // distance from edge to the gauge ring
	strokeFrac = 0.13 // gauge stroke thickness
	ss         = 4    // supersampling factor for anti-aliasing
)

// renderGauge draws one size by rendering at ss× resolution with hard-edged
// tests, then box-downsampling to the target size for smooth (anti-aliased)
// edges — cheaper and simpler than an analytic vector rasteriser.
func renderGauge(size int, pct float64, c1, c2 color.RGBA) *image.RGBA {
	big := drawBig(size*ss, pct, c1, c2)
	return downsample(big, size, ss)
}

// drawBig paints the full-resolution gauge. fs is the supersampled side length.
func drawBig(S int, pct float64, c1, c2 color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, S, S))
	fs := float64(S)

	// Background rounded rectangle (filled with the status gradient).
	pad := math.Max(1, fs*padFrac)
	rectX, rectY := pad, pad
	rectW, rectH := fs-2*pad, fs-2*pad
	radius := fs * radiusFrac

	// Gauge ring. Nudged down slightly (×1.04) so it sits centred given the
	// bottom gap, matching the app icon's look.
	inset := fs * insetFrac
	ringW := fs - 2*inset
	cx := inset + ringW/2
	cy := inset*1.04 + ringW/2
	R := ringW / 2
	stroke := math.Max(2*ss, fs*strokeFrac)
	half := stroke / 2

	sweepVal := arcFull * clampF(pct, 0, 100) / 100

	// Endpoint centres, used to round the arc caps.
	tsX, tsY := arcPoint(cx, cy, R, arcStart)         // track + value start
	teX, teY := arcPoint(cx, cy, R, arcStart+arcFull) // track end
	veX, veY := arcPoint(cx, cy, R, arcStart+sweepVal) // value end

	// Colours. White value arc over a dark "groove" track gives strong contrast.
	track := color.RGBA{0, 0, 0, 120} // translucent black, blended over the bg
	white := color.RGBA{255, 255, 255, 255}

	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5

			var c color.RGBA
			// 1) Background.
			if roundRectSDF(px, py, rectX, rectY, rectW, rectH, radius) <= 0 {
				t := (py - rectY) / rectH
				c = lerp(c1, c2, t)
			}
			if c.A == 0 {
				continue // outside the rounded rect: leave transparent
			}

			d := math.Hypot(px-cx, py-cy)
			onRing := math.Abs(d-R) <= half
			ang := angleOf(px-cx, py-cy)

			// 2) Track (the unfilled groove): full 270° sweep + round caps.
			if (onRing && arcDelta(ang, arcStart) <= arcFull) ||
				within(px, py, tsX, tsY, half) ||
				within(px, py, teX, teY, half) {
				c = over(c, track)
			}

			// 3) Value arc (the filled portion): sweep proportional to pct.
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

// --- small geometry / colour helpers ------------------------------------------

// arcPoint returns the point on a circle (centre cx,cy, radius R) at angleDeg.
func arcPoint(cx, cy, R, angleDeg float64) (float64, float64) {
	r := angleDeg * math.Pi / 180
	return cx + R*math.Cos(r), cy + R*math.Sin(r)
}

// angleOf returns the angle of vector (dx,dy) in [0,360) degrees, measured
// clockwise from the +x axis (because y points down on screen).
func angleOf(dx, dy float64) float64 {
	a := math.Atan2(dy, dx) * 180 / math.Pi
	if a < 0 {
		a += 360
	}
	return a
}

// arcDelta is how far ang lies clockwise past start, in [0,360). A point is in
// an arc of sweep s if arcDelta(ang,start) <= s.
func arcDelta(ang, start float64) float64 {
	return math.Mod(ang-start+360, 360)
}

// within reports whether (px,py) is inside radius r of (cx,cy) — used for the
// round end caps of the arcs.
func within(px, py, cx, cy, r float64) bool {
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

// roundRectSDF is the signed distance to a rounded rectangle: <=0 means inside.
func roundRectSDF(px, py, x, y, w, h, r float64) float64 {
	qx := math.Abs(px-(x+w/2)) - (w/2 - r)
	qy := math.Abs(py-(y+h/2)) - (h/2 - r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

// over alpha-composites src on top of an opaque dst (standard "source over").
func over(dst, src color.RGBA) color.RGBA {
	a := float64(src.A) / 255
	return color.RGBA{
		R: uint8(float64(dst.R)*(1-a) + float64(src.R)*a),
		G: uint8(float64(dst.G)*(1-a) + float64(src.G)*a),
		B: uint8(float64(dst.B)*(1-a) + float64(src.B)*a),
		A: 255,
	}
}

// lerp linearly interpolates between two colours (t in 0..1) — the bg gradient.
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

// bandColors picks the status gradient by usage band: the arc length already
// encodes the precise value, so the colour just gives an at-a-glance signal.
func bandColors(pct float64) (color.RGBA, color.RGBA) {
	switch {
	case pct >= 85: // red
		return color.RGBA{248, 81, 73, 255}, color.RGBA{218, 54, 51, 255}
	case pct >= 50: // amber
		return color.RGBA{227, 179, 65, 255}, color.RGBA{210, 153, 34, 255}
	default: // green
		return color.RGBA{63, 185, 80, 255}, color.RGBA{46, 160, 67, 255}
	}
}

// downsample box-filters the ss× image down to size×size. Averaging is done in
// premultiplied-alpha space so transparent edge pixels don't bleed dark fringes.
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
				scale := a / 255 // un-premultiply
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

// encodeICO packs PNG-compressed images into a Windows .ico container. Windows
// (Vista+) and the systray loader both accept PNG-in-ICO entries.
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
	// ICONDIR header: reserved(0), type(1=icon), image count.
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries) // headers come before the pixel data
	for _, e := range entries {
		// 0 in the width/height byte means "256"; all our sizes are < 256.
		dim := byte(0)
		if e.dim < 256 {
			dim = byte(e.dim)
		}
		out.WriteByte(dim) // width
		out.WriteByte(dim) // height
		out.WriteByte(0)   // palette colours (0 = none)
		out.WriteByte(0)   // reserved
		_ = binary.Write(&out, binary.LittleEndian, uint16(1))  // colour planes
		_ = binary.Write(&out, binary.LittleEndian, uint16(32)) // bits per pixel
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(e.data)))
		_ = binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		out.Write(e.data)
	}
	return out.Bytes()
}
