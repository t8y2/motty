package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// The jump-route pickers narrow by address and account, not only by a label
// the user may not remember.
func TestJumpRouteHostPickersFilterAndChoose(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitProxies)
	state.Click(hitProxyAdd)
	if state.modal != modalNewProxy {
		t.Fatalf("proxy dialog = %d", state.modal)
	}
	state.Click(hitProxyDialogTarget)
	state.proxyEditor.targetSelect.Query.Insert("192.168")
	choices := state.proxyEditor.targetSelect.Choices()
	if len(choices) != 1 || choices[0].Label != "Router" {
		t.Fatalf("address filter = %+v, want only Router", choices)
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitProxyDialogTarget}); !result.Handled {
		t.Fatalf("Enter was not taken: %+v", result)
	}
	router := hostByLabel(t, state, "Router")
	if state.proxyEditor.targetID != router.ID {
		t.Fatalf("chosen target = %d, want Router %d", state.proxyEditor.targetID, router.ID)
	}

	state.Click(hitProxyDialogJump)
	state.proxyEditor.jumpSelect.Query.Insert("deploy")
	choices = state.proxyEditor.jumpSelect.Choices()
	var staging, production bool
	for _, choice := range choices {
		switch choice.Label {
		case "Staging":
			staging = true
		case "Production":
			production = true
		}
	}
	if !staging || !production || len(choices) != 2 {
		t.Fatalf("account filter = %+v, want the deploy hosts", choices)
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
	state.Click(hitProxyDialogTarget)
	targets := state.proxyEditor.targetSelect.Choices()
	if choice, ok := choiceByLabel(targets, desk.Label); !ok || choice.Disabled {
		t.Fatalf("the desktop target = %+v, found=%v", choice, ok)
	}
	state.Click(hitProxyDialogJump)
	jumps := state.proxyEditor.jumpSelect.Choices()
	if choice, ok := choiceByLabel(jumps, desk.Label); !ok || !choice.Disabled {
		t.Fatalf("the desktop jump = %+v, found=%v", choice, ok)
	}
}

func choiceByLabel(choices []components.SingleSelectChoice, label string) (components.SingleSelectChoice, bool) {
	for _, choice := range choices {
		if choice.Label == label {
			return choice, true
		}
	}
	return components.SingleSelectChoice{}, false
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
