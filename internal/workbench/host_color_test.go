package workbench

import (
	"encoding/json"
	"testing"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func TestConnectionColorPersistsCopiesAndUpdatesOpenSessions(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	host := state.hosts[0]
	state.sessions = append(state.sessions, Session{ID: 99, HostID: host.ID, host: host})
	state.openEditHostID(host.ID)
	state.Click(hitHostColors.At(5))
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.hosts[0].Color != "#3b82f6" || state.sessions[0].host.Color != "#3b82f6" {
		t.Fatal("saved color did not reach host and open session")
	}
	restarted := newTestState(t, options)
	if restarted.hosts[0].Color != "#3b82f6" {
		t.Fatal("color lost on restart")
	}
	data, err := json.Marshal(savedHostFromHost(state.hosts[0]))
	if err != nil {
		t.Fatal(err)
	}
	var saved savedHost
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	imported, ok := restarted.hostFromCatalogEntry(saved)
	if !ok || imported.Color != "#3b82f6" {
		t.Fatal("catalog lost color")
	}
	restarted.editor.load(imported)
	if restarted.editor.host(123).Color != "#3b82f6" {
		t.Fatal("copy lost color")
	}
	state.openEditHostID(host.ID)
	state.Click(hitHostColors.At(1))
	state.Click(hitDialogCancel)
	if state.hosts[0].Color != "#3b82f6" {
		t.Fatal("cancel saved color")
	}
	state.openEditHostID(host.ID)
	state.editor.color = *text.NewEditor("#12ABEF")
	state.Click(hitDialogSubmit)
	if state.hosts[0].Color != "#12abef" {
		t.Fatal("custom color not normalized")
	}
	state.openEditHostID(host.ID)
	state.editor.color = *text.NewEditor("#xyz123")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.ErrHostColorInvalid {
		t.Fatal("invalid custom color accepted")
	}
	state.Click(hitHostColors.At(0))
	state.Click(hitDialogSubmit)
	if state.hosts[0].Color != "" || state.sessions[0].host.Color != "" {
		t.Fatal("reset did not clear accent")
	}
}

func TestConnectionColorChoicesFitShortDialog(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	canvas, _ := paintAt(state, ui.Size{Width: 900, Height: 600})
	footer, _ := hitByID(canvas, hitDialogSubmit)
	for i := range hostColors {
		hit, ok := hitByID(canvas, hitHostColors.At(i))
		if !ok || hit.Bounds.Y+hit.Bounds.Height > footer.Bounds.Y || hit.Role != ui.RoleRadio {
			t.Fatal("color choice missing or outside dialog")
		}
	}
	var hit ui.Hit
	for _, h := range canvas.Hits() {
		if h.ID == hostPickerIDs.Trigger && !h.IsLabel {
			hit = h
			break
		}
	}
	if hit.ID.IsZero() || hit.Role != ui.RoleButton || hit.Bounds.Width != 32 || hit.Bounds.Y+hit.Bounds.Height > footer.Bounds.Y {
		t.Fatalf("custom color trigger missing or clipped: %+v", hit)
	}
	for _, hit := range canvas.Hits() {
		if hit.ID == hitDialogColor && hit.Role == ui.RoleTextField {
			t.Fatal("HEX input should only appear inside the picker")
		}
	}
	var name ui.Hit
	for _, h := range canvas.Hits() {
		if h.ID == hitDialogLabel && h.Role == ui.RoleTextField {
			name = h
			break
		}
	}
	if name.Bounds.Width < 290 {
		t.Fatal("color field is taking too much name space", name.Bounds)
	}
}

func TestConnectionHeaderColorPreservesTerminalPalette(t *testing.T) {
	state := newTestState(t, testOptions(t))
	s := Session{host: Host{Color: "#ef4444"}}
	before := state.termPal
	active, line := state.terminalBarColors(&s, false)
	inactive, _ := state.terminalBarColors(&s, true)
	if active == state.pal.workspace || active == inactive || line == state.pal.line {
		t.Fatal("connection color missing from header")
	}
	if state.termPal != before {
		t.Fatal("connection color altered terminal palette")
	}
	s.host.Color = ""
	background, border := state.terminalBarColors(&s, false)
	if background != state.pal.workspace || border != state.pal.line {
		t.Fatal("default header changed")
	}
}
