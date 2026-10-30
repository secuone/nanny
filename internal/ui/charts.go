package ui

import (
	"math"
	"strings"
)

// Charts in one line of text: a sparkline and a horizontal bar. With the
// ASCII icons the look pass redraws their glyphs as _.-=# and |.

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// barPartial are the eighths of a cell, from 1/8 to 7/8.
var barPartial = []rune("▏▎▍▌▋▊▉")

func init() {
	for r, s := range map[rune]string{'▁': "_", '▂': ".", '▃': "-", '▄': "=", '▅': "=", '▆': "#", '▇': "#", '█': "#", '▉': "#", '▊': "#", '▋': "#"} {
		if _, ok := asciiIcons[r]; !ok {
			asciiIcons[r] = s
		}
	}
}

// spark draws vals as one cell each, scaled to the largest; more values than
// w are folded into w cells by their maximum. Nothing is one low bar.
func spark(vals []float64, w int) string {
	if w <= 0 || len(vals) == 0 {
		return ""
	}
	if len(vals) > w {
		folded := make([]float64, w)
		for i, v := range vals {
			j := i * w / len(vals)
			folded[j] = math.Max(folded[j], v)
		}
		vals = folded
	}
	top := 0.0
	for _, v := range vals {
		top = math.Max(top, v)
	}
	var b strings.Builder
	for _, v := range vals {
		i := 0
		if top > 0 && v > 0 {
			i = int(math.Round(v / top * float64(len(sparkRunes)-1)))
		}
		b.WriteRune(sparkRunes[min(max(i, 0), len(sparkRunes)-1)])
	}
	return b.String()
}

// hbar is v out of max as a bar of w cells, in eighths of a cell, padded with
// spaces to w.
func hbar(v, max float64, w int) string {
	if w <= 0 {
		return ""
	}
	cells := 0.0
	if max > 0 && v > 0 {
		cells = math.Min(v/max, 1) * float64(w)
	}
	full := int(cells)
	eighths := int((cells - float64(full)) * 8)
	bar := strings.Repeat("█", full)
	if eighths > 0 && full < w {
		bar += string(barPartial[eighths-1])
	}
	if max > 0 && v > 0 && bar == "" { // something is not nothing
		bar = string(barPartial[0])
	}
	return bar + strings.Repeat(" ", w-lipglossW(bar))
}
