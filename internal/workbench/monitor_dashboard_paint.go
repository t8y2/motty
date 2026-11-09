package workbench

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/monitoring"
	"motty/internal/remotestats"
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

// The header's two actions share one height and one gap, and each is as wide as
// its own label needs from a width the pair was designed at. A single fixed
// width cannot hold every language: Chinese "开始监控" fits 112 and English
// "Start monitoring" needs 148, so the fixed box clipped the English label
// against its own right edge.
const (
	dashboardActionHeight = float32(36)
	dashboardActionGap    = float32(12)
	dashboardActionLeast  = float32(112)
	dashboardActionIcon   = float32(36)
	// The selector's least width, its gap, and the three icon buttons beside
	// it: the room the actions must leave before they are worth their labels.
	dashboardHeaderLeast = float32(120 + 8 + 112 + 12)
)

// dashboardActionWidths is the width of each header action. A label wider than
// the designed width widens its button instead of being clipped.
func dashboardActionWidths(c *gfx.Canvas, theme gfx.Theme, newLabel string, newIcon gfx.Icon, toggleLabel string, toggleIcon gfx.Icon, iconOnly bool) (newWidth, toggleWidth float32) {
	if iconOnly {
		return dashboardActionIcon, dashboardActionIcon
	}
	measure := components.BeginButton(c, theme, gfx.ID{}, gfx.Rect{})
	return max(dashboardActionLeast, measure.WithIcon(newIcon).SizeToFit(newLabel).BoundsWidth()),
		max(dashboardActionLeast, measure.WithIcon(toggleIcon).SizeToFit(toggleLabel).BoundsWidth())
}

