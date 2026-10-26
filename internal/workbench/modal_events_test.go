package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func modalEventDriver(t *testing.T, state *State) *app.Driver {
	t.Helper()
	driver, err := app.NewDriver(app.Config{
		Title: "modal events", Width: 1400, Height: 700, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver
}

func TestAIRenameEscapeKeepsHistoryOpen(t *testing.T) {
	state := newTestState(t, testOptions(t))
	db, err := state.aiStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateConversation("original", ""); err != nil {
		t.Fatal(err)
	}
	state.Click(hitAIToggle)
	state.Click(hitAIHistory)
	state.Click(hitAIHistoryRename.At(0))
	driver := modalEventDriver(t, state)
	if err := driver.Key(ui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone || !state.ai.panel.history {
		t.Fatalf("Escape should close only the dialog: modal=%v history=%v", state.modal, state.ai.panel.history)
	}
}

func TestDialogsBlockBackgroundScroll(t *testing.T) {
	for _, name := range []string{"ai-rename", "host", "forward", "proxy", "form", "ai-provider", "ai-clear", "known-hosts"} {
		t.Run(name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			db, err := state.aiStore()
			if err != nil {
				t.Fatal(err)
			}
			for i := range 30 {
				if _, err := db.CreateConversation(fmt.Sprintf("conversation %02d", i), ""); err != nil {
					t.Fatal(err)
				}
			}
			state.Click(hitAIToggle)
			state.Click(hitAIHistory)
			var closeID ui.ID
			switch name {
			case "ai-rename":
				state.Click(hitAIHistoryRename.At(0))
				closeID = hitAIRenameClose
			case "host":
				state.Click(hitAddHost)
				closeID = hitDialogClose
			case "forward":
				state.openForwardDialog()
				closeID = hitForwardDialogClose
			case "proxy":
				state.openProxyDialog()
				closeID = hitProxyDialogCancel
			case "form":
				state.openForm(formDialog{title: "Test form", submit: "Save"}, formField{label: "Name"})
				closeID = hitFormCancel
			case "ai-provider", "ai-clear":
				state.Click(hitSettings)
				state.selectSettingsCategory(settingsAssistant)
				if name == "ai-provider" {
					state.Click(hitAISetProviderAdd)
					closeID = hitAISetFormClose
				} else {
					state.Click(hitAISetClearHistory)
					closeID = hitAISetConfirmCancel
				}
			case "known-hosts":
				writeKnownHostsFixture(t, state.options.KnownHostsPath)
				state.Click(hitKnownHosts)
				state.Click(hitKnownHostCheckList.At(0))
				state.Click(hitKnownHostsDelete)
				closeID = hitKnownHostsDialogCancel
			}
			driver := modalEventDriver(t, state)
			panel := &state.ai.panel
			if panel.maxTop <= 0 || panel.list.Height <= 0 {
				t.Fatal("fixture needs a scrollable AI history panel")
			}
			top, settingsTop := panel.historyTop, state.settingsScroll
			if err := driver.Scroll(ui.ScrollEvent{
				X: panel.list.X + panel.list.Width/2, Y: panel.list.Y + panel.list.Height/2,
				DeltaY: 120, Precise: true,
			}); err != nil {
				t.Fatal(err)
			}
			if panel.historyTop != top {
				t.Errorf("wheel scrolled AI history behind the dialog: %v -> %v", top, panel.historyTop)
			}
			if state.activeSection == sectionSettings {
				if state.settingsContent <= state.viewport.Height {
					t.Fatal("fixture needs a scrollable settings page")
				}
				if err := driver.Scroll(ui.ScrollEvent{X: 300, Y: 400, DeltaY: 120, Precise: true}); err != nil {
					t.Fatal(err)
				}
				if state.settingsScroll != settingsTop {
					t.Errorf("wheel scrolled settings behind the dialog: %v -> %v", settingsTop, state.settingsScroll)
				}
			}
			var clicked bool
			for _, hit := range driver.Snapshot().Hits {
				if hit.ID == closeID && !hit.IsLabel {
					if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
					clicked = true
					break
				}
			}
			if !clicked || state.modal != modalNone || state.ai.cfg.dialog != aiDialogNone || state.knownHosts.action != knownHostsActionNone {
				t.Fatal("the dialog's close/cancel button did not work")
			}
			// The same wheel gesture must work again after dismissal.
			if err := driver.Scroll(ui.ScrollEvent{
				X: panel.list.X + panel.list.Width/2, Y: panel.list.Y + panel.list.Height/2,
				DeltaY: 120, Precise: true,
			}); err != nil {
				t.Fatal(err)
			}
			if panel.historyTop <= top {
				t.Fatal("AI history did not resume scrolling after dismissal")
			}
		})
	}
}
