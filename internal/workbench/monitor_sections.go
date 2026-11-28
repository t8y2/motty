package workbench

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	"motty/internal/workbench/icons"
)

const monitorInset = float32(12)
const monitorControlsHeight = float32(44)
const monitorTileHeight = float32(128)
const monitorLowerTileHeight = float32(128)
const monitorGap = float32(10)

// monitorNetworkRowHeight is one interface row: its name over its addresses,
// with what it is carrying on the right.
const monitorNetworkRowHeight = float32(40)

// monitorListenRowHeight is one listening socket's row: its address and port on
// the left, the process that owns it on the right.
const monitorListenRowHeight = float32(32)

// monitorProcessRowHeight is one process row: its name and pid on the left, its
// CPU and memory shares on the right.
const monitorProcessRowHeight = float32(44)

// monitorProcessSearchRow is the room the search box takes inside the process
// card, under its header.
const monitorProcessSearchRow = float32(44)

// monitorMemoryRowHeight is one line of the memory breakdown: the pool it names
// on the left, how much of the host's memory is in it on the right.
const monitorMemoryRowHeight = float32(26)
const monitorHostSummaryHeight = float32(112)

// monitorHintHeight is the line under the process list that points at the
// window which lists every process the host runs.
const monitorHintHeight = float32(22)
const monitorHardwareHeaderHeight = float32(32)
const monitorHardwareFactHeight = float32(22)
const monitorBottomPadding = float32(20)

var monitorIntervals = [...]int{1, 2, 5, 10}

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

