package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var eighths = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// barChart renders the tail of data as vertical bars, width x height cells,
// right-aligned (newest sample at the right edge). Returns the rows and the max used for scaling.
func barChart(data []int64, width, height int, style lipgloss.Style) ([]string, int64) {
	if width <= 0 || height <= 0 {
		return nil, 0
	}
	if len(data) > width {
		data = data[len(data)-width:]
	}
	var max int64
	for _, v := range data {
		if v > max {
			max = v
		}
	}
	scale := niceMax(max)
	offset := width - len(data)
	rows := make([]string, height)
	for r := 0; r < height; r++ {
		var b strings.Builder
		level := height - 1 - r // 0 = bottom row
		b.WriteString(strings.Repeat(" ", offset))
		for _, v := range data {
			units := 0
			if scale > 0 {
				units = int(float64(v) / float64(scale) * float64(height*8))
			}
			if v > 0 && units == 0 {
				units = 1
			}
			cell := units - level*8
			switch {
			case cell >= 8:
				b.WriteRune('█')
			case cell <= 0:
				b.WriteRune(' ')
			default:
				b.WriteRune(eighths[cell])
			}
		}
		rows[r] = style.Render(b.String())
	}
	return rows, scale
}

// niceMax rounds v up to a 1/2/5 x 10^n step within its binary unit (KB, MB, …),
// so the axis label formats as a round number like "20 MB".
func niceMax(v int64) int64 {
	if v <= 0 {
		return 0
	}
	unit := int64(1)
	for v/unit >= 1024 {
		unit *= 1024
	}
	x := (v + unit - 1) / unit // 1..1024 in this unit
	for _, step := range []int64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 1024} {
		if step >= x {
			return step * unit
		}
	}
	return 1024 * unit
}

// sparkline renders a single-row sparkline.
func sparkline(data []int64, width int, style lipgloss.Style) string {
	rows, _ := barChart(data, width, 1, style)
	if len(rows) == 0 {
		return ""
	}
	return rows[0]
}
