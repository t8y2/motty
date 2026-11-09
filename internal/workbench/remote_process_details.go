package workbench

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

var (
	hitRemoteProcessMenuDismiss     = tree.ID(remoteToolsHits, "remote-process-menu-dismiss")
	hitRemoteProcessMenuView        = tree.ID(remoteToolsHits, "remote-process-menu-view")
	hitRemoteProcessMenuCopyPID     = tree.ID(remoteToolsHits, "remote-process-menu-copy-pid")
	hitRemoteProcessMenuCopyUser    = tree.ID(remoteToolsHits, "remote-process-menu-copy-user")
	hitRemoteProcessMenuCopyCommand = tree.ID(remoteToolsHits, "remote-process-menu-copy-command")
	hitRemoteProcessMenuTerminate   = tree.ID(remoteToolsHits, "remote-process-menu-terminate")
	hitRemoteProcessMenuKill        = tree.ID(remoteToolsHits, "remote-process-menu-kill")
	hitRemoteProcessMenuNiceLower   = tree.ID(remoteToolsHits, "remote-process-menu-nice-lower")
	hitRemoteProcessMenuNiceNormal  = tree.ID(remoteToolsHits, "remote-process-menu-nice-normal")
	hitRemoteProcessMenuNiceHigher  = tree.ID(remoteToolsHits, "remote-process-menu-nice-higher")
	hitRemoteProcessDetailsDismiss  = tree.ID(remoteToolsHits, "remote-process-details-dismiss")
	hitRemoteProcessDetailsClose    = tree.ID(remoteToolsHits, "remote-process-details-close")
	hitRemoteProcessDetailsCommand  = tree.ID(remoteToolsHits, "remote-process-details-command")
	hitRemoteProcessDetailsEnv      = tree.ID(remoteToolsHits, "remote-process-details-environment")
	hitRemoteProcessDetailsText     = tree.ID(remoteToolsHits, "remote-process-details-text")
	hitRemoteProcessDetailsScroll   = tree.ID(remoteToolsHits, "remote-process-details-scroll")
)

type remoteProcessDetails struct {
	open               bool
	process            remotetools.Process
	tab                gfx.ID
	collectedAt        time.Time
	serial             uint64
	loading            bool
	failed             bool
	status             string
	commandValue       string
	commandLoaded      bool
	commandLoading     bool
	commandFailed      bool
	commandStatus      string
	commandRequest     uint64
	commandCancel      func()
	environmentLoaded  bool
	environmentRequest uint64
	environmentCancel  func()
	environment        []remotetools.ProcessEnvironmentVariable
	text               remoteDetailText
}

type remoteProcessMenu struct {
	open bool
	key  string
	at   gfx.Point
}

func (state *State) remoteProcessByKey(key string) (remotetools.Process, bool) {
	for _, process := range state.remote.procs {
		if remotetools.ProcessKey(process) == key {
			return process, true
		}
	}
	return remotetools.Process{}, false
}

func (state *State) openRemoteProcessDetails(process remotetools.Process) {
	state.remote.processMenu = remoteProcessMenu{}
	state.cancelRemoteProcessEnvironment()
	state.cancelRemoteProcessCommand()
	state.remote.processDetailsSerial++
	serial := state.remote.processDetailsSerial
	state.remote.processDetails = remoteProcessDetails{
		open: true, process: process, collectedAt: state.remote.collectedAt,
		tab: hitRemoteProcessDetailsCommand, serial: serial, commandValue: process.Command,
		commandStatus: state.messages.Tools.RemoteProcessCommandLoading,
	}
	state.loadRemoteProcessCommand()
}

func (state *State) closeRemoteProcessDetails() {
	state.cancelRemoteProcessEnvironment()
	state.cancelRemoteProcessCommand()
	state.remote.processDetails = remoteProcessDetails{}
}

func (state *State) selectRemoteProcessDetailTab(tab gfx.ID) {
	details := &state.remote.processDetails
	if !details.open || tab != hitRemoteProcessDetailsCommand && tab != hitRemoteProcessDetailsEnv {
		return
	}
	if details.tab == tab {
		if tab == hitRemoteProcessDetailsEnv {
			state.loadRemoteProcessEnvironment()
		} else {
			state.loadRemoteProcessCommand()
		}
		return
	}
	if details.tab == hitRemoteProcessDetailsEnv {
		state.cancelRemoteProcessEnvironment()
	} else {
		state.cancelRemoteProcessCommand()
	}
	details.tab = tab
	details.text.reset()
	if tab == hitRemoteProcessDetailsEnv {
		state.loadRemoteProcessEnvironment()
	} else {
		state.loadRemoteProcessCommand()
	}
}

