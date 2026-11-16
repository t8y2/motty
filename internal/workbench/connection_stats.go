package workbench

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/gfx"

	"motty/internal/remotestats"
	"motty/internal/workbench/icons"
	layoutcore "motty/internal/workspace"
)

var connectionStatsHits = tree.Feature("connection-stats")
var (
	hitConnectionStatsAddress    = tree.Sub(connectionStatsHits, tree.Name("address"))
	hitConnectionStatsCopy       = tree.Sub(connectionStatsHits, tree.Name("copy"))
	hitConnectionStatsDisconnect = tree.Sub(connectionStatsHits, tree.Name("disconnect"))
	hitConnectionStatsRestart    = tree.Sub(connectionStatsHits, tree.Name("restart"))
	hitConnectionStatsMetric     = tree.Sub(connectionStatsHits, tree.Name("metric")) // + session id * barMetricCount + metric
)

const barMetricCount = 6

// connectionCopyText is what the connection bar puts on the clipboard: the
// account and address, with the port only when it is not SSH's default. A local
// shell names the account on this computer and localhost, never a network
// address.
func connectionCopyText(host Host, localUser string) string {
	if host.ID == localHostID {
		user := localAccountName(localUser)
		if user == "" {
			return "localhost"
		}
		return user + "@localhost"
	}
	address := strings.TrimSpace(host.Address)
	if address == "" {
		return ""
	}
	user := strings.TrimSpace(host.User)
	port := int(host.Port)
	if port == 0 {
		port = int(host.defaultPort())
	}
	hostText := address
	includePort := !(host.Protocol == protocolSSH && port == int(host.defaultPort()))
	if includePort {
		hostText = net.JoinHostPort(address, strconv.Itoa(port))
	} else if strings.Contains(address, ":") {
		hostText = "[" + address + "]"
	}
	if user == "" {
		return hostText
	}
	return user + "@" + hostText
}

// localAccountName is the account a local shell copies: the platform's login
// without the Windows domain prefix, so DOMAIN\Administrator becomes
// Administrator.
func localAccountName(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.LastIndexAny(value, `\/`); index >= 0 {
		value = value[index+1:]
	}
	return value
}

// connectionStatsLine is the compact reading the accessibility hint and tests
// use: CPU with its core count, memory, the primary disk, network rates and the
// existing latency. The bar itself paints each figure beside its icon.
func connectionStatsLine(sample remotestats.Sample, details remotestats.Details, haveDetails bool, latency string) string {
	var parts []string
	if sample.CPUUnavailable {
		parts = append(parts, "CPU —")
	} else {
		cores := len(sample.Cores)
		if haveDetails && details.CPUCount > 0 {
			cores = details.CPUCount
		}
		if cores > 0 {
			parts = append(parts, fmt.Sprintf("CPU %.0f%% (%dC)", sample.CPUPercent, cores))
		} else {
			parts = append(parts, fmt.Sprintf("CPU %.0f%%", sample.CPUPercent))
		}
	}
	if sample.MemoryTotalBytes == 0 {
		parts = append(parts, "RAM —")
	} else {
		parts = append(parts, "RAM "+formatMemory(sample.MemoryUsedBytes, sample.MemoryTotalBytes))
	}
	if disk, ok := primaryDisk(details); haveDetails && ok && disk.Total > 0 {
		percent := disk.Percent
		if percent == 0 && disk.Used > 0 {
			percent = float64(disk.Used) * 100 / float64(disk.Total)
		}
		parts = append(parts, fmt.Sprintf("Disk %s (%.0f%%)", formatMemory(disk.Used, disk.Total), percent))
	} else {
		parts = append(parts, "Disk —")
	}
	if sample.NetworkUnavailable {
		parts = append(parts, "↓— ↑—")
	} else {
		parts = append(parts, "↓"+formatRate(sample.ReceiveBytesPerSecond)+" ↑"+formatRate(sample.SendBytesPerSecond))
	}
	if latency != "" {
		parts = append(parts, latency)
	}
	return strings.Join(parts, "  ")
}

// primaryDisk is the disk the side monitor calls the primary one: the named
// disk when it is present, otherwise the first physical disk.
func primaryDisk(details remotestats.Details) (remotestats.Disk, bool) {
	for _, disk := range details.Disks {
		if !disk.Virtual && details.PrimaryDisk != "" && strings.EqualFold(disk.Path, details.PrimaryDisk) {
			return disk, true
		}
	}
	for _, disk := range details.Disks {
		if !disk.Virtual {
			return disk, true
		}
	}
	return remotestats.Disk{}, false
}

