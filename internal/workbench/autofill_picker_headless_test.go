//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/sshconn"
)

// headlessAutofillWorld is liveAutofillSession with the window's invalidator
// wired to the headless driver, so state changes made between commands repaint.
func headlessAutofillWorld(t *testing.T) (*autofillWorld, *app.Invalidator) {
	t.Helper()
	w := &autofillWorld{}
	world, options := newWorld(t)
	var invalidator app.Invalidator
	options.Invalidate = invalidator.Invalidate
	options.Now = func() time.Duration { return w.clock }
	w.path = options.StatePath
	w.state = newTestState(t, options)
	fake := connect(t, w.state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	w.state.Wake()
	w.shell = fake.shell(t)
	w.shell.say("\x1b[0m")
	w.state.Wake()
	return w, &invalidator
}

// headlessAutofillView opens a window over the world's state.
func headlessAutofillView(t *testing.T, state *State, invalidator *app.Invalidator) *headless.Session {
	t.Helper()
	view, err := headless.Open(app.Config{
		Title: "password prompt assist", Width: 1100, Height: 720, Tree: Tree(),
		Invalidator: invalidator, Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: state.TerminalInput,
	}, nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(view.Close)
	return view
}

// pickerRowsInView checks that the painted offer is reachable and inside the
// window, which a list opened at the bottom line must still satisfy after it
// flips above the prompt.
func pickerRowsInView(t *testing.T, view *headless.Session) {
	t.Helper()
	snapshot := view.Snapshot()
	for _, id := range []gfx.ID{hitAutofillPickItem.At(0), hitAutofillPickItem.At(1), hitAutofillPickItem.At(2)} {
		hit, ok := hitByIDSnapshot(snapshot.Hits, id)
		if !ok {
			t.Fatalf("row %s is not painted", tree.Text(id))
		}
		if hit.Bounds.X < 0 || hit.Bounds.Y < 0 ||
			hit.Bounds.X+hit.Bounds.Width > float32(snapshot.Width) ||
			hit.Bounds.Y+hit.Bounds.Height > float32(snapshot.Height) {
			t.Fatalf("row %s escapes the window: %+v", tree.Text(id), hit.Bounds)
		}
	}
}

// TestPasswordPromptOfferNative drives the offer through a real headless
// window: the prompt opens it without any rule, the rows are where the pointer
// can reach them, and only the chosen password reaches the shell. Set
// MOTTY_PASSWORD_PROMPT_SCREENSHOTS to a directory to keep the pictures.
func TestPasswordPromptOfferNative(t *testing.T) {
	w, invalidator := headlessAutofillWorld(t)
	state := w.state
	state.setLanguage(i18n.Chinese)
	state.setAppearance(appearanceDark)
	session := &state.sessions[0]
	addCredential(t, state, "root@cloudpivot", "root-secret")
	addCredential(t, state, "deploy@web1", "deploy-secret")
	addCredential(t, state, "db-admin", "db-secret")
	if err := state.secrets.vault.Set(state.passwordName(session), "host-secret"); err != nil {
		t.Fatal(err)
	}
	state.activeSection = sectionTerminal

	view := headlessAutofillView(t, state, invalidator)
	w.output("Password: ")
	if len(state.autofillRules.pick.candidates) != 4 {
		t.Fatalf("the prompt did not offer every saved password: %+v", state.autofillRules.pick.candidates)
	}
	// The state changed outside an input event: advance once so the window's
	// invalidator turns it into a frame.
	if err := view.Wait(20 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	pickerRowsInView(t, view)

	if dir := os.Getenv("MOTTY_PASSWORD_PROMPT_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, "password-offer.png")); err != nil {
			t.Fatal(err)
		}
		if err := view.Keys("down"); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, "password-offer-selected.png")); err != nil {
			t.Fatal(err)
		}
	} else if err := view.Keys("down"); err != nil {
		t.Fatal(err)
	}
	if state.autofillRules.pick.selected != 0 {
		t.Fatalf("Down chose row %d", state.autofillRules.pick.selected)
	}
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	if got := w.shell.typed(); got != "host-secret\r" {
		t.Fatalf("typed %q, want the host password first", got)
	}
	if len(state.autofillRules.pick.candidates) != 0 {
		t.Fatal("the offer stayed open after Enter")
	}
}

// TestPasswordPromptOfferFlipsAtTheBottom puts the prompt on the last line,
// where su's prompt is in practice. The list must flip above it and stay
// entirely inside the window.
func TestPasswordPromptOfferFlipsAtTheBottom(t *testing.T) {
	w, invalidator := headlessAutofillWorld(t)
	state := w.state
	addCredential(t, state, "root@cloudpivot", "root-secret")
	addCredential(t, state, "deploy@web1", "deploy-secret")
	addCredential(t, state, "db-admin", "db-secret")
	state.activeSection = sectionTerminal
	view := headlessAutofillView(t, state, invalidator)

	w.output(strings.Repeat("\r\n", 40))
	w.output("Password: ")
	if len(state.autofillRules.pick.candidates) == 0 {
		t.Fatal("the prompt did not offer the saved passwords")
	}
	if err := view.Wait(20 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	pickerRowsInView(t, view)

	session := &state.sessions[0]
	geometry := session.term.paintGeometry
	cursor := session.term.screen.Cursor()
	promptTop := geometry.top + float32(cursor.Y)*state.cellHeight
	first, ok := hitByIDSnapshot(view.Snapshot().Hits, hitAutofillPickItem.At(0))
	if !ok {
		t.Fatal("the first saved password is not painted")
	}
	if first.Bounds.Y >= promptTop {
		t.Fatalf("the list did not flip above the bottom prompt: row %v, prompt top %v", first.Bounds.Y, promptTop)
	}
}
