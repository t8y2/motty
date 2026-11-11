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

// workspaceHeaderPlan is what the workspace header works out before its page
// declaration: its place, what it says and which controls are condensed.
type workspaceHeaderPlan struct {
	workspaceID    layoutcore.WorkspaceID
	bounds         gfx.Rect
	title          string
	connection     string
	broadcastLabel string
	broadcastWidth float32
	layoutLabel    string
	layoutWidth    float32
	saveWidth      float32
	compact        bool
	temporary      bool
	broadcasting   bool
	status         workspaceConnectionState
	light          lightKind
	color          gfx.Color
}

func (state *State) workspaceManaged(workspaceID layoutcore.WorkspaceID) bool {
	_, ok := state.managedWorkspaceLayouts[workspaceID]
	return ok
}

// planWorkspaceHeader works out the header for the active workspace. It reports
// false when the workspace shows no managed header.
func (state *State) planWorkspaceHeader(canvas *gfx.Canvas, size gfx.Size) (workspaceHeaderPlan, bool) {
	workspaceID := state.deck.ActiveWorkspace()
	if !state.workspaceManaged(workspaceID) || workspaceID == 0 || state.activeSection != sectionTerminal {
		return workspaceHeaderPlan{}, false
	}
	m := state.messages
	frame := state.workspacePixelBoundsFor(workspaceID, size)
	var plan workspaceHeaderPlan
	plan.workspaceID = workspaceID
	plan.bounds = gfx.Rect{X: frame.X, Y: windowBarHeight, Width: frame.Width, Height: workspaceHeaderHeight}
	plan.title = state.workspaceTitle(workspaceID)
	plan.status = state.workspaceConnectionState(workspaceID)
	plan.connection = fmt.Sprintf(m.WorkspaceHeaderConnectionFmt, plan.status.Connected, plan.status.Panes)
	if plan.status.Pending > 0 {
		plan.connection = fmt.Sprintf(m.WorkspaceHeaderPendingFmt, plan.status.Pending)
	} else if plan.status.Empty > 0 {
		plan.connection = fmt.Sprintf(m.WorkspaceHeaderEmptyFmt, plan.status.Empty)
	} else if plan.status.Panes >= 2 && plan.status.Connected < 2 {
		plan.connection = m.WorkspaceHeaderNeedsTwo
	}
	plan.light, plan.color = state.workspaceConnectionLight(plan.status)
	plan.color = state.workspaceStatusInk(plan.color)
	connectionWidth := canvas.TextWidthWeight(plan.connection, metrics.FontSizeS, gfx.WeightMedium) + 28
	plan.broadcasting = state.broadcast == workspaceID
	plan.broadcastLabel = m.WorkspaceHeaderBroadcast
	if plan.broadcasting {
		plan.broadcastLabel = fmt.Sprintf(m.WorkspaceHeaderBroadcastingFmt, plan.status.Connected)
	}
	plan.broadcastWidth = canvas.TextWidthWeight(plan.broadcastLabel, workspaceHeaderLabelSize, gfx.WeightMedium) + 36
	plan.layoutLabel = m.WorkspaceHeaderLayout
	plan.layoutWidth = canvas.TextWidthWeight(plan.layoutLabel, workspaceHeaderLabelSize, gfx.WeightMedium) + 36
	_, plan.temporary = state.temporaryWorkspaceLayouts[workspaceID]
	plan.saveWidth = canvas.TextWidthWeight(m.WorkspaceDraftSave, workspaceHeaderLabelSize, gfx.WeightMedium) + 36
	compactWidth := float32(640)
	if plan.temporary {
		compactWidth = max(compactWidth, connectionWidth+plan.broadcastWidth+plan.layoutWidth+plan.saveWidth+180)
	}
	plan.compact = plan.bounds.Width < compactWidth
	if plan.compact {
		plan.layoutWidth, plan.saveWidth = 30, 30
	}
	return plan, true
}

