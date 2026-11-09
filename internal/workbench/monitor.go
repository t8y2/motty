package workbench

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

var monitorHits = tree.Feature("connection-monitor")
var (
	hitMonitorDivider       = tree.ID(monitorHits, "divider")
	hitMonitorClose         = tree.ID(monitorHits, "close")
	hitMonitorSide          = tree.ID(monitorHits, "side")
	hitMonitorPause         = tree.ID(monitorHits, "pause")
	hitMonitorInterval      = tree.ID(monitorHits, "interval")
	hitMonitorMenuDismiss   = tree.ID(monitorHits, "interval-dismiss")
	hitMonitorEvery1        = tree.ID(monitorHits, "every-1")
	hitMonitorEvery2        = tree.ID(monitorHits, "every-2")
	hitMonitorEvery5        = tree.ID(monitorHits, "every-5")
	hitMonitorSudo          = tree.ID(monitorHits, "sudo")
	hitMonitorDisks         = tree.ID(monitorHits, "more-disks")
	hitMonitorHardware      = tree.ID(monitorHits, "hardware")
	hitMonitorOverview      = tree.ID(monitorHits, "overview")
	hitMonitorDiskPage      = tree.ID(monitorHits, "disk-page")
	hitMonitorNetworkPage   = tree.ID(monitorHits, "network-page")
	hitMonitorCores         = tree.ID(monitorHits, "cores")
	hitMonitorCoresMore     = tree.ID(monitorHits, "cores-more")
	hitMonitorDisksCollapse = tree.ID(monitorHits, "disks-collapse")
	hitMonitorRetry         = tree.ID(monitorHits, "retry")
	hitMonitorScroll        = tree.ID(monitorHits, "scroll")
	hitMonitorThumb         = tree.ID(monitorHits, "scroll-thumb")
)

const monitorWidth = layoutcore.DefaultMonitorWidth

// Activating a quiet pane must not depend on its terminal producing another
// wake-up before the visible monitor starts following it.
func (state *State) syncMonitorTarget() {
	if state.monitorMenuSession != state.activeSessionID {
		state.monitorMenu = false
	}
	if state.monitorResizeWorkspace != state.deck.ActiveWorkspace() || state.monitorResizeSession != state.activeSessionID {
		state.monitorResizing = false
	}
	if state.monitorShown(state.deck.ActiveWorkspace()) {
		state.ensureMonitor(state.contextSession())
	}
}

type monitorDiskText struct {
	path, capacity, percent string
	value                   float64
	virtual                 bool
}
type monitorNetworkText struct{ name, rates string }
type monitorCoreText struct {
	label, percent string
	value          float64
}

// Fixed storage bounds the last minute of samples, independent of session age.
type monitorView struct {
	emptyLines                                []string
	emptyText                                 string
	emptyWidth                                float32
	page                                      int
	coresCollapsed, coresMore, disksCollapsed bool
	// noPerCore is a host whose system has no per-core source (macOS): the
	// section is left out rather than shown empty.
	noPerCore                                                 bool
	cores                                                     []monitorCoreText
	coresTitle, coreMoreText, cpuNote, diskText, diskCapacity string
	diskValue                                                 float64
	diskAvailable                                             bool
	dragging                                                  bool
	grab                                                      float32

	worker                             *remotestats.DetailsWorker
	data                               remotestats.Details
	sample                             remotestats.Sample
	haveDetails, detailsFailed, paused bool
	interval                           int
	disksOpen, hardwareOpen            bool
	scroll, contentHeight              float32
	count                              int
	origin                             time.Time
	x, cpu, memory, disk, down, up     [61]float32
	diskIdentity, networkIdentity      string
	charts                             [4]monitorChart
	refreshStarted                     time.Duration
	refreshVersion                     uint32
	refreshRunning                     bool
	refreshReset                       bool
	version                            uint32
	cpuText                            string
	memoryText                         string
	memoryCapacity                     string
	availableMemory                    string
	downText                           string
	upText                             string
	loadText                           string
	coresText                          string
	uptimeText                         string
	networkText                        string
	intervalText                       string
	moreDisks                          string
	disks                              []monitorDiskText
	networks                           []monitorNetworkText
	language                           string
}

