package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The jump-route pickers are page comboboxes: typing narrows the list to the
// host whose label matches, and Enter in the list chooses it.
func TestJumpRouteHostPickersFilterAndChoose(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatalf("proxy dialog = %d", state.modal)
	}
	clickPageLabel(t, state, gfx.RoleCombobox, state.messages.ProxyFieldTarget)
	pageComboboxType(t, state, state.messages.ProxyFieldTarget, "Rout")
	input := pageComboboxID(t, state, state.messages.ProxyFieldTarget)
	if result := windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyEnter, Target: input}); !result.Handled {
		t.Fatalf("Enter was not taken: %+v", result)
	}
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	router := hostByLabel(t, state, "Router")
	if state.proxyEditor.targetID != router.ID {
		t.Fatalf("chosen target = %d, want Router %d", state.proxyEditor.targetID, router.ID)
	}

	clickPageLabel(t, state, gfx.RoleCombobox, state.messages.ProxyFieldJump)
	pageComboboxType(t, state, state.messages.ProxyFieldJump, "Stag")
	clickPageLabel(t, state, gfx.RoleOption, "Staging")
	collapsed := 0
	for _, host := range state.hosts {
		if host.isSSH() && host.ID != router.ID {
			collapsed++
		}
	}
	if collapsed == 0 {
		t.Fatal("the fixture has no other SSH host to choose")
	}
	if state.proxyEditor.jumpID == router.ID {
		t.Fatal("the jump picker kept the target as its choice")
	}
}

func hostByLabel(t *testing.T, state *State, label string) Host {
	t.Helper()
	for _, host := range state.hosts {
		if host.Label == label {
			return host
		}
	}
	t.Fatalf("host %q is missing", label)
	return Host{}
}

// The host form shows whether the saved host is routed and opens the route
// dialog on that host, returning to the form when the route dialog closes.
func TestHostFormJumpRouteRowOpensPresetRouteAndReturns(t *testing.T) {
	state := newTestState(t, testOptions(t))
	production := hostByLabel(t, state, "Production")
	staging := hostByLabel(t, state, "Staging")
	state.openEditHostID(production.ID)
	if got := state.hostJumpDescription(); got != state.messages.Tools.HostJumpDescriptionDirect {
		t.Fatalf("summary = %q, want direct", got)
	}
	state.Click(hitHostJumpConfigure)
	if state.modal != modalNewProxy || state.proxyEditor.targetID != production.ID || !state.proxyEditor.fromHostForm {
		t.Fatalf("opened modal=%d target=%d fromForm=%v", state.modal, state.proxyEditor.targetID, state.proxyEditor.fromHostForm)
	}
	state.Click(hitProxyDialogCancel)
	if state.modal != modalNewHost {
		t.Fatalf("cancel returned to modal %d, want the host form", state.modal)
	}

	state.Click(hitHostJumpConfigure)
	state.Click(hitProxyDialogSubmit)
	if state.modal != modalNewHost || len(state.proxies) != 1 {
		t.Fatalf("saving route: modal=%d routes=%+v", state.modal, state.proxies)
	}
	want := fmt.Sprintf(state.messages.Tools.HostJumpDescriptionFmt, staging.Label)
	if got := state.hostJumpDescription(); got != want {
		t.Fatalf("summary after save = %q, want %q", got, want)
	}
	state.Click(hitHostJumpConfigure)
	if state.modal != modalNewProxy || !state.proxyEditor.editing || state.proxyEditor.targetID != production.ID {
		t.Fatalf("editing route: modal=%d editing=%v target=%d", state.modal, state.proxyEditor.editing, state.proxyEditor.targetID)
	}
}

// An unsaved host cannot own a route yet: the row says so and its button does
// nothing instead of opening a dialog that could not be saved.
func TestHostFormJumpRouteRowNeedsASavedHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	if got := state.hostJumpDescription(); got != state.messages.Tools.HostJumpDescriptionUnsaved {
		t.Fatalf("summary = %q, want the unsaved hint", got)
	}
	state.Click(hitHostJumpConfigure)
	if state.modal != modalNewHost {
		t.Fatalf("the unsaved host opened the route dialog: modal = %d", state.modal)
	}
}

