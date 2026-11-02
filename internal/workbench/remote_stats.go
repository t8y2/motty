package workbench

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"

	"motty/internal/remotestats"
)

type remoteStatsState uint8

const (
	remoteStatsMeasuring remoteStatsState = iota
	remoteStatsReady
	remoteStatsUnsupported
	remoteStatsFailed
)

// remoteStatsView holds the monitor and its latest reading. The reading is kept
// as one preformatted piece per metric so that painting it, each in its own
// color, formats and allocates nothing.
type remoteStatsView struct {
	panel  monitorView
	worker *remotestats.Worker
	state  remoteStatsState
	text   string   // the whole reading as one line, for accessibility and tests
	parts  []string // the four metrics, then one piece per GPU when the server has any
}

// remoteStatsMetric is one figure of the reading; each has its own color.
type remoteStatsMetric uint8

const (
	statsCPU remoteStatsMetric = iota
	statsMemory
	statsDown
	statsUp
	statsMetricCount
	// Every GPU piece after the four metrics is colored statsGPU.
	statsGPU = statsMetricCount
)

func (state *State) clickRemoteStats(id ui.ID) bool {
	if id != hitRemoteStats {
		return false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || !state.monitorOffered(session) {
		return true
	}
	if session.stats == nil || session.stats.worker == nil && session.stats.state >= remoteStatsUnsupported {
		state.stopRemoteStats(session)
		state.showMonitor()
		return true
	}
	if state.monitorShown(state.deck.ActiveWorkspace()) {
		state.closeMonitor()
	} else {
		state.showMonitor()
	}
	return true
}

func (state *State) stopRemoteStats(session *Session) {
	if session == nil || session.stats == nil {
		return
	}
	if session.stats.worker != nil {
		session.stats.worker.Close()
	}
	if session.stats.panel.worker != nil {
		session.stats.panel.worker.Close()
	}
	session.stats = nil
}

func (state *State) pumpRemoteStats(session *Session) {
	view := session.stats
	if view == nil {
		return
	}
	if view.panel.worker != nil {
		for reading := true; reading; {
			select {
			case result, ok := <-view.panel.worker.Results():
				if !ok {
					view.panel.worker = nil
					reading = false
					break
				}
				view.panel.detailsFailed = result.Err != nil
				if result.Err == nil {
					view.panel.data = result.Details
					view.panel.haveDetails = true
				}
				state.formatMonitor(view)
			default:
				reading = false
			}
		}
	}
	if view.worker == nil {
		return
	}
	for {
		select {
		case result, ok := <-view.worker.Results():
			if !ok {
				view.worker = nil
				if view.panel.worker != nil {
					view.panel.worker.Close()
					view.panel.worker = nil
				}
				return
			}
			if result.Err != nil {
				if errors.Is(result.Err, remotestats.ErrUnsupported) {
					view.state = remoteStatsUnsupported
				} else {
					view.state = remoteStatsFailed
				}
				view.text = ""
				view.worker.Close()
				view.worker = nil
				if view.panel.worker != nil {
					view.panel.worker.Close()
					view.panel.worker = nil
				}
				return
			}
			view.state = remoteStatsReady
			if result.Details != nil {
				view.panel.data = *result.Details
				view.panel.haveDetails = true
			}
			view.text = formatRemoteStats(result.Sample)
			view.parts = remoteStatsParts(result.Sample)
			state.sampleMonitor(view, result.Sample)
		default:
			return
		}
	}
}

func (state *State) remoteStatsText(view *remoteStatsView) string {
	if view == nil {
		return ""
	}
	switch view.state {
	case remoteStatsReady:
		return view.text
	case remoteStatsUnsupported:
		return state.messages.StatsUnsupported
	case remoteStatsFailed:
		return state.messages.StatsUnavailable
	default:
		return state.messages.StatsMeasuring
	}
}

// remoteStatsToggle describes the explicit opt-in beside Files: whether it is
// offered at all (only on a live, connected shell, a server's or this computer's), what its tooltip says and
// whether the monitor is running.
func (state *State) remoteStatsToggle(session *Session) (hint string, style iconButtonStyle, offered bool) {
	if !state.monitorOffered(session) {
		return "", iconButtonQuiet, false
	}
	switch {
	case session.stats == nil:
		return state.messages.TipStatsStart, iconButtonQuiet, true
	case session.stats.worker == nil && session.stats.state >= remoteStatsUnsupported:
		return state.messages.TipStatsRetry, iconButtonDanger, true
	}
	return state.messages.TipStatsStop, iconButtonActive, true
}

// remoteStatsParts formats each metric on its own, with the label that tells
// them apart now that they are colored rather than run together.
func remoteStatsParts(sample remotestats.Sample) []string {
	parts := make([]string, statsMetricCount, int(statsMetricCount)+len(sample.GPUs))
	parts[statsCPU] = fmt.Sprintf("CPU %.0f%%", sample.CPUPercent)
	if sample.CPUUnavailable {
		parts[statsCPU] = "CPU —"
	}
	parts[statsMemory] = "RAM " + formatMemory(sample.MemoryUsedBytes, sample.MemoryTotalBytes)
	parts[statsDown] = "↓" + formatRate(sample.ReceiveBytesPerSecond)
	parts[statsUp] = "↑" + formatRate(sample.SendBytesPerSecond)
	if sample.NetworkUnavailable {
		parts[statsDown], parts[statsUp] = "↓—", "↑—"
	}
	for index := range sample.GPUs {
		parts = append(parts, formatGPU(&sample.GPUs[index], len(sample.GPUs) > 1))
	}
	return parts
}

func formatRemoteStats(sample remotestats.Sample) string {
	text := fmt.Sprintf("CPU %.0f%%  RAM %s  ↓%s ↑%s", sample.CPUPercent,
		formatMemory(sample.MemoryUsedBytes, sample.MemoryTotalBytes),
		formatRate(sample.ReceiveBytesPerSecond), formatRate(sample.SendBytesPerSecond))
	if sample.CPUUnavailable || sample.NetworkUnavailable {
		text = ""
		for i, p := range remoteStatsParts(sample)[:statsMetricCount] {
			if i > 0 {
				text += "  "
			}
			text += p
		}
	}
	for index := range sample.GPUs {
		text += "  " + formatGPU(&sample.GPUs[index], len(sample.GPUs) > 1)
	}
	return text
}

// formatGPU is one accelerator as one piece of the readout: "GPU1 87% 19.5/24.0G
// 71°C 312W", with whatever the driver did not report left out. The number is
// shown only when there is more than one to tell apart.
func formatGPU(gpu *remotestats.GPU, numbered bool) string {
	text := "GPU"
	if numbered {
		text += strconv.Itoa(gpu.Index)
	}
	if gpu.UtilPercent >= 0 {
		text += fmt.Sprintf(" %.0f%%", gpu.UtilPercent)
	}
	if gpu.MemoryTotalBytes > 0 {
		text += " " + formatMemory(gpu.MemoryUsedBytes, gpu.MemoryTotalBytes)
	}
	if gpu.TemperatureC >= 0 {
		text += fmt.Sprintf(" %.0f°C", gpu.TemperatureC)
	}
	if gpu.PowerWatts >= 0 {
		text += fmt.Sprintf(" %.0fW", gpu.PowerWatts)
	}
	return text
}

func formatMemory(used, total uint64) string {
	unit, divisor := byteUnit(total)
	return fmt.Sprintf("%.1f/%.1f%s", float64(used)/divisor, float64(total)/divisor, unit)
}

func formatRate(value uint64) string {
	unit, divisor := byteUnit(value)
	if divisor == 1 {
		return fmt.Sprintf("%dB/s", value)
	}
	return fmt.Sprintf("%.1f%s/s", float64(value)/divisor, unit)
}

func byteUnit(value uint64) (string, float64) {
	switch {
	case value >= 1<<40:
		return "T", 1 << 40
	case value >= 1<<30:
		return "G", 1 << 30
	case value >= 1<<20:
		return "M", 1 << 20
	case value >= 1<<10:
		return "K", 1 << 10
	default:
		return "B", 1
	}
}

// paintRemoteStatsReadout draws the reading from x, each metric in its own
// color, and returns where the next thing may start. While there is no reading
// it draws the one-line state ("measuring", "unavailable") in the quiet color.
// Nothing is drawn that does not fit before limit.
func (state *State) paintRemoteStatsReadout(canvas *ui.Canvas, view *remoteStatsView, x, y, limit float32) float32 {
	pal := state.pal
	const gap = 14
	var widths [int(statsMetricCount) + remotestats.MaxGPUs]float32
	count := 0
	text := ""
	if view.state != remoteStatsReady {
		text = state.remoteStatsText(view)
		widths[0] = min(canvas.TextWidthWeight(text, metrics.FontSizeS, ui.WeightMedium)+2, max(limit-x, 0))
		count = 1
	} else {
		// As many metrics as fit before the limit.
		end := x
		for _, part := range view.parts[:min(len(view.parts), len(widths))] {
			width := canvas.TextWidthWeight(part, metrics.FontSizeS, ui.WeightMedium) + 2
			if end+width > limit {
				break
			}
			widths[count] = width
			count++
			end += width + gap
		}
	}
	// CPU in the accent, memory in blue, the two directions of traffic in green
	// and violet: the palette's own terminal colors, so both themes stay legible.
	blue, violet := pal.ansi[4], pal.ansi[5]
	colors := [statsMetricCount + 1]ui.Color{
		statsCPU: pal.accent, statsMemory: ui.RGB(blue[0], blue[1], blue[2]),
		statsDown: pal.success, statsUp: ui.RGB(violet[0], violet[1], violet[2]),
		statsGPU: pal.warning,
	}
	var last flex.Box
	trailing := float32(0)
	f := state.statsLayout.Begin(canvas, state.Theme(), ui.Rect{X: x, Y: y, Width: max(limit-x, 0), Height: infoBarHeight})
	f.Row().Fill().Items(flex.Center).Children(func() {
		for metric := range count {
			label, color := text, pal.muted
			gapAfter := float32(12)
			if view.state == remoteStatsReady {
				label, color, gapAfter = view.parts[metric], colors[min(metric, int(statsGPU))], gap
			}
			last, trailing = f.Text(label).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(color).Width(widths[metric]).Shrink(0).MarginSides(0, gapAfter, 0, 0), gapAfter
		}
	})
	f.End()
	if count == 0 {
		return x
	}
	// Where the next thing may start: after the last metric and its gap.
	return last.Rect().X + last.Rect().Width + trailing
}
