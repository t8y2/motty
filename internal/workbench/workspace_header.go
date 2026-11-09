package workbench

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// workspaceHeaderLabelSize is the size of the header buttons' labels, one for all
// three so the bar reads as a row.
const workspaceHeaderLabelSize = float32(13)

const workspaceHeaderHeight = float32(38)

var workspaceHeaderHits = tree.Feature("workspace-header")

var (
	hitWorkspaceHeaderBack          = tree.ID(workspaceHeaderHits, "back")
	hitWorkspaceHeaderBroadcast     = tree.ID(workspaceHeaderHits, "broadcast")
	hitWorkspaceHeaderLayout        = tree.ID(workspaceHeaderHits, "layout")
	hitWorkspaceHeaderSave          = tree.ID(workspaceHeaderHits, "save-template")
	hitWorkspaceHeaderLayoutDismiss = tree.ID(workspaceHeaderHits, "layout-dismiss")
)

// workspaceHeaderPaint keeps the controls whose popup anchors are needed after
// the headless layout has finished.
type workspaceHeaderPaint struct {
	back       ui.Box
	connection ui.Box
	broadcast  ui.Box
	layout     ui.Box
	save       ui.Box
}

func (state *State) workspaceManaged(workspaceID layoutcore.WorkspaceID) bool {
	_, ok := state.managedWorkspaceLayouts[workspaceID]
	return ok
}

func (state *State) paintWorkspaceHeader(canvas *gfx.Canvas, size gfx.Size) (open bool) {
	workspaceID := state.deck.ActiveWorkspace()
	if !state.workspaceManaged(workspaceID) || workspaceID == 0 || state.activeSection != sectionTerminal {
		return false
	}

	pal, m := state.pal, state.messages
	frame := state.workspacePixelBoundsFor(workspaceID, size)
	bounds := gfx.Rect{X: frame.X, Y: windowBarHeight, Width: frame.Width, Height: workspaceHeaderHeight}
	background := pal.workspace
	canvas.Fill(bounds, background)
	canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - 1, Width: bounds.Width, Height: 1}, pal.line)

	status := state.workspaceConnectionState(workspaceID)
	connection := fmt.Sprintf(m.WorkspaceHeaderConnectionFmt, status.Connected, status.Panes)
	if status.Pending > 0 {
		connection = fmt.Sprintf(m.WorkspaceHeaderPendingFmt, status.Pending)
	} else if status.Empty > 0 {
		connection = fmt.Sprintf(m.WorkspaceHeaderEmptyFmt, status.Empty)
	} else if status.Panes >= 2 && status.Connected < 2 {
		connection = m.WorkspaceHeaderNeedsTwo
	}
	light, color := state.workspaceConnectionLight(status)
	color = state.workspaceStatusInk(color)
	connectionWidth := canvas.TextWidthWeight(connection, metrics.FontSizeS, gfx.WeightMedium) + 28
	broadcasting := state.broadcast == workspaceID
	broadcastLabel := m.WorkspaceHeaderBroadcast
	if broadcasting {
		broadcastLabel = fmt.Sprintf(m.WorkspaceHeaderBroadcastingFmt, status.Connected)
	}
	broadcastWidth := canvas.TextWidthWeight(broadcastLabel, workspaceHeaderLabelSize, gfx.WeightMedium) + 36
	layoutLabel := m.WorkspaceHeaderLayout
	layoutWidth := canvas.TextWidthWeight(layoutLabel, workspaceHeaderLabelSize, gfx.WeightMedium) + 36
	_, temporary := state.temporaryWorkspaceLayouts[workspaceID]
	saveWidth := canvas.TextWidthWeight(m.WorkspaceDraftSave, workspaceHeaderLabelSize, gfx.WeightMedium) + 36
	compactWidth := float32(640)
	if temporary {
		compactWidth = max(compactWidth, connectionWidth+broadcastWidth+layoutWidth+saveWidth+180)
	}
	compact := bounds.Width < compactWidth
	if compact {
		layoutWidth, saveWidth = 30, 30
	}

	// Every painted element reserves its measured width, including status text.
	var slots workspaceHeaderPaint
	f := state.workspaceHeaderLayout.Begin(canvas, state.Theme(), bounds)
	f.Row().Fill().PadX(10).Gap(6).Items(ui.Center).Children(func() {
		slots.back = f.Slot().Size(30, 30).Shrink(0)
		f.Column().Grow(1).Gap(0).Children(func() {
			f.Text(state.workspaceTitle(workspaceID)).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).
				Color(pal.text).LineHeight(20).Ellipsis()
			if compact {
				f.Text(connection).FontSize(11).Color(color).LineHeight(15).Ellipsis()
			}
		})
		if !compact {
			slots.connection = f.Slot().Size(connectionWidth, 22).Shrink(0).MarginSides(0, 8, 0, 0)
		}
		if temporary {
			slots.save = f.Slot().Size(saveWidth, 30).Shrink(0)
		}
		slots.broadcast = f.Slot().Size(broadcastWidth, 30).Shrink(0)
		slots.layout = f.Slot().Size(layoutWidth, 30).Shrink(0)

	})
	f.End()
	state.paintIconButton(canvas, hitWorkspaceHeaderBack, iconChevronLeft, slots.back.Rect(),
		m.WorkspaceHeaderBack, false, iconButtonQuiet, background)
	if !compact {
		box := slots.connection.Rect()
		paintLight(canvas, box.X+8, box.Y+box.Height/2, light, color, background)
		canvas.Label(connection, gfx.Rect{X: box.X + 20, Y: box.Y, Width: box.Width - 20, Height: box.Height},
			gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: color})
	}
	state.paintWorkspaceBroadcast(canvas, slots.broadcast.Rect(), broadcastLabel, status, broadcasting)
	if temporary {
		if compact {
			state.paintIconButton(canvas, hitWorkspaceHeaderSave, iconFolderPlus, slots.save.Rect(), m.WorkspaceSaveMenu,
				false, iconButtonQuiet, background)
		} else {
			components.BeginButton(canvas, state.Theme(), hitWorkspaceHeaderSave, slots.save.Rect()).Small().FontSize(workspaceHeaderLabelSize).
				WithIcon(lucideIcons[iconFolderPlus]).Ghost(m.WorkspaceDraftSave)
		}
	}
	if compact {
		state.paintIconButton(canvas, hitWorkspaceHeaderLayout, iconColumns2, slots.layout.Rect(), m.WorkspaceHeaderLayout,
			false, iconButtonQuiet, background)
	} else {
		components.BeginButton(canvas, state.Theme(), hitWorkspaceHeaderLayout, slots.layout.Rect()).Small().FontSize(workspaceHeaderLabelSize).
			WithIcon(lucideIcons[iconColumns2]).Ghost(layoutLabel)
	}

	if state.workspaceHeaderMenu == workspaceHeaderMenuLayout {
		state.paintWorkspaceLayoutMenu(canvas, size, slots.layout.Rect())
		open = true
	}
	return open || state.workspaceHeaderMenu == workspaceHeaderMenuTools
}

