package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/workbench/icons"
)

const monitorInset = float32(12)
const monitorControlsHeight = float32(44)
const monitorTileHeight = float32(128)
const monitorLowerTileHeight = float32(128)
const monitorGap = float32(10)

type monitorColors struct{ background, surface, border, text, muted, track, cpu, memory, disk, network gfx.Color }

func (s *State) monitorColors() monitorColors {
	if s.pal.dark {
		return monitorColors{gfx.RGB(13, 17, 25), gfx.RGB(25, 31, 43), gfx.RGB(41, 50, 67), gfx.RGB(237, 242, 250), gfx.RGB(165, 179, 198), gfx.RGB(41, 49, 66), gfx.RGB(85, 196, 225), gfx.RGB(83, 187, 145), gfx.RGB(237, 180, 72), gfx.RGB(170, 131, 247)}
	}
	return monitorColors{gfx.RGB(237, 240, 245), gfx.RGB(248, 250, 252), gfx.RGB(217, 224, 233), gfx.RGB(28, 42, 60), gfx.RGB(98, 114, 136), gfx.RGB(226, 231, 238), gfx.RGB(21, 127, 153), gfx.RGB(27, 133, 94), gfx.RGB(169, 107, 17), gfx.RGB(121, 85, 199)}
}

func monitorRight(c *gfx.Canvas, text string, b gfx.Rect, size float32, color gfx.Color, weight float32) {
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	c.LabelAligned(elide(c, text, size, weight, b.Width), b, gfx.TextStyle{Size: size, Color: color, Weight: weight}, gfx.AlignRight)
}

func monitorRowVisible(viewport gfx.Rect, y, height float32) bool {
	return viewport.Width > 0 && viewport.Height > 0 &&
		y < viewport.Y+viewport.Height && y+height > viewport.Y
}

func monitorDiskCount(p *monitorView) int {
	if p.disksCollapsed {
		return 0
	}
	n := 0
	for _, d := range p.disks {
		if p.page == 1 || p.disksOpen || !d.virtual && n < 3 {
			n++
		}
	}
	return n
}

// monitorCoresBlock is the cores section and the gap after it, or nothing.
func monitorCoresBlock(p *monitorView) float32 {
	if p.noPerCore {
		return 0
	}
	return monitorCoresHeight(p) + monitorGap
}
func monitorCoresHeight(p *monitorView) float32 {
	if p.coresCollapsed {
		return 32
	}
	n := len(p.cores)
	if !p.coresMore {
		n = min(n, 8)
	}
	h := float32(32 + 8 + ((n+1)/2)*34 + 8)
	if n == 0 {
		h += 30
	}
	if len(p.cores) > 8 {
		h += 26
	}
	return h
}
func monitorDisksHeight(p *monitorView) float32 {
	if p.disksCollapsed {
		return 32
	}
	h := float32(32 + 8 + max(1, monitorDiskCount(p))*43 + 4)
	if p.page != 1 && (p.disksOpen || monitorDiskCount(p) < len(p.disks)) {
		h += 26
	}
	return h
}
func monitorContentHeight(p *monitorView) float32 {
	h := float32(8)
	switch p.page {
	case 0:
		h += monitorTileHeight + monitorLowerTileHeight + monitorGap*3 + monitorSummaryHeight + monitorCoresBlock(p) + monitorDisksHeight(p)
	case 1:
		h += monitorTileHeight + monitorGap + monitorDisksHeight(p)
	case 2:
		h += 142 + monitorGap + 32 + float32(max(1, len(p.networks)))*34 + 8
	}
	if p.page == 0 {
		h += monitorGap + 28
		if p.hardwareOpen {
			h += 28 * float32(max(1, len(p.sample.GPUs)))
			if p.data.Kernel != "" {
				h += 24
			}
		}
	}
	return h + 8
}

func (s *State) monitorSection(c *gfx.Canvas, id gfx.ID, b gfx.Rect, label string, icon gfx.Icon, tint gfx.Color, open bool) {
	p := s.monitorColors()
	bg := p.surface
	if c.Hovered() == id {
		bg = s.pal.surfaceSoft
	}
	c.Rounded(b, 9, bg)
	if c.FocusVisible() == id {
		c.RoundedBorder(b, 9, 1.5, p.cpu, bg)
	}
	c.DrawIcon(icon, gfx.Rect{X: b.X + 10, Y: b.Y + (b.Height-14)/2, Width: 14, Height: 14}, tint)
	monitorLabel(c, label, gfx.Rect{X: b.X + 31, Y: b.Y, Width: b.Width - 62, Height: b.Height}, 12, p.text, gfx.WeightSemibold)
	glyph := iconChevronRight
	if open {
		glyph = iconChevronDown
	}
	c.DrawIcon(lucideIcons[glyph], gfx.Rect{X: b.X + b.Width - 23, Y: b.Y + (b.Height-12)/2, Width: 12, Height: 12}, p.muted)
	c.HitWithOptions(id, b, label, gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: open})
}

