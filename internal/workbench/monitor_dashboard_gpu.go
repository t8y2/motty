package workbench

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/monitoring"
	"motty/internal/remotestats"
)

var dashboardMetricNames = [5]string{"cpu", "memory", "network", "disks", "gpu"}
var hitDashboardDisplay = tree.Sub(dashboardHits, tree.Name("display"))
var hitDashboardGPUSelect = tree.Sub(dashboardHits, tree.Name("gpu-select"))
var hitDashboardGPUView = tree.Sub(dashboardHits, tree.Name("gpu-view"))

func (r *dashboardRow) configureDisplay(p *monitoring.Panel, id string) {
	for i, name := range dashboardMetricNames {
		r.visible[i] = p.Shows(id, name)
	}
	r.visible[4] = r.visible[4] && len(r.snapshot.GPUs) > 0
	r.graphCount = 0
	for _, metric := range [4]int{0, 1, 2, 4} {
		if r.visible[metric] {
			r.graphs[r.graphCount] = metric
			r.graphCount++
		}
	}
	for _, page := range []*int{&r.page, &r.simplePage} {
		if (*page == 1 && !r.visible[3]) || (*page == 2 && !r.visible[2]) || (*page == 3 && !r.visible[4]) {
			*page = 0
		}
	}
}

func dashboardViewValues(r *dashboardRow, simple bool) []int {
	values := []int{0}
	if !simple && r.visible[4] {
		values = append(values, 3)
	}
	if r.visible[3] {
		values = append(values, 1)
	}
	if r.visible[2] {
		values = append(values, 2)
	}
	if simple && r.visible[4] {
		values = append(values, 3)
	}
	return values
}
func (r *dashboardRow) prepareGPU() {
	if len(r.snapshot.GPUs) == 0 {
		return
	}
	r.gpuChoice = 0
	for i, g := range r.snapshot.GPUs {
		if monitoring.GPUKey(g.Device) == r.gpuKey {
			r.gpuChoice = i
			break
		}
	}
	r.gpuKey = monitoring.GPUKey(r.snapshot.GPUs[r.gpuChoice].Device)
	if r.gpuPrepared == r.gpuKey {
		return
	}
	r.gpuPrepared = r.gpuKey
	r.gpuX = r.gpuX[:0]
	for i := range r.gpuValues {
		r.gpuValues[i] = r.gpuValues[i][:0]
	}
	duration := float64(0)
	if len(r.x) > 0 && len(r.snapshot.Points) > 0 {
		duration = float64(r.snapshot.Points[len(r.snapshot.Points)-1].At.Sub(r.origin))
	}
	if duration <= 0 {
		return
	}
	for _, p := range r.snapshot.GPUs[r.gpuChoice].Points {
		r.gpuX = append(r.gpuX, float32(float64(p.At.Sub(r.origin))/duration))
		for i := range r.gpuValues {
			r.gpuValues[i] = append(r.gpuValues[i], p.Values[i])
		}
	}
	r.gpuCharts = [2]dashboardChart{}
}
func (r *dashboardRow) gpu() (remotestats.GPU, bool) {
	if len(r.snapshot.GPUs) == 0 {
		return remotestats.GPU{}, false
	}
	g := r.snapshot.GPUs[min(r.gpuChoice, len(r.snapshot.GPUs)-1)]
	return g.Device, g.Present
}
func dashboardGPUName(g remotestats.GPU) string { return fmt.Sprintf("GPU%d · %s", g.Index, g.Name) }
func dashboardGPUPercent(g remotestats.GPU, present bool) string {
	if !present || g.UtilPercent < 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", g.UtilPercent)
}
func dashboardGPUReading(g remotestats.GPU, present bool) (memory, temperature, power, fan string) {
	memory, temperature, power, fan = "—", "—", "—", "—"
	if !present {
		return
	}
	memory = monitorGPUUsage(g.MemoryUsedBytes, g.MemoryTotalBytes, g.MemoryKnown)
	if g.TemperatureC >= 0 {
		temperature = fmt.Sprintf("%.0f°C", g.TemperatureC)
	}
	if g.PowerWatts >= 0 {
		power = fmt.Sprintf("%.0f W", g.PowerWatts)
		if g.PowerLimitWatts > 0 {
			power = fmt.Sprintf("%.0f / %.0f W", g.PowerWatts, g.PowerLimitWatts)
		}
	}
	if g.FanPercent >= 0 {
		fan = fmt.Sprintf("%.0f%%", g.FanPercent)
	}
	return
}
func dashboardGraphColumns(r *dashboardRow, w float32) int {
	if w < 1000 {
		return min(2, max(1, r.graphCount))
	}
	return max(1, r.graphCount)
}
func dashboardDiskColumns(w float32, n int) int {
	return min(max(1, n), max(1, min(4, int((w+8)/380))))
}
func dashboardGPUSummaryHeight(r *dashboardRow, w float32) float32 {
	if len(r.snapshot.GPUs) == 0 {
		return 0
	}
	cols := max(1, min(2, int(w/520)))
	return float32((len(r.snapshot.GPUs)+cols-1)/cols)*40 + 8
}
func dashboardGPUPlotHeight(w float32) float32 {
	if w < 700 {
		return 464
	}
	return 232
}

