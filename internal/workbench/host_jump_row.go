package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var hostJumpHits = tree.Feature("host-jump")

var hitHostJumpConfigure = tree.ID(hostJumpHits, "host-jump-configure")

// hostJumpDescription is the row's summary: whether the saved host is reached
// directly, through another host, or not yet at all because the form is for an
// unsaved copy.
func (state *State) hostJumpDescription() string {
	messages := state.messages.Tools
	host, ok := state.hostByID(state.hostDialogID)
	if state.hostDialogMode != hostDialogEdit || !ok || !host.isRouteTarget() {
		return messages.HostJumpDescriptionUnsaved
	}
	if route, ok := state.proxyRouteForTarget(host.ID); ok {
		jump, found := state.hostByID(route.JumpID)
		if !found {
			return messages.HostJumpDescriptionMissing
		}
		return fmt.Sprintf(messages.HostJumpDescriptionFmt, jump.Label)
	}
	if host.isSSH() {
		if state.sshHostCount() < 2 {
			return messages.HostJumpDescriptionUnavailable
		}
	} else if state.sshHostCount() == 0 {
		// A remote desktop needs only the jump host the route signs in to.
		return messages.HostJumpDescriptionUnavailableDesktop
	}
	return messages.HostJumpDescriptionDirect
}

// hostJumpConfigureEnabled reports whether the row's button can open a route:
// the host must be saved and reachable by a route, with a jump host available.
func (state *State) hostJumpConfigureEnabled() bool {
	host, ok := state.hostByID(state.hostDialogID)
	if state.hostDialogMode != hostDialogEdit || !ok || !host.isRouteTarget() {
		return false
	}
	if _, ok := state.proxyRouteForTarget(host.ID); ok {
		return true
	}
	if host.isSSH() {
		return state.sshHostCount() >= 2
	}
	return state.sshHostCount() >= 1
}

// desktopRouteConfigured reports whether the desktop form's saved host has a
// route: the route then owns its connection, and the same-machine ViaSSH switch
// cannot also apply.
func (state *State) desktopRouteConfigured() bool {
	if state.hostDialogMode != hostDialogEdit {
		return false
	}
	_, ok := state.proxyRouteForTarget(state.hostDialogID)
	return ok
}

// openHostJumpRoute opens the route dialog for the host the form edits, and
// marks it so closing the route dialog returns to the form rather than to the
// page. An existing route opens for editing; otherwise a new one starts with
// this host as its target.
func (state *State) openHostJumpRoute() {
	if !state.hostJumpConfigureEnabled() {
		return
	}
	targetID := state.hostDialogID
	if route, ok := state.proxyRouteForTarget(targetID); ok {
		state.openEditProxyID(route.ID)
		state.proxyEditor.fromHostForm = true
		return
	}
	state.openProxyDialogForHost(targetID, true)
}

// declareHostJumpRow declares the host form's Jump route row: a form field
// whose control is the route summary and its Configure button.
func (state *State) declareHostJumpRow(c ui.C) ui.Box {
	messages, theme, form := state.messages.Tools, state.Theme(), &state.editor.form
	button := components.BeginButton(c.Canvas(), theme, gfx.ID{}, gfx.Rect{}).XSmall().SizeToFit(messages.ProxyConfigure)
	field := ui.FormField(c, &form.jumpField, messages.HostJumpRow).Height(float32(hostFormRowHeight))
	field.Children(func() {
		// The summary sits in a box like the other fields' inputs, with its
		// button inside, so the row has the same edge and height as they do.
		form.body.jumpRow = ui.Row(c).Fill().Items(ui.Center).Gap(8).PadSides(0, 4, 0, 12).
			Background(state.pal.surface).Border(1, state.pal.line).Radius(radius.control).Children(func() {
			ui.Text(c, state.hostJumpDescription()).FontSize(metrics.FontSizeS).Color(state.pal.muted).Ellipsis().Grow(1)
			form.body.jumpAction = ui.Slot(c).Size(button.BoundsWidth(), 24)
		})
	})
	return field
}

// paintHostJumpRow paints the Configure button into the slot the declared row
// left for it.
func (state *State) paintHostJumpRow(canvas *gfx.Canvas) {
	messages := state.messages.Tools
	action := state.editor.form.body.jumpAction
	if state.hostJumpConfigureEnabled() {
		components.BeginButton(canvas, state.Theme(), hitHostJumpConfigure, action.Rect()).XSmall().Outline(messages.ProxyConfigure)
		// A disabled control is not a tab stop: cycling into it would leave the
		// form's focus where it was and skip the fields after it.
		state.rememberHostControl(hitHostJumpConfigure, action.Rect())
		return
	}
	components.BeginButton(canvas, state.Theme(), hitHostJumpConfigure, action.Rect()).XSmall().Disabled().Outline(messages.ProxyConfigure)
}