// connectionStatsBarShown reports whether this session's metrics need the
// monitor: the setting is on and the session can be sampled.
func (state *State) connectionStatsBarShown(session *Session) bool {
	return state.connectionStats && state.monitorOffered(session)
}

// setConnectionStats turns the global connection reading on or off and applies
// it to every session already open.
func (state *State) setConnectionStats(on bool) {
	if state.connectionStats == on {
		return
	}
	state.connectionStats = on
	state.applyConnectionStats()
	state.save()
}

// applyConnectionStats starts the monitor for every live session when the
// setting is on, and stops it when neither the setting nor the side panel wants
// it. The side panel is per workspace, so a session keeps its worker while its
// own workspace has the panel open.
func (state *State) applyConnectionStats() {
	for index := range state.sessions {
		session := &state.sessions[index]
		if state.connectionStats {
			if state.monitorOffered(session) {
				state.ensureMonitor(session)
			}
			continue
		}
		state.stopRemoteStatsIfUnused(session)
	}
}

// statsWanted reports whether any surface still needs this session's worker:
// the terminal bar's setting or the side panel of its workspace.
func (state *State) statsWanted(session *Session) bool {
	return state.connectionStatsBarShown(session) || state.statsKeptByMonitor(session)
}

// stopRemoteStatsIfUnused releases a session's workers only when neither the
// terminal bar nor the side panel wants them. stopRemoteStats itself stays
// unconditional for disconnect, close and retries.
func (state *State) stopRemoteStatsIfUnused(session *Session) {
	if state.statsWanted(session) {
		return
	}
	state.stopRemoteStats(session)
}

// statsKeptByMonitor reports whether the side panel still needs this session's
// worker after the connection setting was turned off.
func (state *State) statsKeptByMonitor(session *Session) bool {
	workspace, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	return ok && state.monitorShown(workspace)
}

// connectionStatsAddress is the text the bar copies: the login that reaches
// this session, not the host's saved account alone.
func (state *State) connectionStatsAddress(session *Session) string {
	if session == nil {
		return ""
	}
	host := session.host
	if !session.isLocal() {
		host.User = state.login(host)
	}
	return connectionCopyText(host, systemLogin())
}

// connectionBarColor is the selected tab's own fill, including a custom tab
// colour, so the bar continues that tab rather than approximating it with the
// palette's uncoloured active tab.
func (state *State) connectionBarColor(session *Session) gfx.Color {
	if workspace, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID)); ok {
		for index := range state.workspaceTabs {
			if state.workspaceTabs[index].ID == workspace {
				return tabFill(state.pal, state.pal.tabActive, state.tabColor(state.workspaceTabs[index]), true, false)
			}
		}
	}
	return state.pal.tabActive
}

// connectionBarAddress caches the copyable address for the always-present bar.
func (state *State) connectionBarAddress(session *Session) string {
	if session == nil {
		return ""
	}
	if session.barAddress == "" {
		session.barAddress = state.connectionStatsAddress(session)
	}
	return session.barAddress
}

// connectionStatsText is the full reading, used for tests and tooltips.
func (state *State) connectionStatsText(session *Session) string {
	if session == nil || session.stats == nil || session.stats.state != remoteStatsReady {
		return "…"
	}
	latency := ""
	if text, _, ok := state.latencyInfo(session); ok {
		latency = strings.TrimPrefix(text, "RTT ")
	}
	return connectionStatsLine(session.stats.panel.sample, session.stats.panel.data, session.stats.panel.haveDetails, latency)
}

// copyConnectionStatsAddress copies the bar's address and leaves the success
// tint the bar's address shows.
func (state *State) copyConnectionStatsAddress(session *Session) {
	text := state.connectionBarAddress(session)
	if text == "" || state.options.SetClipboardText == nil {
		return
	}
	if err := state.options.SetClipboardText(text); err != nil {
		state.status = err.Error()
		return
	}
	session.barCopiedAddress = text
	session.barCopiedUntil = state.now() + 2*time.Second
}

// connectionBarMetric is one icon and its figure in the bar.
type connectionBarMetric struct {
	icon  gfx.Icon
	value string
	level float64 // 0..100 for the water layer; -1 for no water
	color gfx.Color
	water bool
}

// connectionBandColor is the water colour for a percentage: green through 25%,
// yellow through 50%, orange through 75%, red above.
func connectionBandColor(percent float64) gfx.Color {
	switch {
	case percent <= 25:
		return gfx.RGB(34, 197, 94)
	case percent <= 50:
		return gfx.RGB(234, 179, 8)
	case percent <= 75:
		return gfx.RGB(249, 115, 22)
	default:
		return gfx.RGB(239, 68, 68)
	}
}

