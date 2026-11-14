package workbench

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"motty/internal/remotestats"
	layoutcore "motty/internal/workspace"
)

var connectionStatsHits = tree.Feature("connection-stats")
var hitConnectionStatsAddress = tree.Sub(connectionStatsHits, tree.Name("address"))

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

// connectionStatsLine is the compact reading beside the terminal: CPU with its
// core count, memory, the primary disk, network rates and the existing latency.
// It reads the same sample and details the side monitor paints.
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

// connectionStatsBarShown reports whether this session has the compact reading
// above it: the global setting is on and the session can be monitored.
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
		if state.statsKeptByMonitor(session) {
			continue
		}
		state.stopRemoteStats(session)
	}
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

// connectionStatsText is the compact reading, or an ellipsis while the first
// sample is still on its way.
func (state *State) connectionStatsText(session *Session) string {
	if session == nil || session.stats == nil || session.stats.state != remoteStatsReady || session.stats.panel.paused {
		return "…"
	}
	latency := ""
	if text, _, ok := state.latencyInfo(session); ok {
		latency = strings.TrimPrefix(text, "RTT ")
	}
	return connectionStatsLine(session.stats.panel.sample, session.stats.panel.data, session.stats.panel.haveDetails, latency)
}

// copyConnectionStatsAddress copies the bar's address and leaves the success
// tint the monitor's own address copy uses.
func (state *State) copyConnectionStatsAddress(session *Session) {
	text := state.connectionStatsAddress(session)
	if text == "" || state.options.SetClipboardText == nil {
		return
	}
	if err := state.options.SetClipboardText(text); err != nil {
		state.status = err.Error()
		return
	}
	if session.stats != nil {
		session.stats.panel.copiedAddress = text
		session.stats.panel.addressCopiedUntil = state.now() + 2*time.Second
	}
}

// declareConnectionStatsSection declares the bar's strip and its divider, so the
// terminal pane's own closure does not carry the bar's declarations when the
// setting is off.
func (state *State) declareConnectionStatsSection(c ui.C, session *Session, width float32, background, border gfx.Color) {
	ui.Stack(c).Height(infoBarHeight).Shrink(0).Background(background).Children(func() {
		state.declareConnectionStatsBar(c, session, width)
		ui.Row(c).FillWidth().Height(1).Self(ui.End).Background(border)
	})
}

// declareConnectionStatsBar declares the compact reading above a terminal: the
// copyable address and the monitor's figures. When the figures do not fit, they
// become the ellipsis the setting's description promises.
func (state *State) declareConnectionStatsBar(c ui.C, session *Session, width float32) {
	if session == nil || session.term == nil || width <= 0 {
		return
	}
	pal, canvas := state.pal, c.Canvas()
	address := state.connectionStatsAddress(session)
	if address == "" {
		address = state.sessionAddress(session.host)
	}
	stats := state.connectionStatsText(session)
	addressWidth := canvas.TextWidthWeight(address, metrics.FontSizeS, gfx.WeightMedium) + 16
	addressMax := min(addressWidth, max(width*.55, 0))
	available := width - 24 - addressMax - 8
	if available < canvas.TextWidthWeight("…", metrics.FontSizeS, gfx.WeightMedium) ||
		canvas.TextWidthWeight(stats, metrics.FontSizeS, gfx.WeightMedium) > available {
		stats = "…"
	}
	id := hitConnectionStatsAddress.At(int(session.ID))
	copied, left := false, time.Duration(0)
	if session.stats != nil && session.stats.panel.copiedAddress == address {
		if left = session.stats.panel.addressCopiedUntil - state.now(); left > 0 {
			copied = true
			canvas.RequestFrameIn(left)
		}
	}
	fill := gfx.Color{}
	if canvas.Hovered() == id || canvas.Pressed() == id {
		fill = pal.surfaceSoft
	}
	color := pal.muted
	if copied {
		fill = withAlpha(pal.success, 0.12)
		color = pal.success
	}
	ui.Row(c).Fill().Height(infoBarHeight).PadX(12).Gap(8).Items(ui.Center).Children(func() {
		addressRow := ui.Row(c).Height(22).Width(addressMax).Shrink(0).PadX(6).Radius(4).Items(ui.Center)
		if fill.Alpha > 0 {
			addressRow = addressRow.Background(fill)
		}
		addressRow.HitWith(id, address, gfx.HitOptions{Role: gfx.RoleButton, Value: address, Hint: state.messages.Monitor.AddressCopy}).Children(func() {
			ui.Text(c, address).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(color).Width(max(addressMax-12, 0)).Ellipsis()
		})
		ui.Text(c, stats).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Ellipsis().Shrink(1)
	})
}