func (s *State) declareDashboardDisplay(c ui.C, p *monitoring.Panel, h Host, r *dashboardRow, rect gfx.Rect, y float32) {
	if h.ID == 0 {
		return
	}
	m := s.messages.Dashboard
	accessible := m.Display + " · " + h.Label
	anchor := s.declareIconButton(c, hitDashboardDisplay.At(int(h.ID)), iconSettings, accessible, false, iconButtonQuiet)
	anchor.Absolute().Left(rect.X).Top(y+rect.Y).Size(rect.Width, rect.Height)
	if anchor.Clicked() {
		r.infoOpen = !r.infoOpen
	}
	ui.Menu(c, anchor, &r.infoOpen, func(menu *ui.MenuItems) {
		menu.WidthRange(240, 300)
		menu.Label(h.Label)
		menu.Label(m.DisplayHint)
		menu.Label(m.DisplayViews)
		labels := [5]string{m.CPU, m.Memory, m.Network, m.Disk, s.messages.Monitor.GPU}
		for metric, name := range dashboardMetricNames {
			missingGPU := metric == 4 && len(r.snapshot.GPUs) == 0
			item := menu.Item(labels[metric]).Checked(p.Shows(h.CatalogID, name))
			if missingGPU {
				item.Description(m.GPUUnavailable)
			}
			if item.Clicked() {
				c.After(func() { s.toggleDashboardMetric(p, h.CatalogID, name) })
			}
		}
		menu.Separator()
		if menu.Item(m.RestoreDisplay).Clicked() {
			c.After(func() { delete(p.Display, h.CatalogID); s.reconcileDashboard(); s.save() })
		}
	})
}
func (s *State) toggleDashboardMetric(p *monitoring.Panel, host, name string) {
	if p.Display == nil {
		p.Display = make(map[string]monitoring.Display)
	}
	settings := p.Display[host]
	settings.Hidden = slices.Clone(settings.Hidden)
	if p.Shows(host, name) {
		settings.Hidden = append(settings.Hidden, name)
	} else {
		settings.Hidden = slices.DeleteFunc(settings.Hidden, func(v string) bool { return v == name })
	}
	p.Display[host] = settings
	s.reconcileDashboard()
	s.save()
}