// monitorCenter paints a one-line label centered in b with a width-aware ellipsis.
func monitorCenter(c *gfx.Canvas, text string, b gfx.Rect, size float32, color gfx.Color, weight float32) {
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	c.LabelAligned(elide(c, text, size, weight, b.Width), b, gfx.TextStyle{Size: size, Color: color, Weight: weight}, gfx.AlignCenter)
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
func monitorGPUHeight(p *monitorView) float32 {
	if len(p.gpuRows) == 0 {
		return 0
	}
	height := float32(0)
	for index := range p.gpuRows {
		height += 94
		row := &p.gpuRows[index]
		if row.processesKnown {
			if len(row.processes) == 0 {
				height += 24
			} else {
				height += 18 + float32(len(row.processes))*26
			}
		}
		if index < len(p.gpuRows)-1 {
			height += 16
		}
	}
	return height
}

func monitorHardwareHeight(p *monitorView) float32 {
	height := monitorHardwareHeaderHeight
	if !p.hardwareOpen {
		return height
	}
	height += 8 + 4*monitorHardwareFactHeight
	height += 9 + 20 + max(30, monitorGPUHeight(p))
	return height + 8
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

// monitorMemoryHeight is the room the memory breakdown takes: its header alone
// while collapsed, and one line per pool while open, over its bottom padding.
func monitorMemoryHeight(p *monitorView) float32 {
	if !p.memoryOpen {
		return 32
	}
	return 32 + 8 + float32(len(p.memoryRows))*monitorMemoryRowHeight + 8
}

// monitorProcessRowsHeight is the room the rows take: one row each, or the line
// that says the list is empty, over the card's bottom padding.
func monitorProcessRowsHeight(p *monitorView) float32 {
	return float32(max(1, len(p.procShown)))*monitorProcessRowHeight + 8
}

// monitorProcessHeight is the room the process card takes: its header alone while
// collapsed, and its search box plus one row per process while open.
func monitorProcessHeight(p *monitorView) float32 {
	if !p.procsOpen {
		return 32
	}
	return 32 + monitorProcessSearchRow + monitorProcessRowsHeight(p)
}
func monitorContentHeight(p *monitorView, currentSSHUser string) float32 {
	h := float32(8)
	switch p.page {
	case 0:
		h += monitorTileHeight + monitorLowerTileHeight + monitorGap*3 + monitorHostSummaryHeight + monitorCoresBlock(p) + monitorDisksHeight(p)
	case 1:
		h += monitorTileHeight + monitorGap + monitorCoresBlock(p) + monitorMemoryHeight(p) + monitorGap
		h += monitorProcessHeight(p) + monitorGap + monitorHintHeight
	case 2:
		h += monitorTileHeight + monitorGap + monitorDisksHeight(p)
	case 3:
		h += 142 + monitorGap + 32 + float32(max(1, len(p.networks)))*monitorNetworkRowHeight + 8
		h += monitorGap + monitorListensHeight(p)
	}
	if p.page == 0 {
		h += monitorGap + monitorHardwareHeight(p)
		if !p.loginsUnsupported {
			h += monitorGap + monitorLoginsHeight(p, currentSSHUser)
		}
	}
	return h + monitorBottomPadding
}

func monitorLoginsHeight(p *monitorView, currentSSHUser string) float32 {
	if p.loginsClosed {
		return 32
	}
	height := float32(32 + 28)
	currentSSH := currentSSHUser != ""
	if currentSSH {
		height += 32
	}
	if len(p.data.Logins) == 0 || p.detailsFailed || !p.data.LoginsAvailable {
		if currentSSH && p.haveDetails && !p.detailsFailed && p.data.LoginsAvailable {
			return height + 8
		}
		return height + 36 + 8
	}
	height += float32(len(p.data.Logins))*43 + 16
	if p.data.LoginsTruncated {
		height += 24
	}
	return height
}

func (s *State) paintMonitorLogins(c *gfx.Canvas, p *monitorView, currentSSHUser string, b, viewport gfx.Rect) {
	colors, messages := s.monitorColors(), s.messages.Monitor
	if monitorRowVisible(viewport, b.Y, b.Height) {
		c.RoundedBorder(b, cornerPx(10), 1, colors.border, colors.surface)
	}
	s.monitorSection(c, hitMonitorLogins, gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: 32}, messages.Logins, icons.Activity, colors.cpu, !p.loginsClosed)
	if p.loginsClosed {
		return
	}
	c.Fill(gfx.Rect{X: b.X, Y: b.Y + 32, Width: b.Width, Height: 1}, colors.border)
	rowY := b.Y + 34
	if monitorRowVisible(viewport, rowY, 28) {
		monitorLabel(c, messages.LoginsHint, gfx.Rect{X: b.X + 10, Y: rowY, Width: b.Width - 20, Height: 28}, 10, colors.muted, gfx.WeightRegular)
	}
	rowY += 28
	if currentSSHUser != "" {
		if monitorRowVisible(viewport, rowY, 32) {
			monitorLabel(c, currentSSHUser, gfx.Rect{X: b.X + 10, Y: rowY, Width: b.Width*.48 - 10, Height: 21}, 12, colors.text, gfx.WeightSemibold)
			monitorRight(c, messages.CurrentSSHLogin, gfx.Rect{X: b.X + b.Width*.48, Y: rowY, Width: b.Width*.52 - 10, Height: 21}, 11, colors.muted, gfx.WeightRegular)
		}
		rowY += 32
	}
	if len(p.data.Logins) == 0 || p.detailsFailed || !p.data.LoginsAvailable {
		if currentSSHUser != "" && p.haveDetails && !p.detailsFailed && p.data.LoginsAvailable {
			return
		}
		message := messages.NoLogins
		if !p.haveDetails {
			message = messages.WaitingDetails
		} else if p.detailsFailed || !p.data.LoginsAvailable {
			message = messages.LoginsUnavailable
		}
		if monitorRowVisible(viewport, rowY, 36) {
			monitorLabel(c, message, gfx.Rect{X: b.X + 10, Y: rowY, Width: b.Width - 20, Height: 36}, 11, colors.muted, gfx.WeightRegular)
		}
		return
	}
	for _, login := range p.data.Logins {
		if monitorRowVisible(viewport, rowY, 43) {
			monitorLabel(c, login.User, gfx.Rect{X: b.X + 10, Y: rowY, Width: b.Width*.48 - 10, Height: 21}, 12, colors.text, gfx.WeightSemibold)
			monitorRight(c, login.Terminal, gfx.Rect{X: b.X + b.Width*.48, Y: rowY, Width: b.Width*.52 - 10, Height: 21}, 11, colors.muted, gfx.WeightRegular)
			monitorLabel(c, login.Since, gfx.Rect{X: b.X + 10, Y: rowY + 20, Width: b.Width - 20, Height: 19}, 10, colors.muted, gfx.WeightRegular)
		}
		rowY += 43
	}
	if p.data.LoginsTruncated && monitorRowVisible(viewport, rowY, 24) {
		monitorLabel(c, messages.LoginsTruncated, gfx.Rect{X: b.X + 10, Y: rowY, Width: b.Width - 20, Height: 24}, 10, colors.muted, gfx.WeightRegular)
	}
}

func (s *State) monitorSection(c *gfx.Canvas, id gfx.ID, b gfx.Rect, label string, icon gfx.Icon, tint gfx.Color, open bool) {
	p := s.monitorColors()
	bg := p.surface
	if c.Hovered() == id {
		bg = s.pal.surfaceSoft
	}
	c.Rounded(b, cornerPx(9), bg)
	if c.FocusVisible() == id {
		c.RoundedBorder(b, cornerPx(9), 1.5, p.cpu, bg)
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
	c.RoundedBorder(b, cornerPx(10), 1, colors.border, colors.surface)
	c.DrawIcon(icon, gfx.Rect{X: b.X + 10, Y: b.Y + 12, Width: 14, Height: 14}, tint)
	monitorLabel(c, label, gfx.Rect{X: b.X + 30, Y: b.Y + 9, Width: b.Width - 40, Height: 20}, 12, colors.muted, gfx.WeightMedium)
	monitorLabel(c, value, gfx.Rect{X: b.X + 10, Y: b.Y + 32, Width: b.Width - 20, Height: 27}, 22, tint, gfx.WeightSemibold)
	monitorLabel(c, note, gfx.Rect{X: b.X + 10, Y: b.Y + 60, Width: b.Width - 20, Height: 17}, 10, colors.muted, gfx.WeightRegular)
	s.paintMonitorTrend(c, p, gfx.Rect{X: b.X + 2, Y: b.Y + 80, Width: b.Width - 4, Height: b.Height - 84}, metric, label, tint)
}

func (s *State) paintMonitorSummary(c *gfx.Canvas, p *monitorView, b gfx.Rect, session *Session) {
	colors, m := s.monitorColors(), s.messages.Monitor
	c.RoundedBorder(b, cornerPx(10), 1, colors.border, colors.surface)
	host := b.Inset(gfx.Insets{Left: 10, Right: 10, Top: 7})
	host.Height = 18
	s.paintHostInfoText(c, host, session)
	c.Fill(gfx.Rect{X: b.X + 10, Y: b.Y + 31, Width: max(0, b.Width-20), Height: 1}, colors.border)
	labels := [...]string{m.AvailableMemory, m.LoadAverages, m.Uptime}
	values := [...]string{p.availableMemory, p.loadText, p.uptimeText}
	for i, label := range labels {
		row := gfx.Rect{X: b.X + 10, Y: b.Y + 35 + float32(i)*24, Width: b.Width - 20, Height: 24}
		left := row
		left.Width = row.Width * .5
		monitorLabel(c, label, left, 10, colors.muted, gfx.WeightRegular)
		right := row
		right.X += left.Width
		right.Width -= left.Width
		monitorRight(c, values[i], right, 10, colors.text, gfx.WeightMedium)
	}
}

// monitorMore paints a section's quiet expand/collapse action and its hit.
func (s *State) monitorMore(c *gfx.Canvas, id gfx.ID, label string, b gfx.Rect) {
	p := s.monitorColors()
	if b.Width <= 0 || b.Height <= 0 {
		return
	}
	if c.Hovered() == id {
		c.Rounded(b, radius.control, withAlpha(s.pal.surfaceHot, 0.6))
	}
	monitorLabel(c, label, gfx.Rect{X: b.X + 4, Y: b.Y, Width: b.Width - 8, Height: b.Height}, 11, p.muted, gfx.WeightMedium)
	c.HitWithOptions(id, b, label, gfx.HitOptions{Role: gfx.RoleButton})
}

func (s *State) paintMonitorCores(c *gfx.Canvas, p *monitorView, b, viewport gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	c.RoundedBorder(b, cornerPx(10), 1, colors.border, colors.surface)
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
		dashboardBar(c, gfx.Rect{X: left, Y: top + 20, Width: half, Height: 5}, core.value, colors.cpu, colors.track)
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

// paintMonitorGPUs draws each accelerator as metric bars, then the compute
// processes the NVIDIA driver reports against that card.
func (s *State) paintMonitorGPUs(c *gfx.Canvas, p *monitorView, b, viewport gfx.Rect) float32 {
	colors := s.monitorColors()
	x, y, w := b.X+10, b.Y, b.Width-20
	for index := range p.gpuRows {
		row := &p.gpuRows[index]
		if monitorRowVisible(viewport, y, 18) {
			monitorLabel(c, row.name, gfx.Rect{X: x, Y: y, Width: w, Height: 18}, 11, colors.memory, gfx.WeightSemibold)
		}
		y += 22
		rows := [...]struct {
			label, value string
			valueValue   float64
		}{
			{p.powerLabel, row.power, row.powerValue},
			{p.memoryLabel, row.memory, row.memoryValue},
			{p.temperatureLabel, row.temperature, row.temperatureValue},
			{p.utilLabel, row.util, row.utilValue},
		}
		for _, metric := range rows {
			if monitorRowVisible(viewport, y, 16) {
				monitorLabel(c, metric.label, gfx.Rect{X: x, Y: y, Width: w * .15, Height: 16}, 10, colors.muted, gfx.WeightRegular)
				monitorRight(c, metric.value, gfx.Rect{X: x + w*.15, Y: y, Width: w * .42, Height: 16}, 10, colors.text, gfx.WeightMedium)
				if metric.valueValue >= 0 {
					dashboardBar(c, gfx.Rect{X: x + w*.58, Y: y + 5, Width: w * .42, Height: 5}, metric.valueValue, s.monitorMetricColor(metric.valueValue), colors.track)
				}
			}
			y += 18
		}
		if !row.processesKnown {
			// This vendor's processes are not collected; say nothing rather
			// than report a card with none.
		} else if len(row.processes) == 0 {
			if monitorRowVisible(viewport, y, 24) {
				monitorLabel(c, p.noProcessLabel, gfx.Rect{X: x, Y: y, Width: w, Height: 24}, 10, colors.muted, gfx.WeightRegular)
			}
			y += 24
		} else {
			if monitorRowVisible(viewport, y, 18) {
				monitorLabel(c, p.processLabel, gfx.Rect{X: x, Y: y, Width: w, Height: 18}, 10, colors.muted, gfx.WeightSemibold)
			}
			y += 18
			for _, process := range row.processes {
				if monitorRowVisible(viewport, y, 20) {
					monitorLabel(c, process.label, gfx.Rect{X: x + 10, Y: y, Width: w - 82, Height: 20}, 10, colors.text, gfx.WeightRegular)
					monitorRight(c, process.memory, gfx.Rect{X: x + w - 70, Y: y, Width: 60, Height: 20}, 10, colors.muted, gfx.WeightMedium)
				}
				y += 26
			}
		}
		if index < len(p.gpuRows)-1 {
			y += 16
		}
	}
	return y
}

func (s *State) monitorMetricColor(value float64) gfx.Color {
	colors := s.monitorColors()
	switch {
	case value >= 95:
		return s.pal.danger
	case value >= 75:
		return colors.disk
	default:
		return colors.memory
	}
}

func (s *State) paintMonitorDisks(c *gfx.Canvas, p *monitorView, b, viewport gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	c.RoundedBorder(b, cornerPx(10), 1, colors.border, colors.surface)
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
			dashboardBar(c, gfx.Rect{X: x, Y: y + 24, Width: w - 30, Height: 5}, d.value, tint, colors.track)
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

// paintMonitorMemory draws the host's memory as its kernel divides it: one line
// per pool, the pool on the left and how much of the host is in it on the right.
func (s *State) paintMonitorMemory(c *gfx.Canvas, p *monitorView, b, viewport gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	if monitorRowVisible(viewport, b.Y, b.Height) {
		c.RoundedBorder(b, cornerPx(10), 1, colors.border, colors.surface)
	}
	if monitorRowVisible(viewport, b.Y, 32) {
		s.monitorSection(c, hitMonitorMemory, gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: 32}, m.MemoryInfoSection, icons.MemoryStick, colors.memory, p.memoryOpen)
	}
	if !p.memoryOpen {
		return
	}
	if monitorRowVisible(viewport, b.Y+32, 1) {
		c.Fill(gfx.Rect{X: b.X, Y: b.Y + 32, Width: b.Width, Height: 1}, colors.border)
	}
	x, y, w := b.X+10, b.Y+40, b.Width-20
	for _, row := range p.memoryRows {
		if monitorRowVisible(viewport, y, monitorMemoryRowHeight) {
			monitorLabel(c, row.label, gfx.Rect{X: x, Y: y, Width: w * .5, Height: monitorMemoryRowHeight}, 11, colors.muted, gfx.WeightRegular)
			monitorRight(c, row.value, gfx.Rect{X: x + w*.5, Y: y, Width: w * .5, Height: monitorMemoryRowHeight}, 11, colors.text, gfx.WeightMedium)
		}
		y += monitorMemoryRowHeight
	}
}

// paintMonitorProcessHeader paints the process card's top row: what the list is,
// how many rows it reported, whether it is open, and the sort control, whose
// menu hangs off the rectangle this painter last left.
func (s *State) paintMonitorProcessHeader(c *gfx.Canvas, p *monitorView, b gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	p.sortAnchor = gfx.Rect{}
	header := gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: 32}
	if c.Hovered() == hitMonitorProcesses {
		c.Rounded(header.Inset(gfx.Insets{Left: 1, Right: 1, Top: 1}), cornerPx(9), s.pal.surfaceSoft)
	}
	c.DrawIcon(icons.Process, gfx.Rect{X: header.X + 10, Y: header.Y + (header.Height-14)/2, Width: 14, Height: 14}, colors.cpu)
	monitorLabel(c, fmt.Sprintf("%s · %d", m.ProcessSection, len(p.procs)), gfx.Rect{X: header.X + 31, Y: header.Y, Width: header.Width - 62, Height: header.Height}, 12, colors.text, gfx.WeightSemibold)
	glyph := iconChevronRight
	if p.procsOpen {
		glyph = iconChevronDown
	}
	c.DrawIcon(lucideIcons[glyph], gfx.Rect{X: header.X + header.Width - 23, Y: header.Y + (header.Height-12)/2, Width: 12, Height: 12}, colors.muted)
	// The header collapses the card; the sort control drawn over it keeps its
	// own hit, so the two never answer the same press.
	c.HitWithOptions(hitMonitorProcesses, header, m.ProcessSection, gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: p.procsOpen})
	sortButton := gfx.Rect{X: header.X + header.Width - 51, Y: header.Y + 4, Width: 24, Height: 24}
	sorts := [4]string{m.SortCPU, m.SortMemory, m.SortPID, m.SortName}
	s.paintIconButtonWithOptions(c, hitMonitorProcSort, iconArrowUpDown, sortButton, m.SortBy, false, iconButtonQuiet,
		colors.surface, 0, gfx.HitOptions{Expandable: true, Expanded: p.sortMenu, Value: sorts[p.procSort]})
	p.sortAnchor = sortButton
	// The divider belongs to the open card, under its header. A collapsed card
	// ends at this row: drawing it there would double the card's bottom border.
	if p.procsOpen {
		c.Fill(gfx.Rect{X: b.X, Y: b.Y + 31, Width: b.Width, Height: 1}, colors.border)
	}
}

// paintMonitorProcessRows draws the rows the card shows, in the order the sort
// control chose. The card's surface, border and header are the page's, so this
// painter fills only the room the rows take.
func (s *State) paintMonitorProcessRows(c *gfx.Canvas, p *monitorView, b, viewport gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	x, y, w := b.X+10, b.Y, b.Width-20
	if len(p.procShown) == 0 {
		empty := m.ProcessEmpty
		if len(p.procs) > 0 {
			empty = m.ProcessNoMatch
		}
		if monitorRowVisible(viewport, y, monitorProcessRowHeight) {
			monitorLabel(c, empty, gfx.Rect{X: x, Y: y, Width: w, Height: monitorProcessRowHeight}, 11, colors.muted, gfx.WeightRegular)
		}
		return
	}
	for index, process := range p.procShown {
		if monitorRowVisible(viewport, y, monitorProcessRowHeight) {
			row := gfx.Rect{X: b.X, Y: y, Width: b.Width, Height: monitorProcessRowHeight}
			// A row cut by the viewport edge keeps only the part on screen, so
			// neither its surface nor its hit reaches the footer.
			visible := monitorClipRow(viewport, row)
			// The row under the pointer or the row the open menu belongs to
			// reads as one surface, so the hit and the action are the same row.
			if c.Hovered() == hitMonitorProcRows.At(index) || p.procMenu && p.procMenuPID == process.pid {
				c.Rounded(visible.Inset(gfx.Insets{Left: 4, Right: 4, Top: 2, Bottom: 2}), cornerPx(7), s.pal.surfaceSoft)
			}
			monitorLabel(c, fmt.Sprintf("%s · %d", process.name, process.pid),
				gfx.Rect{X: x, Y: y + 2, Width: w * .58, Height: 20}, 11, colors.text, gfx.WeightMedium)
			monitorRight(c, process.cpu, gfx.Rect{X: x + w*.58, Y: y + 2, Width: w * .42, Height: 20}, 10, colors.cpu, gfx.WeightMedium)
			monitorLabel(c, process.rss, gfx.Rect{X: x, Y: y + 22, Width: w * .58, Height: 18}, 9, colors.muted, gfx.WeightRegular)
			monitorRight(c, process.memory, gfx.Rect{X: x + w*.58, Y: y + 22, Width: w * .42, Height: 18}, 9, colors.memory, gfx.WeightRegular)
			c.HitWithOptions(hitMonitorProcRows.At(index), visible, process.name, gfx.HitOptions{Role: gfx.RoleButton})
		}
		y += monitorProcessRowHeight
	}
}

// monitorClipRow keeps the part of a row the viewport shows. A caller that has
// already tested monitorRowVisible gets a rect with some height back.
func monitorClipRow(viewport, row gfx.Rect) gfx.Rect {
	if row.Y < viewport.Y {
		cut := viewport.Y - row.Y
		row.Y, row.Height = viewport.Y, row.Height-cut
	}
	if bottom := viewport.Y + viewport.Height; row.Y+row.Height > bottom {
		row.Height = max(0, bottom-row.Y)
	}
	return row
}

func monitorSystemName(system sshconn.RemoteSystem) string {
	if system == sshconn.SystemUnknown {
		return "—"
	}
	if name := hostSystemDisplayName(system); name != "" {
		return name
	}
	name := system.String()
	if name == "" {
		return "—"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func monitorHardwareVersion(p *monitorView, system sshconn.RemoteSystem) string {
	version := strings.TrimSpace(p.data.Kernel)
	if system == sshconn.SystemWindows {
		version = strings.TrimSpace(strings.TrimPrefix(version, "Windows"))
	}
	if version == "" {
		return "—"
	}
	return version
}

func monitorHardwareFact(c *gfx.Canvas, b gfx.Rect, label, value string, colors monitorColors) {
	left := b
	left.Width *= .42
	monitorLabel(c, label, left, 10, colors.muted, gfx.WeightRegular)
	right := b
	right.X += left.Width
	right.Width -= left.Width
	monitorRight(c, value, right, 10, colors.text, gfx.WeightMedium)
}

func (s *State) paintMonitorHardware(c *gfx.Canvas, p *monitorView, session *Session, b, viewport gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	if monitorRowVisible(viewport, b.Y, b.Height) {
		c.RoundedBorder(b, cornerPx(10), 1, colors.border, colors.surface)
	}
	s.monitorSection(c, hitMonitorHardware, gfx.Rect{X: b.X, Y: b.Y, Width: b.Width, Height: monitorHardwareHeaderHeight}, m.Hardware, icons.Cpu, colors.muted, p.hardwareOpen)
	if !p.hardwareOpen {
		return
	}
	c.Fill(gfx.Rect{X: b.X, Y: b.Y + monitorHardwareHeaderHeight, Width: b.Width, Height: 1}, colors.border)
	system := monitorSystem(session)
	versionLabel := m.HardwareKernelVersion
	if system == sshconn.SystemWindows {
		versionLabel = m.HardwareSystemVersion
	}
	coreCount := p.data.CPUCount
	if coreCount == 0 {
		coreCount = len(p.sample.Cores)
	}
	coreText, memoryText := "—", "—"
	if coreCount > 0 {
		coreText = strconv.Itoa(coreCount)
	}
	if p.sample.MemoryTotalBytes > 0 {
		memoryText = monitorBytes(p.sample.MemoryTotalBytes)
	}
	facts := [...]struct {
		label, value string
	}{
		{m.HardwareSystem, monitorSystemName(system)},
		{versionLabel, monitorHardwareVersion(p, system)},
		{m.HardwareLogicalProcessors, coreText},
		{m.HardwarePhysicalMemory, memoryText},
	}
	x, y, w := b.X+10, b.Y+40, b.Width-20
	for _, fact := range facts {
		if monitorRowVisible(viewport, y, monitorHardwareFactHeight) {
			monitorHardwareFact(c, gfx.Rect{X: x, Y: y, Width: w, Height: monitorHardwareFactHeight}, fact.label, fact.value, colors)
		}
		y += monitorHardwareFactHeight
	}
	c.Fill(gfx.Rect{X: x, Y: y, Width: w, Height: 1}, colors.border)
	y += 9
	if monitorRowVisible(viewport, y, 20) {
		monitorLabel(c, m.GPU, gfx.Rect{X: x, Y: y, Width: w, Height: 20}, 10, colors.muted, gfx.WeightSemibold)
	}
	y += 20
	if len(p.gpuRows) == 0 {
		if monitorRowVisible(viewport, y, 30) {
			monitorLabel(c, m.GPUMetricsUnavailable, gfx.Rect{X: x, Y: y, Width: w, Height: 30}, 10, colors.muted, gfx.WeightRegular)
		}
		return
	}
	s.paintMonitorGPUs(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: monitorGPUHeight(p)}, viewport)
}

func (s *State) paintMonitorNetworkTile(c *gfx.Canvas, p *monitorView, b gfx.Rect, large bool) {
	colors, m := s.monitorColors(), s.messages.Monitor
	c.RoundedBorder(b, cornerPx(10), 1, colors.border, colors.surface)
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
func (s *State) paintMonitorSections(c *gfx.Canvas, session *Session, v *remoteStatsView, b, viewport gfx.Rect) {
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
		s.paintMonitorSummary(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: monitorHostSummaryHeight}, session)
		y += monitorHostSummaryHeight + monitorGap
		if !p.noPerCore {
			h := monitorCoresHeight(p)
			s.paintMonitorCores(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: h}, viewport)
			y += h + monitorGap
		}
		h := monitorDisksHeight(p)
		s.paintMonitorDisks(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: h}, viewport)
		y += h + monitorGap
		h = monitorHardwareHeight(p)
		s.paintMonitorHardware(c, p, session, gfx.Rect{X: x, Y: y, Width: w, Height: h}, viewport)
		y += h + monitorGap
		if !p.loginsUnsupported {
			currentSSHUser := s.monitorCurrentSSHUser(session)
			h = monitorLoginsHeight(p, currentSSHUser)
			s.paintMonitorLogins(c, p, currentSSHUser, gfx.Rect{X: x, Y: y, Width: w, Height: h}, viewport)
		}
	case 2:
		s.monitorTile(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: monitorTileHeight}, m.PrimaryDisk, p.diskText, p.diskCapacity, icons.HardDrive, colors.disk, monitorDisk)
		y += monitorTileHeight + monitorGap
		s.paintMonitorDisks(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: monitorDisksHeight(p)}, viewport)
	case 3:
		s.paintMonitorNetworkTile(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: 142}, true)
		y += 142 + monitorGap
		h := 32 + float32(max(1, len(p.networks)))*monitorNetworkRowHeight + 8
		c.RoundedBorder(gfx.Rect{X: x, Y: y, Width: w, Height: h}, cornerPx(10), 1, colors.border, colors.surface)
		monitorLabel(c, m.NetworkSection, gfx.Rect{X: x + 10, Y: y, Width: w - 20, Height: 32}, 12, colors.text, gfx.WeightSemibold)
		y += 32
		if len(p.networks) == 0 {
			if monitorRowVisible(viewport, y, monitorNetworkRowHeight) {
				monitorLabel(c, p.networkText, gfx.Rect{X: x + 10, Y: y, Width: w - 20, Height: monitorNetworkRowHeight}, 11, colors.muted, gfx.WeightRegular)
			}
		}
		for _, n := range p.networks {
			if monitorRowVisible(viewport, y, monitorNetworkRowHeight) {
				nameWidth := w*.45 - 10
				monitorLabel(c, n.name, gfx.Rect{X: x + 10, Y: y + 3, Width: nameWidth, Height: 18}, 10, colors.text, gfx.WeightRegular)
				if n.addr != "" {
					monitorLabel(c, n.addr, gfx.Rect{X: x + 10, Y: y + 20, Width: nameWidth, Height: 14}, 9, colors.muted, gfx.WeightRegular)
				}
				monitorRight(c, n.rates, gfx.Rect{X: x + w*.45, Y: y, Width: w*.55 - 10, Height: monitorNetworkRowHeight}, 10, colors.text, gfx.WeightRegular)
			}
			y += monitorNetworkRowHeight
		}
		// The interfaces box is h tall and the rows only walked its header and
		// rows: step over its bottom padding before the gap, or the next card
		// sits against it.
		y += 8 + monitorGap
		h = monitorListensHeight(p)
		c.RoundedBorder(gfx.Rect{X: x, Y: y, Width: w, Height: h}, cornerPx(10), 1, colors.border, colors.surface)
		s.monitorSection(c, hitMonitorListens, gfx.Rect{X: x, Y: y, Width: w, Height: 32},
			fmt.Sprintf("%s · %d", m.ListenSection, len(p.listens)), icons.Server, colors.network, p.listensOpen)
		if !p.listensOpen {
			break
		}
		c.Fill(gfx.Rect{X: x, Y: y + 32, Width: w, Height: 1}, colors.border)
		rowY := y + 40
		if len(p.listens) == 0 {
			monitorLabel(c, m.NoListens, gfx.Rect{X: x + 10, Y: rowY, Width: w - 20, Height: monitorListenRowHeight}, 11, colors.muted, gfx.WeightRegular)
			break
		}
		for _, l := range p.listens {
			if monitorRowVisible(viewport, rowY, monitorListenRowHeight) {
				tint := colors.network
				if strings.HasPrefix(l.proto, "udp") {
					tint = colors.cpu
				}
				monitorLabel(c, l.proto, gfx.Rect{X: x + 10, Y: rowY, Width: 34, Height: monitorListenRowHeight}, 9, tint, gfx.WeightMedium)
				monitorLabel(c, fmt.Sprintf("%s:%d", l.address, l.port), gfx.Rect{X: x + 44, Y: rowY, Width: w*.50 - 44, Height: monitorListenRowHeight}, 10, colors.text, gfx.WeightRegular)
				monitorRight(c, monitorListenOwner(l), gfx.Rect{X: x + w*.52, Y: rowY, Width: w*.48 - 10, Height: monitorListenRowHeight}, 10, colors.muted, gfx.WeightRegular)
			}
			rowY += monitorListenRowHeight
		}
	}
}