func (s *State) paintDashboard(c *gfx.Canvas, b gfx.Rect) {
	d, m := &s.dashboard, s.messages.Dashboard
	pal := s.pal
	x, w := b.X+18, b.Width-36
	y := b.Y + 14
	p := s.dashboardPanel()
	theme := s.Theme()
	if p == nil {
		f := s.dashboard.emptyLayout.Begin(c, theme, b)
		icon, button := s.declareEmptyPage(f, b, m.Empty, m.EmptyHint, m.Create)
		f.End()
		s.paintEmptyPage(c, icon, button, iconActivity, hitDashboardNew, m.Create, true)
		return
	}
	actionTheme := theme
	actionTheme.Metrics.ControlHeight = dashboardActionHeight
	running := d.running[p.ID]
	toggle, toggleIcon := m.Start, icons.Play
	if running {
		toggle, toggleIcon = m.Stop, icons.Pause
	}
	compact := w < 720
	newW, toggleW := dashboardActionWidths(c, actionTheme, m.New, icons.Plus, toggle, toggleIcon, compact)
	// A long label must not push the actions over the panel selector and its
	// three icon buttons; two icon buttons fit wherever the labels do not.
	if !compact && w-newW-toggleW-dashboardActionGap < dashboardHeaderLeast {
		compact = true
		newW, toggleW = dashboardActionWidths(c, actionTheme, m.New, icons.Plus, toggle, toggleIcon, true)
	}
	newBox := gfx.Rect{X: x + w - newW - toggleW - dashboardActionGap, Y: y, Width: newW, Height: dashboardActionHeight}
	toggleBox := gfx.Rect{X: x + w - toggleW, Y: y, Width: toggleW, Height: dashboardActionHeight}
	panelW := min(float32(240), max(120, newBox.X-x-132))
	d.selectBounds[0] = gfx.Rect{X: x, Y: y, Width: panelW, Height: 36}
	components.BeginButton(c, theme, hitDashboardEdit, gfx.Rect{X: x + panelW + 8, Y: y + 2, Width: 32}).WithIcon(icons.Pencil).Icon().Ghost(m.Edit)
	sortButton := components.BeginButton(c, theme, hitDashboardSort, gfx.Rect{X: x + panelW + 44, Y: y + 2, Width: 32}).WithIcon(icons.ArrowUpDown).Icon()
	if len(p.Hosts) < 2 {
		sortButton = sortButton.Disabled()
	}
	sortButton.Ghost(m.Sort)
	components.BeginButton(c, theme, hitDashboardDelete, gfx.Rect{X: x + panelW + 80, Y: y + 2, Width: 32}).WithIcon(icons.Trash2).Icon().Ghost(m.Delete)
	newButton := components.BeginButton(c, actionTheme, hitDashboardNew, newBox).WithIcon(icons.Plus)
	toggleButton := components.BeginButton(c, actionTheme, hitDashboardToggle, toggleBox).WithIcon(toggleIcon)
	if compact {
		newButton, toggleButton = newButton.Icon(), toggleButton.Icon()
	}
	newButton.Default(m.New)
	if running {
		toggleButton.Outline(toggle)
	} else {
		toggleButton.Default(toggle)
	}
	y += 58
	controlH := float32(42)
	if w < 860 {
		controlH = 86
	}
	modeOptions := s.dashboardModeTabs()
	selectedMode := hitDashboardDetailed
	if p.Simple {
		selectedMode = hitDashboardSimple
	}
	components.BeginTabs(c, s.Theme(), gfx.Rect{X: x, Y: y, Width: 136, Height: 34}, modeOptions[:], selectedMode).Paint()
	settingsY := y
	searchW := max(100, w-136-16-316)
	if w < 860 {
		settingsY = y + 42
		searchW = w - 152
	}
	searchBox := gfx.Rect{X: x + 152, Y: y, Width: searchW, Height: 34}
	components.PaintInputGroup(c, s.Theme(), searchBox, components.InputGroupSnapshot{ID: hitDashboardSearch, Label: m.Search, Placeholder: m.Search, Value: d.search.String(), NativeEditing: c.Focused() == hitDashboardSearch && d.search.String() != "", InlineStart: components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]}})
	d.selectBounds[1] = gfx.Rect{X: x + w - 304, Y: settingsY, Width: 152, Height: 34}
	d.selectBounds[2] = gfx.Rect{X: x + w - 142, Y: settingsY, Width: 142, Height: 34}
	y += controlH + 10
	c.Fill(gfx.Rect{X: x, Y: y, Width: w, Height: 1}, pal.line)
	y += 8
	footer := gfx.Rect{X: x, Y: b.Y + b.Height - 36, Width: w, Height: 34}
	d.body = gfx.Rect{X: x, Y: y, Width: w, Height: max(0, footer.Y-y-5)}

	width := int(d.body.Width)
	rowW := d.body.Width
	if p.Simple {
		rowW = max(rowW, 960)
	}
	ids := make([]string, 0, len(p.Hosts))
	live := 0
	query := strings.ToLower(strings.TrimSpace(d.search.String()))
	for _, id := range p.Hosts {
		r := s.dashboardRow(p, id, width)
		h, exists := s.dashboardHost(id)
		if !exists {
			r.snapshot.Status = monitoring.Missing
		}
		if r.snapshot.Status == monitoring.Live && running {
			live++
		}
		if query == "" || strings.Contains(strings.ToLower(h.Label+" "+h.Address+" "+h.Tags+" "+h.Group+" "+id), query) {
			ids = append(ids, id)
		}
	}
	d.content = 0
	if p.Simple {
		d.content = 38
		for _, id := range ids {
			d.content += dashboardSimpleHeight(s.dashboardRow(p, id, width))
		}
	} else {
		for _, id := range ids {
			d.content += dashboardHostHeight(s.dashboardRow(p, id, width), rowW-12)
		}
	}
	area := components.BeginScrollArea(c, s.Theme(), d.body, gfx.Size{Width: rowW, Height: d.content}).Offset(d.scrollX, d.scroll).Viewport(hitDashboardScroll, m.Title).VerticalScrollbar(hitDashboardThumb, m.Title, "").HorizontalScrollbar(hitDashboardXThumb, m.Title, "")
	geo := area.BeginContent()
	d.scroll = geo.OffsetY
	d.scrollX = geo.OffsetX
	y = geo.Content.Y
	if p.Simple {
		s.paintDashboardSimpleHead(c, gfx.Rect{X: geo.Content.X, Y: y, Width: geo.Content.Width - 12, Height: 36})
		y += 38
	}
	for _, id := range ids {
		r := s.dashboardRow(p, id, width)
		host, exists := s.dashboardHost(id)
		if !exists {
			host.Label = m.Missing
			host.CatalogID = id
		}
		h := dashboardSimpleHeight(r)
		if !p.Simple {
			h = dashboardHostHeight(r, rowW-12)
		}
		box := gfx.Rect{X: geo.Content.X, Y: y, Width: geo.Content.Width - 12, Height: h}
		if box.Y+box.Height > d.body.Y && box.Y < d.body.Y+d.body.Height {
			if p.Simple {
				s.paintDashboardSimpleRow(c, p, host, r, box)
			} else {
				s.paintDashboardHost(c, p, host, r, box)
			}
		}
		y += h
	}
	if len(ids) == 0 {
		monitorLabel(c, s.messages.KnownHostsNoMatches, gfx.Rect{X: geo.Content.X + 18, Y: y + 30, Width: rowW - 36, Height: 40}, 14, pal.muted, gfx.WeightRegular)
	}
	area.EndContent()
	d.refresh.interval = p.Interval
	d.refresh.intervalText = fmt.Sprintf(m.Every, p.Interval)
	d.refresh.paused = !running
	d.refresh.count = 0
	for _, id := range p.Hosts {
		r := s.dashboardRow(p, id, width)
		if r.snapshot.Status == monitoring.Live {
			d.refresh.count = 1
			d.refresh.version = uint32(r.snapshot.Updated.UnixMilli())
			break
		}
	}
	c.Fill(gfx.Rect{X: x, Y: footer.Y, Width: w, Height: 1}, pal.line)
	monitorLabel(c, fmt.Sprintf(m.Count, len(p.Hosts)), footer, 12, pal.muted, gfx.WeightRegular)
	stateText := m.Waiting
	if running {
		stateText = fmt.Sprintf(m.Collecting, live, len(p.Hosts))
	} else {
		for _, id := range p.Hosts {
			if s.dashboardRow(p, id, width).snapshot.HasSample {
				stateText = m.Stopped
				break
			}
		}
	}
	if w >= 1000 {
		monitorRight(c, stateText, gfx.Rect{X: newBox.X - 174, Y: b.Y + 14, Width: 160, Height: 36}, 12, s.dashboardLiveColor(running), gfx.WeightMedium)
	} else {
		monitorRight(c, stateText, footer, 12, pal.muted, gfx.WeightRegular)
	}
}

