//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestTerminalInputGuardNativeChoiceAndLocaleNotice(t *testing.T) {
	for _, variant := range []struct {
		language i18n.Language
		look     appearance
		name     string
	}{
		{i18n.English, appearanceLight, "en-light"}, {i18n.Chinese, appearanceLight, "zh-light"},
		{i18n.English, appearanceDark, "en-dark"}, {i18n.Chinese, appearanceDark, "zh-dark"},
	} {
		t.Run(variant.name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			state.setLanguage(variant.language)
			state.setAppearance(variant.look)
			owner := &state.sessions[0]
			inputGuardPrompt(state, owner, shell, "utf8", "")
			var invalidator app.Invalidator
			preview, err := headless.Open(app.Config{Title: "Input compatibility", Width: 840, Height: 600, Tree: Tree(), Paint: state.Paint,
				TextInput: state.TerminalInput, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize, Invalidator: &invalidator}, nil, headless.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			if err := preview.ClickLabel(fmt.Sprintf(state.messages.LabelTerminalFmt, owner.Address)); err != nil {
				t.Fatal(err)
			}
			if err := preview.Type("tail "); err != nil {
				t.Fatal(err)
			}
			if err := preview.Type("\u2014500f"); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalTerminalInput || shell.typed() != "tail " {
				t.Fatal("native suspicious input bypassed confirmation")
			}
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			if preview.Snapshot().Focused != hitInputCancel {
				t.Fatalf("unsafe initial confirmation focus: %v", preview.Snapshot().Focused)
			}
			if output := os.Getenv("MOTTY_INPUT_SCREENSHOTS"); output != "" {
				os.MkdirAll(output, 0700)
				if err := preview.Screenshot(filepath.Join(output, "input-confirm-"+variant.name+".png")); err != nil {
					t.Fatal(err)
				}
			}
			if err := preview.ClickLabel(state.messages.TerminalInput.Replace); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalNone || shell.typed() != "tail -500f" {
				t.Fatalf("native choice executed or lost text: %q", shell.typed())
			}
			// A new empty prompt cannot discard the edited command on its own.
			state.typed(owner, []byte{3})
			inputGuardPrompt(state, owner, shell, "other", "C.UTF-8")
			invalidator.Invalidate()
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, hit := range preview.Snapshot().Hits {
				found = found || strings.Contains(hit.Hint, state.messages.TerminalInput.LocaleWarning)
			}
			if !found {
				t.Fatal("locale notice lost its accessible complete hint")
			}
			if output := os.Getenv("MOTTY_INPUT_SCREENSHOTS"); output != "" {
				if err := preview.Screenshot(filepath.Join(output, "input-locale-"+variant.name+".png")); err != nil {
					t.Fatal(err)
				}
			}
			if err := preview.ClickLabel(state.messages.TerminalInput.LocaleFix); err != nil {
				t.Fatal(err)
			}
			preview.Resize(480, 600)
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			if output := os.Getenv("MOTTY_INPUT_SCREENSHOTS"); output != "" {
				if err := preview.Screenshot(filepath.Join(output, "input-repair-narrow-"+variant.name+".png")); err != nil {
					t.Fatal(err)
				}
			}
			before := shell.typed()
			if err := preview.Driver().Key(gfx.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalNone || shell.typed() != before {
				t.Fatal("Escape executed locale repair")
			}
		})
	}
}

func TestTerminalInputGuardNativeSpaceActivatesFocusedChoice(t *testing.T) {
	for _, choice := range []struct {
		name   string
		tabs   int
		target gfx.ID
		want   string
	}{
		{"cancel", 0, hitInputCancel, ""}, {"keep", 1, hitInputKeep, "\u2014"}, {"replace", 2, hitInputReplace, "-"},
	} {
		t.Run(choice.name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			owner := &state.sessions[0]
			inputGuardPrompt(state, owner, shell, "utf8", "")
			state.terminalText(owner.ID, app.TextEvent{Kind: app.TextInsert, Text: "\u2014"})
			driver := paneDriver(t, state, gfx.Size{Width: 840, Height: 600})
			if err := driver.Pump(); err != nil {
				t.Fatal(err)
			}
			if driver.Snapshot().Focused != hitInputCancel {
				t.Fatal("initial choice was not cancel")
			}
			for range choice.tabs {
				if err := driver.Key(gfx.KeyTab, 0); err != nil {
					t.Fatal(err)
				}
			}
			if driver.Snapshot().Focused != choice.target {
				t.Fatal("Tab did not reach explicit replacement")
			}
			if err := driver.Key(gfx.KeySpace, 0); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalNone || shell.typed() != choice.want {
				t.Fatalf("Space did not activate focused choice: modal=%v input=%q", state.modal, shell.typed())
			}
		})
	}
}
