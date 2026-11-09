package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
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
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
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
			var closeID gfx.ID
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
			if err := driver.Scroll(gfx.ScrollEvent{
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
				if err := driver.Scroll(gfx.ScrollEvent{X: 300, Y: 400, DeltaY: 120, Precise: true}); err != nil {
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
			if err := driver.Scroll(gfx.ScrollEvent{
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

// The New Host dialog is a form, not a menu: a press on the scrim must not throw
// away what was typed into it. Canceling stays explicit, through Cancel, the
// close button or Escape.
func TestHostDialogKeepsTypedContentWhenPressedOutside(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "staging-web")
	typeInto(state, hitDialogAddress, "10.0.0.7")
	driver := modalEventDriver(t, state)
	var form gfx.Rect
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == hitHostFormViewport {
			form = hit.Bounds
			break
		}
	}
	if form.Width == 0 {
		t.Fatal("the open dialog has no form viewport")
	}
	// A press on the scrim, beside the form and inside the window.
	if err := driver.Click(max(form.X-24, 4), form.Y+form.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNewHost {
		t.Fatal("a press outside the New Host form closed it")
	}
	if got := state.editor.value(fieldLabel); got != "staging-web" {
		t.Fatalf("a press outside the New Host form lost the typed label: %q", got)
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone || state.status != state.messages.StatusEditCanceled {
		t.Fatal("Escape did not cancel the New Host form")
	}
}

// A form dialog is not a menu: a press on the dimmed area beside it does
// nothing, so a half-filled form is not lost by a stray click. Cancel, the close
// button and Escape still cancel it.
func TestFormDialogsKeepTypedContentWhenPressedOutside(t *testing.T) {
	openModal := func(kind modalKind) func(*State) bool {
		return func(s *State) bool { return s.modal == kind }
	}
	for _, tc := range []struct {
		name    string
		open    func(*testing.T, *State)
		openYet func(*State) bool
		field   gfx.ID
	}{
		{"new-forward", func(_ *testing.T, s *State) { s.openForwardDialog() }, openModal(modalNewForward), hitForwardDialogLabel},
		{"new-jump-route", func(_ *testing.T, s *State) {
			s.openProxyDialog()
			s.proxyEditor.viaShell = true
		}, openModal(modalNewProxy), hitProxyDialogRelay},
		{"highlight-rule", func(_ *testing.T, s *State) { s.openHighlightDialog() }, openModal(modalHighlightRule), hitHighlightPattern},
		{"autofill-rule", func(_ *testing.T, s *State) { s.openAutofillDialog(nil) }, openModal(modalAutofillRule), hitAutofillName},
		{"form", func(_ *testing.T, s *State) {
			s.openForm(formDialog{title: "Test form", submit: "Save"}, formField{label: "Name"})
		}, openModal(modalForm), hitFormFieldList.At(0)},
		{"snippet", func(_ *testing.T, s *State) { s.openSnippetDialog(-1) }, openModal(modalSnippet), hitSnippetName},
		{"move-group", func(_ *testing.T, s *State) {
			s.Click(hitHostList.At(int(s.hosts[0].ID)))
			s.Click(hitMoveHostGroup)
			s.Click(hitHostGroupNew)
		}, openModal(modalMoveHostGroup), hitMoveGroupField},
		{"dashboard", func(_ *testing.T, s *State) { s.openDashboardEditor(false) }, openModal(modalDashboard), hitDashboardName},
		{"outbound-proxy", func(_ *testing.T, s *State) { s.openOutboundProxyDialog() }, openModal(modalOutboundProxy), hitOutboundProxyAddress},
		{"ai-provider", func(_ *testing.T, s *State) {
			s.Click(hitSettings)
			s.selectSettingsCategory(settingsAssistant)
			s.Click(hitAISetProviderAdd)
		}, func(s *State) bool { return s.ai.cfg.dialog == aiDialogProvider }, hitAISetFormName},
		{"ai-rename", func(t *testing.T, s *State) {
			db, err := s.aiStore()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.CreateConversation("original", ""); err != nil {
				t.Fatal(err)
			}
			s.Click(hitAIToggle)
			s.Click(hitAIHistory)
			s.Click(hitAIHistoryRename.At(0))
		}, openModal(modalAIRename), hitAIRenameField},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			tc.open(t, state)
			if !tc.openYet(state) {
				t.Fatal("the fixture did not open its dialog")
			}
			typeInto(state, tc.field, "kept")
			editor := state.EditorFor(tc.field)
			if editor == nil || !strings.Contains(editor.String(), "kept") {
				t.Fatal("the fixture's field did not take the text")
			}
			typed := editor.String()
			driver := modalEventDriver(t, state)
			x, y := dialogOutsidePoint(t, driver)
			if err := driver.Click(x, y); err != nil {
				t.Fatal(err)
			}
			if !tc.openYet(state) {
				t.Fatal("a press beside the form closed it")
			}
			if editor := state.EditorFor(tc.field); editor == nil || editor.String() != typed {
				t.Fatal("a press beside the form lost what was typed")
			}
			if err := driver.Key(gfx.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			if tc.openYet(state) {
				t.Fatal("Escape did not cancel the form")
			}
		})
	}
}

// dialogOutsidePoint is a point on the scrim beside the open dialog: left of its
// leftmost control, halfway down its body.
func dialogOutsidePoint(t *testing.T, driver *app.Driver) (float32, float32) {
	t.Helper()
	left, top, bottom := float32(1<<30), float32(1<<30), float32(0)
	for _, hit := range driver.Snapshot().Hits {
		if hit.Scope != dialogScope || hit.ID.IsZero() {
			continue
		}
		left = min(left, hit.Bounds.X)
		top = min(top, hit.Bounds.Y)
		bottom = max(bottom, hit.Bounds.Y+hit.Bounds.Height)
	}
	if left == 1<<30 {
		t.Fatal("the dialog painted no controls")
	}
	return max(left-16, 4), top + (bottom-top)/2
}
