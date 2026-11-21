package workbench

import (
	"fmt"
	"net"
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/monitoring"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
)

func (s *State) dashboardLiveColor(running bool) gfx.Color {
	if !running {
		return s.pal.muted
	}
	green, _, _ := s.dashboardColors()
	return green
}
func (s *State) dashboardColors() (gfx.Color, gfx.Color, gfx.Color) {
	if s.pal.dark {
		return dashboardGreen, dashboardBlue, dashboardAmber
	}
	return gfx.RGB(12, 122, 91), gfx.RGB(27, 105, 179), gfx.RGB(162, 108, 12)
}
func dashboardHeadingHeight(w float32) float32 {
	if w < 900 {
		return 76
	}
	return 44
}

// paintDashboardHeading paints a card's identity, its status and its freshness.
// The view tabs and the terminal, sudo and retry actions are page controls
// drawn over the same place.
func (s *State) paintDashboardIdentity(c *gfx.Canvas, h Host, b gfx.Rect) {
	s.paintHostCatalogIcon(c, h, gfx.Rect{X: b.X, Y: b.Y + 9, Width: 34, Height: 34}, s.pal.workspace)
	monitorLabel(c, h.Label, gfx.Rect{X: b.X + 46, Y: b.Y + 3, Width: b.Width - 50, Height: 27}, 16, s.pal.text, gfx.WeightSemibold)
	monitorLabel(c, h.Address, gfx.Rect{X: b.X + 46, Y: b.Y + 30, Width: b.Width - 50, Height: 22}, 12, s.pal.muted, gfx.WeightRegular)
}

func (s *State) paintDashboardHeading(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) float32 {
	pal := s.pal
	source := s.dashboard.sources[h.CatalogID]
	canSudo := source != nil && source.session.phase == sshconn.PhaseConnected && source.session.host.System != sshconn.SystemWindows
	header := dashboardHeadingHeight(b.Width)
	y := b.Y
	s.paintHostCatalogIcon(c, h, gfx.Rect{X: b.X + 4, Y: y + 7, Width: 28, Height: 28}, pal.workspace)
	rects := s.dashboardHeadingRects(c, h, b)
	nameW := min(float32(190), c.TextWidthWeight(h.Label, 15, gfx.WeightSemibold)+4)
	monitorLabel(c, h.Label, gfx.Rect{X: b.X + 42, Y: y, Width: nameW, Height: 42}, 15, pal.text, gfx.WeightSemibold)
	port := h.Port
	if port == 0 {
		port = 22
	}
	address := net.JoinHostPort(h.Address, strconv.Itoa(int(port)))
	addrX := b.X + 42 + nameW + 12
	addrW := min(float32(220), c.TextWidthWeight(address, 12, gfx.WeightRegular)+4)
	if b.Width < 600 {
		reserve := float32(114)
		if canSudo {
			reserve += 36
		}
		addrW = min(addrW, max(90, b.Width-nameW-reserve))
	}
	monitorLabel(c, address, gfx.Rect{X: addrX, Y: y, Width: addrW, Height: 42}, 12, pal.muted, gfx.WeightRegular)
	s.paintDashboardState(c, p, h, r, rects.retry)
	if b.Width > 1120 {
		left := max(b.X+b.Width-302, rects.tabs.X+rects.tabs.Width+12+125+12)
		width := b.X + b.Width - 50 - left
		if canSudo {
			width -= 36
		}
		if width >= 170 {
			monitorRight(c, s.messages.Dashboard.Updated+" "+r.updated, gfx.Rect{X: left, Y: y + 7, Width: width, Height: 28}, 11, pal.muted, gfx.WeightRegular)
		}
	}
	return header
}

func (s *State) paintDashboardState(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) {
	dashboardGreen, _, _ := s.dashboardColors()
	running := s.dashboard.running[p.ID]
	if !running && r.snapshot.Status != monitoring.Missing {
		return
	}
	if dashboardRetryShown(s, p, h, r) {
		return
	}
	label := s.dashboardStatus(r.snapshot.Status, running, r.snapshot.HasSample)
	if r.snapshot.Status == monitoring.Missing {
		label = s.messages.Dashboard.Missing
	}
	color := s.pal.muted
	if running && r.snapshot.Status == monitoring.Live {
		color = dashboardGreen
	}
	if running && r.snapshot.Status >= monitoring.Failed {
		color = s.pal.warning
	}
	monitorLabel(c, label, b, 12, color, gfx.WeightMedium)
}

// A simple row's heading band is as tall as its shortest row. A disk or network
// table grows the row below that band, where the view strip stays at its top.
const (
	dashboardSimpleRowLeast = float32(116)
	dashboardSimpleTabInset = float32(7)
)

