package workbench

import (
	"fmt"
	"net"
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/monitoring"
	"motty/internal/remotestats"
	"motty/internal/workbench/icons"
)

func (s *State) dashboardLiveColor(running bool) ui.Color {
	if !running {
		return s.pal.muted
	}
	green, _, _ := s.dashboardColors()
	return green
}
func (s *State) dashboardColors() (ui.Color, ui.Color, ui.Color) {
	if s.pal.dark {
		return dashboardGreen, dashboardBlue, dashboardAmber
	}
	return ui.RGB(12, 122, 91), ui.RGB(27, 105, 179), ui.RGB(162, 108, 12)
}
func dashboardHeadingHeight(w float32) float32 {
	if w < 900 {
		return 76
	}
	return 44
}

func (s *State) paintDashboardHeading(c *ui.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b ui.Rect) float32 {
	pal := s.pal
	header := dashboardHeadingHeight(b.Width)
	y := b.Y
	s.paintHostCatalogIcon(c, h, ui.Rect{X: b.X + 4, Y: y + 7, Width: 28, Height: 28}, pal.workspace)
	nameW := min(float32(190), c.TextWidthWeight(h.Label, 15, ui.WeightSemibold)+4)
	monitorLabel(c, h.Label, ui.Rect{X: b.X + 42, Y: y, Width: nameW, Height: 42}, 15, pal.text, ui.WeightSemibold)
	port := h.Port
	if port == 0 {
		port = 22
	}
	address := net.JoinHostPort(h.Address, strconv.Itoa(int(port)))
	addrX := b.X + 42 + nameW + 12
	addrW := min(float32(220), c.TextWidthWeight(address, 12, ui.WeightRegular)+4)
	if b.Width < 600 {
		addrW = min(addrW, max(90, b.Width-nameW-114))
	}
	monitorLabel(c, address, ui.Rect{X: addrX, Y: y, Width: addrW, Height: 42}, 12, pal.muted, ui.WeightRegular)
	tabsX, tabsY := addrX+addrW+16, y+5
	if b.Width < 900 {
		tabsX = b.X + 4
		tabsY = y + 40
	}
	options := s.dashboardViewOptions(h.ID, false)
	tabsW := float32(210)
	for _, option := range options {
		tabsW = max(tabsW, (c.TextWidthWeight(option.Label, 12, ui.WeightMedium)+44)*3)
	}
	theme := s.Theme().WithFontSize(12).WithControlHeight(30)
	components.BeginTabs(c, theme, ui.Rect{X: tabsX, Y: tabsY, Width: tabsW, Height: 30}, options[:], options[r.page].ID).Paint()

	statusX := tabsX + tabsW + 8
	if b.Width < 900 {
		statusX = addrX + addrW + 12
	}
	statusW := min(float32(125), max(0, b.X+b.Width-44-statusX))
	s.paintDashboardState(c, p, h, r, ui.Rect{X: statusX, Y: y + 7, Width: statusW, Height: 28})
	if b.Width > 1120 {
		left := max(b.X+b.Width-302, statusX+statusW+12)
		width := b.X + b.Width - 50 - left
		if width >= 170 {
			monitorRight(c, s.messages.Dashboard.Updated+" "+r.updated, ui.Rect{X: left, Y: y + 7, Width: width, Height: 28}, 11, pal.muted, ui.WeightRegular)
		}
	}
	if h.ID != 0 {
		components.BeginButton(c, s.Theme(), hitDashboardTerminalList.At(int(h.ID)), ui.Rect{X: b.X + b.Width - 38, Y: y + 5, Width: 32}).WithIcon(lucideIcons[iconTerminal]).Icon().Ghost(s.messages.Dashboard.Terminal)
	}
	return header
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
func dashboardSimpleColumns(b ui.Rect) (float32, float32) {
	host := min(float32(260), b.Width*.24)
	return b.X + host, b.X + host + 58
}
func (s *State) paintDashboardSimpleHead(c *ui.Canvas, b ui.Rect) {
	c.Fill(b, s.pal.surfaceSoft)
	tabs, data := dashboardSimpleColumns(b)
	for _, cell := range []struct {
		x, w  float32
		label string
	}{{b.X + 8, tabs - b.X - 16, s.messages.Dashboard.Host}, {tabs, 48, s.messages.Dashboard.View}, {data, b.X + b.Width - data - 8, s.messages.Dashboard.Metrics}} {
		monitorLabel(c, cell.label, ui.Rect{X: cell.x, Y: b.Y, Width: cell.w, Height: b.Height}, 12, s.pal.muted, ui.WeightMedium)
	}
}
func (s *State) paintDashboardSimpleTabs(c *ui.Canvas, h Host, r *dashboardRow, b ui.Rect) {
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
func (s *State) paintDashboardProblem(c *ui.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b ui.Rect) {
	if r.snapshot.Status >= monitoring.Failed || s.dashboard.running[p.ID] && r.snapshot.Status != monitoring.Live {
		s.paintDashboardState(c, p, h, r, b)
	}
}
func (s *State) paintDashboardDeviceView(c *ui.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b ui.Rect) {
	m, pal := s.messages.Dashboard, s.pal
	label := m.Network
	metric := 2
	if r.page == 1 {
		label = s.messages.Monitor.PrimaryDisk
		metric = 3
	}
	chart := ui.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: 170}
	if s.dashboardShown(chart) {
		c.RoundedBorder(chart, 7, 1, pal.line, pal.workspace)
		c.DrawIcon([]ui.Icon{icons.Wifi, icons.HardDrive}[r.page%2], ui.Rect{X: chart.X + 12, Y: chart.Y + 11, Width: 14, Height: 14}, pal.muted)
		monitorLabel(c, label, ui.Rect{X: chart.X + 34, Y: chart.Y + 4, Width: 180, Height: 28}, 13, pal.text, ui.WeightMedium)
		s.paintDashboardChart(c, p, h, r, ui.Rect{X: chart.X + 8, Y: chart.Y + 33, Width: chart.Width - 16, Height: chart.Height - 36}, metric, label)
	}
	y := b.Y + 178
	if r.snapshot.DetailsFailed {
		monitorLabel(c, s.messages.Monitor.DetailsUnavailable, ui.Rect{X: b.X + 12, Y: y, Width: b.Width - 24, Height: 42}, 12, pal.warning, ui.WeightRegular)
		return
	}
	if r.page == 1 {
		positions := []float32{b.X + 12, b.X + b.Width*.36, b.X + b.Width*.52, b.X + b.Width*.68, b.X + b.Width*.84, b.X + b.Width - 10}
		for i, label := range []string{m.Mount, m.Used, m.Total, m.Available, s.messages.Dashboard.Disk} {
			monitorLabel(c, label, ui.Rect{X: positions[i], Y: y, Width: positions[i+1] - positions[i] - 8, Height: 28}, 12, pal.muted, ui.WeightMedium)
		}
		y += 30
		for _, disk := range r.snapshot.Details.Disks {
			if !s.dashboardShown(ui.Rect{Y: y, Height: 32}) {
				y += 38
				continue
			}
			color := pal.text
			if disk.Virtual {
				color = pal.muted
			}
			for i, value := range []string{disk.Path, monitorBytes(disk.Used), monitorBytes(disk.Total), monitorBytes(disk.Available), fmt.Sprintf("%.0f%%", disk.Percent)} {
				monitorLabel(c, value, ui.Rect{X: positions[i], Y: y, Width: positions[i+1] - positions[i] - 10, Height: 32}, 13, color, ui.WeightRegular)
			}
			y += 38
		}
	} else {
		x := dashboardDeviceColumns(b.X, b.Width)
		labels := []string{m.Interface, m.Receive, m.Send, m.ReceivedTotal, m.SentTotal}
		for i, label := range labels {
			monitorLabel(c, label, ui.Rect{X: x[i], Y: y, Width: x[i+1] - x[i] - 8, Height: 28}, 12, pal.muted, ui.WeightMedium)
		}
		y += 30
		for _, n := range r.snapshot.Details.Networks {
			if !s.dashboardShown(ui.Rect{Y: y, Height: 32}) {
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
				monitorLabel(c, value, ui.Rect{X: x[i], Y: y, Width: x[i+1] - x[i] - 8, Height: 32}, 13, pal.text, ui.WeightRegular)
			}
			y += 38
		}
	}
	c.Fill(ui.Rect{X: b.X, Y: b.Y + b.Height - 6, Width: b.Width, Height: 1}, pal.line)
}

func (s *State) dashboardViewOptions(host uint32, simple bool) [3]components.TabOption {
	labels := [3]string{s.messages.Monitor.Overview, s.messages.Monitor.Disks, s.messages.Monitor.Network}
	var out [3]components.TabOption
	for i, label := range labels {
		id := hitDashboardViewList.At(int(host)*3 + i)
		if simple {
			id = hitDashboardSimpleViewList.At(int(host)*3 + i)
		}
		out[i] = components.TabOption{ID: id, Label: label, Icon: []ui.Icon{icons.Activity, icons.HardDrive, icons.Wifi}[i], Disabled: host == 0}
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