// dashboardBar paints one bounded metric as Viva's progress bar, in the
// metric's own color over the palette's track line.
func dashboardBar(c *gfx.Canvas, theme gfx.Theme, b gfx.Rect, value float64, color, track gfx.Color) {
	theme.Primary, theme.Muted = color, track
	fraction := float32(0)
	if !math.IsNaN(value) {
		fraction = float32(min(max(value, 0), 100) / 100)
	}
	components.Progress(c, theme, b, fraction)
}
func (s *State) paintDashboardIdentity(c *gfx.Canvas, h Host, b gfx.Rect) {
	s.paintHostCatalogIcon(c, h, gfx.Rect{X: b.X, Y: b.Y + 9, Width: 34, Height: 34}, s.pal.workspace)
	monitorLabel(c, h.Label, gfx.Rect{X: b.X + 46, Y: b.Y + 3, Width: b.Width - 50, Height: 27}, 16, s.pal.text, gfx.WeightSemibold)
	monitorLabel(c, h.Address, gfx.Rect{X: b.X + 46, Y: b.Y + 30, Width: b.Width - 50, Height: 22}, 12, s.pal.muted, gfx.WeightRegular)
}
func (s *State) paintDashboardState(c *gfx.Canvas, p *monitoring.Panel, h Host, r *dashboardRow, b gfx.Rect) {
	dashboardGreen, _, _ := s.dashboardColors()
	running := s.dashboard.running[p.ID]
	if !running && r.snapshot.Status != monitoring.Missing {
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
	if running && r.snapshot.Status == monitoring.Failed && h.ID != 0 {
		theme := s.Theme().WithControlHeight(b.Height)
		components.BeginButton(c, theme, hitDashboardRetryList.At(int(h.ID)), b).WithIcon(icons.RotateCw).Ghost(s.messages.Dashboard.Retry)
	} else {
		monitorLabel(c, label, b, 12, color, gfx.WeightMedium)
	}
}
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
			dashboardBar(c, s.Theme(), gfx.Rect{X: box.X + box.Width*.35, Y: box.Y + 17, Width: box.Width * .3, Height: 6}, disk.Percent, dashboardAmber, pal.line)
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
	if cache.language != s.language {
		cache.state = components.ChartState{}
	}
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
		tint := color
		name := label
		if metric == 2 {
			name = s.messages.Dashboard.Receive
			if i == 1 {
				tint = dashboardBlue
				name = s.messages.Dashboard.Send
			}
		}
		cache.gradient[i] = components.ChartAreaGradient{From: tint, To: tint}
		cache.gradient[i].To.Alpha = 0
		cache.style[i] = components.ChartAreaStyle{Opacity: .22, Gradient: &cache.gradient[i]}
		cache.series[i] = components.ChartSeries{Key: name, Label: name, Color: tint, AreaStyle: &cache.style[i]}
		cache.values[i] = r.values[valueIndex+i]
	}
	yaxis := components.ChartAxis{Fixed: true, Min: 0, Max: 100, Format: dashboardPercent}
	if metric == 2 {
		yaxis = components.ChartAxis{Format: dashboardNetworkAxis}
	}
	components.PaintAreaChart(&cache.canvas, theme, gfx.Rect{Width: b.Width, Height: b.Height}, r.chartIDs[metric], components.AreaChart{Series: cache.series[:count], X: r.x, Values: cache.values[:count], Curve: components.ChartCurveMonotone, Fill: components.ChartFillGradient, StrokeWidth: 1.5, XAxis: components.ChartAxis{Linear: true, Fixed: true, Min: 0, Max: 1, Ticks: 3, Format: r.clock}, YAxis: yaxis, Label: label, Version: uint32(r.snapshot.Version), HideTooltip: true}, &cache.state)
	cache.canvas.EndFrame()
	cache.version, cache.size, cache.theme = r.snapshot.Version, size, theme
	cache.language, cache.scale, cache.focused = s.language, c.PixelScale(), focused
	c.Embed(&cache.canvas, gfx.Point{X: b.X, Y: b.Y}, 0)
}

