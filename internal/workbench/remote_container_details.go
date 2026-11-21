package workbench

import (
	"context"
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
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
	// usage is the docker stats sample the listing held when the dialog
	// opened, so the overview and the table show the same figures.
	usage      remotetools.ContainerUsage
	usageKnown bool
	// The inspect read fills the overview's configuration rows. It has its own
	// request, because switching to the environment tab cancels it.
	inspectLoaded  bool
	inspectLoading bool
	inspectFailed  bool
	inspectStatus  string
	inspectRequest uint64
	inspectCancel  func()
	inspect        remotetools.ContainerDetails
	text           remoteDetailText
}

func (state *State) cancelRemoteContainerInspect() {
	details := &state.remote.containerDetails
	details.inspectRequest++
	details.inspectLoading = false
	if details.inspectCancel != nil {
		details.inspectCancel()
		details.inspectCancel = nil
	}
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
	state.cancelRemoteContainerInspect()
	state.remote.containerDetailsSerial++
	state.remote.containerDetails = remoteContainerDetails{
		open: true, container: container, tab: hitRemoteContainerDetailsOverview,
		serial: state.remote.containerDetailsSerial,
	}
	if usage, ok := state.remote.containerUsage[container.ID]; ok {
		state.remote.containerDetails.usage = usage
		state.remote.containerDetails.usageKnown = true
	}
	state.loadRemoteContainerInspect()
}

func (state *State) closeRemoteContainerDetails() {
	state.cancelRemoteContainerEnvironment()
	state.cancelRemoteContainerInspect()
	state.remote.containerDetails = remoteContainerDetails{}
}

func (state *State) declareRemoteContainerMenuItems(menu *ui.MenuItems, after func(func()), act func(gfx.ID)) {
	actions, count := state.remoteToolActions()
	run := func(fn func()) {
		if after != nil {
			after(fn)
			return
		}
		fn()
	}
	for index := 0; index < count; index++ {
		action := actions[index]
		if action.ID == hitRemoteClose {
			continue
		}
		item := menu.Item(action.Label).Disabled(action.Disabled)
		if action.Variant == components.ButtonDestructive {
			item = item.Destructive()
		}
		if item.Clicked() {
			id := action.ID
			run(func() { act(id) })
		}
	}
}

