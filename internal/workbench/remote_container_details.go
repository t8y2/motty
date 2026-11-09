package workbench

import (
	"context"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

var (
	hitRemoteContainerDetails            = tree.ID(remoteToolsHits, "remote-container-details")
	hitRemoteContainerDetailsDismiss     = tree.ID(remoteToolsHits, "remote-container-details-dismiss")
	hitRemoteContainerDetailsClose       = tree.ID(remoteToolsHits, "remote-container-details-close")
	hitRemoteContainerDetailsOverview    = tree.ID(remoteToolsHits, "remote-container-details-overview")
	hitRemoteContainerDetailsEnvironment = tree.ID(remoteToolsHits, "remote-container-details-environment")
	hitRemoteContainerDetailsText        = tree.ID(remoteToolsHits, "remote-container-details-text")
	hitRemoteContainerDetailsScroll      = tree.ID(remoteToolsHits, "remote-container-details-scroll")
)

type remoteContainerDetails struct {
	open               bool
	container          remotetools.Container
	tab                gfx.ID
	serial             uint64
	loading            bool
	failed             bool
	status             string
	environmentLoaded  bool
	environmentRequest uint64
	environmentCancel  func()
	environment        []remotetools.ProcessEnvironmentVariable
	text               remoteDetailText
}

func (state *State) cancelRemoteContainerEnvironment() {
	details := &state.remote.containerDetails
	details.environmentRequest++
	details.loading = false
	if details.environmentCancel != nil {
		details.environmentCancel()
		details.environmentCancel = nil
	}
}

func (state *State) openRemoteContainerDetails(container remotetools.Container) {
	state.closeRemoteProcessDetails()
	state.cancelRemoteContainerEnvironment()
	state.remote.containerDetailsSerial++
	state.remote.containerDetails = remoteContainerDetails{
		open: true, container: container, tab: hitRemoteContainerDetailsOverview,
		serial: state.remote.containerDetailsSerial,
	}
}

func (state *State) closeRemoteContainerDetails() {
	state.cancelRemoteContainerEnvironment()
	state.remote.containerDetails = remoteContainerDetails{}
}

func (state *State) selectRemoteContainerDetailTab(tab gfx.ID) {
	details := &state.remote.containerDetails
	if !details.open || tab != hitRemoteContainerDetailsOverview && tab != hitRemoteContainerDetailsEnvironment {
		return
	}
	if details.tab == tab {
		if tab == hitRemoteContainerDetailsEnvironment {
			state.loadRemoteContainerEnvironment()
		}
		return
	}
	if details.tab == hitRemoteContainerDetailsEnvironment {
		state.cancelRemoteContainerEnvironment()
	}
	details.tab = tab
	details.text.reset()
	if tab == hitRemoteContainerDetailsEnvironment {
		state.loadRemoteContainerEnvironment()
	}
}

func (state *State) loadRemoteContainerEnvironment() {
	details := &state.remote.containerDetails
	if !details.open || details.tab != hitRemoteContainerDetailsEnvironment ||
		details.loading || details.environmentLoaded && !details.failed {
		return
	}
	session, ok := state.remoteSession()
	if !ok {
		details.loading = false
		details.failed = true
		details.environmentLoaded = true
		details.status = state.messages.History.StatusNotConnected
		return
	}
	if state.remote.stale {
		details.loading = false
		details.failed = true
		details.environmentLoaded = true
		details.status = state.messages.Tools.RemoteRefreshRequired
		return
	}
	sourceID := state.remote.session
	container := details.container
	serial := details.serial
	details.environmentRequest++
	request := details.environmentRequest
	details.loading, details.failed, details.status = true, false, ""
	runner := remotetools.ForSystem(commandRunnerFor(session), session.host.System.String())
	ctx, cancel := context.WithTimeout(state.remoteToolsContext(), remoteTimeout)
	details.environmentCancel = cancel
	go func() {
		defer cancel()
		environment, err := remotetools.ReadContainerEnvironment(ctx, runner, container.ID)
		state.postRemoteToolsUpdate(func(state *State) {
			state.applyRemoteContainerEnvironment(serial, request, sourceID, container, environment, err)
		})
	}()
}

func (state *State) applyRemoteContainerEnvironment(
	serial, request uint64,
	sourceID uint32,
	container remotetools.Container,
	environment []remotetools.ProcessEnvironmentVariable,
	err error,
) {
	details := &state.remote.containerDetails
	if !details.open || details.serial != serial || state.remote.session != sourceID ||
		details.environmentRequest != request || details.container.ID != container.ID {
		return
	}
	details.environmentCancel = nil
	details.loading = false
	details.environmentLoaded = true
	if err != nil {
		details.failed = true
		details.status = state.remoteErrorText(err)
		return
	}
	details.failed, details.status = false, ""
	details.environment = environment
	details.text.clearSelection()
	details.text.scroll = 0
}

func (state *State) clickRemoteContainerDetails(id gfx.ID) bool {
	details := &state.remote.containerDetails
	if !details.open {
		return false
	}
	switch id {
	case hitRemoteContainerDetailsDismiss, hitRemoteContainerDetailsClose:
		state.closeRemoteContainerDetails()
	case hitRemoteContainerDetailsOverview, hitRemoteContainerDetailsEnvironment:
		state.selectRemoteContainerDetailTab(id)
	}
	// The detail dialog is modal. Swallow stale hits from the obscured list so
	// a click cannot act on a different container underneath it.
	return true
}

func (state *State) remoteContainerDetailTabs() [2]components.TabOption {
	m := state.messages.Tools
	return [2]components.TabOption{
		{ID: hitRemoteContainerDetailsOverview, Label: m.RemoteContainerOverview, Icon: lucideIcons[iconInfo]},
		{ID: hitRemoteContainerDetailsEnvironment, Label: m.RemoteProcessEnvironment, Icon: lucideIcons[iconSliders]},
	}
}

func (state *State) remoteContainerDetailTab() gfx.ID {
	if state.remote.containerDetails.tab == hitRemoteContainerDetailsEnvironment {
		return hitRemoteContainerDetailsEnvironment
	}
	return hitRemoteContainerDetailsOverview
}

func (state *State) remoteContainerOverviewBody(container remotetools.Container) string {
	m := state.messages.Tools
	rows := [][2]string{
		{m.RemoteColName, processText(container.Name)},
		{m.RemoteColID, processText(container.ID)},
		{m.RemoteColImage, processText(container.Image)},
		{m.RemoteColState, processText(container.State)},
		{m.RemoteColStatus, processText(container.Status)},
	}
	return remoteDetailRows(rows)
}

func (state *State) paintRemoteContainerDetails(canvas *gfx.Canvas, viewport gfx.Rect) {
	details := &state.remote.containerDetails
	if !details.open {
		return
	}
	m := state.messages.Tools
	contentWidth := min(float32(940), max(viewport.Width-32, 360))
	bodyHeight := min(float32(620), max(viewport.Height-132, 320))
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer + 2, Scope: dialogScope,
		DismissID: hitRemoteContainerDetailsDismiss, CloseID: hitRemoteContainerDetailsClose,
		InitialFocusID: hitRemoteContainerDetailsClose,
		ContentSize:    gfx.Size{Width: contentWidth}, FitBody: true, BodyHeight: bodyHeight,
		ButtonSize: components.ButtonSizeSmall,
	})
	layout := dialog.Begin(m.RemoteContainerDetailsTitle, details.container.Name)
	state.paintRemoteContainerDetailsBody(canvas, layout.Body, details.container)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitRemoteContainerDetailsClose, Label: m.RemoteClose, Variant: components.ButtonSecondary},
	)
	dialog.End()
}

