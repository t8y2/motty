package workbench

import (
	"fmt"
	"math"
	"net"
	"strconv"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/monitoring"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
)

type dashboardRow struct {
	page                                            int
	simplePage                                      int
	chartIDs                                        [4]gfx.ID
	snapshot                                        monitoring.Snapshot
	width                                           int
	x                                               []float32
	values                                          [5][]float32
	charts                                          [4]dashboardChart
	origin                                          time.Time
	clock                                           func([]byte, float32) []byte
	label, cpu, memory, capacity, down, up, updated string
	disks                                           []remotestats.Disk
	visible                                         [5]bool
	graphs                                          [4]int
	graphCount                                      int
	infoOpen                                        bool
	gpuKey, gpuPrepared                             string
	gpuChoice                                       int
	gpuX                                            []float32
	gpuValues                                       [2][]float32
	gpuCharts                                       [2]dashboardChart
	gpuChartIDs                                     [2]gfx.ID
}

type dashboardChart struct {
	monitorChart
	canvas   gfx.Canvas
	version  uint64
	size     gfx.Size
	theme    gfx.Theme
	language i18n.Language
	scale    float32
	focused  bool
}

var dashboardGreen = gfx.RGB(38, 214, 163)
var dashboardBlue = gfx.RGB(99, 180, 248)
var dashboardAmber = gfx.RGB(239, 180, 64)