func (state *State) selectRemoteContainerDetailTab(tab gfx.ID) {
	details := &state.remote.containerDetails
	if !details.open || tab != hitRemoteContainerDetailsOverview && tab != hitRemoteContainerDetailsEnvironment {
		return
	}
	if details.tab == tab {
		if tab == hitRemoteContainerDetailsEnvironment {
			state.loadRemoteContainerEnvironment()
		} else {
			state.loadRemoteContainerInspect()
		}
		return
	}
	if details.tab == hitRemoteContainerDetailsEnvironment {
		state.cancelRemoteContainerEnvironment()
	} else {
		state.cancelRemoteContainerInspect()
	}
	details.tab = tab
	details.text.reset()
	if tab == hitRemoteContainerDetailsEnvironment {
		state.loadRemoteContainerEnvironment()
	} else {
		state.loadRemoteContainerInspect()
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
}

// loadRemoteContainerInspect reads the configuration the overview shows. It
// runs when the dialog opens and again when the overview tab is clicked after
// a failure, as the environment tab re-reads its own list.
func (state *State) loadRemoteContainerInspect() {
	details := &state.remote.containerDetails
	if !details.open || state.remoteContainerDetailTab() != hitRemoteContainerDetailsOverview ||
		details.inspectLoading || details.inspectLoaded && !details.inspectFailed {
		return
	}
	session, ok := state.remoteSession()
	if !ok {
		details.inspectLoading = false
		details.inspectFailed = true
		details.inspectLoaded = true
		details.inspectStatus = state.messages.History.StatusNotConnected
		return
	}
	if state.remote.stale {
		details.inspectLoading = false
		details.inspectFailed = true
		details.inspectLoaded = true
		details.inspectStatus = state.messages.Tools.RemoteRefreshRequired
		return
	}
	sourceID := state.remote.session
	container := details.container
	serial := details.serial
	details.inspectRequest++
	request := details.inspectRequest
	details.inspectLoading, details.inspectFailed = true, false
	details.inspectStatus = ""
	runner := remotetools.ForSystem(commandRunnerFor(session), session.host.System.String())
	ctx, cancel := context.WithTimeout(state.remoteToolsContext(), remoteTimeout)
	details.inspectCancel = cancel
	go func() {
		defer cancel()
		inspect, err := remotetools.ReadContainerDetails(ctx, runner, container.ID)
		state.postRemoteToolsUpdate(func(state *State) {
			state.applyRemoteContainerInspect(serial, request, sourceID, container, inspect, err)
		})
	}()
}

func (state *State) applyRemoteContainerInspect(
	serial, request uint64,
	sourceID uint32,
	container remotetools.Container,
	inspect remotetools.ContainerDetails,
	err error,
) {
	details := &state.remote.containerDetails
	if !details.open || details.serial != serial || state.remote.session != sourceID ||
		details.inspectRequest != request || details.container.ID != container.ID {
		return
	}
	details.inspectCancel = nil
	details.inspectLoading = false
	details.inspectLoaded = true
	if err != nil {
		details.inspectFailed = true
		details.inspectStatus = state.remoteErrorText(err)
		return
	}
	details.inspectFailed, details.inspectStatus = false, ""
	details.inspect = inspect
	details.text.clearSelection()
}

func (state *State) clickRemoteContainerDetails(id gfx.ID) bool {
	details := &state.remote.containerDetails
	if !details.open {
		return false
	}
	switch id {
	case hitRemoteContainerDetailsDismiss, hitRemoteContainerDetailsClose:
		state.closeRemoteContainerDetails()
	}
	// The detail dialog is modal. Swallow stale hits from the obscured list so
	// a click cannot act on a different container underneath it.
	return true
}

// remoteContainerDetailTabIDs are the dialog's tabs, in order.
var remoteContainerDetailTabIDs = [2]gfx.ID{hitRemoteContainerDetailsOverview, hitRemoteContainerDetailsEnvironment}

func (state *State) remoteContainerDetailTabLabel(tab gfx.ID) string {
	m := state.messages.Tools
	if tab == hitRemoteContainerDetailsEnvironment {
		return m.RemoteProcessEnvironment
	}
	return m.RemoteContainerOverview
}

func (state *State) remoteContainerDetailTab() gfx.ID {
	if state.remote.containerDetails.tab == hitRemoteContainerDetailsEnvironment {
		return hitRemoteContainerDetailsEnvironment
	}
	return hitRemoteContainerDetailsOverview
}

func (state *State) remoteContainerOverviewBody(container remotetools.Container) string {
	m, details := state.messages.Tools, &state.remote.containerDetails
	facts := [][2]string{
		{m.RemoteColName, processText(container.Name)},
		{m.RemoteColID, processText(container.ID)},
		{m.RemoteColImage, processText(container.Image)},
		{m.RemoteColState, processText(container.State)},
		{m.RemoteColStatus, processText(container.Status)},
	}
	if details.usageKnown {
		facts = append(facts,
			[2]string{m.RemoteProcessCPU, containerPercent(details.usage.CPUPercent)},
			[2]string{m.RemoteProcessMemory, fmt.Sprintf("%s / %s (%s)",
				details.usage.MemoryUsed, details.usage.MemoryLimit, containerPercent(details.usage.MemoryPercent))},
			[2]string{m.RemoteContainerNetworkIO, details.usage.NetIO},
			[2]string{m.RemoteContainerBlockIO, details.usage.BlockIO},
			[2]string{m.RemoteContainerPIDs, details.usage.PIDs},
		)
	}
	var configuration [][2]string
	switch {
	case details.inspectLoading && !details.inspectLoaded:
		configuration = append(configuration, [2]string{m.RemoteContainerConfiguration, m.RemoteContainerConfigurationLoading})
	case details.inspectFailed:
		configuration = append(configuration, [2]string{m.RemoteContainerConfiguration, details.inspectStatus})
	case details.inspectLoaded:
		configuration = containerInspectRows(details.inspect, m)
	}
	return remoteDetailSections(facts, configuration)
}

// remoteDetailSections turns label/value rows into the detail body's source.
// Every label is padded to the widest one's cells, so values share a column on
// the fenced block's fixed-pitch grid; sections are separated by a blank line.
func remoteDetailSections(sections ...[][2]string) string {
	width := 0
	for _, rows := range sections {
		for _, row := range rows {
			width = max(width, remoteTextCells(row[0]))
		}
	}
	var source strings.Builder
	for _, rows := range sections {
		if len(rows) == 0 {
			continue
		}
		if source.Len() > 0 {
			source.WriteString("\n\n")
		}
		for rowIndex, row := range rows {
			if rowIndex > 0 {
				source.WriteByte('\n')
			}
			source.WriteString(row[0])
			source.WriteString(strings.Repeat(" ", width-remoteTextCells(row[0])+2))
			source.WriteString(row[1])
		}
	}
	return source.String()
}

// remoteTextCells is how many cells value covers on the fixed-pitch grid the
// detail body is laid out on.
func remoteTextCells(value string) int {
	cells := 0
	for _, r := range value {
		cells += text.CellWidth(r)
	}
	return cells
}

// containerInspectRows lists the configuration docker inspect holds: the
// command that started the container, its ports, networks and mounts. Values
// with several entries keep one row each, so a long list stays readable.
func containerInspectRows(inspect remotetools.ContainerDetails, m i18n.ToolsMessages) [][2]string {
	rows := [][2]string{
		{m.RemoteContainerCreated, inspect.Created.Format("2006-01-02 15:04:05 MST")},
		{m.RemoteContainerCommand, processText(inspect.Command)},
		{m.RemoteContainerImageID, processText(shortContainerImageID(inspect.ImageID))},
		{m.RemoteContainerRestartPolicy, processText(inspect.RestartPolicy)},
	}
	for _, port := range inspect.Ports {
		rows = append(rows, [2]string{m.RemoteContainerPorts, port})
	}
	for _, network := range inspect.Networks {
		rows = append(rows, [2]string{m.RemoteContainerNetworks, strings.TrimSpace(network.Name + " " + network.Address)})
	}
	for _, mount := range inspect.Mounts {
		rows = append(rows, [2]string{m.RemoteContainerMounts, containerMountText(mount)})
	}
	return rows
}

func shortContainerImageID(image string) string {
	image = strings.TrimPrefix(image, "sha256:")
	if len(image) > 12 {
		return image[:12]
	}
	return image
}

func containerMountText(mount remotetools.ContainerMount) string {
	mode := "rw"
	if mount.ReadOnly {
		mode = "ro"
	}
	source := processText(mount.Source)
	if mount.Type == "volume" && mount.Name != "" {
		source = mount.Name
	}
	return mount.Type + " " + source + " → " + processText(mount.Destination) + " (" + mode + ")"
}

// declareRemoteContainerDetailsDialog declares the open container details as a
// dialog of the page. Its body stays a painted slot: the environment table's
// text selection is the application's.
func (state *State) declareRemoteContainerDetailsDialog(c ui.C) {
	details := &state.remote.containerDetails
	if !details.open {
		return
	}
	m := state.messages.Tools
	viewport := c.Bounds()
	contentWidth := min(float32(940), max(viewport.Width-32, 360))
	bodyHeight := min(float32(620), max(viewport.Height-132, 320))
	height := dialogContentHeight(c.Canvas(), *c.Theme(), dialogViewport(c), contentWidth, details.container.Name, bodyHeight) - smallDialogFooterAdjust(*c.Theme())
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.RemoteContainerDetailsTitle, Description: details.container.Name, Width: contentWidth,
		Height: height, Layer: dialogLayer + 2, Scope: dialogScope,
		DismissID: hitRemoteContainerDetailsDismiss, CloseID: hitRemoteContainerDetailsClose,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Pad(8).Gap(8).Children(func() {
			tab := state.remoteContainerDetailTab()
			strip := ui.Tabs(c, &tab, remoteContainerDetailTabIDs[:], state.remoteContainerDetailTabLabel).Width(min(float32(320), viewport.Width)).Height(34).Shrink(0)
			// A click on the active tab asks for that read again, as it does for
			// the process details.
			if index, clicked := strip.TabClicked(); clicked && index >= 0 && index < len(remoteContainerDetailTabIDs) {
				id := remoteContainerDetailTabIDs[index]
				c.After(func() { state.selectRemoteContainerDetailTab(id) })
			}
			state.declareRemoteContainerDetailsText(c, contentWidth-32)
		})
	})
	dialog.Footer(func() {
		if dialogButton(c, m.RemoteClose, components.ButtonSecondary, false).Small().Shrink(0).AutoFocus().Clicked() {
			c.After(func() { state.closeRemoteContainerDetails() })
		}
	})
	if dialog.Dismissed() {
		c.After(func() { state.closeRemoteContainerDetails() })
	}
}