// Panel width is workspace state. Narrow windows use a drawer without
// overwriting the preferred desktop width.
func (state *State) monitorShown(workspace layoutcore.WorkspaceID) bool {
	c, ok := state.deck.Context(workspace)
	return ok && c.MonitorOpen
}
func (state *State) monitorPanelWidth(workspace layoutcore.WorkspaceID, size gfx.Size) float32 {
	c, _ := state.deck.Context(workspace)
	width := c.MonitorWidth
	if width == 0 {
		if state.monitorWidth > 0 {
			width = state.monitorWidth
		} else {
			width = monitorWidth
		}
	}
	files := float32(0)
	if c.Open {
		files = state.contextPanelExtent(state.baseWorkspacePixelBounds(size), c.Width) + 1
	}
	return min(max(width, layoutcore.MinMonitorWidth), min(layoutcore.MaxMonitorWidth, max(layoutcore.MinMonitorWidth, state.baseWorkspacePixelBounds(size).Width-files-minPaneWidth-1)))
}

// aiPreferredWidth returns the configured or default width of the AI panel,
// without querying aiPanelLimits or aiSharedWidth (avoiding circular recursion
// with independentMonitorReserve).
func (state *State) aiPreferredWidth(size gfx.Size) float32 {
	if state.ai.panelWidth > 0 {
		return max(state.ai.panelWidth, aiPanelMin)
	}
	return min(aiPanelWidth, size.Width*0.42)
}

// monitorAndAICanCoexist reports whether the workspace has room to dock both the
// resource monitor and the assistant beside the terminal and context panels.
func (state *State) monitorAndAICanCoexist(workspace layoutcore.WorkspaceID, size gfx.Size) bool {
	if size.Width < contextDrawerAt || size.Width < aiDockAt {
		return false
	}
	base := state.baseWorkspacePixelBounds(size)
	files := float32(0)
	if c, ok := state.deck.Context(workspace); ok && c.Open && size.Width >= contextDrawerAt {
		files = state.contextPanelExtent(base, c.Width) + 1
	}
	monitorWidth := state.monitorPanelWidth(workspace, size) + 1
	aiWidth := state.aiPreferredWidth(size) + 1
	minTerminal := max(minPaneWidth, aiMinTerminal)
	return base.Width-files-monitorWidth-aiWidth >= minTerminal
}

func (state *State) independentMonitorReserve(workspace layoutcore.WorkspaceID, size gfx.Size) float32 {
	if !state.monitorShown(workspace) || size.Width < contextDrawerAt {
		return 0
	}
	c, _ := state.deck.Context(workspace)
	files := float32(0)
	if c.Open {
		files = state.contextPanelExtent(state.baseWorkspacePixelBounds(size), c.Width) + 1
	}
	width := state.monitorPanelWidth(workspace, size)
	aiWidth := float32(0)
	if state.aiPanelVisible() {
		if !state.monitorAndAICanCoexist(workspace, size) {
			return 0
		}
		aiWidth = state.aiPreferredWidth(size) + 1
	}
	if state.baseWorkspacePixelBounds(size).Width-files-width-1-aiWidth < minPaneWidth {
		return 0
	}
	return width + 1
}
func (state *State) monitorDrawer(size gfx.Size) bool {
	return state.monitorShown(state.deck.ActiveWorkspace()) && state.independentMonitorReserve(state.deck.ActiveWorkspace(), size) == 0
}

func (state *State) ensureMonitor(session *Session) {
	if session == nil || !state.monitorOffered(session) {
		return
	}
	if session.stats == nil {
		session.stats = &remoteStatsView{state: remoteStatsMeasuring}
		session.stats.panel.interval = 2
	}
	v := session.stats
	system := session.host.System
	if !monitorSupported(system) {
		v.state = remoteStatsUnsupported
		return
	}
	if v.panel.paused {
		return
	}
	v.panel.noPerCore = system == sshconn.SystemDarwin
	runner := monitorRunnerFor(session)
	if v.worker == nil && v.state < remoteStatsUnsupported {
		v.worker = startMonitorWorker(runner, state.notifyFor(session), system, v.panel.interval)
	}
	if v.panel.worker == nil && v.state < remoteStatsUnsupported {
		v.panel.worker = startMonitorDetails(runner, state.notifyFor(session), system, 5)
	}
}