// Broadcast uses a warning treatment because it changes where keystrokes go.
// The text and switch semantics change immediately; only the surface eases.
func (state *State) paintWorkspaceBroadcast(canvas *gfx.Canvas, bounds gfx.Rect, label string, status workspaceConnectionState, enabled bool) {
	pal := state.pal
	available := enabled || workspaceCanBroadcast(status)
	target := float32(0)
	if enabled {
		target = 1
	}
	amount := canvas.Animate(hitWorkspaceHeaderBroadcast, 0, target, gfx.Motion{Duration: 140 * time.Millisecond, Easing: gfx.EaseOut})
	background := pal.workspace.Mix(pal.warning, .12*amount)
	foreground := state.workspaceStatusInk(pal.muted)
	hint := state.messages.Tools.BroadcastStart
	if enabled {
		foreground, hint = state.workspaceStatusInk(pal.warning), state.messages.Tools.BroadcastStop
	}
	if !available {
		foreground = pal.faint
		hint = state.messages.Tools.StatusBroadcastNeedsTwo
		if status.Pending > 0 {
			hint = fmt.Sprintf(state.messages.WorkspaceBroadcastWaitingFmt, status.Pending)
		}
	}
	if available && (canvas.Hovered() == hitWorkspaceHeaderBroadcast || canvas.Pressed() == hitWorkspaceHeaderBroadcast) {
		background = background.Mix(foreground, .10)
	}
	canvas.Rounded(bounds, radius.control, background)
	if canvas.FocusVisible() == hitWorkspaceHeaderBroadcast {
		canvas.RoundedBorder(bounds, radius.control, 1.5, pal.accent, background)
	}
	canvas.DrawIcon(lucideIcons[iconRadio], gfx.Rect{X: bounds.X + 9, Y: bounds.Y + 7, Width: 16, Height: 16}, foreground)
	canvas.Label(label, gfx.Rect{X: bounds.X + 30, Y: bounds.Y, Width: max(0, bounds.Width-36), Height: bounds.Height},
		gfx.TextStyle{Size: workspaceHeaderLabelSize, Weight: gfx.WeightMedium, Color: foreground})
	canvas.HitWithOptions(hitWorkspaceHeaderBroadcast, bounds, hint, gfx.HitOptions{
		Role: gfx.RoleSwitch, Checked: enabled, Disabled: !available, Hint: hint,
		Description: label,
	})
}