func (s *State) declareDashboardGPUControls(c ui.C, p *monitoring.Panel, h Host, r *dashboardRow, row gfx.Rect, y float32) {
	if h.ID == 0 || len(r.snapshot.GPUs) == 0 {
		return
	}
	if p.Simple {
		if r.simplePage != 0 || !r.visible[4] {
			return
		}
		_, data := dashboardSimpleColumns(row)
		count := 0
		for _, v := range r.visible {
			if v {
				count++
			}
		}
		width := (row.Width - data - 48) / float32(count)
		s.declareDashboardGPUSelect(c, r, data+float32(count-1)*width+34, y+4, max(70, width-46))
		return
	}
	if r.page != 3 && (r.page != 0 || !r.visible[4]) {
		return
	}
	header := dashboardHeadingHeight(row.Width)
	if r.page == 3 {
		cols := max(1, min(2, int(row.Width/520)))
		width := (row.Width - float32(cols-1)*8) / float32(cols)
		for i, g := range r.snapshot.GPUs {
			memory, temp, _, _ := dashboardGPUReading(g.Device, g.Present)
			label := fmt.Sprintf("%s   %s · %s · %s", dashboardGPUName(g.Device), dashboardGPUPercent(g.Device, g.Present), memory, temp)
			b := ui.Button(c, label).Outline().Small().Hit(hitDashboardGPUSelect.At(int(h.ID)*remotestats.MaxGPUs+i), label)
			if i != r.gpuChoice {
				b.Ghost()
			}
			b.Absolute().Left(float32(i%cols)*(width+8)).Top(y+header+float32(i/cols)*40).Size(width, 32)
		}
		return
	}
	cols := dashboardGraphColumns(r, row.Width)
	width := (row.Width - float32(cols-1)*8) / float32(cols)
	for i, metric := range r.graphs[:r.graphCount] {
		if metric == 4 {
			s.declareDashboardGPUSelect(c, r, float32(i%cols)*(width+8)+34, y+header+float32(i/cols)*164+5, max(70, width-115))
		}
	}
}
func (s *State) declareDashboardGPUSelect(c ui.C, r *dashboardRow, x, y, width float32) {
	old := r.gpuChoice
	values := make([]int, len(r.snapshot.GPUs))
	for n := range values {
		values[n] = n
	}
	ui.Select(c, &r.gpuChoice, values, func(n int) string { return dashboardGPUName(r.snapshot.GPUs[n].Device) }).Label(s.messages.Monitor.GPU).Absolute().Left(x).Top(y).Size(width, 26)
	if old != r.gpuChoice {
		r.gpuKey = monitoring.GPUKey(r.snapshot.GPUs[r.gpuChoice].Device)
		r.gpuPrepared = ""
		r.prepareGPU()
	}
}

