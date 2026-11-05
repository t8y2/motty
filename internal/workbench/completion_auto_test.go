package workbench

import (
	"testing"
)

func TestAutoCompletionOpensAfterConfirmedEcho(t *testing.T) {
	for _, line := range []string{"ca", "cat -"} {
		t.Run(line, func(t *testing.T) {
			w := newCompletionWorld(t)
			w.state.completion.autoPopup = true
			w.type_(line)
			if w.state.completion.list.active {
				t.Fatal("automatic list opened before the quiet interval")
			}
			w.tick()
			w.state.Wake()
			if !w.state.completion.list.active || len(w.state.completion.list.candidates) == 0 {
				t.Fatalf("automatic list did not open for %q", line)
			}
		})
	}
}

func TestAutoCompletionRejectsUnsafeOrStalePrompts(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*completionWorld)
	}{
		{"Off", func(w *completionWorld) { w.state.setCompletionMode(completionOff) }},
		{"NoHistory", func(w *completionWorld) { w.session.host.NoHistory = true }},
		{"stale prompt", func(w *completionWorld) { w.session.term.promptVersion++ }},
		{"IME", func(w *completionWorld) { w.session.term.preedit.Composing = true }},
		{"AltScreen", func(w *completionWorld) { w.shell.say("\x1b[?1049h"); w.state.Wake() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newCompletionWorld(t)
			w.state.completion.autoPopup = true
			w.type_("ca")
			test.edit(w)
			w.tick()
			w.state.Wake()
			if w.state.completion.list.active {
				t.Fatal("automatic list opened in an unsafe context")
			}
		})
	}
	for _, line := range []string{"export TOKEN=secret", "sudo -S password"} {
		w := newCompletionWorld(t)
		w.state.completion.autoPopup = true
		w.type_(line)
		w.tick()
		w.state.Wake()
		if w.state.completion.list.active {
			t.Fatalf("automatic list exposed a sensitive line %q", line)
		}
	}
}

func TestAutoCompletionWaitsForEchoInsteadOfTrustingKeystrokes(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.completion.autoPopup = true
	w.shell.onWrite = nil
	w.type_("ca")
	w.tick()
	w.state.Wake()
	if w.state.completion.list.active {
		t.Fatal("automatic list opened without remote echo")
	}
	w.shell.say("ca")
	w.state.Wake()
	if !w.state.completion.list.active {
		t.Fatal("automatic list did not open after matching echo")
	}
}
