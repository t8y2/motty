package workbench

import (
	"fmt"
	"math"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/monitoring"
	"motty/internal/sshconn"
)

// The second column belongs to each host. Mixed views can share one compact
// list without changing another host's selection or its collection lifetime.
func (s *State) paintDashboardSimpleRow(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) {
	tabs, data := dashboardSimpleColumns(b)
	s.paintDashboardIdentity(c, h, gfx.Rect{X: b.X + 8, Y: b.Y + 17, Width: tabs - b.X - 20, Height: 55})
	s.paintDashboardProblem(c, p, h, r, gfx.Rect{X: b.X + 54, Y: b.Y + 75, Width: tabs - b.X - 64, Height: 18})
	s.paintDashboardSimpleTabs(c, h, r, gfx.Rect{X: tabs, Y: b.Y + 7, Width: 40, Height: 90})
	metrics := gfx.Rect{X: data, Y: b.Y, Width: b.X + b.Width - data - 48, Height: b.Height}
	if r.simplePage == 0 {
		s.paintDashboardSimpleMetrics(c, r, metrics)
	} else {
		s.paintDashboardSimpleDeviceMetrics(c, r, metrics)
	}
	if h.ID != 0 {
		components.BeginButton(c, s.Theme(), hitDashboardTerminalList.At(int(h.ID)), gfx.Rect{X: b.X + b.Width - 38, Y: b.Y + 24, Width: 32}).WithIcon(lucideIcons[iconTerminal]).Icon().Ghost(s.messages.Dashboard.Terminal)
		if source := s.dashboard.sources[h.CatalogID]; source != nil && source.session.phase == sshconn.PhaseConnected && source.session.host.System != sshconn.SystemWindows {
			s.paintDashboardSudo(c, h.ID, source, gfx.Rect{X: b.X + b.Width - 74, Y: b.Y + 24, Width: 32})
		}
	}
	c.Fill(gfx.Rect{X: b.X, Y: b.Y + b.Height - 1, Width: b.Width, Height: 1}, s.pal.line)
}
func (s *State) paintDashboardSimpleMetrics(c *gfx.Canvas, r *dashboardRow, b gfx.Rect) {
	m, pal := s.messages.Dashboard, s.pal
	green, blue, amber := s.dashboardColors()
	w := b.Width / 4
	texts := [3]string{r.cpu, r.memory, "—"}
	notes := [3]string{"", r.capacity, ""}
	values := [3]float64{math.NaN(), math.NaN(), math.NaN()}
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
		texts[2] = fmt.Sprintf("%.0f%%", disk.Percent)
		notes[2] = monitorCapacity(disk.Used, disk.Total)
		values[2] = disk.Percent
	}
	colors := [3]gfx.Color{green, blue, amber}
	labels := [3]string{m.CPU, m.Memory, m.Disk}
	for i := 0; i < 3; i++ {
		x := b.X + float32(i)*w
		monitorLabel(c, labels[i], gfx.Rect{X: x, Y: b.Y + 8, Width: w - 18, Height: 18}, 12, pal.muted, gfx.WeightMedium)
		monitorLabel(c, texts[i], gfx.Rect{X: x, Y: b.Y + 29, Width: w - 18, Height: 27}, 18, pal.text, gfx.WeightSemibold)
		dashboardBar(c, s.Theme(), gfx.Rect{X: x, Y: b.Y + 61, Width: w - 20, Height: 5}, values[i], colors[i], pal.line)
		monitorLabel(c, notes[i], gfx.Rect{X: x, Y: b.Y + 72, Width: w - 18, Height: 20}, 11, pal.muted, gfx.WeightRegular)
	}
	x := b.X + 3*w
	monitorLabel(c, m.Network, gfx.Rect{X: x, Y: b.Y + 8, Width: w - 6, Height: 18}, 12, pal.muted, gfx.WeightMedium)
	monitorLabel(c, "↓ "+r.down, gfx.Rect{X: x, Y: b.Y + 31, Width: w - 6, Height: 24}, 12, green, gfx.WeightMedium)
	monitorLabel(c, "↑ "+r.up, gfx.Rect{X: x, Y: b.Y + 60, Width: w - 6, Height: 24}, 12, blue, gfx.WeightMedium)
}
func (s *State) paintDashboardSimpleDeviceMetrics(c *gfx.Canvas, r *dashboardRow, b gfx.Rect) {
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
