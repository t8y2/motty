//go:build darwin || windows || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

// savePasteScreenshot writes a PNG when MOTTY_PASTE_SCREENSHOTS names a folder.
// The dialogs are hardest to judge from hit geometry alone; this is the same
// opt-in hook the terminal-input confirmation uses for its native review.
func savePasteScreenshot(t *testing.T, preview *headless.Session, name string) {
	t.Helper()
	dir := os.Getenv("MOTTY_PASTE_SCREENSHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := preview.Screenshot(filepath.Join(dir, name+".png")); err != nil {
		t.Fatal(err)
	}
}

func TestPasteReviewHeadlessDialogOpensAndSends(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.setLanguage(i18n.Chinese)
	state.options.ClipboardText = func() (string, error) { return "one\ntwo", nil }
	var invalidator app.Invalidator
	var nativeInput app.TextInput
	preview, err := headless.Open(app.Config{Title: "Paste review", Width: 900, Height: 700, Tree: Tree(), Paint: state.Paint,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return state.TerminalInput(context)
			}
			editor := state.EditorFor(context.ID)
			if editor == nil {
				return nil
			}
			nativeInput = app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
			return &nativeInput
		},
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize, Invalidator: &invalidator}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()

	state.pasteIntoTerminal(&state.sessions[0])
	if err := preview.Frame(); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalPasteReview {
		t.Fatalf("multi-line paste opened modal %v", state.modal)
	}
	if got := preview.Snapshot().Focused; got != hitPasteReviewEditor {
		t.Fatalf("initial focus = %v, want %v", got, hitPasteReviewEditor)
	}
	found := map[gfx.ID]bool{}
	for _, hit := range preview.Snapshot().Hits {
		switch hit.ID {
		case hitPasteReviewEditor, hitPasteReviewWhole, hitPasteReviewLines, hitPasteReviewCancel:
			found[hit.ID] = true
			r := hit.Bounds
			if r.X < 0 || r.Y < 0 || r.X+r.Width > 900 || r.Y+r.Height > 700 {
				t.Fatalf("hit %v outside the window: %+v", hit.ID, r)
			}
		}
	}
	for _, id := range []gfx.ID{hitPasteReviewEditor, hitPasteReviewWhole, hitPasteReviewLines, hitPasteReviewCancel} {
		if !found[id] {
			t.Fatalf("the dialog did not paint %v", id)
		}
	}
	if shell.typed() != "" {
		t.Fatalf("the preview sent before a choice: %q", shell.typed())
	}
	for _, look := range []struct {
		name       string
		appearance appearance
	}{{"light", appearanceLight}, {"dark", appearanceDark}} {
		state.setAppearance(look.appearance)
		if err := preview.Frame(); err != nil {
			t.Fatal(err)
		}
		if err := preview.Rest(); err != nil {
			t.Fatal(err)
		}
		savePasteScreenshot(t, preview, "paste-review-dialog-"+look.name)
	}
	if err := preview.Type("!"); err != nil {
		t.Fatal(err)
	}
	if err := preview.ClickLabel(state.messages.PasteReview.Whole); err != nil {
		t.Fatal(err)
	}
	if got := shell.typed(); got != "one\rtwo!" {
		t.Fatalf("whole paste sent %q", got)
	}
	if state.modal != modalNone {
		t.Fatalf("the review stayed open after sending: %v", state.modal)
	}

	state.pasteIntoTerminal(&state.sessions[0])
	if err := preview.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := preview.ClickLabel(state.messages.PasteReview.Lines); err != nil {
		t.Fatal(err)
	}
	if got := shell.typed(); got != "one\rtwo!one\r" {
		t.Fatalf("line-by-line paste sent %q", got)
	}
	if !state.sessions[0].term.pasteQueue.active {
		t.Fatal("the remaining line was not queued")
	}
}

// TestPasteReviewSettingsHeadlessScreenshot captures the terminal setting that
// turns the confirmation on and off. It only runs when the screenshot folder is
// requested, so ordinary test runs do not pay for the page walk.
func TestPasteReviewSettingsHeadlessScreenshot(t *testing.T) {
	if os.Getenv("MOTTY_PASTE_SCREENSHOTS") == "" {
		t.Skip("set MOTTY_PASTE_SCREENSHOTS to capture settings screenshots")
	}
	state := newTestState(t, testOptions(t))
	// Opening Terminal settings normally discovers Windows shells and starts a
	// WSL listing goroutine; this screenshot does not need either.
	state.localShellMenu.loaded = true
	state.setLanguage(i18n.Chinese)
	var invalidator app.Invalidator
	var nativeInput app.TextInput
	preview, err := headless.Open(app.Config{Title: "Settings", Width: 900, Height: 700, Tree: Tree(), Paint: state.Paint,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			editor := state.EditorFor(context.ID)
			if editor == nil {
				return nil
			}
			nativeInput = app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
			return &nativeInput
		},
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize, Invalidator: &invalidator}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	if err := preview.Frame(); err != nil {
		t.Fatal(err)
	}
	found := false
	for offset := float32(0); offset <= state.settingsContent && !found; offset += 160 {
		state.settingsScroll = offset
		if err := preview.Frame(); err != nil {
			t.Fatal(err)
		}
		if err := preview.Rest(); err != nil {
			t.Fatal(err)
		}
		for _, hit := range preview.Snapshot().Hits {
			if hit.ID == hitSettingsPasteConfirmToggle {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("the multi-line paste setting was never painted")
	}
	for _, look := range []struct {
		name       string
		appearance appearance
	}{{"light", appearanceLight}, {"dark", appearanceDark}} {
		state.setAppearance(look.appearance)
		if err := preview.Frame(); err != nil {
			t.Fatal(err)
		}
		if err := preview.Rest(); err != nil {
			t.Fatal(err)
		}
		savePasteScreenshot(t, preview, "paste-review-settings-"+look.name)
	}
}