// Selects are declared in the overlay pass, outside the workspace clip. Both
// the trigger and its popup come from Viva; the application only owns values.
func (s *State) paintDashboardMenu(c *gfx.Canvas) {
	d, m := &s.dashboard, s.messages.Dashboard
	p := s.dashboardPanel()
	if s.activeSection != sectionDashboard || p == nil {
		return
	}
	controls := [...]struct {
		id           gfx.ID
		label, value string
		rows         int
		icon         gfx.Icon
	}{
		{hitDashboardSwitch, m.Title, p.Name, len(d.panels), gfx.Icon{}},
		{hitDashboardInterval, m.Sampling, fmt.Sprintf(m.Every, p.Interval), len(dashboardIntervals), remoteRefreshClock},
		{hitDashboardRetention, m.Retention, m.Retention + ": " + s.dashboardRetention(p.Retention), len(dashboardRetentions), gfx.Icon{}},
	}
	for i, item := range controls {
		b := d.selectBounds[i]
		open := d.menu == i+1 && s.modal == modalNone
		viewport := s.windowRect()
		height := min(float32(item.rows*32+8), 420, max(40, viewport.Height-b.Y-b.Height-12))
		list := gfx.Rect{X: b.X, Y: b.Y + b.Height + 4, Width: max(144, b.Width), Height: height}
		control := components.BeginSelect(c, s.Theme(), b, components.SelectOptions{TriggerID: item.id, Label: item.label, Value: item.value, Icon: item.icon, Open: open, Content: list, Layer: popupLayer, Scope: hitDashboardDismiss, DismissID: hitDashboardDismiss, Viewport: viewport, Scroll: s.selectScroll(open, hitDashboardDismiss)})
		if i == 0 {
			for j, panel := range d.panels {
				control.AddItem(components.SelectItem{ID: hitDashboardPanelList.At(j), Label: panel.Name, Selected: panel.ID == p.ID})
			}
		} else if i == 1 {
			for j, n := range dashboardIntervals {
				control.AddItem(components.SelectItem{ID: hitDashboardIntervalList.At(j), Label: fmt.Sprintf(m.Every, n), Selected: n == p.Interval})
			}
		} else {
			for j, n := range dashboardRetentions {
				control.AddItem(components.SelectItem{ID: hitDashboardRetentionList.At(j), Label: s.dashboardRetention(n), Selected: n == p.Retention})
			}
		}
		if open {
			s.menuPlaced(control.ContentBounds())
		}
		control.End()
		if i == 1 {
			// The animated hands share the terminal monitor's SVG frames. Viva
			// Select owns the clock slot, trigger, focus, and native chevron.
			frame := monitorRefreshFrame(c, &d.refresh, d.refresh.count > 0)
			c.DrawIcon(remoteRefreshHands[frame], gfx.Rect{X: b.X + 11, Y: b.Y + (b.Height-16)/2, Width: 16, Height: 16}, s.Theme().Foreground)
		}
	}
}