// monitorProcessTopHeight is the part of the process tab above its card: the
// two meters, the cores and the memory breakdown, and the gap under the last
// card for the one that follows it.
func monitorProcessTopHeight(p *monitorView) float32 {
	return 8 + monitorTileHeight + monitorGap + monitorCoresBlock(p) + monitorMemoryHeight(p) + monitorGap
}

// monitorProcessHintHeight is the line under the card and the page's bottom
// padding after it.
func monitorProcessHintHeight() float32 {
	return monitorGap + monitorHintHeight + monitorBottomPadding
}

// paintMonitorProcessTop draws the process tab down to the memory card; the
// search box and the list follow it as declarations of the page.
func (s *State) paintMonitorProcessTop(c *gfx.Canvas, p *monitorView, b, viewport gfx.Rect) {
	colors, m := s.monitorColors(), s.messages.Monitor
	x, y, w := b.X+monitorInset, b.Y+8, max(0, b.Width-monitorInset*2)
	half := (w - monitorGap) / 2
	s.monitorTile(c, p, gfx.Rect{X: x, Y: y, Width: half, Height: monitorTileHeight}, m.CPU, p.cpuText, p.cpuNote, icons.Cpu, colors.cpu, monitorCPU)
	s.monitorTile(c, p, gfx.Rect{X: x + half + monitorGap, Y: y, Width: half, Height: monitorTileHeight}, m.Memory, p.memoryText, p.memoryCapacity, icons.MemoryStick, colors.memory, monitorMemory)
	y += monitorTileHeight + monitorGap
	if !p.noPerCore {
		h := monitorCoresHeight(p)
		s.paintMonitorCores(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: h}, viewport)
		y += h + monitorGap
	}
	s.paintMonitorMemory(c, p, gfx.Rect{X: x, Y: y, Width: w, Height: monitorMemoryHeight(p)}, viewport)
}

