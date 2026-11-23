package workbench

import (
	"strconv"

	"github.com/vivaui/viva/gfx"
)

// monitorChartSeries is one line of a monitor trend: its color, the label that
// names it to assistive technology, and its values. A NaN is a gap, never a
// zero.
type monitorChartSeries struct {
	color  gfx.Color
	label  string
	values []float32
}

// monitorChartOptions is the frame around the data: its domain, which axes it
// shows, and the label and focus state the chart reports.
type monitorChartOptions struct {
	xMin, xMax       float32
	yMin, yMax       float32
	fixedY           bool
	hiddenAxes       bool
	xTicks           int
	formatX          func(dst []byte, value float32) []byte
	formatY          func(dst []byte, value float32) []byte
	label            string
	focused          bool
	seriesCountLabel string
}

// chartFiniteValue reports whether value is a real reading, not a NaN gap.
func chartFiniteValue(value float32) bool { return value == value }

// paintMonitorTrend paints one area chart with the same data and presentation
// the monitor surfaces use: a monotone-ish line over a faded area fill, ticks
// on the axes it is given, and one accessible hit naming the plot. It replaces
// the canvas-level chart component so the charts can be painted inside the
// page's hand-painted scroll content.
func paintMonitorTrend(canvas *gfx.Canvas, bounds gfx.Rect, id gfx.ID, x []float32, series []monitorChartSeries, options monitorChartOptions) {
	if bounds.Width < 2 || bounds.Height < 2 || len(x) == 0 {
		return
	}
	plot := bounds
	if !options.hiddenAxes {
		plot.Y += 4
		plot.Height -= 4
		if options.formatX != nil {
			plot.Height -= 16
		}
		if options.formatY != nil {
			plot.X += 30
			plot.Width -= 34
		}
	}
	if plot.Width < 1 || plot.Height < 1 {
		return
	}
	xMin, xMax := options.xMin, options.xMax
	if xMax <= xMin {
		xMin, xMax = x[0], x[len(x)-1]
	}
	if xMax <= xMin {
		xMax = xMin + 1
	}
	yMin, yMax := options.yMin, options.yMax
	if !options.fixedY || yMax <= yMin {
		yMin, yMax = 0, 0
		first := true
		for _, s := range series {
			for _, value := range s.values {
				if !chartFiniteValue(value) {
					continue
				}
				if first || value < yMin {
					yMin = value
				}
				if first || value > yMax {
					yMax = value
				}
				first = false
			}
		}
		if first {
			return
		}
		if yMax <= yMin {
			yMax = yMin + 1
		}
		yMin = min(yMin, 0)
		yMax += (yMax - yMin) * 0.08
	}
	// The series and its fill are built in the plot's own units, the view box
	// DrawPath maps into plot: adding plot's origin here as well would draw them
	// once more to the right and below, past the chart's right edge.
	py := func(value float32) float32 {
		return plot.Height - (value-yMin)/(yMax-yMin)*plot.Height
	}
	px := func(value float32) float32 {
		return (value - xMin) / (xMax - xMin) * plot.Width
	}
	baseline := plot.Height
	for _, s := range series {
		if len(s.values) == 0 {
			continue
		}
		// The line, broken at each gap.
		builder := canvas.PathBuilder()
		started := false
		for i, value := range s.values {
			if i >= len(x) || !chartFiniteValue(value) {
				started = false
				continue
			}
			if !started {
				builder.MoveTo(px(x[i]), py(value))
				started = true
				continue
			}
			builder.LineTo(px(x[i]), py(value))
		}
		if builder.Len() == 0 {
			continue
		}
		canvas.DrawPath(builder.Path(plot.Width, plot.Height), plot, gfx.PathStyle{
			Stroke: s.color, StrokeWidth: 1.5, Join: gfx.JoinRound, Cap: gfx.CapRound,
		})
		// The faded area under the line, one run at a time.
		for start := 0; start < len(s.values); {
			for start < len(s.values) && (start >= len(x) || !chartFiniteValue(s.values[start])) {
				start++
			}
			end := start
			for end < len(s.values) && end < len(x) && chartFiniteValue(s.values[end]) {
				end++
			}
			if end-start >= 2 {
				fill := canvas.PathBuilder()
				fill.MoveTo(px(x[start]), baseline)
				for i := start; i < end; i++ {
					fill.LineTo(px(x[i]), py(s.values[i]))
				}
				fill.LineTo(px(x[end-1]), baseline)
				fill.Close()
				gradient := gfx.LinearGradient{
					Start: gfx.Point{X: 0, Y: 0}, End: gfx.Point{X: 0, Y: plot.Height},
					From: withAlpha(s.color, .22), To: withAlpha(s.color, 0),
				}
				canvas.DrawPath(fill.Path(plot.Width, plot.Height), plot, gfx.PathStyle{
					Fill: s.color, FillGradient: &gradient,
				})
			}
			start = end
		}
	}
	// Axis labels.
	if options.formatX != nil && !options.hiddenAxes {
		ticks := options.xTicks
		if ticks <= 0 {
			ticks = 3
		}
		var scratch [32]byte
		for tick := 0; tick < ticks; tick++ {
			t := float32(0)
			if ticks > 1 {
				t = float32(tick) / float32(ticks-1)
			}
			value := xMin + (xMax-xMin)*t
			text := string(options.formatX(scratch[:0], value))
			width := canvas.TextWidth(text, 10)
			origin := plot.X + t*plot.Width - width/2
			origin = min(max(origin, bounds.X), bounds.X+bounds.Width-width)
			canvas.Label(text, gfx.Rect{X: origin, Y: plot.Y + plot.Height + 2, Width: width + 2, Height: 14}, gfx.TextStyle{Size: 10, Color: chartAxisColor(options, series)})
		}
	}
	if options.formatY != nil && !options.hiddenAxes {
		var scratch [32]byte
		for tick := 0; tick < 5; tick++ {
			t := float32(tick) / 4
			value := yMin + (yMax-yMin)*(1-t)
			text := string(options.formatY(scratch[:0], value))
			canvas.LabelAligned(text, gfx.Rect{X: bounds.X, Y: plot.Y + t*plot.Height - 7, Width: max(plot.X-bounds.X-6, 0), Height: 14},
				gfx.TextStyle{Size: 10, Color: chartAxisColor(options, series)}, gfx.AlignRight)
		}
	}
	label := options.label
	if label == "" {
		label = "Chart"
	}
	value := ""
	if options.focused && len(series) > 0 && len(series[0].values) > 0 {
		last := series[0].values[len(series[0].values)-1]
		if chartFiniteValue(last) {
			var scratch [32]byte
			value = series[0].label + " " + string(strconv.AppendFloat(scratch[:0], float64(last), 'f', 1, 32))
		}
	}
	canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleGroup, Cursor: gfx.PointerCursorCrosshair, Value: value})
}

// chartAxisColor is the muted ink of the axis labels.
func chartAxisColor(options monitorChartOptions, series []monitorChartSeries) gfx.Color {
	if len(series) > 0 {
		color := series[0].color
		color.Alpha = .55
		return color
	}
	return gfx.Color{Red: .5, Green: .5, Blue: .5, Alpha: .8}
}
