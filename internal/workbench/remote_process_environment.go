package workbench

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/remotetools"
)

var (
	hitRemoteProcessEnvironment     = tree.ID(remoteToolsHits, "remote-process-environment")
	hitRemoteProcessEnvironmentBar  = tree.ID(remoteToolsHits, "remote-process-environment-bar")
	hitRemoteProcessEnvironmentRows = tree.Sub(remoteToolsHits, tree.Name("remote-process-environment-rows"))
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
		details.loading || details.environmentLoaded {
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
	runner := remotetools.ForSystem(session.conn, session.host.System.String())
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
	details.environmentScroll = 0
	details.environmentScrollValue = ""
}

func (state *State) paintRemoteProcessDetailsBody(canvas *ui.Canvas, bounds ui.Rect, process remotetools.Process, collectedAt time.Time) {
	theme := state.Theme()
	const (
		pad       = float32(8)
		rowHeight = float32(34)
	)
	content := ui.Rect{
		X: bounds.X + pad, Y: bounds.Y + pad,
		Width: max(bounds.Width-pad*2, 0), Height: max(bounds.Height-pad*2, 0),
	}
	infoHeight := rowHeight * 5
	infoBounds := ui.Rect{X: content.X, Y: content.Y, Width: content.Width, Height: min(infoHeight, content.Height)}
	labelWidth := min(float32(150), max(content.Width*0.18, 96))
	valueWidth := max((content.Width-labelWidth*2)/2, 0)
	columns := []components.TableColumn{
		{Width: labelWidth}, {Width: valueWidth}, {Width: labelWidth}, {Width: 0},
	}
	table := components.BeginTable(canvas, theme, infoBounds, columns).RowHeight(rowHeight)
	for _, pair := range state.remoteProcessDetailPairRows(process, collectedAt) {
		table.Row(ui.ID{}, []string{pair[0].label, pair[0].value, pair[1].label, pair[1].value}, false)
	}

	tabsY := infoBounds.Bottom() + 10
	tabsBounds := ui.Rect{
		X: content.X, Y: tabsY, Width: min(float32(300), content.Width),
		Height: min(float32(34), max(content.Bottom()-tabsY, 0)),
	}
	if tabsBounds.Height <= 0 {
		return
	}
	options := state.remoteProcessDetailTabs()
	components.BeginTabs(canvas, theme, tabsBounds, options[:], state.remoteProcessDetailTab()).Paint()

	panelY := tabsBounds.Bottom() + 8
	panelBounds := ui.Rect{
		X: content.X, Y: panelY, Width: content.Width,
		Height: max(content.Bottom()-panelY, 0),
	}
	if panelBounds.Height <= 0 {
		return
	}
	if state.remoteProcessDetailTab() == hitRemoteProcessDetailsEnv {
		state.paintRemoteProcessEnvironment(canvas, panelBounds)
	} else {
		state.paintRemoteProcessCommand(canvas, panelBounds, process.Command)
	}
}

func (state *State) paintRemoteProcessCommand(canvas *ui.Canvas, bounds ui.Rect, command string) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	theme, pal := state.Theme(), state.pal
	canvas.RoundedBorder(bounds, radius.control, 1, pal.line, pal.workspace)
	f := state.settingsRow.Begin(canvas, theme, bounds)
	f.Column().Fill().PadSides(12, 9, 12, 9).Children(func() {
		f.Wrap(processText(command)).FontSize(theme.Metrics.FontSizeM).LineHeight(19).Color(pal.text)
	})
	f.End()
}

