// Package assets holds embedded tray icons.
package assets

import _ "embed"

//go:embed icon_green.ico
var IconGreen []byte

//go:embed icon_amber.ico
var IconAmber []byte

//go:embed icon_red.ico
var IconRed []byte

// IconFor returns the icon bytes appropriate for a utilization percentage.
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
