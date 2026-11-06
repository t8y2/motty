//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
)

func TestHeadlessCompletionPreviewKeysRestoreWithoutExecution(t *testing.T) {
	w := newCompletionPreviewWorld(t, true)
	var invalidator app.Invalidator
	view, err := headless.Open(app.Config{
		Title: "command preview", Width: 900, Height: 600, Tree: Tree(),
		Paint: w.state.Paint, TextInput: w.state.TerminalInput,
		OnPointer: w.state.Pointer, OnKey: w.state.Key,
		OnResize: w.state.Resize, OnWake: w.state.Wake, Invalidator: &invalidator,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	pumpEcho := func() {
		t.Helper()
		w.state.Wake()
		invalidator.Invalidate()
		if err := view.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
	}
	if err := view.Click(30, 55); err != nil {
		t.Fatal(err)
	}
	w.state.openCompletions()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	before := w.shell.typed()
	if err := view.Keys("down"); err != nil {
		t.Fatal(err)
	}
	if got := w.shell.typed(); got != before+"\x15cat -e" {
		t.Fatalf("driver did not preview through its keyboard route: %q", got)
	}
	pumpEcho()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if got := w.line(t); got != "cat -e" || !strings.Contains(strings.Join(view.Texts(), " "), "cat -e") {
		t.Fatalf("headless frame is stale after preview: line=%q texts=%q", got, view.Texts())
	}
	if directory := os.Getenv("MOTTY_COMPLETION_SCREENSHOTS"); directory != "" {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(directory, "preview-selected.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err := view.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	if got := w.shell.typed(); got != before+"\x15cat -e\x15cat -" {
		t.Fatalf("driver Escape did not restore the original line: %q", got)
	}
	pumpEcho()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if got := w.line(t); got != "cat -" || w.state.completion.list.active {
		t.Fatalf("driver restored %q, list active=%v", got, w.state.completion.list.active)
	}
	w.state.openCompletions()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("down"); err != nil {
		t.Fatal(err)
	}
	pumpEcho()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	before = w.shell.typed()
	if err := view.Keys("enter"); err != nil {
		t.Fatal(err)
	}
	if got := w.shell.typed(); got != before || strings.ContainsAny(got, "\r\n") {
		t.Fatalf("driver first Enter executed the suggestion: %q", got)
	}
}