func (state *State) paintRemoteProcessEnvironment(canvas *ui.Canvas, bounds ui.Rect) {
	details, m, theme, pal := &state.remote.processDetails, state.messages.Tools, state.Theme(), state.pal
	tableBounds := bounds
	headerHeight := components.TableHeaderHeight(theme)
	rowHeight := components.TableRowHeight(theme)
	keyWidth := min(float32(300), max(tableBounds.Width*0.36, 140))
	columns := []components.TableColumn{{Width: keyWidth}, {Width: 0}}
	headerBounds := ui.Rect{
		X: tableBounds.X, Y: tableBounds.Y, Width: tableBounds.Width,
		Height: min(headerHeight, tableBounds.Height),
	}
	header := components.BeginTable(canvas, theme, headerBounds, columns).HeaderHeight(headerHeight)
	header.Header([]string{m.RemoteProcessEnvironmentName, m.RemoteProcessEnvironmentValue})

	bodyBounds := ui.Rect{
		X: tableBounds.X, Y: headerBounds.Bottom(), Width: tableBounds.Width,
		Height: max(tableBounds.Height-headerBounds.Height, 0),
	}
	details.environmentBounds = bodyBounds
	message, color := "", pal.muted
	switch {
	case details.loading:
		message = m.RemoteProcessEnvironmentLoading
	case details.failed:
		message, color = details.status, pal.danger
	case len(details.environment) == 0:
		message = m.RemoteProcessEnvironmentEmpty
	}
	if message != "" {
		canvas.LabelCenter(message, bodyBounds, ui.TextStyle{Size: theme.Metrics.FontSizeS, Color: color})
		return
	}

	contentHeight := float32(len(details.environment)) * rowHeight
	area := components.BeginScrollArea(canvas, theme, bodyBounds, ui.Size{Width: bodyBounds.Width, Height: contentHeight}).
		Offset(0, details.environmentScroll).
		Viewport(hitRemoteProcessEnvironment, m.RemoteProcessEnvironment).
		VerticalScrollbar(hitRemoteProcessEnvironmentBar, m.RemoteProcessEnvironment, details.environmentScrollValue)
	geometry := area.Geometry()
	details.environmentScroll = geometry.OffsetY
	details.environmentScrollBar = geometry
	details.environmentScrollValue = strconv.Itoa(1+int(geometry.OffsetY/rowHeight)) + "/" + strconv.Itoa(len(details.environment))
	geometry = area.BeginContent()
	details.environmentScroll = geometry.OffsetY

	first := int(geometry.OffsetY / rowHeight)
	first = min(max(first, 0), len(details.environment))
	visible := int(geometry.Viewport.Height/rowHeight) + 2
	last := min(first+visible, len(details.environment))
	remainder := geometry.OffsetY - float32(first)*rowHeight
	rowsBounds := ui.Rect{
		X: geometry.Viewport.X, Y: geometry.Viewport.Y - remainder, Width: geometry.Viewport.Width,
		Height: min(geometry.Viewport.Height+rowHeight, float32(last-first)*rowHeight),
	}
	table := components.BeginTable(canvas, theme, rowsBounds, columns).RowHeight(rowHeight)
	for index := first; index < last; index++ {
		item := details.environment[index]
		table.Row(hitRemoteProcessEnvironmentRows.At(index), []string{item.Name, item.Value}, false)
	}
	area.EndContent()
}

func (state *State) remoteProcessEnvironmentScrollPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	details := &state.remote.processDetails
	if !details.open || state.remoteProcessDetailTab() != hitRemoteProcessDetailsEnv ||
		len(details.environment) == 0 ||
		(!details.environmentDragging &&
			!(event.Kind == ui.PointerDown && event.Button == ui.PointerButtonPrimary &&
				event.Target == hitRemoteProcessEnvironmentBar)) {
		return ui.EventResult{}, false
	}
	if event.Kind == ui.PointerDown && event.Button != ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	geometry := details.environmentScrollBar
	before := details.environmentScroll
	switch event.Kind {
	case ui.PointerDown:
		details.environmentDragging = true
		details.environmentGrab = geometry.VerticalThumb.Height / 2
		if event.Y >= geometry.VerticalThumb.Y &&
			event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
			details.environmentGrab = event.Y - geometry.VerticalThumb.Y
		} else {
			details.environmentScroll = geometry.VerticalOffsetForThumb(event.Y - details.environmentGrab)
		}
	case ui.PointerMove:
		if !event.Captured {
			return ui.EventResult{}, true
		}
		details.environmentScroll = geometry.VerticalOffsetForThumb(event.Y - details.environmentGrab)
	case ui.PointerUp:
		details.environmentDragging = false
	case ui.PointerExit:
		if !event.Captured {
			details.environmentDragging = false
		}
	default:
		return ui.EventResult{}, false
	}
	return ui.EventResult{Handled: true, Repaint: before != details.environmentScroll}, true
}

func (state *State) remoteProcessEnvironmentWheel(event ui.ScrollEvent) (ui.EventResult, bool) {
	details := &state.remote.processDetails
	if !details.open || state.remoteProcessDetailTab() != hitRemoteProcessDetailsEnv ||
		details.environmentScrollBar.MaxOffsetY <= 0 {
		return ui.EventResult{}, false
	}
	inside := pointInRect(event.X, event.Y, details.environmentBounds) ||
		event.Target == hitRemoteProcessEnvironment ||
		event.Target == hitRemoteProcessEnvironmentBar ||
		event.Target.Scope() == hitRemoteProcessEnvironmentRows
	if !inside {
		return ui.EventResult{}, false
	}
	_, rowHeight := state.remoteProcessTableMetrics()
	before := details.environmentScroll
	details.environmentScroll = min(max(details.environmentScroll+event.StepY(rowHeight), 0), details.environmentScrollBar.MaxOffsetY)
	return ui.EventResult{Handled: true, Repaint: before != details.environmentScroll}, true
}
