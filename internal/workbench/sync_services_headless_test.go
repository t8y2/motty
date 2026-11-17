//go:build windows || darwin || linux

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

func TestSyncServicesFourRowsAndNativeEditEnableControls(t *testing.T) {
	state, _, _ := backupTestState(t)
	state.localShellMenu.loaded = true
	for _, id := range syncServices {
		saveServiceForTest(t, state, id, "password-for-"+string(id))
	}
	state.setLanguage(i18n.Chinese)
	view, err := headless.Open(state.WindowConfig(app.Config{Width: 1100, Height: 820, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize, TextInput: func(context app.TextInputContext) *app.TextInput {
		if editor := state.EditorFor(context.ID); editor != nil {
			return &app.TextInput{Editor: editor}
		}
		return nil
	}}), nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	click := func(id gfx.ID) {
		t.Helper()
		for offset := float32(0); offset <= state.settingsContent+120; offset += 120 {
			state.settingsScroll = offset
			if err := view.Frame(); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			for _, hit := range view.Snapshot().Hits {
				if hit.ID == id && hit.Bounds.Height > 0 {
					if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
					return
				}
			}
			if state.modal != modalNone {
				break
			}
		}
		t.Fatal("unreachable control", tree.Text(id))
	}
	shot := func(name string) {
		t.Helper()
		if directory := os.Getenv("MOTTY_SYNC_SERVICES_SCREENSHOTS"); directory != "" {
			if err := os.MkdirAll(directory, 0700); err != nil {
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
	click(hitSyncServiceToggle.At(2))
	if state.activeSyncService() != syncServiceGitHub {
		t.Fatal("GitHub enable did not activate it")
	}
	for _, look := range []struct {
		name       string
		appearance appearance
	}{{"light", appearanceLight}, {"dark", appearanceDark}} {
		state.setAppearance(look.appearance)
		state.settingsScroll = 150
		if err := view.Frame(); err != nil {
			t.Fatal(err)
		}
		shot("services-" + look.name)
	}
	click(hitSyncConfigureS3)
	shot("s3-bucket")
	click(hitFormSubmit)
	if state.form.title != state.messages.Tools.S3LoginTitle {
		t.Fatal("Next did not open S3 credentials")
	}
	shot("s3-credentials")
	foundTest := false
	for _, hit := range view.Snapshot().Hits {
		foundTest = foundTest || hit.ID == hitFormTest
	}
	if !foundTest {
		t.Fatal("S3 credentials have no test control")
	}
	click(hitFormSubmit)
	if state.activeSyncService() != syncServiceGitHub {
		t.Fatal("saving S3 changed enabled service")
	}
	click(hitSyncServiceToggle.At(1))
	if state.activeSyncService() != syncServiceS3 {
		t.Fatal("S3 enable failed")
	}
	click(hitSyncServiceToggle.At(1))
	if state.sync.configured() {
		t.Fatal("S3 disable failed")
	}
	for _, dialog := range []struct {
		id   gfx.ID
		name string
	}{{hitSyncConfigure, "webdav"}, {hitSyncConfigureGist, "github"}, {hitSyncConfigureGitee, "gitee"}} {
		click(dialog.id)
		shot(dialog.name + "-form")
		click(hitFormCancel)
	}
}
