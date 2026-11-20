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
}

// declareRemoteProcessDetailsBody declares the dialog's body: the facts about the
// process in two columns of pairs, the tabs under them, and the tab's content as
// a page scroll area.
func (state *State) declareRemoteProcessDetailsBody(c ui.C, width float32, process remotetools.Process, collectedAt time.Time) {
	theme, pal := c.Theme(), state.pal
	labelWidth := min(float32(150), max(width*0.18, 96))
	details, m := &state.remote.processDetails, state.messages.Tools
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
		strip := ui.Tabs(c, &tab, remoteProcessDetailTabIDs[:], state.remoteProcessDetailTabLabel).Width(min(float32(300), width)).Height(34).Shrink(0).MarginSides(2, 0, 0, 0)
		// A click on the active tab asks for that read again, as the container
		// details' strip does.
		if index, clicked := strip.TabClicked(); clicked && index >= 0 && index < len(remoteProcessDetailTabIDs) {
			id := remoteProcessDetailTabIDs[index]
			c.After(func() { state.selectRemoteProcessDetailTab(id) })
		}
		if state.remoteProcessDetailTab() != hitRemoteProcessDetailsEnv {
			// The old panel painted the status over its top 28 pixels and the
			// command under it with no gap; keep them in one column so the gap
			// between the tabs and the panel is the only one.
			ui.Column(c).Grow(1).Basis(0).FillWidth().Gap(0).Children(func() {
				if details.commandStatus != "" {
					color := pal.muted
					if details.commandFailed {
						color = pal.danger
					}
					ui.Text(c, details.commandStatus).Height(28).LineHeight(28).Shrink(0).FontSize(theme.Metrics.FontSizeM).Color(color).Ellipsis()
				}
				state.declareRemoteDetail(c, &details.text, remoteDetailSpec{
					textID: hitRemoteProcessDetailsText, textIDLabel: m.RemoteProcessDetailsTitle,
					scrollbarID: hitRemoteProcessDetailsScroll, scrollbarLabel: m.RemoteProcessDetailsTitle,
					source: remoteDetailSource(processText(details.commandValue)), width: width - 16,
				})
			})
			return
		}
		empty := ""
		if len(details.environment) == 0 {
			empty = m.RemoteProcessEnvironmentEmpty
		}
		notice, tone := state.remoteDetailNotice(details.loading, details.failed, details.status, empty)
		spec := remoteDetailSpec{
			textID: hitRemoteProcessDetailsText, textIDLabel: m.RemoteProcessEnvironment,
			scrollbarID: hitRemoteProcessDetailsScroll, scrollbarLabel: m.RemoteProcessEnvironment,
			notice: notice, noticeTone: tone, width: width - 16,
		}
		if notice == "" {
			spec.columns, spec.header, spec.rows = remoteEnvironmentTable(details.environment,
				[2]string{m.RemoteProcessEnvironmentName, m.RemoteProcessEnvironmentValue})
		}
		state.declareRemoteDetail(c, &details.text, spec)
	})
}

func (state *State) remoteProcessEnvironmentWheel(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	details := &state.remote.processDetails
	if !details.open {
		return gfx.EventResult{}, false
	}
	return state.remoteDetailWheel(event, &details.text, hitRemoteProcessDetailsText, hitRemoteProcessDetailsScroll)
}
