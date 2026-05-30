// Package assets holds embedded tray icons.
package assets

// Go note: the blank import `_ "embed"` loads the embed package for its side
// effect (it enables the //go:embed directive) without naming it in code.
import _ "embed"

// Go note: //go:embed is a compiler directive — at build time it reads the named
// file and stores its bytes in the variable right below. That is why the .ico
// files must be committed: the exe bakes them in, so there is nothing to load at
// runtime. The directive must sit immediately above its variable.

//go:embed icon_green.ico
var IconGreen []byte

//go:embed icon_amber.ico
var IconAmber []byte

//go:embed icon_red.ico
var IconRed []byte

// IconFor returns the icon bytes appropriate for a utilization percentage:
// green under 50%, amber from 50%, red from 85%.
func IconFor(utilizationPct float64) []byte {
	switch {
	case utilizationPct >= 85:
		return IconRed
	case utilizationPct >= 50:
		return IconAmber
	default:
		return IconGreen
	}
}