func (s *State) monitorTile(c *gfx.Canvas, p *monitorView, b gfx.Rect, label, value, note string, icon gfx.Icon, tint gfx.Color, metric int) {
	colors := s.monitorColors()
	c.RoundedBorder(b, 10, 1, colors.border, colors.surface)
	c.DrawIcon(icon, gfx.Rect{X: b.X + 10, Y: b.Y + 12, Width: 14, Height: 14}, tint)
	monitorLabel(c, label, gfx.Rect{X: b.X + 30, Y: b.Y + 9, Width: b.Width - 40, Height: 20}, 12, colors.muted, gfx.WeightMedium)
	monitorLabel(c, value, gfx.Rect{X: b.X + 10, Y: b.Y + 32, Width: b.Width - 20, Height: 27}, 22, tint, gfx.WeightSemibold)
	monitorLabel(c, note, gfx.Rect{X: b.X + 10, Y: b.Y + 60, Width: b.Width - 20, Height: 17}, 10, colors.muted, gfx.WeightRegular)
	s.paintMonitorTrend(c, p, gfx.Rect{X: b.X + 2, Y: b.Y + 80, Width: b.Width - 4, Height: b.Height - 84}, metric, label, tint)
}

const monitorSummaryHeight = float32(92)

func (s *State) paintMonitorSummary(c *gfx.Canvas, p *monitorView, b gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	c.RoundedBorder(b, 10, 1, colors.border, colors.surface)
	labels := [...]string{m.AvailableMemory, m.LoadAverages, m.Uptime}
	values := [...]string{p.availableMemory, p.loadText, p.uptimeText}
	for i, label := range labels {
		row := gfx.Rect{X: b.X + 10, Y: b.Y + 7 + float32(i)*26, Width: b.Width - 20, Height: 26}
		left := row
		left.Width = row.Width * .50
		monitorLabel(c, label, left, 10, colors.muted, gfx.WeightRegular)
		right := row
		right.X += left.Width
		right.Width -= left.Width
		monitorRight(c, values[i], right, 10, colors.text, gfx.WeightMedium)
	}
}

func (s *State) monitorMore(c *gfx.Canvas, id gfx.ID, label string, b gfx.Rect) {
	p := s.monitorColors()
	theme := s.Theme()
	theme.Foreground = p.muted
	components.BeginButton(c, theme, id, b).XSmall().Ghost(label)
}

func (s *State) paintMonitorCores(c *gfx.Canvas, p *monitorView, b, viewport gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	c.RoundedBorder(b, 10, 1, colors.border, colors.surface)
	s.monitorSection(c, hitMonitorCores, gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: 32}, p.coresTitle, icons.Cpu, colors.cpu, !p.coresCollapsed)
	if p.coresCollapsed {
		return
	}
	c.Fill(gfx.Rect{X: b.X, Y: b.Y + 32, Width: b.Width, Height: 1}, colors.border)
	x, y, w := b.X+10, b.Y+40, b.Width-20
	if len(p.cores) == 0 {
		monitorLabel(c, m.NoCores, gfx.Rect{X: x, Y: y, Width: w, Height: 30}, 11, colors.muted, gfx.WeightRegular)
		return
	}
	count := len(p.cores)
	if !p.coresMore {
		count = min(count, 8)
	}
	half := (w - 12) / 2
	for i, core := range p.cores[:count] {
		left, top := x+float32(i%2)*(half+12), y+float32(i/2)*34
		if !monitorRowVisible(viewport, top, 25) {
			continue
		}
		monitorLabel(c, core.label, gfx.Rect{X: left, Y: top, Width: half * .57, Height: 16}, 10, colors.muted, gfx.WeightRegular)
		monitorRight(c, core.percent, gfx.Rect{X: left + half*.57, Y: top, Width: half * .43, Height: 16}, 10, colors.text, gfx.WeightMedium)
		theme := s.Theme()
		theme.Primary = colors.cpu
		theme.Muted = colors.track
		components.Progress(c, theme, gfx.Rect{X: left, Y: top + 20, Width: half, Height: 5}, float32(core.value/100))
	}
	if len(p.cores) > 8 {
		text := p.coreMoreText
		if p.coresMore {
			text = m.Collapse
		}
		moreY := y + float32((count+1)/2)*34
		if monitorRowVisible(viewport, moreY, 24) {
			s.monitorMore(c, hitMonitorCoresMore, text, gfx.Rect{X: x, Y: moreY, Width: w, Height: 24})
		}
	}
}

