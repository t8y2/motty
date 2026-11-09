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
	if state.hostDialogMode != hostDialogEdit || !ok || !host.isSSH() {
		return messages.HostJumpDescriptionUnsaved
	}
	if route, ok := state.proxyRouteForTarget(host.ID); ok {
		jump, found := state.hostByID(route.JumpID)
		if !found {
			return messages.HostJumpDescriptionMissing
		}
		return fmt.Sprintf(messages.HostJumpDescriptionFmt, jump.Label)
	}
	if state.sshHostCount() < 2 {
		return messages.HostJumpDescriptionUnavailable
	}
	return messages.HostJumpDescriptionDirect
}

// hostJumpConfigureEnabled reports whether the row's button can open a route:
// the host must be saved, SSH, and either already routed or accompanied by
// another SSH host to route through.
func (state *State) hostJumpConfigureEnabled() bool {
	host, ok := state.hostByID(state.hostDialogID)
	if state.hostDialogMode != hostDialogEdit || !ok || !host.isSSH() {
		return false
	}
	if _, ok := state.proxyRouteForTarget(host.ID); ok {
		return true
	}
	return state.sshHostCount() >= 2
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

func (state *State) paintHostJumpRow(canvas *gfx.Canvas, bounds gfx.Rect) {
	messages, theme := state.messages.Tools, state.Theme()
	content := components.BeginField(canvas, theme, bounds).Paint(messages.HostJumpRow, "", "")
	button := components.BeginButton(canvas, theme, hitHostJumpConfigure, gfx.Rect{}).XSmall().SizeToFit(messages.ProxyConfigure)
	var action ui.Box
	f := state.partLayout.Begin(canvas, theme, content)
	// The summary sits in a box like the other fields' inputs, with its button
	// inside, so the row has the same edge and height as they do.
	f.Row().Fill().Items(ui.Center).Gap(8).PadSides(0, 4, 0, 12).
		Background(state.pal.surface).Border(1, state.pal.line).Radius(radius.control).Children(func() {
		f.Text(state.hostJumpDescription()).FontSize(metrics.FontSizeS).Color(state.pal.muted).Ellipsis().Grow(1)
		action = f.Slot().Size(button.BoundsWidth(), 24)
	})
	f.End()
	if state.hostJumpConfigureEnabled() {
		components.BeginButton(canvas, theme, hitHostJumpConfigure, action.Rect()).XSmall().Outline(messages.ProxyConfigure)
		// A disabled control is not a tab stop: cycling into it would leave the
		// form's focus where it was and skip the fields after it.
		state.rememberHostControl(hitHostJumpConfigure, action.Rect())
		return
	}
	components.BeginButton(canvas, theme, hitHostJumpConfigure, action.Rect()).XSmall().Disabled().Outline(messages.ProxyConfigure)
}