// connectionPingColor follows the latency bands: green through 50ms, yellow
// through 100ms, orange through 200ms, red above.
func connectionPingColor(ms int64) gfx.Color {
	switch {
	case ms <= 50:
		return gfx.RGB(34, 197, 94)
	case ms <= 100:
		return gfx.RGB(234, 179, 8)
	case ms <= 200:
		return gfx.RGB(249, 115, 22)
	default:
		return gfx.RGB(239, 68, 68)
	}
}

// connectionBarMetrics formats the six figures. Unsupported or not-yet-sampled
// sessions keep the icons with an em dash value and no water.
func (state *State) connectionBarMetrics(session *Session) [barMetricCount]connectionBarMetric {
	var metrics [barMetricCount]connectionBarMetric
	metrics[0].icon, metrics[0].value, metrics[0].level = icons.Cpu, "--", -1
	metrics[1].icon, metrics[1].value, metrics[1].level = icons.MemoryStick, "--", -1
	metrics[2].icon, metrics[2].value, metrics[2].level = icons.HardDrive, "--", -1
	metrics[3].icon, metrics[3].value, metrics[3].level = icons.ArrowUp, "--", -1
	metrics[4].icon, metrics[4].value, metrics[4].level = icons.ArrowDown, "--", -1
	metrics[5].icon, metrics[5].value, metrics[5].level = icons.Activity, "--", -1
	metrics[0].water, metrics[1].water, metrics[2].water, metrics[5].water = true, true, true, true
	if session == nil || session.stats == nil || session.stats.state != remoteStatsReady {
		return metrics
	}
	view := session.stats
	sample, details := view.panel.sample, view.panel.data
	if !sample.CPUUnavailable {
		if view.panel.haveDetails && details.CPUCount > 0 {
			metrics[0].value = fmt.Sprintf("%.0f%% (%dC)", sample.CPUPercent, details.CPUCount)
		} else {
			metrics[0].value = fmt.Sprintf("%.0f%%", sample.CPUPercent)
		}
		metrics[0].level = sample.CPUPercent
		metrics[0].color = connectionBandColor(sample.CPUPercent)
	} else {
		metrics[0].water = false
	}
	if sample.MemoryTotalBytes > 0 {
		metrics[1].value = formatMemory(sample.MemoryUsedBytes, sample.MemoryTotalBytes)
		metrics[1].level = float64(sample.MemoryUsedBytes) * 100 / float64(sample.MemoryTotalBytes)
		metrics[1].color = connectionBandColor(metrics[1].level)
	} else {
		metrics[1].water = false
	}
	if disk, ok := primaryDisk(details); view.panel.haveDetails && ok && disk.Total > 0 {
		percent := disk.Percent
		if percent == 0 && disk.Used > 0 {
			percent = float64(disk.Used) * 100 / float64(disk.Total)
		}
		metrics[2].value = fmt.Sprintf("%s (%.0f%%)", formatMemory(disk.Used, disk.Total), percent)
		metrics[2].level = percent
		metrics[2].color = connectionBandColor(percent)
	} else {
		metrics[2].water = false
	}
	if sample.NetworkUnavailable {
		metrics[3].value, metrics[4].value = "--", "--"
	} else {
		metrics[3].value = formatRate(sample.SendBytesPerSecond)
		metrics[4].value = formatRate(sample.ReceiveBytesPerSecond)
	}
	if state.latencyAvailable(session) {
		ms := atomic.LoadInt64(&session.rttMs)
		metrics[5].value = fmt.Sprintf("%dms", ms)
		metrics[5].level = min(float64(ms)*100/200, 100)
		metrics[5].color = connectionPingColor(ms)
	} else {
		metrics[5].water = false
	}
	return metrics
}