func (s *State) paintMonitorDisks(c *gfx.Canvas, p *monitorView, b, viewport gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	c.RoundedBorder(b, 10, 1, colors.border, colors.surface)
	s.monitorSection(c, hitMonitorDisksCollapse, gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: 32}, m.DiskSection, icons.HardDrive, colors.disk, !p.disksCollapsed)
	if p.disksCollapsed {
		return
	}
	c.Fill(gfx.Rect{X: b.X, Y: b.Y + 32, Width: b.Width, Height: 1}, colors.border)
	x, y, w := b.X+10, b.Y+40, b.Width-20
	if monitorDiskCount(p) == 0 {
		monitorLabel(c, m.NoDisks, gfx.Rect{X: x, Y: y, Width: w, Height: 35}, 11, colors.muted, gfx.WeightRegular)
		return
	}
	shown := 0
	for _, d := range p.disks {
		if p.page != 1 && !p.disksOpen && (d.virtual || shown >= 3) {
			continue
		}
		if monitorRowVisible(viewport, y, 36) {
			monitorLabel(c, d.path, gfx.Rect{X: x, Y: y, Width: w * .36, Height: 18}, 11, colors.text, gfx.WeightMedium)
			monitorRight(c, d.capacity, gfx.Rect{X: x + w*.36, Y: y, Width: w * .64, Height: 18}, 10, colors.muted, gfx.WeightRegular)
			tint := colors.cpu
			if d.value >= 70 {
				tint = colors.disk
			}
			if d.value >= 95 {
				tint = s.pal.danger
			}
			theme := s.Theme()
			theme.Primary = tint
			theme.Muted = colors.track
			components.Progress(c, theme, gfx.Rect{X: x, Y: y + 24, Width: w - 30, Height: 5}, float32(d.value/100))
			monitorRight(c, d.percent, gfx.Rect{X: x + w - 28, Y: y + 17, Width: 28, Height: 18}, 10, colors.muted, gfx.WeightMedium)
		}
		y += 43
		shown++
	}
	if p.page != 1 && (p.disksOpen || shown < len(p.disks)) {
		text := p.moreDisks
		if p.disksOpen {
			text = m.Collapse
		}
		if monitorRowVisible(viewport, y, 24) {
			s.monitorMore(c, hitMonitorDisks, text, gfx.Rect{X: x, Y: y, Width: w, Height: 24})
		}
	}
}

func (s *State) paintMonitorNetworkTile(c *gfx.Canvas, p *monitorView, b gfx.Rect, large bool) {
	colors, m := s.monitorColors(), s.messages.Monitor
	c.RoundedBorder(b, 10, 1, colors.border, colors.surface)
	c.DrawIcon(icons.Wifi, gfx.Rect{X: b.X + 10, Y: b.Y + 12, Width: 14, Height: 14}, colors.network)
	monitorLabel(c, m.Network, gfx.Rect{X: b.X + 30, Y: b.Y + 9, Width: b.Width - 40, Height: 20}, 12, colors.muted, gfx.WeightMedium)
	size := float32(12)
	if large {
		size = 18
	}
	c.DrawIcon(lucideIcons[iconDownload], gfx.Rect{X: b.X + 10, Y: b.Y + 37, Width: 11, Height: 11}, colors.network)
	monitorLabel(c, p.downText, gfx.Rect{X: b.X + 26, Y: b.Y + 31, Width: b.Width - 32, Height: 23}, size, colors.network, gfx.WeightSemibold)
	c.DrawIcon(lucideIcons[iconUpload], gfx.Rect{X: b.X + 10, Y: b.Y + 59, Width: 11, Height: 11}, colors.cpu)
	monitorLabel(c, p.upText, gfx.Rect{X: b.X + 26, Y: b.Y + 53, Width: b.Width - 32, Height: 20}, 10, colors.cpu, gfx.WeightRegular)
	plot := gfx.Rect{X: b.X + 5, Y: b.Y + 75, Width: b.Width - 10, Height: max(12, b.Height-80)}
	s.paintMonitorTrend(c, p, plot, monitorDownload, p.networkText, colors.network)
}

