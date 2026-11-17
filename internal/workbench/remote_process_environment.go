package workbench

import (
	"context"
	"errors"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
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

// declareRemoteProcessDetailsBody declares the dialog's body: the facts about the
// process in two columns of pairs, the tabs under them, and the room the tab's
// text is painted in.
func (state *State) declareRemoteProcessDetailsBody(c ui.C, width float32, process remotetools.Process, collectedAt time.Time) {
	theme, pal := c.Theme(), state.pal
	labelWidth := min(float32(150), max(width*0.18, 96))
	ui.Column(c).Fill().Pad(8).Gap(8).Children(func() {
		for index, pair := range state.remoteProcessDetailPairRows(process, collectedAt) {
			ui.Row(c.KeyInt(index)).FillWidth().Height(34).Shrink(0).Items(ui.Center).PadSides(0, 8, 0, 8).Children(func() {
				ui.Text(c, pair[0].label).Width(labelWidth).Shrink(0).FontSize(theme.Metrics.FontSizeM).Color(pal.muted).Ellipsis()
				ui.Text(c, pair[0].value).Grow(1).Basis(0).FontSize(theme.Metrics.FontSizeM).Color(pal.text).Ellipsis()
				ui.Text(c, pair[1].label).Width(labelWidth).Shrink(0).FontSize(theme.Metrics.FontSizeM).Color(pal.muted).Ellipsis()
				ui.Text(c, pair[1].value).Grow(1).Basis(0).FontSize(theme.Metrics.FontSizeM).Color(pal.text).Ellipsis()
			})
		}
		tab := state.remoteProcessDetailTab()
		if ui.Tabs(c, &tab, remoteProcessDetailTabIDs[:], state.remoteProcessDetailTabLabel).Width(min(float32(300), width)).Height(34).Shrink(0).MarginSides(2, 0, 0, 0).Changed() {
			c.After(func() { state.selectRemoteProcessDetailTab(tab) })
		}
		ui.Slot(c).Grow(1).Draw(func(d *ui.DrawContext) {
			state.paintRemoteProcessDetailsPanel(d.Canvas, d.Rect)
		})
	})
}

func (state *State) paintRemoteProcessDetailsPanel(canvas *gfx.Canvas, panelBounds gfx.Rect) {
	theme := state.Theme()
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