func (state *State) showMonitor() {
	session := state.contextSession()
	if session == nil {
		return
	}
	id := state.deck.ActiveWorkspace()
	if state.aiPanelVisible() && !state.monitorAndAICanCoexist(id, state.viewport) {
		state.setAIPanelOpen(false)
	}
	c, _ := state.deck.Context(id)
	c.MonitorOpen = true
	if state.monitorWidth > 0 && (c.MonitorWidth <= 0 || c.MonitorWidth == layoutcore.DefaultMonitorWidth) {
		c.MonitorWidth = state.monitorWidth
	}
	_ = state.deck.SetContext(id, c)
	state.ensureMonitor(session)
	state.monitorMenu = false
	state.contextRect = gfx.Rect{}
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
	state.save()
}

func (state *State) closeMonitor() {
	id := state.deck.ActiveWorkspace()
	c, _ := state.deck.Context(id)
	c.MonitorOpen = false
	state.monitorResizing = false
	_ = state.deck.SetContext(id, c)
	state.stopRemoteStats(state.contextSession())
	state.monitorMenu = false
	state.monitorRect = gfx.Rect{}
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
	state.save()
}

func (state *State) clickMonitor(id gfx.ID) bool {
	switch id {
	case hitMonitorSide:
		state.toggleMonitorSide()
		return true
	case hitMonitorClose:
		state.closeMonitor()
		return true
	case hitMonitorMenuDismiss:
		state.monitorMenu = false
		return true
	}
	s := state.contextSession()
	if s == nil || s.stats == nil {
		return false
	}
	v := s.stats
	switch id {
	case hitMonitorSudo:
		state.monitorMenu = false
		state.toggleSudoCommands(s, false)
	case hitMonitorPause:
		v.panel.refreshReset = true
		v.panel.paused = !v.panel.paused
		if v.panel.paused {
			if v.worker != nil {
				v.worker.Close()
				v.worker = nil
			}
			if v.panel.worker != nil {
				v.panel.worker.Close()
				v.panel.worker = nil
			}
		} else {
			v.state = remoteStatsMeasuring
			state.ensureMonitor(s)
		}
	case hitMonitorInterval:
		state.monitorMenu = !state.monitorMenu
		state.monitorMenuSession = s.ID
	case hitMonitorEvery1, hitMonitorEvery2, hitMonitorEvery5:
		if state.monitorMenuSession != s.ID {
			state.monitorMenu = false
			return true
		}
		n := 2
		if id == hitMonitorEvery1 {
			n = 1
		}
		if id == hitMonitorEvery5 {
			n = 5
		}
		v.panel.interval = n
		v.panel.refreshReset = true
		state.monitorMenu = false
		if v.worker != nil {
			v.worker.Close()
			v.worker = nil
		}
		if !v.panel.paused {
			v.state = remoteStatsMeasuring
			state.ensureMonitor(s)
		}
		state.formatMonitor(v)
	case hitMonitorDisks:
		v.panel.disksOpen = !v.panel.disksOpen
	case hitMonitorHardware:
		v.panel.hardwareOpen = !v.panel.hardwareOpen
	case hitMonitorOverview, hitMonitorDiskPage, hitMonitorNetworkPage:
		v.panel.page = 0
		if id == hitMonitorDiskPage {
			v.panel.page = 1
		}
		if id == hitMonitorNetworkPage {
			v.panel.page = 2
		}
		v.panel.scroll = 0
	case hitMonitorCores:
		v.panel.coresCollapsed = !v.panel.coresCollapsed
	case hitMonitorCoresMore:
		v.panel.coresMore = !v.panel.coresMore
	case hitMonitorDisksCollapse:
		v.panel.disksCollapsed = !v.panel.disksCollapsed
	case hitMonitorRetry:
		state.stopRemoteStats(s)
		state.ensureMonitor(s)
	default:
		return false
	}
	return true
}

func (state *State) toggleMonitorSide() {
	id := state.deck.ActiveWorkspace()
	c, ok := state.deck.Context(id)
	if !ok || !c.MonitorOpen {
		return
	}
	c.MonitorLeft = !c.MonitorLeft
	if state.deck.SetContext(id, c) != nil {
		return
	}
	state.monitorResizing = false
	state.draggedContextDivider = false
	state.monitorMenu = false
	state.monitorRect, state.contextRect = gfx.Rect{}, gfx.Rect{}
	state.fitWorkspaceTerminals(id)
	state.fitSFTP(state.viewport)
	state.save()
}