// openRemoteProcessMenuOn runs while a process row is painted. A secondary
// press selects the row and opens the same fixed action menu in the main window
// and in a native remote-tools window.
func (state *State) openRemoteProcessMenuOn(canvas *gfx.Canvas, id gfx.ID, index int) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary ||
		!state.remoteToolsVisible() || state.remote.processDetails.open {
		return
	}
	if index < 0 || index >= len(state.remote.rows) {
		return
	}
	row := &state.remote.rows[index]
	state.remote.selected = row.key
	state.remote.processMenu = remoteProcessMenu{
		open: true, key: row.key, at: gfx.Point{X: event.X, Y: event.Y},
	}
	canvas.Consume()
}

func (state *State) clickRemoteProcessDetails(id gfx.ID) bool {
	details := &state.remote.processDetails
	if !details.open {
		return false
	}
	switch id {
	case hitRemoteProcessDetailsDismiss, hitRemoteProcessDetailsClose:
		state.closeRemoteProcessDetails()
		return true
	case hitRemoteProcessDetailsCommand, hitRemoteProcessDetailsEnv:
		state.selectRemoteProcessDetailTab(id)
		return true
	case hitRemoteTerminate, hitRemoteKill,
		hitRemoteNiceLower, hitRemoteNiceNormal, hitRemoteNiceHigher:
		state.closeRemoteProcessDetails()
		state.clickRemoteTools(id)
		return true
	default:
		// Keep the detail dialog modal even for direct API calls. The window and
		// canvas overlay already intercept pointer hits, but consuming every
		// action here prevents stale hit IDs from reaching the obscured tools
		// page during the paint that first opened the dialog.
		return true
	}
}

func (state *State) remoteProcessDetailTabs() [2]components.TabOption {
	m := state.messages.Tools
	return [2]components.TabOption{
		{ID: hitRemoteProcessDetailsCommand, Label: m.RemoteProcessCommand, Icon: lucideIcons[iconTerminal]},
		{ID: hitRemoteProcessDetailsEnv, Label: m.RemoteProcessEnvironment, Icon: lucideIcons[iconSliders]},
	}
}

func (state *State) remoteProcessDetailTab() gfx.ID {
	if state.remote.processDetails.tab == hitRemoteProcessDetailsEnv {
		return hitRemoteProcessDetailsEnv
	}
	return hitRemoteProcessDetailsCommand
}

func (state *State) processMenuProcess() (remotetools.Process, bool) {
	if !state.remote.processMenu.open {
		return remotetools.Process{}, false
	}
	return state.remoteProcessByKey(state.remote.processMenu.key)
}

func (state *State) clickRemoteProcessMenu(id gfx.ID) bool {
	menu := &state.remote.processMenu
	if !menu.open {
		return false
	}
	process, found := state.processMenuProcess()
	menu.open = false
	if !found {
		return true
	}
	switch id {
	case hitRemoteProcessMenuDismiss:
		return true
	case hitRemoteProcessMenuView:
		state.openRemoteProcessDetails(process)
	case hitRemoteProcessMenuCopyPID:
		state.copyRemoteProcessValue(fmt.Sprint(process.PID))
	case hitRemoteProcessMenuCopyUser:
		state.copyRemoteProcessValue(process.User)
	case hitRemoteProcessMenuCopyCommand:
		state.copyRemoteProcessValue(process.Command)
	case hitRemoteProcessMenuTerminate:
		state.clickRemoteTools(hitRemoteTerminate)
	case hitRemoteProcessMenuKill:
		state.clickRemoteTools(hitRemoteKill)
	case hitRemoteProcessMenuNiceLower:
		state.clickRemoteTools(hitRemoteNiceLower)
	case hitRemoteProcessMenuNiceNormal:
		state.clickRemoteTools(hitRemoteNiceNormal)
	case hitRemoteProcessMenuNiceHigher:
		state.clickRemoteTools(hitRemoteNiceHigher)
	}
	return true
}

func (state *State) copyRemoteProcessValue(value string) {
	if value == "" || value == "—" {
		return
	}
	copy := state.options.SetClipboardText
	if state.remoteWindow != nil && state.remoteWindow.owner != nil {
		copy = state.remoteWindow.owner.options.SetClipboardText
	}
	view := &state.remote
	if copy == nil {
		return
	}
	if err := copy(value); err != nil {
		view.failed = true
		view.status = fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err)
		return
	}
	view.failed = false
	view.status = state.messages.StatusClipboardCopied
}

