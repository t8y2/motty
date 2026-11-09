package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

const (
	monitorCPU = iota
	monitorMemory
	monitorDisk
	monitorDownload
)

type monitorChart struct {
	state    components.ChartState
	series   [2]components.ChartSeries
	values   [2][]float32
	style    [2]components.ChartAreaStyle
	gradient [2]components.ChartAreaGradient
}

var monitorChartHits = [...]gfx.ID{
	tree.ID(monitorHits, "cpu-trend"), tree.ID(monitorHits, "memory-trend"),
	tree.ID(monitorHits, "disk-trend"), tree.ID(monitorHits, "network-trend"),
}

// Every metric shares the same real 60-second domain. Missing points are NaN;
// neither startup nor reconnect is padded with invented historical readings.
func (s *State) paintMonitorTrend(c *gfx.Canvas, p *monitorView, b gfx.Rect, metric int, label string, color gfx.Color) {
	if p.count < 2 || b.Width <= 0 || b.Height <= 0 {
		return
	}
	start, last := 0, p.x[p.count-1]
	// Retain one point before the visible window to intersect its left edge.
	for start < p.count-2 && p.x[start+1] < last-60 {
		start++
	}
	values := [...]*[61]float32{&p.cpu, &p.memory, &p.disk, &p.down}
	cache := &p.charts[metric]
	count := 1
	if metric == monitorDownload {
		count = 2
	}
	for i := 0; i < count; i++ {
		tint, name := color, label
		if i == 1 {
			tint = s.monitorColors().cpu
			name = s.messages.SFTPUpload
		}
		cache.gradient[i].From, cache.gradient[i].To = tint, tint
		cache.gradient[i].To.Alpha = 0
		cache.style[i] = components.ChartAreaStyle{Opacity: .22, Gradient: &cache.gradient[i]}
		if s.pal.dark {
			cache.style[i].Opacity = .28
		}
		cache.series[i] = components.ChartSeries{Key: name, Label: name, Color: tint, AreaStyle: &cache.style[i]}
	}
	cache.values[0] = values[metric][start:p.count]
	if count == 2 {
		cache.values[1] = p.up[start:p.count]
	}
	axis := components.ChartAxis{Hidden: true}
	if metric <= monitorDisk {
		axis.Fixed = true
		axis.Min = 0
		axis.Max = 100
	}
	theme := s.Theme()
	theme.Border = gfx.Color{}
	c.Clip(b)
	components.PaintAreaChart(c, theme, b, monitorChartHits[metric], components.AreaChart{
		Series: cache.series[:count], X: p.x[start:p.count], Values: cache.values[:count],
		Curve: components.ChartCurveMonotone, Fill: components.ChartFillGradient, StrokeWidth: 1.5,
		XAxis: components.ChartAxis{Hidden: true, Linear: true, Fixed: true, Min: last - 60, Max: last},
		YAxis: axis, HideTooltip: true, Label: label, Version: p.version,
	}, &cache.state)
	c.Unclip()
}
