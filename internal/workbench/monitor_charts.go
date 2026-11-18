package workbench

import (
	"github.com/vivaui/viva/gfx"
)

const (
	monitorCPU = iota
	monitorMemory
	monitorDisk
	monitorDownload
)

type monitorChart struct {
	series [2]monitorChartSeries
	values [2][]float32
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
		cache.series[i].color, cache.series[i].label = tint, name
	}
	cache.series[0].values = values[metric][start:p.count]
	if count == 2 {
		cache.series[1].values = p.up[start:p.count]
	}
	options := monitorChartOptions{hiddenAxes: true, label: label, focused: c.Focused() == monitorChartHits[metric]}
	if metric <= monitorDisk {
		options.fixedY, options.yMin, options.yMax = true, 0, 100
	}
	options.xMin, options.xMax = last-60, last
	c.Clip(b)
	paintMonitorTrend(c, b, monitorChartHits[metric], p.x[start:p.count], cache.series[:count], options)
	c.Unclip()
}