func (state *State) paintRemoteProcessMenu(canvas *gfx.Canvas, viewport gfx.Rect) {
	menu := &state.remote.processMenu
	if !menu.open {
		return
	}
	process, found := state.processMenuProcess()
	if !found {
		*menu = remoteProcessMenu{}
		return
	}
	rows := state.remoteProcessMenuRows(process)
	width, height := rows.size(canvas, state.contextMenuTheme())
	width = min(width, 480)
	bounds := state.popupAt(canvas, viewport, menu.at, width, height, 6)
	popup := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer + 1, Scope: dialogScope, DismissID: hitRemoteProcessMenuDismiss,
		Viewport: viewport, Scroll: state.menuScrollFor(hitRemoteProcessMenuDismiss), RowHeight: contextMenuRow,
	})
	state.menuPlaced(popup.Bounds())
	rows.paint(&popup, nil)
	popup.End()
}

func (state *State) remoteProcessMenuRows(process remotetools.Process) menuRows {
	m := state.messages.Tools
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	actionsBlocked := state.remoteProcessActionsBlocked(process)
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitRemoteProcessMenuView, Label: m.RemoteProcessDetails, Icon: icon(iconInfo)})
	rows.add(components.ContextMenuItem{ID: hitRemoteProcessMenuCopyPID, Label: m.RemoteProcessCopyPID, Icon: icon(iconCopy)})
	rows.add(components.ContextMenuItem{ID: hitRemoteProcessMenuCopyUser, Label: m.RemoteProcessCopyUser, Icon: icon(iconCopy), Disabled: process.User == "" || process.User == "—"})
	rows.add(components.ContextMenuItem{ID: hitRemoteProcessMenuCopyCommand, Label: m.RemoteProcessCopyCommand, Icon: icon(iconCopy), Disabled: process.Command == ""})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitRemoteProcessMenuTerminate, Label: m.RemoteTerminate, Icon: icon(iconStop), Disabled: actionsBlocked})
	rows.add(components.ContextMenuItem{ID: hitRemoteProcessMenuKill, Label: m.RemoteForceKill, Icon: icon(iconTrash), Variant: components.ContextMenuItemDestructive, Disabled: actionsBlocked})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitRemoteProcessMenuNiceLower, Label: m.RemoteReniceLower, Icon: icon(iconSliders), Disabled: actionsBlocked})
	rows.add(components.ContextMenuItem{ID: hitRemoteProcessMenuNiceNormal, Label: m.RemoteReniceNormal, Icon: icon(iconSliders), Disabled: actionsBlocked})
	rows.add(components.ContextMenuItem{ID: hitRemoteProcessMenuNiceHigher, Label: m.RemoteReniceHigher, Icon: icon(iconArrowUp), Disabled: actionsBlocked})
	return rows
}

func (state *State) remoteProcessActionsBlocked(process remotetools.Process) bool {
	view := &state.remote
	return view.loading || view.stale || view.collectedAt.IsZero() ||
		process.PID <= 1 || process.StartIdentity == ""
}

func (state *State) paintRemoteProcessDetails(canvas *gfx.Canvas, viewport gfx.Rect) {
	details := &state.remote.processDetails
	if !details.open {
		return
	}
	m := state.messages.Tools
	contentWidth := min(float32(980), max(viewport.Width-32, 360))
	bodyHeight := min(float32(620), max(viewport.Height-132, 320))
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer + 2, Scope: dialogScope,
		DismissID: hitRemoteProcessDetailsDismiss, CloseID: hitRemoteProcessDetailsClose,
		InitialFocusID: hitRemoteProcessDetailsClose,
		ContentSize:    gfx.Size{Width: contentWidth}, FitBody: true, BodyHeight: bodyHeight,
		ButtonSize: components.ButtonSizeSmall,
	})
	process := details.process
	description := shortCommand(process.Command)
	if description == "" {
		description = fmt.Sprintf("PID %d", process.PID)
	}
	layout := dialog.Begin(m.RemoteProcessDetailsTitle, description)
	state.paintRemoteProcessDetailsBody(canvas, layout.Body, process, details.collectedAt)
	actionsBlocked := state.remoteProcessActionsBlocked(process)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitRemoteTerminate, Label: m.RemoteTerminate, Variant: components.ButtonOutline, Disabled: actionsBlocked},
		components.ButtonAction{ID: hitRemoteKill, Label: m.RemoteForceKill, Variant: components.ButtonDestructive, Disabled: actionsBlocked},
		components.ButtonAction{ID: hitRemoteNiceLower, Label: m.RemoteReniceLower, Variant: components.ButtonOutline, Disabled: actionsBlocked},
		components.ButtonAction{ID: hitRemoteNiceNormal, Label: m.RemoteReniceNormal, Variant: components.ButtonOutline, Disabled: actionsBlocked},
		components.ButtonAction{ID: hitRemoteNiceHigher, Label: m.RemoteReniceHigher, Variant: components.ButtonOutline, Disabled: actionsBlocked},
		components.ButtonAction{ID: hitRemoteProcessDetailsClose, Label: m.RemoteClose, Variant: components.ButtonSecondary},
	)
	dialog.End()
}

