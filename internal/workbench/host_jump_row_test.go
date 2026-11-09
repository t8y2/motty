package workbench

import (
	"fmt"
	"testing"

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