// paintMonitorProcessHint paints, under the card, where the whole host's list
// lives: this page shows what it samples, not every process.
func (s *State) paintMonitorProcessHint(c *gfx.Canvas, b, viewport gfx.Rect) {
	y := b.Y + monitorGap
	if !monitorRowVisible(viewport, y, monitorHintHeight) {
		return
	}
	monitorCenter(c, s.messages.Monitor.ProcessFullListHint,
		gfx.Rect{X: b.X + monitorInset, Y: y, Width: max(0, b.Width-2*monitorInset), Height: monitorHintHeight},
		10, s.monitorColors().muted, gfx.WeightRegular)
}

// monitorListensHeight is the room the listening-port section takes: its header
// alone while collapsed, and one row per socket while open.
func monitorListensHeight(p *monitorView) float32 {
	if !p.listensOpen {
		return 32
	}
	if len(p.listens) == 0 {
		return 32 + 8 + monitorListenRowHeight + 8
	}
	return 32 + 8 + float32(len(p.listens))*monitorListenRowHeight + 8
}

// monitorListenOwner is the process that owns the socket, with its pid where the
// host reports one. The protocol has its own column, so an unknown owner is a
// dash rather than a second reading of it.
func monitorListenOwner(l monitorListenText) string {
	if l.process != "" && l.process != "-" {
		if l.pid > 0 {
			return fmt.Sprintf("%s %d", l.process, l.pid)
		}
		return l.process
	}
	return "—"
}
