package workbench

import (
	"fmt"
	"math"

	"github.com/vivaui/viva/gfx"
	"motty/internal/monitoring"
)

// paintDashboardSimpleHead paints the simple view's column headings. It is part
// of the scrolled content, at the same place the hand-painted table put it.
func (s *State) paintDashboardSimpleHead(c *gfx.Canvas, b gfx.Rect) {
	c.Fill(b, s.pal.surfaceSoft)
	tabs, data := dashboardSimpleColumns(b)
	for _, cell := range []struct {
		x, w  float32
		label string
	}{{b.X + 8, tabs - b.X - 16, s.messages.Dashboard.Host}, {tabs, 48, s.messages.Dashboard.View}, {data, b.X + b.Width - data - 8, s.messages.Dashboard.Metrics}} {
		monitorLabel(c, cell.label, gfx.Rect{X: cell.x, Y: b.Y, Width: cell.w, Height: b.Height}, 12, s.pal.muted, gfx.WeightMedium)
	}
}

// The second column belongs to each host. Mixed views can share one compact
// list without changing another host's selection or its collection lifetime.
func (s *State) paintDashboardSimpleRow(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) {
	tabs, data := dashboardSimpleColumns(b)
	s.paintDashboardIdentity(c, h, gfx.Rect{X: b.X + 8, Y: b.Y + 17, Width: tabs - b.X - 20, Height: 55})
	s.paintDashboardProblem(c, p, h, r, gfx.Rect{X: b.X + 54, Y: b.Y + 75, Width: tabs - b.X - 64, Height: 18})
	metrics := gfx.Rect{X: data, Y: b.Y, Width: b.X + b.Width - data - 48, Height: b.Height}
	if r.simplePage == 0 {
		s.paintDashboardSimpleMetrics(c, r, metrics)
	} else {
		s.paintDashboardSimpleDeviceMetrics(c, r, metrics)
	}
	c.Fill(gfx.Rect{X: b.X, Y: b.Y + b.Height - 1, Width: b.Width, Height: 1}, s.pal.line)
}
func (s *State) paintDashboardSimpleMetrics(c *gfx.Canvas, r *dashboardRow, b gfx.Rect) {
	m, pal := s.messages.Dashboard, s.pal
	green, blue, amber := s.dashboardColors()
	texts := [5]string{r.cpu, r.memory, "", "—", "—"}
	notes := [5]string{"", r.capacity, "", "", ""}
	values := [5]float64{math.NaN(), math.NaN(), math.NaN(), math.NaN(), math.NaN()}
	if r.snapshot.HasSample {
		if !r.snapshot.Sample.CPUUnavailable {
			values[0] = r.snapshot.Sample.CPUPercent
		}
		if r.snapshot.Sample.MemoryTotalBytes > 0 {
			values[1] = 100 * float64(r.snapshot.Sample.MemoryUsedBytes) / float64(r.snapshot.Sample.MemoryTotalBytes)
		}
	}
	if len(r.disks) > 0 && !r.snapshot.DetailsFailed {
		disk := r.disks[0]
		texts[3] = fmt.Sprintf("%.0f%%", disk.Percent)
		notes[3] = monitorCapacity(disk.Used, disk.Total)
		values[3] = disk.Percent
	}
	if g, present := r.gpu(); len(r.snapshot.GPUs) > 0 {
		texts[4] = dashboardGPUPercent(g, present)
		memory, _, _, _ := dashboardGPUReading(g, present)
		notes[4] = s.messages.Monitor.GPUMemory + " " + memory
		if present && g.UtilPercent >= 0 {
			values[4] = g.UtilPercent
		}
	}
	labels := [5]string{m.CPU, m.Memory, m.Network, m.Disk, s.messages.Monitor.GPU}
	colors := [5]gfx.Color{green, blue, green, amber, green}
	count := 0
	for _, shown := range r.visible {
		if shown {
			count++
		}
	}
	if count == 0 {
		monitorLabel(c, m.NoDisplay, b, 12, pal.muted, gfx.WeightRegular)
		return
	}
	width := b.Width / float32(count)
	n := 0
	for _, metric := range [5]int{0, 1, 3, 2, 4} {
		if !r.visible[metric] {
			continue
		}
		x := b.X + float32(n)*width
		n++
		monitorLabel(c, labels[metric], gfx.Rect{X: x, Y: b.Y + 8, Width: width - 12, Height: 18}, 12, pal.muted, gfx.WeightMedium)
		if metric == 2 {
			monitorLabel(c, "↓ "+r.down, gfx.Rect{X: x, Y: b.Y + 31, Width: width - 8, Height: 24}, 12, green, gfx.WeightMedium)
			monitorLabel(c, "↑ "+r.up, gfx.Rect{X: x, Y: b.Y + 60, Width: width - 8, Height: 24}, 12, blue, gfx.WeightMedium)
		} else {
			offset := float32(0)
			if metric == 3 && len(r.disks) > 0 && !r.snapshot.DetailsFailed {
				monitorLabel(c, r.disks[0].Path+" · "+fmt.Sprintf(m.MountCount, len(r.disks)), gfx.Rect{X: x, Y: b.Y + 26, Width: width - 14, Height: 14}, 10, pal.muted, gfx.WeightRegular)
				offset = 14
			}
			monitorLabel(c, texts[metric], gfx.Rect{X: x, Y: b.Y + 29 + offset, Width: width - 14, Height: 27}, 18, pal.text, gfx.WeightSemibold)
			dashboardBar(c, gfx.Rect{X: x, Y: b.Y + 61 + offset, Width: width - 20, Height: 5}, values[metric], colors[metric], pal.line)
			monitorLabel(c, notes[metric], gfx.Rect{X: x, Y: b.Y + 72 + offset, Width: width - 14, Height: 20}, 11, pal.muted, gfx.WeightRegular)
		}
	}
}

