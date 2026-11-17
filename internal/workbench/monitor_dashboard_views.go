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

func (s *State) paintDashboardHeading(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) float32 {
	pal := s.pal
	source := s.dashboard.sources[h.CatalogID]
	canSudo := source != nil && source.session.phase == sshconn.PhaseConnected && source.session.host.System != sshconn.SystemWindows
	header := dashboardHeadingHeight(b.Width)
	y := b.Y
	s.paintHostCatalogIcon(c, h, gfx.Rect{X: b.X + 4, Y: y + 7, Width: 28, Height: 28}, pal.workspace)
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
	tabsX, tabsY := addrX+addrW+16, y+5
	if b.Width < 900 {
		tabsX = b.X + 4
		tabsY = y + 40
	}
	options := s.dashboardViewOptions(h.ID, false)
	tabsW := float32(210)
	for _, option := range options {
		tabsW = max(tabsW, (c.TextWidthWeight(option.Label, 12, gfx.WeightMedium)+44)*3)
	}
	theme := s.Theme().WithFontSize(12).WithControlHeight(30)
	components.BeginTabs(c, theme, gfx.Rect{X: tabsX, Y: tabsY, Width: tabsW, Height: 30}, options[:], options[r.page].ID).Paint()

	statusX := tabsX + tabsW + 8
	if b.Width < 900 {
		statusX = addrX + addrW + 12
	}
	statusEnd := b.X + b.Width - 44
	if canSudo {
		statusEnd -= 36
	}
	statusW := min(float32(125), max(0, statusEnd-statusX))
	s.paintDashboardState(c, p, h, r, gfx.Rect{X: statusX, Y: y + 7, Width: statusW, Height: 28})
	if b.Width > 1120 {
		left := max(b.X+b.Width-302, statusX+statusW+12)
		width := b.X + b.Width - 50 - left
		if canSudo {
			width -= 36
		}
		if width >= 170 {
			monitorRight(c, s.messages.Dashboard.Updated+" "+r.updated, gfx.Rect{X: left, Y: y + 7, Width: width, Height: 28}, 11, pal.muted, gfx.WeightRegular)
		}
	}
	if h.ID != 0 {
		components.BeginButton(c, s.Theme(), hitDashboardTerminalList.At(int(h.ID)), gfx.Rect{X: b.X + b.Width - 38, Y: y + 5, Width: 32}).WithIcon(lucideIcons[iconTerminal]).Icon().Ghost(s.messages.Dashboard.Terminal)
		if canSudo {
			s.paintDashboardSudo(c, h.ID, source, gfx.Rect{X: b.X + b.Width - 74, Y: y + 5, Width: 32})
		}
	}
	return header
}

func (s *State) paintDashboardSudo(c *gfx.Canvas, hostID uint32, source *dashboardSource, bounds gfx.Rect) {
	label := s.messages.DashboardSudoEnable
	if source.session.sudoCommands {
		label = s.messages.DashboardSudoDisable
	}
	components.BeginButton(c, s.Theme(), hitDashboardSudoList.At(int(hostID)), bounds).WithIcon(lucideIcons[iconLock]).Icon().Ghost(label)
}
func dashboardSimpleHeight(r *dashboardRow) float32 {
	n := 0
	if r.simplePage == 1 {
		n = len(r.disks)
	}
	if r.simplePage == 2 {
		n = len(r.snapshot.Details.Networks)
	}
	return max(104, 40+float32(n)*30)
}
func dashboardSimpleColumns(b gfx.Rect) (float32, float32) {
	host := min(float32(260), b.Width*.24)
	return b.X + host, b.X + host + 58
}
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
func (s *State) paintDashboardSimpleTabs(c *gfx.Canvas, h Host, r *dashboardRow, b gfx.Rect) {
	options := s.dashboardViewOptions(h.ID, true)
	for i := range options {
		options[i].IconOnly = true
	}
	theme := s.Theme()
	theme.Muted = withAlpha(s.pal.surfaceSoft, .12)
	theme.Background = withAlpha(s.pal.accent, .12)
	theme.Input = withAlpha(s.pal.accent, .24)
	components.BeginTabs(c, theme, b, options[:], options[r.simplePage].ID).Vertical().Paint()
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

func (s *State) dashboardViewOptions(host uint32, simple bool) [3]components.TabOption {
	labels := [3]string{s.messages.Monitor.Overview, s.messages.Monitor.Disks, s.messages.Monitor.Network}
	var out [3]components.TabOption
	for i, label := range labels {
		id := hitDashboardViewList.At(int(host)*3 + i)
		if simple {
			id = hitDashboardSimpleViewList.At(int(host)*3 + i)
		}
		out[i] = components.TabOption{ID: id, Label: label, Icon: []gfx.Icon{icons.Activity, icons.HardDrive, icons.Wifi}[i], Disabled: host == 0}
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