type processDetailPair struct {
	label string
	value string
}

func (state *State) remoteProcessDetailPairs(process remotetools.Process, collectedAt time.Time) [10]processDetailPair {
	m := state.messages.Tools
	return [10]processDetailPair{
		{m.RemoteProcessPID, fmt.Sprint(process.PID)},
		{m.RemoteProcessParentPID, processInteger(process.ParentPID)},
		{m.RemoteProcessUser, processText(process.User)},
		{m.RemoteProcessState, processStateText(state, process.State)},
		{m.RemoteProcessCPU, processPercent(process.CPU)},
		{m.RemoteProcessMemory, processPercent(process.Memory)},
		{m.RemoteProcessResidentMemory, processBytes(process.ResidentBytes)},
		{m.RemoteProcessVirtualMemory, processBytes(process.VirtualBytes)},
		{m.RemoteProcessElapsed, processText(process.Elapsed)},
		{m.RemoteProcessCollectedAt, processCollectedAt(collectedAt)},
	}
}

func (state *State) remoteProcessDetailPairRows(process remotetools.Process, collectedAt time.Time) [5][2]processDetailPair {
	flat := state.remoteProcessDetailPairs(process, collectedAt)
	return [5][2]processDetailPair{
		{flat[0], flat[1]},
		{flat[2], flat[3]},
		{flat[4], flat[5]},
		{flat[6], flat[7]},
		{flat[8], flat[9]},
	}
}

func processText(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

func processInteger(value int) string {
	if value <= 0 {
		return "—"
	}
	return fmt.Sprint(value)
}

func processPercent(value float64) string {
	return fmt.Sprintf("%.1f%%", value)
}

func processBytes(value uint64) string {
	if value == 0 {
		return "—"
	}
	return formatBytes(int64(value))
}

func processCollectedAt(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Format("15:04:05")
}

func processStateText(state *State, value string) string {
	m := state.messages.Tools
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "—"
	}
	switch {
	case strings.EqualFold(trimmed, "Ready"):
		return m.RemoteProcessStateReady
	case strings.EqualFold(trimmed, "Running"):
		return m.RemoteProcessStateRunning
	case strings.EqualFold(trimmed, "Sleeping"):
		return m.RemoteProcessStateSleeping
	case strings.EqualFold(trimmed, "Waiting"):
		return m.RemoteProcessStateWaiting
	case strings.EqualFold(trimmed, "Zombie"):
		return m.RemoteProcessStateZombie
	case strings.EqualFold(trimmed, "Stopped"):
		return m.RemoteProcessStateStopped
	case strings.EqualFold(trimmed, "Paging"):
		return m.RemoteProcessStatePaging
	case strings.EqualFold(trimmed, "Idle"):
		return m.RemoteProcessStateIdle
	case strings.EqualFold(trimmed, "Blocked"):
		return m.RemoteProcessStateBlocked
	case strings.EqualFold(trimmed, "Suspended"):
		return m.RemoteProcessStateSuspended
	case strings.EqualFold(trimmed, "Terminated"):
		return m.RemoteProcessStateTerminated
	case strings.EqualFold(trimmed, "Growing"):
		return m.RemoteProcessStateGrowing
	case strings.EqualFold(trimmed, "Not Responding"):
		return m.RemoteProcessStateNotResponding
	case strings.EqualFold(trimmed, "Unknown"):
		return m.RemoteProcessStateUnknown
	}
	switch strings.ToUpper(trimmed[:1]) {
	case "R":
		return m.RemoteProcessStateRunning
	case "S":
		return m.RemoteProcessStateSleeping
	case "D":
		return m.RemoteProcessStateWaiting
	case "Z":
		return m.RemoteProcessStateZombie
	case "T":
		return m.RemoteProcessStateStopped
	case "W":
		return m.RemoteProcessStatePaging
	case "I":
		return m.RemoteProcessStateIdle
	case "P":
		return m.RemoteProcessStateSuspended
	case "X", "K":
		return m.RemoteProcessStateTerminated
	}
	return trimmed
}

