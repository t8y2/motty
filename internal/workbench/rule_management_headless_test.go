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
	"motty/internal/autofill"
	"motty/internal/highlight"
	"motty/internal/i18n"
)

func ruleManagerNativeView(t *testing.T, state *State, width, height int, scale float64) *headless.Session {
	t.Helper()
	view, err := headless.Open(app.Config{
		Title: "Motty", Width: width, Height: height, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(context.ID); editor != nil {
				return &app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
			}
			return nil
		},
	}, nil, headless.Options{Scale: scale})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { view.Close() })
	return view
}

func clickRuleNative(t *testing.T, view *headless.Session, id gfx.ID) {
	t.Helper()
	for _, hit := range view.Snapshot().Hits {
		if hit.ID == id && !hit.IsLabel && !hit.Disabled {
			if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no enabled visible control %v", id)
}

func openRuleManagerNative(t *testing.T, state *State, view *headless.Session, id gfx.ID) {
	t.Helper()
	openRuleSettingNative(t, state, view, id)
	if state.modal != modalRuleManager {
		t.Fatal("settings management action did not open its dialog")
	}
}

func openRuleSettingNative(t *testing.T, state *State, view *headless.Session, id gfx.ID) {
	t.Helper()
	clickRuleNative(t, view, hitSettings)
	clickRuleNative(t, view, hitSettingsCategoryList.At(int(settingsTerminal)))
	for attempt := 0; attempt < 60; attempt++ {
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id && !hit.IsLabel {
				clickRuleNative(t, view, id)
				return
			}
		}
		bounds := state.settingsWorkspaceBounds(gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height})
		if err := view.Scroll(gfx.ScrollEvent{X: bounds.X + bounds.Width*0.75, Y: bounds.Y + bounds.Height/2, DeltaY: 160, Precise: true}); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("management action was not reachable through settings scrolling")
}

func TestRuleManagerNativeImportFiltersBeforeOpeningList(t *testing.T) {
	_, options := newWorld(t)
	path := filepath.Join(t.TempDir(), "rules.txt")
	if err := os.WriteFile(path, []byte("ERROR | red\nERROR | red\nWARN | yellow\nwrong | purple\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	view := ruleManagerNativeView(t, state, 1400, 900, 1)
	openRuleSettingNative(t, state, view, hitSettingsHighlightImport)
	if len(state.highlighting.rules) != 2 {
		t.Fatalf("native import kept %d rules, want two valid unique rules", len(state.highlighting.rules))
	}
	clickRuleNative(t, view, hitSettingsHighlightImport)
	if len(state.highlighting.rules) != 2 {
		t.Fatal("native repeat import appended duplicates")
	}
	clickRuleNative(t, view, hitSettingsHighlightManage)
	if state.modal != modalRuleManager {
		t.Fatal("imported list did not open")
	}
	clickRuleNative(t, view, hitRuleManagerAll)
	if len(state.ruleManager.selected) != 2 {
		t.Fatal("native select-all did not include the imported rules")
	}
}

func screenshotRuleManager(t *testing.T, view *headless.Session, name string) {
	t.Helper()
	if directory := os.Getenv("MOTTY_RULE_MANAGEMENT_SCREENSHOTS"); directory != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(directory, name+".png")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuleManagerNativeHighlightScrollEditAndDelete(t *testing.T) {
	for _, layout := range []struct {
		name          string
		width, height int
		scale         float64
		language      i18n.Language
	}{{"desktop", 1400, 900, 1, i18n.Chinese}, {"narrow", 900, 600, 1.5, i18n.English}} {
		t.Run(layout.name, func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			state.setLanguage(layout.language)
			state.setAppearance(appearanceDark)
			for index := range 30 {
				state.highlighting.rules = append(state.highlighting.rules, highlightRule{Pattern: fmt.Sprintf("SERVICE_%02d", index), Slot: highlight.Slots()[index%6], Regex: index%3 == 0, CaseSensitive: index%2 == 0})
			}
			view := ruleManagerNativeView(t, state, layout.width, layout.height, layout.scale)
			openRuleManagerNative(t, state, view, hitSettingsHighlightManage)
			if state.ruleManager.list == nil || state.ruleManager.list.Max() <= 0 {
				t.Fatal("long list has no bounded scroll area")
			}
			clickRuleNative(t, view, hitRuleManagerSelect.At(0))
			bounds := state.ruleManager.bounds
			beforeHover := state.ruleManager.offset
			if err := view.Move(bounds.X+bounds.Width-4, bounds.Y+40); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if state.ruleManager.offset != beforeHover {
				t.Fatal("hovering over the scrollbar changed its offset without a press")
			}
			bar, ok := view.Hit("Vertical scrollbar")
			if !ok {
				t.Fatal("long list has no visible scrollbar while the pointer is over it")
			}
			if err := view.Press(bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+bar.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			if err := view.Move(bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+bar.Bounds.Height/2+100); err != nil {
				t.Fatal(err)
			}
			if err := view.Release(bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+bar.Bounds.Height/2+100); err != nil {
				t.Fatal(err)
			}
			if state.ruleManager.offset <= 0 {
				t.Fatal("dragging the visible scrollbar did not move the list")
			}
			if err := view.Scroll(gfx.ScrollEvent{X: bounds.X + bounds.Width/2, Y: bounds.Y + bounds.Height/2, DeltaY: -10000, Precise: true}); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if layout.name == "desktop" {
				screenshotRuleManager(t, view, "highlight-management")
			}
			if err := view.Scroll(gfx.ScrollEvent{X: bounds.X + bounds.Width/2, Y: bounds.Y + bounds.Height/2, DeltaY: 10000, Precise: true}); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			clickRuleNative(t, view, hitRuleManagerSelect.At(29))
			if !state.ruleManager.selected[0] || !state.ruleManager.selected[29] {
				t.Fatal("scrolling lost off-screen selection")
			}
			clickRuleNative(t, view, hitRuleManagerEdit.At(29))
			clickRuleNative(t, view, hitHighlightPattern)
			if err := view.Keys("cmd+a"); err != nil {
				t.Fatal(err)
			}
			if err := view.Type("DEPLOYMENT_FAILED"); err != nil {
				t.Fatal(err)
			}
			if err := view.Keys("Enter"); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			if state.modal != modalRuleManager || state.highlighting.rules[29].Pattern != "DEPLOYMENT_FAILED" || !state.ruleManager.selected[0] {
				t.Fatal("native keyboard edit did not return to the selected list")
			}
			clickRuleNative(t, view, hitRuleManagerDelete)
			clickRuleNative(t, view, hitRuleDeleteCancel)
			if len(state.highlighting.rules) != 30 || len(state.ruleManager.selected) != 2 {
				t.Fatal("cancel mutated the rules or selection")
			}
			clickRuleNative(t, view, hitRuleManagerDelete)
			if layout.name == "desktop" {
				screenshotRuleManager(t, view, "highlight-delete-confirmation")
			}
			clickRuleNative(t, view, hitRuleDeleteConfirm)
			if state.modal != modalRuleManager || len(state.highlighting.rules) != 28 || state.highlighting.rules[0].Pattern != "SERVICE_01" {
				t.Fatal("native batch confirmation removed the wrong rules")
			}
			clickRuleNative(t, view, hitRuleManagerClose)
			if state.modal != modalNone || len(state.ruleManager.selected) != 0 {
				t.Fatal("closing manager retained its selection")
			}
		})
	}
}

func TestRuleManagerNativeAutofillKeepsSecretsAndArming(t *testing.T) {
	w := liveAutofillSession(t)
	state := w.state
	state.setLanguage(i18n.Chinese)
	state.setAppearance(appearanceDark)
	w.addRule(t, "Deploy token", `^Deploy token:`, "", autofill.SendText, "never-render-this-secret")
	w.addRule(t, "Sudo password", sudoPromptPattern, "", autofill.SendPassword, "")
	view := ruleManagerNativeView(t, state, 1400, 900, 1)
	openRuleManagerNative(t, state, view, hitSettingsAutofillManage)
	for _, value := range view.Texts() {
		if strings.Contains(value, "never-render-this-secret") {
			t.Fatal("manager rendered vault text")
		}
	}
	clickRuleNative(t, view, hitAutofillToggleList.At(1))
	if !state.autofillRules.rules[1].Enabled || !state.autofillRules.armed || w.shell.typed() != "" {
		t.Fatal("enabling a rule sent text or did not arm it")
	}
	clickRuleNative(t, view, hitRuleManagerSelect.At(0))
	screenshotRuleManager(t, view, "autofill-management")
	clickRuleNative(t, view, hitRuleManagerDelete)
	clickRuleNative(t, view, hitRuleDeleteCancel)
	if !state.ruleManager.selected[0] || len(state.autofillRules.rules) != 2 {
		t.Fatal("canceling autofill deletion changed selected rules")
	}
	clickRuleNative(t, view, hitRuleManagerEdit.At(0))
	if state.autofillRules.fixed.String() != "" {
		t.Fatal("editing loaded the secret into the form")
	}
	if err := view.Keys("Escape"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalRuleManager {
		t.Fatal("Escape from editor did not return to manager")
	}
}