// One bounded, timestamped buffer for every metric; missing observations remain
// gaps rather than zeroes. Slow disk metadata is held only while available.
func (state *State) sampleMonitor(v *remoteStatsView, sample remotestats.Sample) {
	p := &v.panel
	p.sample = sample
	if p.interval == 0 {
		p.interval = 2
	}
	state.formatMonitor(v)
	if p.origin.IsZero() {
		p.origin = time.Now()
	}
	p.appendHistory(float32(time.Since(p.origin).Seconds()))
}

func (p *monitorView) appendHistory(seconds float32) {
	if p.count > 0 && seconds-p.x[p.count-1] > max(5, float32(p.interval*3)) {
		// Pause, lost sampling, or a slow response must not draw an interpolated
		// line across an interval for which no observation exists.
		p.pushHistory(p.x[p.count-1] + float32(max(p.interval, 1)))
	}
	p.pushHistory(seconds)
	i := p.count - 1
	sample := p.sample
	if !sample.CPUUnavailable {
		p.cpu[i] = float32(sample.CPUPercent)
	}
	if sample.MemoryTotalBytes > 0 {
		p.memory[i] = 100 * float32(sample.MemoryUsedBytes) / float32(sample.MemoryTotalBytes)
	}
	if p.diskAvailable && !p.detailsFailed {
		p.disk[i] = float32(p.diskValue)
	}
	if !sample.NetworkUnavailable {
		p.down[i] = float32(sample.ReceiveBytesPerSecond) / 1024
		p.up[i] = float32(sample.SendBytesPerSecond) / 1024
	}
	if p.networkIdentity != sample.NetworkInterface {
		nan := float32(math.NaN())
		for j := 0; j < i; j++ {
			p.down[j], p.up[j] = nan, nan
		}
		p.networkIdentity = sample.NetworkInterface
	}
	p.version++
}

func (p *monitorView) pushHistory(seconds float32) {
	arrays := [...]*[61]float32{&p.x, &p.cpu, &p.memory, &p.disk, &p.down, &p.up}
	if p.count == len(p.x) {
		for _, a := range arrays {
			copy(a[:], a[1:])
		}
	} else {
		p.count++
	}
	i := p.count - 1
	p.x[i] = seconds
	nan := float32(math.NaN())
	p.cpu[i], p.memory[i], p.disk[i], p.down[i], p.up[i] = nan, nan, nan, nan, nan
}