func (state *State) declareRemoteContainerDetailsText(c ui.C, width float32) {
	details, m := &state.remote.containerDetails, state.messages.Tools
	if state.remoteContainerDetailTab() != hitRemoteContainerDetailsEnvironment {
		state.declareRemoteDetail(c, &details.text, remoteDetailSpec{
			textID: hitRemoteContainerDetailsText, textIDLabel: m.RemoteContainerDetailsTitle,
			scrollbarID: hitRemoteContainerDetailsScroll, scrollbarLabel: m.RemoteContainerDetailsTitle,
			source: remoteDetailSource(state.remoteContainerOverviewBody(details.container)), width: width,
		})
		return
	}
	empty := ""
	if len(details.environment) == 0 {
		empty = m.RemoteContainerEnvironmentEmpty
	}
	notice, tone := state.remoteDetailNotice(details.loading, details.failed, details.status, empty)
	spec := remoteDetailSpec{
		textID: hitRemoteContainerDetailsText, textIDLabel: m.RemoteProcessEnvironment,
		scrollbarID: hitRemoteContainerDetailsScroll, scrollbarLabel: m.RemoteProcessEnvironment,
		notice: notice, noticeTone: tone, width: width,
	}
	if notice == "" {
		spec.columns, spec.header, spec.rows = remoteEnvironmentTable(details.environment,
			[2]string{m.RemoteProcessEnvironmentName, m.RemoteProcessEnvironmentValue})
	}
	state.declareRemoteDetail(c, &details.text, spec)
}

func (state *State) remoteContainerDetailsTextPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	details := &state.remote.containerDetails
	if !details.open {
		return gfx.EventResult{}, false
	}
	return state.remoteDetailTextPointer(event, &details.text, hitRemoteContainerDetailsText)
}

func (state *State) remoteContainerDetailsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !state.remoteToolsVisible() || !state.remote.containerDetails.open {
		return gfx.EventResult{}, false
	}
	details := &state.remote.containerDetails
	if result, handled := state.remoteDetailTextShortcut(event, &details.text); handled {
		return result, true
	}
	if result, handled := state.remoteDetailKeyboardScroll(event, &details.text,
		hitRemoteContainerDetailsText, hitRemoteContainerDetailsScroll); handled {
		return result, true
	}
	if event.Key == gfx.KeyEscape && !event.Repeat {
		state.closeRemoteContainerDetails()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat && event.Target == hitRemoteContainerDetailsClose {
		state.closeRemoteContainerDetails()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	// The dialog is the page's: its focus trap and the Router's activation of the
	// focused footer button take every key this handler leaves empty, while none
	// of the page's own shortcuts may act underneath it.
	return gfx.EventResult{}, true
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
