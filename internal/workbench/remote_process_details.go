package workbench

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/remotetools"
)

var (
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

func (state *State) remoteProcessByKey(key string) (remotetools.Process, bool) {
	for _, process := range state.remote.procs {
		if remotetools.ProcessKey(process) == key {
			return process, true
		}
	}
	return remotetools.Process{}, false
}

func (state *State) openRemoteProcessDetails(process remotetools.Process) {
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

// declareRemoteProcessMenuItems declares the process context menu: the row the
// table last selected, and the same actions the process buttons offer. The
// tools window and the workbench modal share it; each supplies how an action is
// run and how it is deferred past the paint.
func (state *State) declareRemoteProcessMenuItems(menu *ui.MenuItems, after func(func()), act func(gfx.ID)) {
	m := state.messages.Tools
	process, found := state.selectedProcess()
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	blocked := !found || state.remoteProcessActionsBlocked(process)
	run := func(fn func()) {
		if after != nil {
			after(fn)
			return
		}
		fn()
	}
	if menu.Item(m.RemoteProcessDetails).Icon(icon(iconInfo)).Disabled(!found).Clicked() && found {
		run(func() { state.openRemoteProcessDetails(process) })
	}
	if menu.Item(m.RemoteProcessCopyPID).Icon(icon(iconCopy)).Disabled(!found).Clicked() && found {
		run(func() { state.copyRemoteProcessValue(fmt.Sprint(process.PID)) })
	}
	if menu.Item(m.RemoteProcessCopyUser).Icon(icon(iconCopy)).
		Disabled(!found || process.User == "" || process.User == "—").Clicked() && found {
		run(func() { state.copyRemoteProcessValue(process.User) })
	}
	if menu.Item(m.RemoteProcessCopyCommand).Icon(icon(iconCopy)).
		Disabled(!found || process.Command == "").Clicked() && found {
		run(func() { state.copyRemoteProcessValue(process.Command) })
	}
	menu.Separator()
	if menu.Item(m.RemoteTerminate).Icon(icon(iconStop)).Disabled(blocked).Clicked() && found {
		run(func() { act(hitRemoteTerminate) })
	}
	if menu.Item(m.RemoteForceKill).Icon(icon(iconTrash)).Destructive().Disabled(blocked).Clicked() && found {
		run(func() { act(hitRemoteKill) })
	}
	menu.Separator()
	if menu.Item(m.RemoteReniceLower).Icon(icon(iconSliders)).Disabled(blocked).Clicked() && found {
		run(func() { act(hitRemoteNiceLower) })
	}
	if menu.Item(m.RemoteReniceNormal).Icon(icon(iconSliders)).Disabled(blocked).Clicked() && found {
		run(func() { act(hitRemoteNiceNormal) })
	}
	if menu.Item(m.RemoteReniceHigher).Icon(icon(iconArrowUp)).Disabled(blocked).Clicked() && found {
		run(func() { act(hitRemoteNiceHigher) })
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

// declareRemoteProcessDetailsDialog declares the open process details as a
// dialog of the page. Its body stays a painted slot: the environment table's
// text selection is the application's. act runs an action after the detail
// dialog has closed, as the tools window and the workbench modal each do.
func (state *State) declareRemoteProcessDetailsDialog(c ui.C, act func(gfx.ID)) {
	details := &state.remote.processDetails
	if !details.open {
		return
	}
	m := state.messages.Tools
	process := details.process
	description := shortCommand(process.Command)
	if description == "" {
		description = fmt.Sprintf("PID %d", process.PID)
	}
	viewport := c.Bounds()
	contentWidth := min(float32(980), max(viewport.Width-32, 360))
	bodyHeight := min(float32(620), max(viewport.Height-132, 320))
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), gfx.Rect{Width: viewport.Width, Height: viewport.Height}, components.DialogOptions{
		ContentSize: gfx.Size{Width: contentWidth}, FitBody: true, BodyHeight: bodyHeight,
		ButtonSize: components.ButtonSizeSmall,
	}).Layout(description)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.RemoteProcessDetailsTitle, Description: description, Width: contentWidth,
		Height: layout.Content.Height, Layer: dialogLayer + 2, Scope: dialogScope,
		DismissID: hitRemoteProcessDetailsDismiss, CloseID: hitRemoteProcessDetailsClose,
	})
	dialog.Body(func() {
		ui.Slot(c).Fill().Draw(func(d *ui.DrawContext) {
			state.paintRemoteProcessDetailsBody(d.Canvas, d.Rect, process, details.collectedAt)
		})
	})
	if canvas := c.Canvas(); canvas != nil {
		switch {
		case canvas.Clicked(hitRemoteProcessDetailsCommand):
			c.After(func() { state.selectRemoteProcessDetailTab(hitRemoteProcessDetailsCommand) })
		case canvas.Clicked(hitRemoteProcessDetailsEnv):
			c.After(func() { state.selectRemoteProcessDetailTab(hitRemoteProcessDetailsEnv) })
		}
	}
	blocked := state.remoteProcessActionsBlocked(process)
	dialog.Footer(func() {
		action := func(id gfx.ID, label string, variant components.ButtonVariant) {
			button := dialogButton(c, label, variant, blocked).Small().Shrink(0)
			if button.Clicked() {
				c.After(func() {
					state.closeRemoteProcessDetails()
					act(id)
				})
			}
		}
		action(hitRemoteTerminate, m.RemoteTerminate, components.ButtonOutline)
		action(hitRemoteKill, m.RemoteForceKill, components.ButtonDestructive)
		action(hitRemoteNiceLower, m.RemoteReniceLower, components.ButtonOutline)
		action(hitRemoteNiceNormal, m.RemoteReniceNormal, components.ButtonOutline)
		action(hitRemoteNiceHigher, m.RemoteReniceHigher, components.ButtonOutline)
		if dialogButton(c, m.RemoteClose, components.ButtonSecondary, false).Small().Shrink(0).AutoFocus().Clicked() {
			c.After(func() { state.closeRemoteProcessDetails() })
		}
	})
	if dialog.Dismissed() {
		c.After(func() { state.closeRemoteProcessDetails() })
	}
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
		return gfx.EventResult{Handled: true}, true
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