func (s *State) paintDashboardSimpleDeviceMetrics(c *gfx.Canvas, r *dashboardRow, b gfx.Rect) {
	if r.simplePage == 3 {
		s.paintDashboardGPUSimple(c, r, b)
		return
	}
	m, pal := s.messages.Dashboard, s.pal
	if r.snapshot.DetailsFailed {
		monitorLabel(c, s.messages.Monitor.DetailsUnavailable, gfx.Rect{X: b.X, Y: b.Y + 22, Width: b.Width, Height: 45}, 12, pal.warning, gfx.WeightRegular)
		return
	}
	labels := []string{m.Mount, m.Usage, m.Used, m.Total, m.Available}
	if r.simplePage == 2 {
		labels = []string{m.Interface, m.Receive, m.Send, m.ReceivedTotal, m.SentTotal}
	}
	widths := [6]float32{0, .24, .43, .62, .81, 1}
	for i, label := range labels {
		monitorLabel(c, label, gfx.Rect{X: b.X + b.Width*widths[i], Y: b.Y + 8, Width: b.Width*(widths[i+1]-widths[i]) - 10, Height: 18}, 11, pal.muted, gfx.WeightMedium)
	}
	n := len(r.disks)
	if r.simplePage == 2 {
		n = len(r.snapshot.Details.Networks)
	}
	for row := 0; row < n; row++ {
		var cells [5]string
		if r.simplePage == 1 {
			disk := r.disks[row]
			cells = [5]string{disk.Path, fmt.Sprintf("%.0f%%", disk.Percent), monitorBytes(disk.Used), monitorBytes(disk.Total), monitorBytes(disk.Available)}
		} else {
			network := r.snapshot.Details.Networks[row]
			down, up := "—", "—"
			if network.HasRate {
				down = dashboardRate(network.ReceivePerSecond)
				up = dashboardRate(network.SendPerSecond)
			}
			rx, tx := dashboardNetworkTotals(network)
			cells = [5]string{network.Name, down, up, rx, tx}
		}
		for i, value := range cells {
			monitorLabel(c, value, gfx.Rect{X: b.X + b.Width*widths[i], Y: b.Y + 30 + float32(row)*30, Width: b.Width*(widths[i+1]-widths[i]) - 10, Height: 26}, 12, pal.text, gfx.WeightRegular)
		}
	}
	if n == 0 {
		label := "—"
		if r.snapshot.Status >= monitoring.Failed {
			label = s.dashboardStatus(r.snapshot.Status, true, r.snapshot.HasSample)
		}
		monitorLabel(c, label, gfx.Rect{X: b.X, Y: b.Y + 31, Width: b.Width, Height: 32}, 12, pal.muted, gfx.WeightRegular)
	}
}