func (s *State) dashboardRow(p *monitoring.Panel, id string, width int) *dashboardRow {
	d := &s.dashboard
	if d.rows == nil {
		d.rows = make(map[string]*dashboardRow)
	}
	key := p.ID + "/" + id
	r := d.rows[key]
	if r == nil {
		r = &dashboardRow{}
		for i := range r.chartIDs {
			r.chartIDs[i] = tree.ID(dashboardHits, fmt.Sprintf("chart-%s-%s-%d", p.ID, id, i))
		}
		for i := range r.gpuChartIDs {
			r.gpuChartIDs[i] = tree.ID(dashboardHits, fmt.Sprintf("gpu-chart-%s-%s-%d", p.ID, id, i))
		}
		d.rows[key] = r
	}
	version := r.snapshot.Version
	if width != r.width {
		version = 0
	}
	if snap, changed := d.engine.Read(p.ID, id, max(1, width/8), version); changed {
		r.snapshot = snap
		r.gpuPrepared = ""
		r.width = width
		r.cpu = "—"
		r.memory = "—"
		r.capacity = ""
		r.down = "—"
		r.up = "—"
		r.updated = ""
		if snap.HasSample {
			if !snap.Sample.CPUUnavailable {
				r.cpu = fmt.Sprintf("%.0f%%", snap.Sample.CPUPercent)
			}
			if snap.Sample.MemoryTotalBytes > 0 {
				r.memory = fmt.Sprintf("%.0f%%", 100*float64(snap.Sample.MemoryUsedBytes)/float64(snap.Sample.MemoryTotalBytes))
				r.capacity = monitorCapacity(snap.Sample.MemoryUsedBytes, snap.Sample.MemoryTotalBytes)
			}
			if !snap.Sample.NetworkUnavailable {
				r.down = dashboardRate(snap.Sample.ReceiveBytesPerSecond)
				r.up = dashboardRate(snap.Sample.SendBytesPerSecond)
			}
			r.updated = snap.Updated.Format("2006-01-02 15:04:05")
		}
		r.disks = r.disks[:0]
		for _, disk := range snap.Details.Disks {
			if !disk.Virtual {
				r.disks = append(r.disks, disk)
			}
		}
		r.x = r.x[:0]
		for i := range r.values {
			r.values[i] = r.values[i][:0]
		}
		if len(snap.Points) > 0 {
			last := snap.Points[len(snap.Points)-1].At
			available := last.Sub(snap.Points[0].At)
			duration := min(time.Duration(p.Retention)*time.Minute, max(time.Minute, time.Duration(p.Interval*2)*time.Second, available))
			r.origin = last.Add(-duration)
			for _, point := range snap.Points {
				r.x = append(r.x, float32(float64(point.At.Sub(r.origin))/float64(duration)))
				for i, v := range point.Values {
					if i == 2 || i == 3 {
						v /= 1024
					}
					r.values[i] = append(r.values[i], v)
				}
			}
			origin := r.origin
			layout := "15:04"
			if duration <= 5*time.Minute {
				layout = "15:04:05"
			}
			if duration >= 24*time.Hour {
				layout = "01-02 15:04"
			}
			r.clock = func(dst []byte, v float32) []byte {
				return origin.Add(time.Duration(float64(v)*float64(duration))).AppendFormat(dst, layout)
			}
		}
	}
	r.configureDisplay(p, id)
	r.prepareGPU()
	return r
}
func dashboardRate(n uint64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MiB/s", float64(n)/(1<<20))
	}
	if n >= 1<<10 {
		return fmt.Sprintf("%.0f KiB/s", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B/s", n)
}
func dashboardPercent(dst []byte, v float32) []byte {
	dst = strconv.AppendFloat(dst, float64(v), 'f', 0, 32)
	return append(dst, '%')
}
func dashboardNetworkAxis(dst []byte, v float32) []byte {
	if v >= 1024 {
		dst = strconv.AppendFloat(dst, float64(v/1024), 'f', 0, 32)
		return append(dst, " MiB/s"...)
	}
	dst = strconv.AppendFloat(dst, float64(v), 'f', 0, 32)
	return append(dst, " KiB/s"...)
}

// declareDashboardEmpty declares the dashboard's first-run state in the
// workbench page: with no panel there is nothing else to paint there.
func (s *State) declareDashboardEmpty(c ui.C) ui.Box {
	if s.activeSection != sectionDashboard || s.dashboardPanel() != nil {
		return ui.Box{}
	}
	m := s.messages.Dashboard
	width, height := s.hostsWorkspaceSize()
	return s.declareEmptyPageRegion(c, gfx.Rect{Width: width, Height: height}, m.Empty, m.EmptyHint, m.Create, s.pal.muted, hitDashboardNew, iconGauge, true)
}

// dashboardBar paints one bounded metric as a progress bar, in the metric's
// own color over the palette's track line.
// dashboardHostHeight is a detailed card's height for a row width.
func dashboardHostHeight(r *dashboardRow, w float32) float32 {
	header := dashboardHeadingHeight(w)
	if r.page == 3 {
		return header + dashboardGPUSummaryHeight(r, w) + dashboardGPUPlotHeight(w) + 54
	}
	if r.page == 1 {
		return header + 178 + float32(max(1, len(r.snapshot.Details.Disks)))*38 + 44
	}
	if r.page == 2 {
		return header + 178 + float32(max(1, len(r.snapshot.Details.Networks)))*38 + 44
	}
	charts := float32((r.graphCount+dashboardGraphColumns(r, w)-1)/dashboardGraphColumns(r, w)) * 164
	if r.graphCount == 0 {
		charts = 44
	}
	if r.visible[4] {
		charts += dashboardGPUSummaryHeight(r, w)
	}
	disks := float32(0)
	if r.visible[3] {
		cols := dashboardDiskColumns(w, len(r.disks))
		disks = float32(max(1, (len(r.disks)+cols-1)/cols)) * 40
	}
	return header + charts + disks + 18
}

func dashboardBar(canvas *gfx.Canvas, b gfx.Rect, value float64, color, track gfx.Color) {
	fraction := float32(0)
	if !math.IsNaN(value) {
		fraction = float32(min(max(value, 0), 100) / 100)
	}
	radius := b.Height / 2
	canvas.Rounded(b, radius, track)
	if fraction <= 0 {
		return
	}
	fill := gfx.Rect{X: b.X, Y: b.Y, Width: max(b.Width*fraction, b.Height), Height: b.Height}
	canvas.Rounded(fill, radius, color)
}

// dashboardHeadingRects is the place of a detailed card's heading and of the
// controls the page declares over it: its view tabs, its terminal and sudo
// actions, and the status rect its retry action takes.
func (s *State) dashboardHeadingRects(c *gfx.Canvas, h Host, b gfx.Rect) dashboardRowControls {
	out := dashboardRowControls{}
	source := s.dashboard.sources[h.CatalogID]
	canSudo := source != nil && source.session.phase == sshconn.PhaseConnected && source.session.host.System != sshconn.SystemWindows
	y := b.Y
	displayW := float32(32)
	nameW := min(float32(190), c.TextWidthWeight(h.Label, 15, gfx.WeightSemibold)+4)
	if b.Width < 600 {
		reserve := displayW + 194
		if canSudo {
			reserve += 36
		}
		nameW = min(nameW, max(64, b.Width-reserve))
	}
	port := h.Port
	if port == 0 {
		port = 22
	}
	address := net.JoinHostPort(h.Address, strconv.Itoa(int(port)))
	addrX := b.X + 42 + nameW + 12
	addrW := min(float32(220), c.TextWidthWeight(address, 12, gfx.WeightRegular)+4)
	if b.Width < 600 {
		end := b.X + b.Width - displayW - 46
		if canSudo {
			end -= 36
		}
		addrW = min(addrW, max(0, end-addrX))
	}
	tabsX, tabsY := addrX+addrW+16, y+5
	if b.Width < 900 {
		tabsX = b.X + 4
		tabsY = y + 40
	}
	options := s.dashboardViewOptions(h.ID, false)
	tabsW := float32(210)
	count := 0
	for _, option := range options {
		if !option.Disabled {
			count++
		}
	}
	// Native tabs share the strip equally, so every slot must fit its longest
	// translated label at the control's actual font size, including its icon.
	for _, option := range options {
		if option.Disabled {
			continue
		}
		tabsW = max(tabsW, (c.TextWidthWeight(option.Label, s.Theme().Metrics.FontSizeM, gfx.WeightMedium)+44)*float32(count)+6)
	}
	statusX := tabsX + tabsW + 8
	if b.Width < 900 {
		statusX = addrX + addrW + 12
	}
	statusEnd := b.X + b.Width - displayW - 46
	if canSudo {
		statusEnd -= 36
	}
	statusW := min(float32(125), max(0, statusEnd-statusX))
	out.tabs = gfx.Rect{X: tabsX, Y: tabsY, Width: tabsW, Height: 30}
	out.retry = gfx.Rect{X: statusX, Y: y + 7, Width: statusW, Height: 28}
	out.terminal = gfx.Rect{X: b.X + b.Width - 32, Y: y + 5, Width: 32, Height: 32}
	out.display = gfx.Rect{X: out.terminal.X - 38, Y: y + 5, Width: displayW, Height: 32}
	out.sudo = gfx.Rect{X: out.display.X - 36, Y: y + 5, Width: 32, Height: 32}
	return out
}

func (s *State) paintDashboardHost(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) {
	_, _, amber := s.dashboardColors()
	m, pal := s.messages.Dashboard, s.pal
	header := s.paintDashboardHeading(c, p, h, r, b)
	body := gfx.Rect{X: b.X, Y: b.Y + header, Width: b.Width, Height: b.Height - header}
	if r.page == 3 {
		s.paintDashboardGPUView(c, r, body)
		return
	}
	if r.page != 0 {
		s.paintDashboardDeviceView(c, p, h, r, body)
		return
	}
	y := body.Y
	cols := dashboardGraphColumns(r, b.Width)
	chartW := (b.Width - float32(cols-1)*8) / float32(cols)
	labels := [5]string{m.CPU, m.Memory, m.Network, m.Disk, s.messages.Monitor.GPU}
	glyphs := [5]gfx.Icon{icons.Cpu, icons.MemoryStick, icons.Wifi, icons.HardDrive, icons.Cpu}
	for n, metric := range r.graphs[:r.graphCount] {
		box := gfx.Rect{X: b.X + float32(n%cols)*(chartW+8), Y: y + float32(n/cols)*164, Width: chartW, Height: 156}
		if !s.dashboardShown(box) {
			continue
		}
		c.RoundedBorder(box, cornerPx(7), 1, pal.line, pal.workspace)
		c.DrawIcon(glyphs[metric], gfx.Rect{X: box.X + 12, Y: box.Y + 13, Width: 14, Height: 14}, pal.muted)
		if metric != 4 {
			monitorLabel(c, labels[metric], gfx.Rect{X: box.X + 34, Y: box.Y + 6, Width: 74, Height: 28}, 13, pal.text, gfx.WeightMedium)
		}
		value := r.cpu
		font := float32(20)
		switch metric {
		case 1:
			value = r.memory
		case 2:
			value = "↓ " + r.down + "   ↑ " + r.up
			font = 12
		case 4:
			g, present := r.gpu()
			value = dashboardGPUPercent(g, present)
		}
		monitorRight(c, value, gfx.Rect{X: box.X + 85, Y: box.Y + 6, Width: max(0, box.Width-97), Height: 28}, font, pal.text, gfx.WeightSemibold)
		if metric == 1 && r.capacity != "" {
			monitorRight(c, r.capacity, gfx.Rect{X: box.X + 34, Y: box.Y + 33, Width: box.Width - 46, Height: 16}, 11, pal.muted, gfx.WeightRegular)
		}
		if metric == 4 {
			g, present := r.gpu()
			memory, temp, _, _ := dashboardGPUReading(g, present)
			monitorRight(c, s.messages.Monitor.GPUMemory+" "+memory+" · "+temp, gfx.Rect{X: box.X + 12, Y: box.Y + 33, Width: box.Width - 24, Height: 16}, 11, pal.muted, gfx.WeightRegular)
		}
		plot := gfx.Rect{X: box.X + 4, Y: box.Y + 52, Width: box.Width - 8, Height: box.Height - 56}
		if metric == 4 {
			s.paintDashboardGPUChart(c, r, plot, 0)
		} else {
			s.paintDashboardChart(c, p, h, r, plot, metric, labels[metric])
		}
	}
	if r.graphCount == 0 {
		monitorLabel(c, m.NoDisplay, gfx.Rect{X: b.X + 12, Y: y, Width: b.Width - 24, Height: 40}, 12, pal.muted, gfx.WeightRegular)
		y += 44
	} else {
		y += float32((r.graphCount+cols-1)/cols) * 164
	}
	if r.visible[4] {
		s.paintDashboardGPUSummary(c, r, gfx.Rect{X: b.X, Y: y, Width: b.Width})
		y += dashboardGPUSummaryHeight(r, b.Width)
	}
	if r.visible[3] {
		cols := dashboardDiskColumns(b.Width, len(r.disks))
		diskW := (b.Width - float32(cols-1)*8) / float32(cols)
		if len(r.disks) == 0 || r.snapshot.DetailsFailed {
			monitorLabel(c, s.dashboardDiskMessage(r), gfx.Rect{X: b.X + 12, Y: y, Width: b.Width - 24, Height: 34}, 12, pal.muted, gfx.WeightRegular)
		} else {
			for i, disk := range r.disks {
				box := gfx.Rect{X: b.X + float32(i%cols)*(diskW+8), Y: y + float32(i/cols)*40, Width: diskW, Height: 34}
				if !s.dashboardShown(box) {
					continue
				}
				c.RoundedBorder(box, cornerPx(6), 1, pal.line, pal.workspace)
				c.DrawIcon(icons.HardDrive, gfx.Rect{X: box.X + 8, Y: box.Y + 10, Width: 14, Height: 14}, amber)
				monitorLabel(c, disk.Path, gfx.Rect{X: box.X + 28, Y: box.Y, Width: max(40, box.Width-242), Height: 34}, 12, pal.text, gfx.WeightMedium)
				monitorRight(c, fmt.Sprintf("%.0f%%", disk.Percent), gfx.Rect{X: box.X + box.Width - 210, Y: box.Y, Width: 38, Height: 34}, 13, pal.text, gfx.WeightSemibold)
				dashboardBar(c, gfx.Rect{X: box.X + box.Width - 160, Y: box.Y + 14, Width: 58, Height: 5}, disk.Percent, amber, pal.line)
				monitorRight(c, monitorCapacity(disk.Used, disk.Total), gfx.Rect{X: box.X + box.Width - 96, Y: box.Y, Width: 88, Height: 34}, 11, pal.muted, gfx.WeightRegular)
			}
		}
	}
	c.Fill(gfx.Rect{X: b.X, Y: b.Y + b.Height - 6, Width: b.Width, Height: 1}, pal.line)
}

// dashboardShown reports whether b, in the scrolled content, meets the visible
// body. A host lists every mount it has (a Docker host dozens), so only what is
// on screen is drawn.
func (s *State) dashboardShown(b gfx.Rect) bool {
	body := s.dashboard.body
	return b.Y+b.Height > body.Y && b.Y < body.Y+body.Height
}

func (s *State) paintDashboardChart(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect, metric int, label string) {
	dashboardGreen, dashboardBlue, dashboardAmber := s.dashboardColors()
	if len(r.x) == 0 {
		if s.dashboard.running[p.ID] || !r.snapshot.HasSample {
			// An empty plot holds one placeholder for the whole chart area, so it
			// sits in the middle of it rather than at the left edge where a
			// chart's data would start.
			monitorCenter(c, s.dashboardStatus(r.snapshot.Status, s.dashboard.running[p.ID], false), b, 12, s.pal.muted, gfx.WeightRegular)
		}
		return
	}
	cache := &r.charts[metric]
	size := gfx.Size{Width: b.Width, Height: b.Height}
	theme := s.Theme()
	theme.Border = gfx.Color{}
	focused := c.Focused() == r.chartIDs[metric]
	// The clock and scrolling repaint without changing samples. Keep the chart
	// in local coordinates so these frames can reuse its tessellation and text.
	if cache.version == r.snapshot.Version && cache.size == size && cache.theme == theme && cache.language == s.language && cache.scale == c.PixelScale() && cache.focused == focused {
		c.Embed(&cache.canvas, gfx.Point{X: b.X, Y: b.Y}, 0)
		return
	}
	cache.canvas.BeginEmbedded(c, 0)
	color := dashboardGreen
	valueIndex := metric
	if metric == 1 {
		color = dashboardBlue
	}
	if metric == 3 {
		color = dashboardAmber
		valueIndex = 4
	}
	count := 1
	if metric == 2 {
		count = 2
	}
	for i := 0; i < count; i++ {
		tint, name := color, label
		if metric == 2 {
			name = s.messages.Dashboard.Receive
			if i == 1 {
				tint = dashboardBlue
				name = s.messages.Dashboard.Send
			}
		}
		cache.series[i].color, cache.series[i].label = tint, name
		cache.series[i].values = r.values[valueIndex+i]
	}
	options := monitorChartOptions{
		xMin: 0, xMax: 1, fixedY: metric != 2,
		yMin: 0, yMax: 100, xTicks: 3,
		formatX: r.clock, label: label, focused: focused,
	}
	if metric == 2 {
		options.formatY = dashboardNetworkAxis
	} else {
		options.formatY = dashboardPercent
	}
	paintMonitorTrend(&cache.canvas, gfx.Rect{Width: b.Width, Height: b.Height}, r.chartIDs[metric], r.x, cache.series[:count], options)
	cache.canvas.EndFrame()
	cache.version, cache.size, cache.theme = r.snapshot.Version, size, theme
	cache.language, cache.scale, cache.focused = s.language, c.PixelScale(), focused
	c.Embed(&cache.canvas, gfx.Point{X: b.X, Y: b.Y}, 0)
}
