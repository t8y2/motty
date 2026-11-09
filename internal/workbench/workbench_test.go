package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestSelectingAndConnectingHostCreatesTab(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitHostList.At(2))
	openHost(state)

	if state.selectedHostID != 2 {
		t.Fatalf("selected host = %d", state.selectedHostID)
	}
	if len(state.sessions) != 1 {
		t.Fatalf("sessions = %d", len(state.sessions))
	}
	if got := state.sessionTitle(state.activeSessionID); got != "Staging" {
		t.Fatalf("active title = %q", got)
	}
}

func TestSteadyStatePaintDoesNotAllocate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)

	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("steady-state paint allocations = %v", allocations)
	}
}

func BenchmarkPaintWorkspace(b *testing.B) {
	state := newTestState(b, testOptions(b))
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func TestHostDialogValidationAndSubmit(t *testing.T) {
	state := newTestState(t, testOptions(t))

	state.Click(hitAddHost)
	if !(state.modal == modalNewHost) {
		t.Fatal("New Host did not open the dialog")
	}

	// Empty label must not submit.
	state.Click(hitDialogSubmit)
	if !(state.modal == modalNewHost) || state.dialogError == "" {
		t.Fatalf("empty form submitted: open=%v error=%q", (state.modal == modalNewHost), state.dialogError)
	}

	// The same destination under the same name must not submit.
	typeInto(state, hitDialogLabel, "Production")
	typeInto(state, hitDialogAddress, "prod.example.com")
	typeInto(state, hitDialogUser, "deploy")
	state.Click(hitDialogSubmit)
	if !(state.modal == modalNewHost) || state.dialogError != state.messages.ErrAddressTaken {
		t.Fatalf("duplicate host accepted: error=%q", state.dialogError)
	}

	// A valid form appends the host and closes.
	state.editor.label.SetSelection(text.Range{Start: 0, End: len("Production")})
	typeInto(state, hitDialogLabel, "Edge")
	state.editor.address.SetSelection(text.Range{Start: 0, End: len("prod.example.com")})
	typeInto(state, hitDialogAddress, "edge.example.com")
	state.editor.user.SetSelection(text.Range{Start: 0, End: len("deploy")})
	typeInto(state, hitDialogUser, "ops")
	state.Click(hitDialogSubmit)
	if (state.modal == modalNewHost) || len(state.hosts) != 4 {
		t.Fatalf("valid form not submitted: open=%v hosts=%d", (state.modal == modalNewHost), len(state.hosts))
	}
	added := state.hosts[3]
	if added.Label != "Edge" || added.Address != "edge.example.com" || added.User != "ops" || added.Port != 22 {
		t.Fatalf("added host = %+v", added)
	}
	if state.selectedHostID != added.ID {
		t.Fatalf("new host not selected: %d", state.selectedHostID)
	}

	// Cancel discards.
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Temp")
	state.Click(hitDialogCancel)
	if (state.modal == modalNewHost) || len(state.hosts) != 4 {
		t.Fatal("cancel did not discard the form")
	}
}

func TestLanguageSwitchRetargetsCatalog(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if state.messages.ColumnName != "NAME" {
		t.Fatalf("default language = %q", state.messages.ColumnName)
	}
	useChinese(t, state)
	if state.messages.ColumnName != "名称" {
		t.Fatalf("after switch = %q", state.messages.ColumnName)
	}
	// The dialog reads the active catalog too.
	state.Click(hitAddHost)
	state.Click(hitDialogSubmit)
	if state.dialogError != state.messages.ErrLabelRequired {
		t.Fatalf("dialog error not localized: %q", state.dialogError)
	}
}

// typeInto delivers text the way the runtime does: it asks which editor backs
// the focused hit, then applies a native insert event to it.
func typeInto(state *State, hit gfx.ID, value string) {
	app.TextEvent{Kind: app.TextInsert, Text: value}.Apply(state.EditorFor(hit))
}

func TestHostDialogDeclaresInitialFocusAndFieldEditors(t *testing.T) {
	state := newTestState(t, testOptions(t))
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}

	state.Paint(&canvas, size)
	if !canvas.TrappedFocusScope().IsZero() || !canvas.InitialFocus().IsZero() {
		t.Fatal("closed dialog must not trap focus")
	}
	for _, hit := range []gfx.ID{hitDialogLabel, hitDialogAddress, hitDialogUser, hitDialogGroup, hitDialogPort} {
		if state.EditorFor(hit) != nil {
			t.Fatalf("hit %d has an editor while the dialog is closed", hit)
		}
	}

	state.Click(hitAddHost)
	canvas.Reset()
	state.Paint(&canvas, size)
	if canvas.TrappedFocusScope() != dialogScope || canvas.InitialFocus() != hitDialogLabel {
		t.Fatalf("dialog scope=%d initial focus=%d, want %d/%d",
			canvas.TrappedFocusScope(), canvas.InitialFocus(), dialogScope, hitDialogLabel)
	}
	seen := map[*text.Editor]bool{}
	for _, hit := range []gfx.ID{hitDialogLabel, hitDialogAddress, hitDialogUser, hitDialogGroup, hitDialogPort} {
		editor := state.EditorFor(hit)
		if editor == nil || seen[editor] {
			t.Fatalf("hit %d editor = %p (nil or shared)", hit, editor)
		}
		seen[editor] = true
	}
	if state.EditorFor(hitConnect) != nil {
		t.Fatal("a non-field hit must not resolve to an editor")
	}
}