func (s *State) paintDashboardDialog(c *gfx.Canvas, viewport gfx.Rect) {
	e, m := &s.dashboard.editor, s.messages.Dashboard
	theme := s.Theme()
	title, submit := m.New, m.Create
	if e.id != "" {
		title, submit = m.Edit, m.Save
	}
	groups, count, shown := s.dashboardPickerGroups()
	initial := hitDashboardName
	if e.page == 1 {
		initial = hitDashboardOrderTab
		if len(e.selected) > 0 {
			initial = dashboardOrderID(e.selected[0])
		}
	}
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(c, theme, viewport, components.DialogOptions{Layer: dialogLayer, Scope: dialogScope, CloseID: hitDashboardDialogCancel, InitialFocusID: initial, ContentSize: gfx.Size{Width: 780}, FitBody: true, BodyHeight: min(600, max(360, viewport.Height-240))})
	layout := dialog.Begin(title, "")
	b := layout.Body
	y := b.Y + 4
	options := s.dashboardEditorTabs()
	components.BeginTabs(c, theme, gfx.Rect{X: b.X + 4, Y: y, Width: 260, Height: 34}, options[:], options[e.page].ID).Paint()
	countText := fmt.Sprintf(m.PickerCount, len(e.selected), shown, count)
	if e.page == 1 {
		countText = fmt.Sprintf(m.Selected, len(e.selected))
	}
	monitorRight(c, countText, gfx.Rect{X: b.X + 270, Y: y, Width: max(0, b.Width-274), Height: 34}, 12, s.pal.muted, gfx.WeightRegular)
	y += 48
	if e.page == 0 {
		for _, field := range []struct {
			id                        gfx.ID
			label, value, placeholder string
		}{{hitDashboardName, m.Name, e.name.String(), m.NamePlaceholder}, {hitDashboardNote, m.Note, e.note.String(), m.NotePlaceholder}} {
			box := components.BeginField(c, theme, gfx.Rect{X: b.X + 4, Y: y, Width: b.Width - 8, Height: 62}).For(field.id).Paint(field.label, "", "")
			components.PaintInput(c, theme, box, components.InputSnapshot{ID: field.id, Label: field.label, Value: field.value, Placeholder: field.placeholder, Focused: c.Focused() == field.id, PlatformEditing: c.Focused() == field.id && field.value != ""})
			y += 64
		}
		y += 6
		s.paintDashboardPickerFilters(c, gfx.Rect{X: b.X + 4, Y: y, Width: b.Width - 8, Height: 34})
	} else {
		monitorLabel(c, m.SortHint, gfx.Rect{X: b.X + 4, Y: y, Width: b.Width - 8, Height: 34}, 12, s.pal.muted, gfx.WeightRegular)
	}
	y += 46
	e.bounds = gfx.Rect{X: b.X + 4, Y: y, Width: b.Width - 8, Height: max(40, b.Y+b.Height-y-56)}
	columns := dashboardPickerColumns(e.bounds.Width - 12)
	e.content = dashboardPickerHeight(groups, columns)
	if e.page == 1 {
		e.content = max(0, float32(len(e.selected))*48-4) + 6
	}
	area := components.BeginScrollArea(c, theme, e.bounds, gfx.Size{Width: e.bounds.Width, Height: e.content}).Offset(0, e.scroll).Viewport(hitDashboardHostScroll, m.Hosts).VerticalScrollbar(hitDashboardHostThumb, m.Hosts, "")
	geo := area.BeginContent()
	e.scroll = geo.OffsetY
	if e.page == 1 {
		s.paintDashboardHostOrder(c, geo)
	} else {
		s.paintDashboardPickerGroups(c, geo, groups, columns)
	}
	area.EndContent()
	hint, color := m.PickerHint, s.pal.muted
	if e.page == 1 {
		hint = m.OrderHint
	}
	if e.error != "" {
		hint, color = e.error, s.pal.danger
	}
	var lines [3]string
	for i, line := range c.WrapLines(lines[:0], hint, 12, gfx.WeightRegular, b.Width-8, len(lines)) {
		monitorLabel(c, line, gfx.Rect{X: b.X + 4, Y: b.Y + b.Height - 48 + float32(i)*17, Width: b.Width - 8, Height: 17}, 12, color, gfx.WeightRegular)
	}
	dialog.Actions(layout, components.ButtonAction{ID: hitDashboardDialogCancel, Label: s.messages.Cancel, Variant: components.ButtonSecondary}, components.ButtonAction{ID: hitDashboardDialogSave, Label: submit})
	dialog.End()
}