// paintConnectionTopBar paints the address and actions, then the six metrics
// when the setting is on. It only runs for a terminal pane: the host page and
// the empty connect page have no bar.
func (state *State) paintConnectionTopBar(canvas *gfx.Canvas, rect gfx.Rect, session *Session) {
	if session == nil || session.term == nil || rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	pal := state.pal
	left, right := rect.X+10, rect.X+rect.Width-10
	mid := rect.Y + rect.Height/2
	address := state.connectionBarAddress(session)

	copied, feedback := false, time.Duration(0)
	if address != "" && session.barCopiedAddress == address {
		if feedback = session.barCopiedUntil - state.now(); feedback > 0 {
			copied = true
			canvas.RequestFrameIn(feedback)
		}
	}
	addressWidth := canvas.TextWidthWeight(address, metrics.FontSizeS, gfx.WeightMedium) + 2
	// Keep the address and its three actions whole, and reserve the six metric
	// icons when the setting is on; only then let the address elide.
	metricReserve := float32(0)
	if state.connectionStats {
		metricReserve = barMetricCount * (14 + 10)
	}
	addressMax := min(addressWidth, max(rect.Width-20-3*22-metricReserve, 0))
	if address != "" && addressMax >= 24 && left+addressMax <= right {
		bounds := gfx.Rect{X: left, Y: rect.Y, Width: addressMax, Height: rect.Height}
		color := pal.text
		if copied {
			color = pal.success
		}
		canvas.Label(elide(canvas, address, metrics.FontSizeS, gfx.WeightMedium, addressMax), bounds,
			gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: color})
		canvas.HitWithOptions(hitConnectionStatsAddress.At(int(session.ID)), bounds, address,
			gfx.HitOptions{Role: gfx.RoleButton, Value: address, Hint: state.messages.Monitor.AddressCopy})
		left += addressMax + 6
	}

	action := func(id gfx.ID, icon gfx.Icon, label, hint string, enabled bool) {
		if left+18 > right {
			return
		}
		bounds := gfx.Rect{X: left, Y: mid - 9, Width: 18, Height: 18}
		if enabled && (canvas.Hovered() == id || canvas.Pressed() == id) {
			canvas.Rounded(bounds, 4, withAlpha(pal.text, 0.10))
		}
		color := pal.muted
		if !enabled {
			color = pal.faint
		}
		canvas.DrawIcon(icon, gfx.Rect{X: bounds.X + 2, Y: bounds.Y + 2, Width: 14, Height: 14}, color)
		canvas.HitWithOptions(id, bounds, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint, Disabled: !enabled})
		left += 22
	}
	action(hitConnectionStatsCopy.At(int(session.ID)), icons.Copy, state.messages.Monitor.AddressCopy,
		state.messages.Monitor.AddressCopy, address != "")
	action(hitConnectionStatsDisconnect.At(int(session.ID)), icons.CircleStop, state.messages.MenuDisconnect,
		state.messages.MenuDisconnect, state.canDisconnect(session))
	action(hitConnectionStatsRestart.At(int(session.ID)), icons.RotateCw, state.messages.MenuReconnect,
		state.messages.MenuReconnect, state.canReconnect(session) || state.canReconnectLive(session))

	if !state.connectionStats {
		return
	}
	figures := state.connectionBarMetrics(session)
	// Try the values first; if they do not fit, keep the icons and drop the
	// values. The water layer stays either way.
	fullWidth := float32(0)
	for _, metric := range figures {
		fullWidth += 14 + 4 + canvas.TextWidthWeight(metric.value, metrics.FontSizeS, gfx.WeightMedium) + 10
	}
	showValues := left+fullWidth <= right
	for index, metric := range figures {
		width := float32(14)
		if showValues {
			width += 4 + canvas.TextWidthWeight(metric.value, metrics.FontSizeS, gfx.WeightMedium)
		}
		if left+width > right {
			break
		}
		iconRect := gfx.Rect{X: left, Y: mid - 7, Width: 14, Height: 14}
		canvas.DrawIcon(metric.icon, iconRect, pal.muted)
		if metric.water && metric.level >= 0 {
			level := min(max(metric.level, 0), 100) / 100
			fill := iconRect.Height * float32(level)
			if fill > 0 {
				canvas.Fill(gfx.Rect{X: iconRect.X, Y: iconRect.Y + iconRect.Height - fill, Width: iconRect.Width, Height: fill},
					withAlpha(metric.color, 0.45))
				canvas.Fill(gfx.Rect{X: iconRect.X, Y: iconRect.Y + iconRect.Height - fill, Width: iconRect.Width, Height: 1},
					withAlpha(metric.color, 0.9))
			}
		}
		metricID := hitConnectionStatsMetric.At(int(session.ID)*barMetricCount + index)
		canvas.HitWithOptions(metricID, iconRect, metric.value, gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: metric.value})
		left += 14
		if showValues {
			valueWidth := canvas.TextWidthWeight(metric.value, metrics.FontSizeS, gfx.WeightMedium)
			canvas.Label(metric.value, gfx.Rect{X: left + 4, Y: rect.Y, Width: valueWidth + 1, Height: rect.Height},
				gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.muted})
			left += 4 + valueWidth
		}
		left += 10
	}
}