func (state *State) formatMonitor(v *remoteStatsView) {
	p := &v.panel
	m := state.messages.Monitor
	p.language = state.language.String()
	p.cpuText = fmt.Sprintf("%.1f%%", p.sample.CPUPercent)
	if p.sample.CPUUnavailable {
		p.cpuText = "—"
	}
	p.memoryText = "—"
	if p.sample.MemoryTotalBytes > 0 {
		p.memoryText = fmt.Sprintf("%.0f%%", 100*float64(p.sample.MemoryUsedBytes)/float64(p.sample.MemoryTotalBytes))
	}
	p.memoryCapacity = monitorCapacity(p.sample.MemoryUsedBytes, p.sample.MemoryTotalBytes)
	p.availableMemory = "—"
	if p.sample.MemoryTotalBytes > 0 && p.sample.MemoryUsedBytes <= p.sample.MemoryTotalBytes {
		p.availableMemory = monitorBytes(p.sample.MemoryTotalBytes - p.sample.MemoryUsedBytes)
	}
	p.downText = monitorBytes(p.sample.ReceiveBytesPerSecond) + "/s"
	p.upText = monitorBytes(p.sample.SendBytesPerSecond) + "/s"
	if p.sample.NetworkUnavailable {
		p.downText, p.upText = "—", "—"
	}
	p.networkText = m.Network + " · " + m.AllNetworks
	if p.sample.NetworkInterface != "" {
		p.networkText = m.Network + " · " + p.sample.NetworkInterface
	}
	p.loadText = m.WaitingDetails
	p.coresText = "—"
	if p.haveDetails && p.data.CPUCount > 0 {
		p.coresText = fmt.Sprintf(m.CPUCountFmt, p.data.CPUCount)
		p.loadText = fmt.Sprintf(m.LoadFmt, p.data.Load[0], p.data.Load[1], p.data.Load[2])
	}
	if p.data.LoadUnavailable {
		p.loadText = m.LoadUnavailable
	}
	p.cpuNote = p.coresText
	if p.haveDetails && !p.data.LoadUnavailable {
		p.cpuNote += " · " + fmt.Sprintf(m.LoadShortFmt, p.data.Load[0])
	}
	p.cores = p.cores[:0]
	singleGroup := true
	for _, core := range p.sample.Cores {
		if strings.Contains(core.ID, ",") && !strings.HasPrefix(core.ID, "0,") {
			singleGroup = false
			break
		}
	}
	for _, core := range p.sample.Cores {
		name := strings.ReplaceAll(core.ID, ",", ":")
		id := core.ID
		if singleGroup {
			id = strings.TrimPrefix(id, "0,")
		}
		if n, err := strconv.Atoi(id); err == nil {
			name = strconv.Itoa(n + 1)
		}
		p.cores = append(p.cores, monitorCoreText{label: "Core " + name, percent: fmt.Sprintf("%.1f%%", core.Percent), value: core.Percent})
	}
	p.coresTitle = fmt.Sprintf(m.CoresTitleFmt, max(p.data.CPUCount, len(p.cores)))
	if p.data.CPUCount == 0 && len(p.cores) == 0 {
		p.coresTitle = m.CPU + " · " + m.Unavailable
	}
	p.coreMoreText = fmt.Sprintf(m.CoresMoreFmt, len(p.cores))
	if p.data.CPUCount > len(p.cores) {
		p.coreMoreText = fmt.Sprintf(m.PartialCoresFmt, len(p.cores), p.data.CPUCount)
	}
	up := p.sample.UptimeSeconds
	if up == 0 {
		up = p.data.Uptime
	}
	p.uptimeText = fmt.Sprintf(m.UptimeFmt, int(up)/86400, int(up)%86400/3600)
	if p.interval == 0 {
		p.interval = 2
	}
	p.intervalText = fmt.Sprintf(m.IntervalFmt, p.interval)
	p.disks = p.disks[:0]
	for _, d := range p.data.Disks {
		p.disks = append(p.disks, monitorDiskText{path: d.Path, capacity: monitorCapacity(d.Used, d.Total), percent: fmt.Sprintf("%.0f%%", d.Percent), value: d.Percent, virtual: d.Virtual})
	}
	diskIdentity := ""
	p.diskAvailable = false
	p.diskValue = 0
	p.diskText, p.diskCapacity = "—", m.Unavailable
	for _, d := range p.disks {
		if !d.virtual {
			if p.diskAvailable && !strings.EqualFold(d.path, p.data.PrimaryDisk) {
				continue
			}
			diskIdentity = d.path
			p.diskAvailable = true
			p.diskText = d.percent
			p.diskCapacity = d.capacity
			p.diskValue = d.value
			if p.data.PrimaryDisk == "" || strings.EqualFold(d.path, p.data.PrimaryDisk) {
				break
			}
		}
	}
	if diskIdentity != p.diskIdentity {
		for i := 0; i < p.count; i++ {
			p.disk[i] = float32(math.NaN())
		}
		p.diskIdentity = diskIdentity
	}
	visible := 0
	for _, d := range p.disks {
		if !d.virtual && visible < 3 {
			visible++
		}
	}
	p.moreDisks = fmt.Sprintf(m.MoreDisksFmt, max(0, len(p.disks)-visible))
	p.networks = p.networks[:0]
	for _, n := range p.data.Networks {
		down, up := "—", "—"
		if n.HasRate {
			down, up = monitorBytes(n.ReceivePerSecond)+"/s", monitorBytes(n.SendPerSecond)+"/s"
		}
		p.networks = append(p.networks, monitorNetworkText{name: n.Name, rates: down + " / " + up})
	}
}

func monitorBytes(value uint64) string {
	units := [...]string{"B", "KiB", "MiB", "GiB", "TiB"}
	v := float64(value)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", value)
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
func monitorCapacity(used, total uint64) string {
	if total == 0 {
		return "—"
	}
	units := [...]string{"B", "KiB", "MiB", "GiB", "TiB"}
	scale := float64(1)
	i := 0
	for float64(total)/scale >= 1024 && i < len(units)-1 {
		scale *= 1024
		i++
	}
	if used > 0 && float64(used)/scale < .1 {
		return fmt.Sprintf("%s / %.1f %s", monitorBytes(used), float64(total)/scale, units[i])
	}
	return fmt.Sprintf("%.1f / %.1f %s", float64(used)/scale, float64(total)/scale, units[i])
}