func (state *State) paintRemoteContainerDetailsBody(canvas *gfx.Canvas, bounds gfx.Rect, container remotetools.Container) {
	const pad = float32(8)
	content := gfx.Rect{
		X: bounds.X + pad, Y: bounds.Y + pad,
		Width: max(bounds.Width-pad*2, 0), Height: max(bounds.Height-pad*2, 0),
	}
	tabsBounds := gfx.Rect{
		X: content.X, Y: content.Y, Width: min(float32(320), content.Width),
		Height: min(float32(34), content.Height),
	}
	if tabsBounds.Height <= 0 {
		return
	}
	options := state.remoteContainerDetailTabs()
	components.BeginTabs(canvas, state.Theme(), tabsBounds, options[:], state.remoteContainerDetailTab()).Paint()
	panelY := tabsBounds.Bottom() + 8
	panelBounds := gfx.Rect{
		X: content.X, Y: panelY, Width: content.Width,
		Height: max(content.Bottom()-panelY, 0),
	}
	if panelBounds.Height <= 0 {
		return
	}
	details, m := &state.remote.containerDetails, state.messages.Tools
	if state.remoteContainerDetailTab() != hitRemoteContainerDetailsEnvironment {
		state.paintRemoteDetailCode(
			canvas, panelBounds, &details.text, state.remoteContainerOverviewBody(container),
			m.RemoteContainerDetailsTitle, m.RemoteContainerDetailsTitle,
			hitRemoteContainerDetailsText, hitRemoteContainerDetailsScroll,
		)
		return
	}
	state.paintRemoteEnvironmentView(
		canvas, panelBounds, &details.text,
		details.loading, details.failed, details.status, m.RemoteContainerEnvironmentEmpty,
		details.environment,
		[2]string{m.RemoteProcessEnvironmentName, m.RemoteProcessEnvironmentValue},
		m.RemoteProcessEnvironment, m.RemoteProcessEnvironment,
		hitRemoteContainerDetailsText, hitRemoteContainerDetailsScroll,
	)
}

func (state *State) remoteContainerDetailsTextPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	details := &state.remote.containerDetails
	if !details.open {
		return gfx.EventResult{}, false
	}
	return state.remoteDetailTextPointer(event, &details.text, hitRemoteContainerDetailsText)
}

func (state *State) remoteContainerDetailsScrollPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	details := &state.remote.containerDetails
	if !details.open {
		return gfx.EventResult{}, false
	}
	return state.remoteDetailScrollPointer(event, &details.text, hitRemoteContainerDetailsScroll)
}

func (state *State) remoteContainerDetailsWheel(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	details := &state.remote.containerDetails
	if !details.open {
		return gfx.EventResult{}, false
	}
	return state.remoteDetailWheel(event, &details.text, hitRemoteContainerDetailsText, hitRemoteContainerDetailsScroll)
}

func (state *State) remoteContainerDetailsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !state.remoteToolsVisible() || !state.remote.containerDetails.open {
		return gfx.EventResult{}, false
	}
	details := &state.remote.containerDetails
	if result, handled := state.remoteDetailTextShortcut(event, &details.text); handled {
		return result, true
	}
	if result, handled := state.remoteDetailKeyboardScroll(
		event, &details.text,
		hitRemoteContainerDetailsText, hitRemoteContainerDetailsScroll,
	); handled {
		return result, true
	}
	options := state.remoteContainerDetailTabs()
	if event.Target == hitRemoteContainerDetailsOverview || event.Target == hitRemoteContainerDetailsEnvironment {
		if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat {
			state.selectRemoteContainerDetailTab(event.Target)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if next, handled := components.TabsKeyDown(
			event.Target, options[:], components.TabsHorizontal, components.DirectionLTR, event.Key,
		); handled {
			state.selectRemoteContainerDetailTab(next)
			return gfx.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}, true
		}
	}
	if event.Key == gfx.KeyTab && event.Modifiers&^gfx.ModifierShift == 0 {
		return gfx.EventResult{Handled: true, ContinueTraversal: true}, true
	}
	if event.Key == gfx.KeyEscape && !event.Repeat {
		state.closeRemoteContainerDetails()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat && event.Target == hitRemoteContainerDetailsClose {
		state.closeRemoteContainerDetails()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{Handled: true}, true
}
