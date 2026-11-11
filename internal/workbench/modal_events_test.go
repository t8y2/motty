package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/autofill"
)

func modalEventDriver(t *testing.T, state *State) *app.Driver {
	t.Helper()
	return modalEventDriverSize(t, state, 1400, 700)
}

// modalEventDriverSize is modalEventDriver for a window of a chosen size.
func modalEventDriverSize(t *testing.T, state *State, width, height int) *app.Driver {
	t.Helper()
	driver, err := app.NewDriver(state.WindowConfig(app.Config{
		Title: "modal events", Width: width, Height: height, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
		TextInput: func(c app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(c.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver
}

// clickPaintedHit presses the control named id through the driver's real
// pointer event pass, the way a window does.
func clickPaintedHit(t *testing.T, driver *app.Driver, id gfx.ID) {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id && !hit.IsLabel && hit.Bounds.Width > 0 {
			if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("the painted control %v was not found", id)
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
	clickAIPanelPage(t, state, hitAIHistory)
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
			clickAIPanelPage(t, state, hitAIHistory)
			var closeID gfx.ID
			closeLabel := ""
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
				closeID = hitProxyDialogClose
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
					closeLabel = state.messages.Cancel
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
				if (!closeID.IsZero() && hit.ID == closeID && !hit.IsLabel) || (closeLabel != "" && hit.Label == closeLabel && hit.Role == gfx.RoleButton) {
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
			clickAIPanelPage(t, s, hitAIHistory)
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

// The dialog bodies declared as pages lay their controls out themselves; a
// real pointer press on what the page laid out must still reach the action the
// control always had.
func TestConvertedDialogBodiesTakeRealPointerEvents(t *testing.T) {
	for _, tc := range []struct {
		name  string
		open  func(*State)
		click gfx.ID
		want  func(*State) bool
	}{
		{"host form", func(s *State) { s.openHostDialog() }, hitDialogAdvanced,
			func(s *State) bool { return s.editor.advancedOpen }},
		{"close tabs", func(s *State) {
			s.tabsClose = tabsClose{count: 2, running: []string{"Production", "Staging"}}
			s.modal = modalCloseTabs
		}, hitTabsCloseQuiet, func(s *State) bool { return s.tabsClose.quiet }},
		{"autofill rule", func(s *State) { s.openAutofillDialog(nil) }, hitAutofillText,
			func(s *State) bool { return s.autofillRules.send == autofill.SendText }},
		{"forward", func(s *State) { s.openForwardDialog() }, hitForwardDialogResume,
			func(s *State) bool { return s.forwardEditor.resume }},
		{"snippet", func(s *State) { s.openSnippetDialog(-1) }, hitSnippetRun,
			func(s *State) bool { return !s.snippets.run }},
		{"jump route", func(s *State) { s.openProxyDialog() }, hitProxyDialogViaShell,
			func(s *State) bool { return s.proxyEditor.viaShell }},
		{"form", func(s *State) {
			s.openForm(formDialog{title: "Test form", submit: "Save", checkLabel: "Consent"}, formField{label: "Name"})
		}, hitFormCheckbox, func(s *State) bool { return s.form.checked }},
		{"highlight rule", func(s *State) { s.openHighlightDialog() }, hitHighlightRegex,
			func(s *State) bool { return s.highlighting.regex }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			tc.open(state)
			driver := modalEventDriverSize(t, state, 1400, 900)
			clickPaintedHit(t, driver, tc.click)
			if !tc.want(state) {
				t.Fatal("a real press on the control the page laid out did not act")
			}
		})
	}
}

// The route settings dialog has no switch to press, so its proof is that a
// field the body page laid out takes the pointer and the typing that follows.
func TestOutboundProxyBodyTakesRealPointerAndText(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openOutboundProxyDialog()
	driver := modalEventDriverSize(t, state, 1400, 900)
	clickPaintedHit(t, driver, hitOutboundProxyBypass)
	if got := driver.Snapshot().Focused; got != hitOutboundProxyBypass {
		t.Fatalf("the pressed field is not focused: %v", got)
	}
	if err := driver.Type("deploy"); err != nil {
		t.Fatal(err)
	}
	if got := state.proxyForm.bypass.String(); !strings.HasSuffix(got, "deploy") {
		t.Fatalf("typing reached %q", got)
	}
}

// The vault dialogs were already pages; the unlock body's field must still take
// the pointer and the text typed into it.
func TestVaultUnlockBodyTakesRealPointerAndText(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openVaultUnlock(nil, nil)
	driver := modalEventDriverSize(t, state, 1400, 900)
	clickPaintedHit(t, driver, hitVaultUnlockField)
	if err := driver.Type("master password"); err != nil {
		t.Fatal(err)
	}
	if got := state.secrets.unlock.String(); got != "master password" {
		t.Fatalf("typing reached %q", got)
	}
}
