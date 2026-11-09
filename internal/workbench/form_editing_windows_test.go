package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// Use the same generic text provider as the desktop entry point. No field-specific
// shortcut handlers: host, settings, and secure fields must all work in Viva.
func formEditingDriver(t *testing.T, state *State) *app.Driver {
	t.Helper()
	var input app.TextInput
	driver, err := app.NewDriver(app.Config{
		Title: "form editing", Width: 1400, Height: 1100, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return state.TerminalInput(context)
			}
			if special := state.TerminalFindInput(context); special != nil {
				return special
			}
			if special := state.AIPanelInput(context); special != nil {
				return special
			}
			editor := state.EditorFor(context.ID)
			if editor == nil {
				return nil
			}
			input = app.TextInput{Editor: editor}
			return &input
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver
}

func focusFormField(t *testing.T, driver *app.Driver, id gfx.ID) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID != id || hit.IsLabel || (hit.Role != gfx.RoleTextField && hit.Role != gfx.RoleCombobox) {
			continue
		}
		if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
		if driver.Snapshot().Focused != id {
			t.Fatalf("field %s did not receive focus", tree.Text(id))
		}
		return
	}
	t.Fatalf("field %s was not painted", tree.Text(id))
}

func formChord(t *testing.T, driver *app.Driver, mods gfx.Modifiers, key rune) {
	t.Helper()
	if handled, err := driver.ShortcutChord(mods, key); err != nil || !handled {
		t.Fatalf("%c handled=%v err=%v", key, handled, err)
	}
}

func TestWindowsHostFormsUseFrameworkEditing(t *testing.T) {
	for _, mode := range []string{"new", "edit", "copy"} {
		t.Run(mode, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			switch mode {
			case "new":
				state.Click(hitAddHost)
			case "edit":
				state.openEditHostID(state.hosts[0].ID)
			case "copy":
				state.openCopyHostID(state.hosts[0].ID)
			}
			state.editor.advancedOpen, state.editor.algoMode = true, algoCustom
			driver := formEditingDriver(t, state)
			for _, id := range []gfx.ID{hitDialogLabel, hitDialogAddress, hitDialogUser, hitDialogPort, hitDialogGroup, hitDialogTags, hitDialogAlgorithms, hitDialogOTP} {
				focusFormField(t, driver, id)
				editor := state.EditorFor(id)
				formChord(t, driver, gfx.ModifierControl, 'a')
				if err := driver.Type("前旧值后"); err != nil {
					t.Fatal(err)
				}
				if err := driver.SelectText(1, 3); err != nil {
					t.Fatal(err)
				}
				driver.SetClipboard("中文主机")
				formChord(t, driver, gfx.ModifierControl, 'v')
				if editor.String() != "前中文主机后" {
					t.Fatalf("%s paste=%q", tree.Text(id), editor.String())
				}
				formChord(t, driver, gfx.ModifierControl, 'z')
				if editor.String() != "前旧值后" {
					t.Fatal("undo did not restore selected text")
				}
				formChord(t, driver, gfx.ModifierControl, 'y')
				formChord(t, driver, gfx.ModifierControl, 'a')
				formChord(t, driver, gfx.ModifierControl, 'c')
				if driver.Clipboard() != "前中文主机后" {
					t.Fatal("copy lost Unicode selection")
				}
				formChord(t, driver, gfx.ModifierControl, 'x')
				if editor.String() != "" {
					t.Fatal("cut did not remove selection")
				}
				formChord(t, driver, gfx.ModifierControl, 'z')
				if editor.String() != "前中文主机后" {
					t.Fatal("cut was not undoable")
				}
				// Searchable selectors own their popup until it is dismissed;
				// leave it before testing editing in the next independent field.
				if id == hitDialogGroup || id == hitDialogTags {
					if err := driver.Key(gfx.KeyEscape, 0); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestWindowsProviderFormUsesFrameworkClipboard(t *testing.T) {
	world := newSettingsWorld(t, newLister())
	state := world.state
	state.Click(hitAISetProviderAdd)
	driver := formEditingDriver(t, state)
	for _, tc := range []struct {
		id    gfx.ID
		value string
	}{
		{hitAISetFormName, "测试服务商"}, {hitAISetFormURL, "https://example.com/v1"}, {hitAISetFormModelsURL, "https://example.com/models/v1"}, {hitAISetFormKey, "sk-test-key"},
	} {
		focusFormField(t, driver, tc.id)
		formChord(t, driver, gfx.ModifierControl, 'a')
		driver.SetClipboard(tc.value)
		formChord(t, driver, gfx.ModifierControl, 'v')
		if got := state.EditorFor(tc.id).String(); got != tc.value {
			t.Fatalf("provider field %s=%q", tree.Text(tc.id), got)
		}
		formChord(t, driver, gfx.ModifierControl, 'a')
		driver.SetClipboard("previous clipboard")
		formChord(t, driver, gfx.ModifierControl, 'c')
		if tc.id == hitAISetFormKey {
			formChord(t, driver, gfx.ModifierControl, 'x')
			if driver.Clipboard() != "previous clipboard" || state.EditorFor(tc.id).String() != tc.value {
				t.Fatal("secure field exported or cut its key")
			}
		} else if driver.Clipboard() != tc.value {
			t.Fatal("provider copy failed")
		}
	}
}

func TestWindowsFormNavigationSingleLineAndFocus(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	driver := formEditingDriver(t, state)
	focusFormField(t, driver, hitDialogAddress)
	driver.SetClipboard("\r\nhost.example.com\r\n")
	formChord(t, driver, gfx.ModifierControl, 'v')
	editor := state.EditorFor(hitDialogAddress)
	if editor.String() != "host.example.com" {
		t.Fatalf("single line=%q", editor.String())
	}
	for _, tc := range []struct {
		key   gfx.Key
		mods  gfx.Modifiers
		caret int
	}{
		{gfx.KeyHome, 0, 0}, {gfx.KeyEnd, gfx.ModifierShift, 16},
		{gfx.KeyEnd, 0, 16}, {gfx.KeyLeft, gfx.ModifierControl, 13},
		{gfx.KeyLeft, gfx.ModifierControl | gfx.ModifierShift, 12},
	} {
		if err := driver.Key(tc.key, tc.mods); err != nil {
			t.Fatal(err)
		}
		if editor.Caret() != tc.caret {
			t.Fatalf("key %v caret=%d", tc.key, editor.Caret())
		}
	}
	formChord(t, driver, gfx.ModifierControl, 'a')
	driver.SetClipboard("")
	formChord(t, driver, gfx.ModifierControl, 'v')
	if editor.String() != "host.example.com" {
		t.Fatal("empty clipboard deleted selection")
	}
	initial := driver.Snapshot().Focused
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused == initial {
		t.Fatal("Tab did not move focus")
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if driver.Snapshot().Focused != initial {
		t.Fatal("Shift+Tab did not return focus")
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone {
		t.Fatal("Escape did not cancel the form")
	}
}

func TestWindowsFormPasteDoesNotReachActiveTerminal(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAssistant)
	state.Click(hitAISetProviderAdd)
	driver := formEditingDriver(t, state)
	focusFormField(t, driver, hitAISetFormName)
	driver.SetClipboard("provider name")
	formChord(t, driver, gfx.ModifierControl, 'v')
	if shell.typed() != "" || state.ai.cfg.name.String() != "provider name" {
		t.Fatalf("terminal=%q field=%q", shell.typed(), state.ai.cfg.name.String())
	}
}
