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
		d.rows[key] = r
	}
	version := r.snapshot.Version
	if width != r.width {
		version = 0
	}
	if snap, changed := d.engine.Read(p.ID, id, max(1, width/8), version); changed {
		r.snapshot = snap
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
	return s.declareEmptyPageRegion(c, gfx.Rect{Width: width, Height: height}, m.Empty, m.EmptyHint, m.Create, s.pal.muted, hitDashboardNew, iconActivity, true)
}

// dashboardBar paints one bounded metric as a progress bar, in the metric's
// own color over the palette's track line.
// dashboardHostHeight is a detailed card's height for a row width.
func dashboardHostHeight(r *dashboardRow, w float32) float32 {
	header := dashboardHeadingHeight(w)
	if r.page == 1 {
		return header + 178 + float32(max(1, len(r.snapshot.Details.Disks)))*38 + 44
	}
	if r.page == 2 {
		return header + 178 + float32(max(1, len(r.snapshot.Details.Networks)))*38 + 44
	}
	cols := 2
	if w < 760 {
		cols = 1
	}
	charts := float32(164)
	if w < 760 {
		charts = 3*164 + 16
	}
	return header + charts + float32(max(1, (len(r.disks)+cols-1)/cols))*48 + 18
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
	nameW := min(float32(190), c.TextWidthWeight(h.Label, 15, gfx.WeightSemibold)+4)
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
	statusX := tabsX + tabsW + 8
	if b.Width < 900 {
		statusX = addrX + addrW + 12
	}
	statusEnd := b.X + b.Width - 44
	if canSudo {
		statusEnd -= 36
	}
	statusW := min(float32(125), max(0, statusEnd-statusX))
	out.tabs = gfx.Rect{X: tabsX, Y: tabsY, Width: tabsW, Height: 30}
	out.retry = gfx.Rect{X: statusX, Y: y + 7, Width: statusW, Height: 28}
	out.terminal = gfx.Rect{X: b.X + b.Width - 38, Y: y + 5, Width: 32, Height: 32}
	out.sudo = gfx.Rect{X: b.X + b.Width - 74, Y: y + 5, Width: 32, Height: 32}
	return out
}

func (s *State) paintDashboardHost(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) {
	_, _, dashboardAmber := s.dashboardColors()
	m, pal := s.messages.Dashboard, s.pal
	header := s.paintDashboardHeading(c, p, h, r, b)
	if r.page != 0 {
		s.paintDashboardDeviceView(c, p, h, r, gfx.Rect{X: b.X, Y: b.Y + header, Width: b.Width, Height: b.Height - header})
		return
	}
	y := b.Y + header
	chartW := (b.Width - 16) / 3
	vertical := b.Width < 760
	if vertical {
		chartW = b.Width
	}
	for i, label := range []string{m.CPU, m.Memory, m.Network} {
		box := gfx.Rect{X: b.X + float32(i)*(chartW+8), Y: y, Width: chartW, Height: 156}
		if vertical {
			box.X = b.X
			box.Y = y + float32(i)*172
		}
		if !s.dashboardShown(box) {
			continue
		}
		c.RoundedBorder(box, 7, 1, pal.line, pal.workspace)
		c.DrawIcon([]gfx.Icon{icons.Cpu, icons.MemoryStick, icons.Wifi}[i], gfx.Rect{X: box.X + 12, Y: box.Y + 13, Width: 14, Height: 14}, pal.muted)
		monitorLabel(c, label, gfx.Rect{X: box.X + 34, Y: box.Y + 6, Width: 74, Height: 28}, 13, pal.text, gfx.WeightMedium)
		value := r.cpu
		if i == 1 {
			value = r.memory
		}
		if i == 2 {
			value = "↓ " + r.down + "   ↑ " + r.up
		}
		font := float32(20)
		if i == 2 {
			font = 12
		}
		monitorRight(c, value, gfx.Rect{X: box.X + 85, Y: box.Y + 6, Width: max(0, box.Width-97), Height: 28}, font, pal.text, gfx.WeightSemibold)
		plot := gfx.Rect{X: box.X + 4, Y: box.Y + 39, Width: box.Width - 8, Height: box.Height - 43}
		s.paintDashboardChart(c, p, h, r, plot, i, label)
	}
	y += 164
	if vertical {
		y += 344
	}
	cols := 2
	if vertical {
		cols = 1
	}
	diskW := (b.Width - float32(cols-1)*8) / float32(cols)
	if len(r.disks) == 0 || r.snapshot.DetailsFailed {
		monitorLabel(c, s.dashboardDiskMessage(r), gfx.Rect{X: b.X + 12, Y: y, Width: b.Width - 24, Height: 40}, 12, pal.muted, gfx.WeightRegular)
	} else {
		for i, disk := range r.disks {
			box := gfx.Rect{X: b.X + float32(i%cols)*(diskW+8), Y: y + float32(i/cols)*48, Width: diskW, Height: 40}
			if !s.dashboardShown(box) {
				continue
			}
			c.RoundedBorder(box, 6, 1, pal.line, pal.workspace)
			c.DrawIcon(icons.HardDrive, gfx.Rect{X: box.X + 10, Y: box.Y + 13, Width: 14, Height: 14}, dashboardAmber)
			monitorLabel(c, disk.Path, gfx.Rect{X: box.X + 32, Y: box.Y, Width: max(0, box.Width*.18-24), Height: 40}, 12, pal.text, gfx.WeightMedium)
			monitorLabel(c, fmt.Sprintf("%.0f%%", disk.Percent), gfx.Rect{X: box.X + box.Width*.2, Y: box.Y, Width: 52, Height: 40}, 16, pal.text, gfx.WeightSemibold)
			dashboardBar(c, gfx.Rect{X: box.X + box.Width*.35, Y: box.Y + 17, Width: box.Width * .3, Height: 6}, disk.Percent, dashboardAmber, pal.line)
			monitorRight(c, monitorCapacity(disk.Used, disk.Total), gfx.Rect{X: box.X + box.Width*.67, Y: box.Y, Width: box.Width*.31 - 8, Height: 40}, 11, pal.muted, gfx.WeightRegular)
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
			monitorLabel(c, s.dashboardStatus(r.snapshot.Status, s.dashboard.running[p.ID], false), b, 12, s.pal.muted, gfx.WeightRegular)
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