func (s *State) paintDashboardGPUChart(c *gfx.Canvas, r *dashboardRow, b gfx.Rect, index int) {
	g, present := r.gpu()
	label := s.messages.Monitor.Utilization
	if index == 1 {
		label = s.messages.Monitor.GPUMemory
	}
	if len(r.gpuX) == 0 || !present || (index == 0 && g.UtilPercent < 0) || (index == 1 && g.MemoryTotalBytes == 0 && !g.MemoryKnown && g.MemoryUsedBytes == 0) {
		monitorCenter(c, s.messages.Monitor.GPUMetricsUnavailable, b, 12, s.pal.muted, gfx.WeightRegular)
		return
	}
	cache := &r.gpuCharts[index]
	size := gfx.Size{Width: b.Width, Height: b.Height}
	theme := s.Theme()
	theme.Border = gfx.Color{}
	focused := c.Focused() == r.gpuChartIDs[index]
	if cache.version == r.snapshot.Version && cache.size == size && cache.theme == theme && cache.language == s.language && cache.scale == c.PixelScale() && cache.focused == focused {
		c.Embed(&cache.canvas, gfx.Point{X: b.X, Y: b.Y}, 0)
		return
	}
	green, blue, _ := s.dashboardColors()
	color := green
	if index == 1 {
		color = blue
	}
	cache.canvas.BeginEmbedded(c, 0)
	cache.series[0].color = color
	cache.series[0].label = label
	cache.series[0].values = r.gpuValues[index]
	options := monitorChartOptions{xMin: 0, xMax: 1, fixedY: true, yMin: 0, yMax: 100, xTicks: 3, formatX: r.clock, formatY: dashboardPercent, label: label, focused: focused}
	if index == 1 {
		options.fixedY = g.MemoryTotalBytes > 0
		options.yMax = float32(float64(g.MemoryTotalBytes) / (1 << 30))
		options.formatY = func(dst []byte, v float32) []byte {
			dst = strconv.AppendFloat(dst, float64(v), 'f', 0, 32)
			return append(dst, " GiB"...)
		}
	}
	paintMonitorTrend(&cache.canvas, gfx.Rect{Width: b.Width, Height: b.Height}, r.gpuChartIDs[index], r.gpuX, cache.series[:1], options)
	cache.canvas.EndFrame()
	cache.version, cache.size, cache.theme = r.snapshot.Version, size, theme
	cache.language, cache.scale, cache.focused = s.language, c.PixelScale(), focused
	c.Embed(&cache.canvas, gfx.Point{X: b.X, Y: b.Y}, 0)
}
func (s *State) paintDashboardGPUSummary(c *gfx.Canvas, r *dashboardRow, b gfx.Rect) {
	cols := max(1, min(2, int(b.Width/520)))
	width := (b.Width - float32(cols-1)*8) / float32(cols)
	for i, g := range r.snapshot.GPUs {
		box := gfx.Rect{X: b.X + float32(i%cols)*(width+8), Y: b.Y + float32(i/cols)*40, Width: width, Height: 32}
		if !s.dashboardShown(box) {
			continue
		}
		memory, temp, _, _ := dashboardGPUReading(g.Device, g.Present)
		c.RoundedBorder(box, cornerPx(6), 1, s.pal.line, s.pal.workspace)
		monitorLabel(c, fmt.Sprintf("%s   %s · %s · %s", dashboardGPUName(g.Device), dashboardGPUPercent(g.Device, g.Present), memory, temp), box.Inset(gfx.All(7)), 12, s.pal.text, gfx.WeightRegular)
	}
}
func (s *State) paintDashboardGPUView(c *gfx.Canvas, r *dashboardRow, b gfx.Rect) {
	y := b.Y + dashboardGPUSummaryHeight(r, b.Width)
	cols := 2
	if b.Width < 700 {
		cols = 1
	}
	width := (b.Width - float32(cols-1)*8) / float32(cols)
	g, present := r.gpu()
	memory, temp, power, fan := dashboardGPUReading(g, present)
	for i, label := range [2]string{s.messages.Monitor.Utilization, s.messages.Monitor.GPUMemory} {
		box := gfx.Rect{X: b.X + float32(i%cols)*(width+8), Y: y + float32(i/cols)*232, Width: width, Height: 224}
		if !s.dashboardShown(box) {
			continue
		}
		c.RoundedBorder(box, cornerPx(7), 1, s.pal.line, s.pal.workspace)
		monitorLabel(c, label, gfx.Rect{X: box.X + 12, Y: box.Y + 6, Width: box.Width / 2, Height: 28}, 13, s.pal.text, gfx.WeightMedium)
		value := dashboardGPUPercent(g, present)
		if i == 1 {
			value = memory
		}
		monitorRight(c, value, gfx.Rect{X: box.X + box.Width/2, Y: box.Y + 6, Width: box.Width/2 - 12, Height: 28}, 16, s.pal.text, gfx.WeightSemibold)
		s.paintDashboardGPUChart(c, r, gfx.Rect{X: box.X + 4, Y: box.Y + 38, Width: box.Width - 8, Height: box.Height - 42}, i)
	}
	y += dashboardGPUPlotHeight(b.Width)
	stats := gfx.Rect{X: b.X, Y: y, Width: b.Width, Height: 36}
	c.RoundedBorder(stats, cornerPx(6), 1, s.pal.line, s.pal.workspace)
	for i, text := range [3]string{s.messages.Monitor.Temperature + " " + temp, s.messages.Monitor.Power + " " + power, s.messages.Dashboard.Fan + " " + fan} {
		box := gfx.Rect{X: b.X + float32(i)*b.Width/3, Y: y, Width: b.Width / 3, Height: 36}
		monitorCenter(c, text, box, 13, s.pal.text, gfx.WeightMedium)
		if i > 0 {
			c.Fill(gfx.Rect{X: box.X, Y: y + 9, Width: 1, Height: 18}, s.pal.line)
		}
	}
}

func (s *State) paintDashboardGPUSimple(c *gfx.Canvas, r *dashboardRow, b gfx.Rect) {
	labels := [5]string{s.messages.Monitor.GPU, s.messages.Monitor.Utilization, s.messages.Monitor.GPUMemory, s.messages.Monitor.Temperature, s.messages.Monitor.Power}
	positions := [6]float32{0, .30, .43, .65, .79, 1}
	for i, label := range labels {
		monitorLabel(c, label, gfx.Rect{X: b.X + b.Width*positions[i], Y: b.Y + 8, Width: b.Width*(positions[i+1]-positions[i]) - 8, Height: 18}, 11, s.pal.muted, gfx.WeightMedium)
	}
	for n, g := range r.snapshot.GPUs {
		memory, temp, power, _ := dashboardGPUReading(g.Device, g.Present)
		cells := [5]string{dashboardGPUName(g.Device), dashboardGPUPercent(g.Device, g.Present), memory, temp, power}
		for i, value := range cells {
			monitorLabel(c, value, gfx.Rect{X: b.X + b.Width*positions[i], Y: b.Y + 30 + float32(n)*30, Width: b.Width*(positions[i+1]-positions[i]) - 8, Height: 26}, 12, s.pal.text, gfx.WeightRegular)
		}
	}
}