// A remote desktop is a route target like an SSH host: its form shows the row
// and opens the route on it, and once a route exists the same-machine ViaSSH
// switch gives way, dimmed and inert.
func TestDesktopHostFormOwnsAJumpRoute(t *testing.T) {
	state := newTestState(t, testOptions(t))
	desk := addStreamHost(state, Host{Label: "Desk", Address: "10.0.0.9", User: "ops", Protocol: protocolVNC, Port: 5900})
	state.openEditHostID(desk.ID)
	if state.editor.protocol != protocolVNC {
		t.Fatalf("the form opened on %v", state.editor.protocol)
	}
	if got := state.hostJumpDescription(); got != state.messages.Tools.HostJumpDescriptionDirect {
		t.Fatalf("summary = %q, want direct", got)
	}
	if !state.hostJumpConfigureEnabled() {
		t.Fatal("the desktop's jump route cannot be configured")
	}
	canvas, _ := paintAt(state, gfx.Size{Width: 900, Height: 900})
	if via, ok := hitByID(canvas, hitDialogViaSSH); !ok || via.Disabled {
		t.Fatalf("the ViaSSH switch before a route is %+v, found=%v", via, ok)
	}

	state.Click(hitHostJumpConfigure)
	if state.modal != modalNewProxy || state.proxyEditor.targetID != desk.ID || !state.proxyEditor.fromHostForm {
		t.Fatalf("opened modal=%d target=%d fromForm=%v", state.modal, state.proxyEditor.targetID, state.proxyEditor.fromHostForm)
	}
	state.Click(hitProxyDialogSubmit)
	if state.modal != modalNewHost || len(state.proxies) != 1 {
		t.Fatalf("saving the route: modal=%d routes=%+v error=%q", state.modal, state.proxies, state.proxyDialogError)
	}
	jump, ok := state.hostByID(state.proxies[0].JumpID)
	if !ok || !jump.isSSH() {
		t.Fatalf("the route's jump = %+v, found=%v", jump, ok)
	}
	want := fmt.Sprintf(state.messages.Tools.HostJumpDescriptionFmt, jump.Label)
	if got := state.hostJumpDescription(); got != want {
		t.Fatalf("summary after saving = %q, want %q", got, want)
	}

	canvas, texts := paintAt(state, gfx.Size{Width: 900, Height: 900})
	if via, ok := hitByID(canvas, hitDialogViaSSH); !ok || !via.Disabled {
		t.Fatalf("the ViaSSH switch after the route is %+v, found=%v", via, ok)
	}
	found := false
	for _, text := range texts {
		if text == state.messages.Desktop.ViaSSHRouted {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the ViaSSH row does not say that the route carries the connection")
	}
	before := state.editor.viaSSH
	state.Click(hitDialogViaSSH)
	if state.editor.viaSSH != before {
		t.Fatal("the dimmed ViaSSH switch still toggles")
	}
}

// A route makes a remote desktop its target; the jump itself stays an SSH host,
// since the route has to sign in to it.
func TestJumpRoutePickersOfferDesktopsAsTargetsOnly(t *testing.T) {
	state := newTestState(t, testOptions(t))
	desk := addStreamHost(state, Host{Label: "Desk", Address: "10.0.0.9", User: "alice", Protocol: protocolRDP, Port: 3389})
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatalf("proxy dialog = %d", state.modal)
	}
	if state.proxyEditor.targetID != desk.ID {
		t.Fatalf("target = %d, want the selected desktop %d", state.proxyEditor.targetID, desk.ID)
	}
	clickPageLabel(t, state, gfx.RoleCombobox, state.messages.ProxyFieldTarget)
	canvas, _ := paintedTexts(state)
	if !hasLabel(canvas, desk.Label, gfx.RoleOption) {
		t.Fatalf("the desktop is not offered as a target: %+v", canvas.Hits())
	}
	windowKeyRoutes(state, gfx.KeyEvent{Key: gfx.KeyEscape, Target: pageComboboxID(t, state, state.messages.ProxyFieldTarget)})
	clickPageLabel(t, state, gfx.RoleCombobox, state.messages.ProxyFieldJump)
	canvas, _ = paintedTexts(state)
	if hasLabel(canvas, desk.Label, gfx.RoleOption) {
		t.Fatalf("the desktop is offered as a jump: %+v", canvas.Hits())
	}
}

// A desktop route needs the jump alone: one SSH host is enough when the target
// is not itself an SSH host.
func TestDesktopRouteNeedsOnlyTheJumpHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump := hostByLabel(t, state, "Staging")
	desk := addStreamHost(state, Host{Label: "Desk", Address: "10.0.0.9", User: "alice", Protocol: protocolRDP, Port: 3389})
	state.hosts = []Host{jump, desk}
	state.selectedHostID = desk.ID
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatalf("proxy dialog = %d", state.modal)
	}
	if state.proxyEditor.targetID != desk.ID || state.proxyEditor.jumpID != jump.ID {
		t.Fatalf("preset target=%d jump=%d, want desk %d and jump %d", state.proxyEditor.targetID, state.proxyEditor.jumpID, desk.ID, jump.ID)
	}
	state.Click(hitProxyDialogSubmit)
	if state.modal != modalNone || len(state.proxies) != 1 || state.proxyDialogError != "" {
		t.Fatalf("saving the route: modal=%d routes=%+v error=%q", state.modal, state.proxies, state.proxyDialogError)
	}
}