func dashboardSimpleHeight(r *dashboardRow) float32 {
	n := 0
	if r.simplePage == 1 {
		n = len(r.disks)
	}
	if r.simplePage == 2 {
		n = len(r.snapshot.Details.Networks)
	}
	return max(dashboardSimpleRowLeast, 40+float32(n)*30)
}
func dashboardSimpleColumns(b gfx.Rect) (float32, float32) {
	host := min(float32(260), b.Width*.24)
	return b.X + host, b.X + host + 58
}
func dashboardDeviceColumns(x, w float32) [7]float32 {
	return [7]float32{x + 8, x + w*.26, x + w*.45, x + w*.59, x + w*.73, x + w*.86, x + w - 8}
}
func (s *State) paintDashboardProblem(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) {
	if r.snapshot.Status >= monitoring.Failed || s.dashboard.running[p.ID] && r.snapshot.Status != monitoring.Live {
		s.paintDashboardState(c, p, h, r, b)
	}
}
func (s *State) paintDashboardDeviceView(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) {
	m, pal := s.messages.Dashboard, s.pal
	label := m.Network
	metric := 2
	if r.page == 1 {
		label = s.messages.Monitor.PrimaryDisk
		metric = 3
	}
	chart := gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: 170}
	if s.dashboardShown(chart) {
		c.RoundedBorder(chart, 7, 1, pal.line, pal.workspace)
		c.DrawIcon([]gfx.Icon{icons.Wifi, icons.HardDrive}[r.page%2], gfx.Rect{X: chart.X + 12, Y: chart.Y + 11, Width: 14, Height: 14}, pal.muted)
		monitorLabel(c, label, gfx.Rect{X: chart.X + 34, Y: chart.Y + 4, Width: 180, Height: 28}, 13, pal.text, gfx.WeightMedium)
		s.paintDashboardChart(c, p, h, r, gfx.Rect{X: chart.X + 8, Y: chart.Y + 33, Width: chart.Width - 16, Height: chart.Height - 36}, metric, label)
	}
	y := b.Y + 178
	if r.snapshot.DetailsFailed {
		monitorLabel(c, s.messages.Monitor.DetailsUnavailable, gfx.Rect{X: b.X + 12, Y: y, Width: b.Width - 24, Height: 42}, 12, pal.warning, gfx.WeightRegular)
		return
	}
	if r.page == 1 {
		positions := []float32{b.X + 12, b.X + b.Width*.36, b.X + b.Width*.52, b.X + b.Width*.68, b.X + b.Width*.84, b.X + b.Width - 10}
		for i, label := range []string{m.Mount, m.Used, m.Total, m.Available, s.messages.Dashboard.Disk} {
			monitorLabel(c, label, gfx.Rect{X: positions[i], Y: y, Width: positions[i+1] - positions[i] - 8, Height: 28}, 12, pal.muted, gfx.WeightMedium)
		}
		y += 30
		for _, disk := range r.snapshot.Details.Disks {
			if !s.dashboardShown(gfx.Rect{Y: y, Height: 32}) {
				y += 38
				continue
			}
			color := pal.text
			if disk.Virtual {
				color = pal.muted
			}
			for i, value := range []string{disk.Path, monitorBytes(disk.Used), monitorBytes(disk.Total), monitorBytes(disk.Available), fmt.Sprintf("%.0f%%", disk.Percent)} {
				monitorLabel(c, value, gfx.Rect{X: positions[i], Y: y, Width: positions[i+1] - positions[i] - 10, Height: 32}, 13, color, gfx.WeightRegular)
			}
			y += 38
		}
	} else {
		x := dashboardDeviceColumns(b.X, b.Width)
		labels := []string{m.Interface, m.Receive, m.Send, m.ReceivedTotal, m.SentTotal}
		for i, label := range labels {
			monitorLabel(c, label, gfx.Rect{X: x[i], Y: y, Width: x[i+1] - x[i] - 8, Height: 28}, 12, pal.muted, gfx.WeightMedium)
		}
		y += 30
		for _, n := range r.snapshot.Details.Networks {
			if !s.dashboardShown(gfx.Rect{Y: y, Height: 32}) {
				y += 38
				continue
			}
			down, up := "—", "—"
			if n.HasRate {
				down = dashboardRate(n.ReceivePerSecond)
				up = dashboardRate(n.SendPerSecond)
			}
			rx, tx := dashboardNetworkTotals(n)
			for i, value := range []string{n.Name, down, up, rx, tx} {
				monitorLabel(c, value, gfx.Rect{X: x[i], Y: y, Width: x[i+1] - x[i] - 8, Height: 32}, 13, pal.text, gfx.WeightRegular)
			}
			y += 38
		}
	}
	c.Fill(gfx.Rect{X: b.X, Y: b.Y + b.Height - 6, Width: b.Width, Height: 1}, pal.line)
}

// dashboardViewIcons is the leading glyph of each host view: overview, disks
// and network, in the order the view strip declares them.
func dashboardViewIcons(index int) gfx.Icon {
	if index < 0 || index >= 3 {
		return gfx.Icon{}
	}
	return [...]gfx.Icon{icons.Gauge, icons.HardDrive, icons.Wifi}[index]
}

// dashboardViewOptions is the host view strip; the page declares it as ui.Tabs
// and the window's key handler still uses Viva's tab navigation.
func (s *State) dashboardViewOptions(host uint32, simple bool) [3]components.TabOption {
	labels := [3]string{s.messages.Monitor.Overview, s.messages.Monitor.Disks, s.messages.Monitor.Network}
	var out [3]components.TabOption
	for i, label := range labels {
		id := hitDashboardViewList.At(int(host)*3 + i)
		if simple {
			id = hitDashboardSimpleViewList.At(int(host)*3 + i)
		}
		out[i] = components.TabOption{ID: id, Label: label, Icon: []gfx.Icon{icons.Gauge, icons.HardDrive, icons.Wifi}[i], Disabled: host == 0}
	}
	return out
}

func (s *State) dashboardDiskMessage(r *dashboardRow) string {
	if r.snapshot.DetailsFailed {
		return s.messages.Monitor.DetailsUnavailable
	}
	if r.snapshot.Status >= monitoring.Failed && !r.snapshot.HasSample {
		return s.dashboardStatus(r.snapshot.Status, true, false)
	}
	if r.snapshot.Details.CPUCount > 0 {
		return s.messages.Monitor.NoDisks
	}
	return s.messages.Monitor.WaitingDetails
}

func dashboardNetworkTotals(n remotestats.Network) (string, string) {
	if n.TotalsUnavailable {
		return "—", "—"
	}
	return monitorBytes(n.Received), monitorBytes(n.Sent)
}