// declareWorkspaceHeader declares the managed workspace's header in the page:
// its title, the connection status and the controls, with the slots the painter
// keeps.
func (state *State) declareWorkspaceHeader(c ui.C, size gfx.Size) {
	plan, ok := state.planWorkspaceHeader(c.Canvas(), size)
	if !ok {
		state.workspaceHeaderPlan, state.workspaceHeaderSlots = workspaceHeaderPlan{}, workspaceHeaderPaint{}
		return
	}
	state.workspaceHeaderPlan = plan
	pal, m := state.pal, state.messages
	var slots workspaceHeaderPaint
	container := ui.Column(c).Absolute().
		Left(plan.bounds.X).Top(plan.bounds.Y).Width(plan.bounds.Width).Height(plan.bounds.Height).
		Background(pal.workspace)
	container.Children(func() {
		ui.Row(c).Fill().PadX(10).Gap(6).Items(ui.Center).Children(func() {
			slots.back = state.declareIconButton(c, hitWorkspaceHeaderBack, iconChevronLeft, m.WorkspaceHeaderBack, false, iconButtonQuiet).Size(30, 30).Shrink(0)
			ui.Column(c).Grow(1).Gap(0).Children(func() {
				ui.Text(c, plan.title).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).
					Color(pal.text).LineHeight(20).Ellipsis()
				if plan.compact {
					ui.Text(c, plan.connection).FontSize(11).Color(plan.color).LineHeight(15).Ellipsis()
				}
			})
			if !plan.compact {
				connectionWidth := c.Canvas().TextWidthWeight(plan.connection, metrics.FontSizeS, gfx.WeightMedium) + 28
				slots.connection = ui.Slot(c).Size(connectionWidth, 22).Shrink(0).MarginSides(0, 8, 0, 0)
			}
			if plan.temporary {
				if plan.compact {
					slots.save = state.declareIconButton(c, hitWorkspaceHeaderSave, iconFolderPlus, m.WorkspaceSaveMenu, false, iconButtonQuiet).Size(plan.saveWidth, 30).Shrink(0)
				} else {
					slots.save = ui.Slot(c).Size(plan.saveWidth, 30).Shrink(0)
				}
			}
			slots.broadcast = ui.Slot(c).Size(plan.broadcastWidth, 30).Shrink(0)
			if plan.compact {
				slots.layout = state.declareIconButton(c, hitWorkspaceHeaderLayout, iconColumns2, m.WorkspaceHeaderLayout, false, iconButtonQuiet).Size(plan.layoutWidth, 30).Shrink(0)
			} else {
				slots.layout = ui.Slot(c).Size(plan.layoutWidth, 30).Shrink(0)
			}
		})
	})
	state.workspaceHeaderSlots = slots
}

func (state *State) paintWorkspaceHeader(canvas *gfx.Canvas, size gfx.Size) (open bool) {
	plan := state.workspaceHeaderPlan
	if plan.bounds.Width <= 0 || plan.bounds.Height <= 0 {
		return false
	}
	pal, m := state.pal, state.messages
	bounds, slots := plan.bounds, state.workspaceHeaderSlots
	background := pal.workspace
	canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - 1, Width: bounds.Width, Height: 1}, pal.line)
	if !plan.compact {
		box := slots.connection.Rect()
		paintLight(canvas, box.X+8, box.Y+box.Height/2, plan.light, plan.color, background)
		canvas.Label(plan.connection, gfx.Rect{X: box.X + 20, Y: box.Y, Width: box.Width - 20, Height: box.Height},
			gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: plan.color})
	}
	state.paintWorkspaceBroadcast(canvas, slots.broadcast.Rect(), plan.broadcastLabel, plan.status, plan.broadcasting)
	if plan.temporary && !plan.compact {
		components.BeginButton(canvas, state.Theme(), hitWorkspaceHeaderSave, slots.save.Rect()).Small().FontSize(workspaceHeaderLabelSize).
			WithIcon(lucideIcons[iconFolderPlus]).Ghost(m.WorkspaceDraftSave)
	}
	if !plan.compact {
		components.BeginButton(canvas, state.Theme(), hitWorkspaceHeaderLayout, slots.layout.Rect()).Small().FontSize(workspaceHeaderLabelSize).
			WithIcon(lucideIcons[iconColumns2]).Ghost(plan.layoutLabel)
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
