package workbench

import (
	"context"
	"errors"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

func (state *State) cancelRemoteProcessEnvironment() {
	details := &state.remote.processDetails
	details.environmentRequest++
	details.loading = false
	if details.environmentCancel != nil {
		details.environmentCancel()
		details.environmentCancel = nil
	}
}

func (state *State) loadRemoteProcessEnvironment() {
	details := &state.remote.processDetails
	if !details.open || details.tab != hitRemoteProcessDetailsEnv ||
		details.loading || details.environmentLoaded && !details.failed {
		return
	}
	view := &state.remote
	session, ok := state.remoteSession()
	if !ok {
		details.loading = false
		details.failed = true
		details.environmentLoaded = true
		details.status = state.messages.History.StatusNotConnected
		return
	}
	if view.stale {
		details.loading = false
		details.failed = true
		details.environmentLoaded = true
		details.status = state.messages.Tools.RemoteRefreshRequired
		return
	}
	sourceID := view.session
	process := details.process
	serial := details.serial
	details.environmentRequest++
	request := details.environmentRequest
	details.loading = true
	details.failed = false
	details.status = ""
	runner := remotetools.ForSystem(commandRunnerFor(session), session.host.System.String())
	parent := state.remoteToolsContext()
	ctx, cancel := context.WithTimeout(parent, remoteTimeout)
	details.environmentCancel = cancel
	go func() {
		defer cancel()
		environment, err := remotetools.ReadProcessEnvironment(ctx, runner, process)
		state.postRemoteToolsUpdate(func(state *State) {
			state.applyRemoteProcessEnvironment(serial, request, sourceID, process, environment, err)
		})
	}()
}

func (state *State) applyRemoteProcessEnvironment(
	serial, request uint64,
	sourceID uint32,
	process remotetools.Process,
	environment []remotetools.ProcessEnvironmentVariable,
	err error,
) {
	details := &state.remote.processDetails
	if !details.open || details.serial != serial || state.remote.session != sourceID ||
		details.environmentRequest != request ||
		remotetools.ProcessKey(details.process) != remotetools.ProcessKey(process) {
		return
	}
	details.environmentCancel = nil
	details.loading = false
	details.environmentLoaded = true
	if err != nil {
		details.failed = true
		if errors.Is(err, remotetools.ErrProcessEnvironmentUnsupported) {
			details.status = state.messages.Tools.RemoteProcessEnvironmentUnsupported
		} else {
			details.status = state.remoteErrorText(err)
		}
		return
	}
	details.failed = false
	details.status = ""
	details.environment = environment
	details.text.clearSelection()
	details.text.scroll = 0
}

func (state *State) paintRemoteProcessDetailsBody(canvas *gfx.Canvas, bounds gfx.Rect, process remotetools.Process, collectedAt time.Time) {
	theme := state.Theme()
	const (
		pad       = float32(8)
		rowHeight = float32(34)
	)
	content := gfx.Rect{
		X: bounds.X + pad, Y: bounds.Y + pad,
		Width: max(bounds.Width-pad*2, 0), Height: max(bounds.Height-pad*2, 0),
	}
	infoHeight := rowHeight * 5
	infoBounds := gfx.Rect{X: content.X, Y: content.Y, Width: content.Width, Height: min(infoHeight, content.Height)}
	labelWidth := min(float32(150), max(content.Width*0.18, 96))
	valueWidth := max((content.Width-labelWidth*2)/2, 0)
	columns := []components.TableColumn{
		{Width: labelWidth}, {Width: valueWidth}, {Width: labelWidth}, {Width: 0},
	}
	table := components.BeginTable(canvas, theme, infoBounds, columns).RowHeight(rowHeight)
	for _, pair := range state.remoteProcessDetailPairRows(process, collectedAt) {
		table.Row(gfx.ID{}, []string{pair[0].label, pair[0].value, pair[1].label, pair[1].value}, false)
	}

	tabsY := infoBounds.Bottom() + 10
	tabsBounds := gfx.Rect{
		X: content.X, Y: tabsY, Width: min(float32(300), content.Width),
		Height: min(float32(34), max(content.Bottom()-tabsY, 0)),
	}
	if tabsBounds.Height <= 0 {
		return
	}
	options := state.remoteProcessDetailTabs()
	components.BeginTabs(canvas, theme, tabsBounds, options[:], state.remoteProcessDetailTab()).Paint()

	panelY := tabsBounds.Bottom() + 8
	panelBounds := gfx.Rect{
		X: content.X, Y: panelY, Width: content.Width,
		Height: max(content.Bottom()-panelY, 0),
	}
	if panelBounds.Height <= 0 {
		return
	}
	details, m := &state.remote.processDetails, state.messages.Tools
	if state.remoteProcessDetailTab() != hitRemoteProcessDetailsEnv {
		commandBounds := panelBounds
		if details.commandStatus != "" && commandBounds.Height > 0 {
			statusHeight := min(float32(28), commandBounds.Height)
			color := state.pal.muted
			if details.commandFailed {
				color = state.pal.danger
			}
			status := canvas.FitLine(
				details.commandStatus,
				theme.Metrics.FontSizeM,
				gfx.WeightRegular,
				commandBounds.Width,
			)
			canvas.Label(status, gfx.Rect{
				X: commandBounds.X, Y: commandBounds.Y,
				Width: commandBounds.Width, Height: statusHeight,
			}, gfx.TextStyle{
				Size: theme.Metrics.FontSizeM, Weight: gfx.WeightRegular, Color: color,
			})
			commandBounds.Y += statusHeight
			commandBounds.Height = max(commandBounds.Height-statusHeight, 0)
		}
		if commandBounds.Height <= 0 {
			return
		}
		state.paintRemoteDetailCode(
			canvas, commandBounds, &details.text, processText(details.commandValue),
			m.RemoteProcessDetailsTitle, m.RemoteProcessDetailsTitle,
			hitRemoteProcessDetailsText, hitRemoteProcessDetailsScroll,
		)
		return
	}
	state.paintRemoteEnvironmentView(
		canvas, panelBounds, &details.text,
		details.loading, details.failed, details.status, m.RemoteProcessEnvironmentEmpty,
		details.environment,
		[2]string{m.RemoteProcessEnvironmentName, m.RemoteProcessEnvironmentValue},
		m.RemoteProcessEnvironment, m.RemoteProcessEnvironment,
		hitRemoteProcessDetailsText, hitRemoteProcessDetailsScroll,
	)
}

func (state *State) remoteProcessEnvironmentScrollPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	details := &state.remote.processDetails
	if !details.open {
		return gfx.EventResult{}, false
	}
	return state.remoteDetailScrollPointer(event, &details.text, hitRemoteProcessDetailsScroll)
}

func (state *State) remoteProcessEnvironmentWheel(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	details := &state.remote.processDetails
	if !details.open {
		return gfx.EventResult{}, false
	}
	return state.remoteDetailWheel(event, &details.text, hitRemoteProcessDetailsText, hitRemoteProcessDetailsScroll)
}