// Semantic colors also serve as small text here, so daylight needs darker ink
// than the status-dot palette, including over the broadcast button's tint.
func (state *State) workspaceStatusInk(color gfx.Color) gfx.Color {
	if !state.pal.dark {
		return color.Mix(state.pal.text, .28)
	}
	return color.Mix(state.pal.text, .08)
}

func (state *State) workspaceConnectionLight(status workspaceConnectionState) (lightKind, gfx.Color) {
	switch {
	case status.Pending > 0:
		return lightBusy, state.pal.warning
	case status.Failed > 0 && status.Connected > 0:
		return lightWaiting, state.pal.warning
	case status.Failed > 0:
		return lightFault, state.pal.danger
	case status.Connected > 0 && status.Connected == status.Panes:
		return lightOn, state.pal.success
	case status.Connected > 0:
		return lightOn, state.pal.success
	default:
		return lightIdle, state.pal.faint
	}
}

func (state *State) workspaceTitle(workspaceID layoutcore.WorkspaceID) string {
	for _, tab := range state.workspaceTabs {
		if tab.ID == workspaceID {
			return state.tabLabel(tab)
		}
	}
	return ""
}

func (state *State) clickWorkspaceHeader(id gfx.ID) bool {
	if state.clickWorkspaceArrangement(id) || state.clickWorkspacePaneTools(id) {
		return true
	}
	if !state.workspaceManaged(state.deck.ActiveWorkspace()) || state.activeSection != sectionTerminal {
		return false
	}
	switch {
	case id.Scope() == hitWorkspacePaneTabs:
		if index, ok := id.In(hitWorkspacePaneTabs); ok && index < len(state.panes) {
			state.activatePane(state.deck.ActiveWorkspace(), state.panes[index].ID)
		}
	case id == hitWorkspaceHeaderBack:
		state.leaveWorkspaceForLibrary()
	case id == hitWorkspaceHeaderBroadcast:
		state.toggleBroadcast()
	case id == hitWorkspaceHeaderSave:
		state.openWorkspaceSave(state.deck.ActiveWorkspace())
	case id == hitWorkspaceHeaderLayout:
		if state.workspaceHeaderMenu == workspaceHeaderMenuLayout {
			state.workspaceHeaderMenu = workspaceHeaderMenuNone
		} else {
			state.openWorkspaceArrangement()
		}
	default:
		return false
	}
	return true
}

func (state *State) leaveWorkspaceForLibrary() {
	state.clearBroadcast()
	state.status = state.messages.Tools.StatusBroadcastOff
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
		state.releaseIdleSFTP(session)
	}
	state.activeSection = sectionWorkspaces
	state.workspaceHeaderMenu = workspaceHeaderMenuNone
	state.save()
}

func (state *State) rebuildManagedWorkspace(workspaceID layoutcore.WorkspaceID) {
	state.syncActiveSession()
	state.refreshWorkspaceViews()
	state.fitWorkspaceTerminals(workspaceID)
	state.fitSFTP(state.viewport)
	state.save()
}

func (state *State) workspaceHeaderKey(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || state.workspaceHeaderMenu == workspaceHeaderMenuNone {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEscape && !event.Repeat {
		state.workspaceHeaderMenu = workspaceHeaderMenuNone
		return gfx.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}
	}
	if result, handled := state.menuKey(event); handled {
		return result
	}
	return gfx.EventResult{}
}

type workspaceHeaderMenuState uint8

const (
	workspaceHeaderMenuNone workspaceHeaderMenuState = iota
	workspaceHeaderMenuLayout
	workspaceHeaderMenuTools
)