// paintMonitorSections paints the stat sections in b, the scrolled content. Rows entirely outside
// viewport, the part of it that shows, are skipped: a host with hundreds of mounts would
// otherwise draw every row on every frame.
func (s *State) paintMonitorSections(c *gfx.Canvas, v *remoteStatsView, b, viewport gfx.Rect) {
	p := &v.panel
	colors, m := s.monitorColors(), s.messages.Monitor
	x, y, w := b.X+monitorInset, b.Y+8, max(0, b.Width-monitorInset*2)
	switch p.page {
	case 0:
		half := (w - monitorGap) / 2
		s.monitorTile(c, p, gfx.Rect{X: x, Y: y, Width: half, Height: monitorTileHeight}, m.CPU, p.cpuText, p.cpuNote, icons.Cpu, colors.cpu, monitorCPU)
		s.monitorTile(c, p, gfx.Rect{X: x + half + monitorGap, Y: y, Width: half, Height: monitorTileHeight}, m.Memory, p.memoryText, p.memoryCapacity, icons.MemoryStick, colors.memory, monitorMemory)
		y += monitorTileHeight + monitorGap
		s.monitorTile(c, p, gfx.Rect{X: x, Y: y, Width: half, Height: monitorLowerTileHeight}, m.PrimaryDisk, p.diskText, p.diskCapacity, icons.HardDrive, colors.disk, monitorDisk)
		s.paintMonitorNetworkTile(c, p, gfx.Rect{X: x + half + monitorGap, Y: y, Width: half, Height: monitorLowerTileHeight}, false)
		y += monitorLowerTileHeight + monitorGap
		s.paintMonitorSummary(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: monitorSummaryHeight})
		y += monitorSummaryHeight + monitorGap
		if !p.noPerCore {
			h := monitorCoresHeight(p)
			s.paintMonitorCores(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: h}, viewport)
			y += h + monitorGap
		}
		h := monitorDisksHeight(p)
		s.paintMonitorDisks(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: h}, viewport)
		y += h + monitorGap
		s.monitorSection(c, hitMonitorHardware, gfx.Rect{X: x, Y: y, Width: w, Height: 28}, m.Hardware, icons.Cpu, colors.muted, p.hardwareOpen)
		y += 28
		if p.hardwareOpen {
			if p.data.Kernel != "" {
				monitorLabel(c, p.data.Kernel, gfx.Rect{X: x + 10, Y: y, Width: w - 20, Height: 24}, 11, colors.muted, gfx.WeightRegular)
				y += 24
			}
			if len(p.sample.GPUs) == 0 {
				monitorLabel(c, m.NoHardware, gfx.Rect{X: x + 10, Y: y, Width: w - 20, Height: 28}, 11, colors.muted, gfx.WeightRegular)
			} else {
				for _, part := range v.parts[min(int(statsMetricCount), len(v.parts)):] {
					monitorLabel(c, part, gfx.Rect{X: x + 10, Y: y, Width: w - 20, Height: 28}, 11, colors.muted, gfx.WeightRegular)
					y += 28
				}
			}
		}
	case 1:
		s.monitorTile(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: monitorTileHeight}, m.PrimaryDisk, p.diskText, p.diskCapacity, icons.HardDrive, colors.disk, monitorDisk)
		y += monitorTileHeight + monitorGap
		s.paintMonitorDisks(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: monitorDisksHeight(p)}, viewport)
	case 2:
		s.paintMonitorNetworkTile(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: 142}, true)
		y += 142 + monitorGap
		h := float32(32 + max(1, len(p.networks))*34 + 8)
		c.RoundedBorder(gfx.Rect{X: x, Y: y, Width: w, Height: h}, 10, 1, colors.border, colors.surface)
		monitorLabel(c, m.NetworkSection, gfx.Rect{X: x + 10, Y: y, Width: w - 20, Height: 32}, 12, colors.text, gfx.WeightSemibold)
		y += 32
		if len(p.networks) == 0 {
			if monitorRowVisible(viewport, y, 34) {
				monitorLabel(c, p.networkText, gfx.Rect{X: x + 10, Y: y, Width: w - 20, Height: 34}, 11, colors.muted, gfx.WeightRegular)
			}
		}
		for _, n := range p.networks {
			if monitorRowVisible(viewport, y, 34) {
				monitorLabel(c, n.name, gfx.Rect{X: x + 10, Y: y, Width: w*.45 - 10, Height: 34}, 10, colors.muted, gfx.WeightRegular)
				monitorRight(c, n.rates, gfx.Rect{X: x + w*.45, Y: y, Width: w*.55 - 10, Height: 34}, 10, colors.text, gfx.WeightRegular)
			}
			y += 34
		}
	}
}