func (state *State) remoteProcessDetailsKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !state.remoteToolsVisible() {
		return gfx.EventResult{}, false
	}
	if state.remote.processDetails.open {
		if result, handled := state.remoteDetailTextShortcut(event, &state.remote.processDetails.text); handled {
			return result, true
		}
		details := &state.remote.processDetails
		if result, handled := state.remoteDetailKeyboardScroll(
			event, &details.text,
			hitRemoteProcessDetailsText, hitRemoteProcessDetailsScroll,
		); handled {
			return result, true
		}
		options := state.remoteProcessDetailTabs()
		if event.Target == hitRemoteProcessDetailsCommand || event.Target == hitRemoteProcessDetailsEnv {
			if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat {
				state.selectRemoteProcessDetailTab(event.Target)
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
			if next, handled := components.TabsKeyDown(
				event.Target, options[:], components.TabsHorizontal, components.DirectionLTR, event.Key,
			); handled {
				state.selectRemoteProcessDetailTab(next)
				return gfx.EventResult{Focus: next, FocusVisible: true, Handled: true, Repaint: true}, true
			}
		}
		if event.Key == gfx.KeyTab && event.Modifiers&^gfx.ModifierShift == 0 {
			return gfx.EventResult{Handled: true, ContinueTraversal: true}, true
		}
		if event.Key == gfx.KeyEscape && !event.Repeat {
			state.closeRemoteProcessDetails()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat {
			switch event.Target {
			case hitRemoteProcessDetailsClose:
				state.closeRemoteProcessDetails()
				return gfx.EventResult{Handled: true, Repaint: true}, true
			case hitRemoteTerminate, hitRemoteKill,
				hitRemoteNiceLower, hitRemoteNiceNormal, hitRemoteNiceHigher:
				if state.remoteProcessActionsBlocked(details.process) {
					return gfx.EventResult{Handled: true}, true
				}
				state.closeRemoteProcessDetails()
				state.clickRemoteTools(event.Target)
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		}
		return gfx.EventResult{Handled: true}, true
	}
	if state.remote.processMenu.open && event.Key == gfx.KeyEscape && !event.Repeat {
		state.remote.processMenu = remoteProcessMenu{}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

func (state *State) remoteProcessDetailsTextPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	details := &state.remote.processDetails
	if !details.open {
		return gfx.EventResult{}, false
	}
	return state.remoteDetailTextPointer(event, &details.text, hitRemoteProcessDetailsText)
}

func (state *State) remoteProcessDetailsScrollPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	details := &state.remote.processDetails
	if !details.open {
		return gfx.EventResult{}, false
	}
	return state.remoteDetailScrollPointer(event, &details.text, hitRemoteProcessDetailsScroll)
}

func (state *State) remoteProcessMenuPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if !state.remoteToolsVisible() || !state.remote.processMenu.open ||
		event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary ||
		event.Target != hitRemoteProcessMenuDismiss {
		return gfx.EventResult{}, false
	}
	state.remote.processMenu = remoteProcessMenu{}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (w *remoteToolsWindow) pointer(event gfx.PointerEvent) gfx.EventResult {
	if w.closed {
		return gfx.EventResult{Handled: true}
	}
	if w.ui.modal == modalSudoSFTP {
		return gfx.EventResult{}
	}
	if result, handled := w.ui.remoteProcessMenuPointer(event); handled {
		return result
	}
	if result, handled := w.ui.remoteProcessDetailsTextPointer(event); handled {
		return result
	}
	if result, handled := w.ui.remoteContainerDetailsTextPointer(event); handled {
		return result
	}
	if result, handled := w.ui.remoteContainerDetailsScrollPointer(event); handled {
		return result
	}
	if result, handled := w.ui.remoteProcessScrollPointer(event); handled {
		return result
	}
	if result, handled := w.ui.menuPointer(event); handled {
		return result
	}
	return gfx.EventResult{}
}
