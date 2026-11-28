//go:build darwin || windows || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestBulkAuthenticationNarrowWindowRequiresExplicitSave(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		state := newTestState(t, testOptions(t))
		state.setLanguage(language)
		host := state.hosts[0]
		state.Click(hitHostCheckList.At(int(host.ID)))
		state.Click(hitBulkHostPassword)
		view, err := headless.Open(state.WindowConfig(app.Config{
			Title: "Batch authentication", Width: 640, Height: 480, Tree: Tree(),
			Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		}), nil, headless.Options{Scale: 1, ReferenceText: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(view.Close)
		if err := view.Wait(300 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := view.Keys("Space"); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if !state.bulkPassword.applyUser {
			t.Fatal("Space did not enable the focused username field")
		}
		if err := view.Keys("Tab"); err != nil {
			t.Fatal(err)
		}
		if err := view.Type("reviewed-user"); err != nil {
			t.Fatal(err)
		}
		if err := view.Keys("Enter"); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if state.bulkPassword.review == nil || state.hosts[0].User != host.User {
			t.Fatalf("Enter did not open a read-only review: %q", state.dialogError)
		}
		if err := view.ClickLabel(state.messages.BulkAuthSave); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if state.modal != modalNone || state.hosts[0].User != "reviewed-user" {
			t.Fatalf("explicit save failed: modal=%d error=%q", state.modal, state.dialogError)
		}
	}
}
