//go:build darwin || linux || windows

package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/ai/store"
	"motty/internal/i18n"
	"motty/internal/localshell"
)

func TestCLILoginNativeInputAndFormRendering(t *testing.T) {
	w := newSettingsWorld(t, newLister("default"))
	s := w.state
	s.setLanguage(i18n.Chinese)
	var invalidator app.Invalidator
	s.options.Invalidate = invalidator.Invalidate
	var field app.TextInput
	view, err := headless.Open(app.Config{Title: "Motty CLI settings", Width: 1280, Height: 900, Tree: Tree(), Invalidator: &invalidator, OnWake: s.Wake, Paint: s.Paint, OnPointer: s.Pointer, OnKey: s.Key, OnShortcut: s.Shortcut, OnScroll: s.Scroll, OnResize: s.Resize, TextInput: func(c app.TextInputContext) *app.TextInput {
		if input := s.TerminalInput(c); input != nil {
			return input
		}
		if editor := s.EditorFor(c.ID); editor != nil {
			field = app.TextInput{Editor: editor}
			return &field
		}
		return nil
	}}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	clickID := func(id ui.ID) {
		t.Helper()
		for _, h := range view.Snapshot().Hits {
			if h.ID == id && !h.Block {
				if err := view.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatal("missing clickable control", id)
	}
	clickID(hitAISetProviderAdd)
	for i, p := range aiPresets {
		if p.kind == store.KindCodexCLI {
			s.aiChoosePreset(i)
			break
		}
	}
	invalidator.Invalidate()
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	clickID(hitAISetFormName)
	if err := view.Keys("cmd+a"); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("Codex CLI"); err != nil {
		t.Fatal(err)
	}
	clickID(hitAISetFormCLIPath)
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("MOTTY_CLI_SCREENSHOT_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, "cli-provider-form.png")); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
			t.Fatal(err)
		}
	}
	clickID(hitAISetFormSubmit)
	s.cancelAIFetch()
	if len(s.ai.providers) != 1 {
		t.Fatal("native form did not save")
	}
	p := s.ai.providers[0]
	path, _ := os.Executable()
	p.CLIPath = path
	db, _ := s.aiStore()
	if err := db.SaveProvider(p); err != nil {
		t.Fatal(err)
	}
	s.reloadAI()
	auth := &fakeShell{done: make(chan struct{})}
	s.options.CLIAuthStart = func(localshell.Config) (Shell, error) { return auth, nil }
	invalidator.Invalidate()
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	clickID(hitAICLILogin)
	if err := view.WaitFor(func(app.Snapshot) bool { s.Wake(); return s.ai.login != nil && s.ai.login.term.shell != nil }, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	invalidator.Invalidate()
	if err := view.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	clickID(hitAICLILoginTerminal)
	if dir := os.Getenv("MOTTY_CLI_SCREENSHOT_DIR"); dir != "" {
		if err := view.Screenshot(filepath.Join(dir, "cli-login-terminal.png")); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
			t.Fatal(err)
		}
	}
	if err := view.Type("fixture-code"); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if auth.typed() != "fixture-code\r" {
		t.Fatal("native login input did not reach its PTY")
	}
	if err := view.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	if s.ai.login != nil || !auth.closed {
		t.Fatal("native Escape did not cancel login")
	}
}