// Cards compose Viva's surface and checkbox. The checkbox owns the entire
// card's hit region, including its keyboard and checked-state semantics.
func (s *State) paintDashboardHostChoice(c *gfx.Canvas, h Host, b gfx.Rect, id gfx.ID, selected, missing bool) {
	theme := s.Theme()
	background := s.pal.surface
	if selected {
		background = s.pal.accentSoft
	} else if c.Hovered() == id {
		background = s.pal.surfaceSoft
	}
	theme.Card = tintHostSurface(background, h.Color, .10)
	components.PaintCard(c, theme, b, components.CardOptions{Size: components.CardSizeSmall})
	if selected || c.FocusVisible() == id {
		c.RoundedBorder(b, theme.Metrics.RadiusSurface+4, 1.5, s.pal.accent, theme.Card)
	}
	paintHostColorMark(c, b, h.Color)
	s.paintHostCatalogIcon(c, h, gfx.Rect{X: b.X + 8, Y: b.Y + (b.Height-20)/2, Width: 20, Height: 20}, background)
	color := s.pal.text
	if missing {
		color = s.pal.warning
	}
	monitorLabel(c, h.Label, gfx.Rect{X: b.X + 36, Y: b.Y, Width: max(0, b.Width-66), Height: b.Height}, 13, color, gfx.WeightMedium)
	side := components.CheckboxExtent(theme)
	components.BeginCheckbox(c, theme, id, gfx.Rect{X: b.X + b.Width - 28, Y: b.Y + (b.Height-side)/2}, selected).Label(h.Label).HitBounds(b).Paint()
}
