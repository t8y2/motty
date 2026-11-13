//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestS3NavigationNativeInput(test *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		for _, scale := range []float64{1, 1.5, 2} {
			test.Run(fmt.Sprintf("%s/%g", language, scale), func(test *testing.T) {
				server, _ := newFakeBucket(test)
				state, _, _ := backupTestState(test)
				state.setLanguage(language)
				state.openS3Configure()
				var invalidator app.Invalidator
				state.options.Invalidator = &invalidator
				view, err := headless.Open(app.Config{Width: 900, Height: 700, Tree: Tree(), Paint: state.Paint,
					OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll,
					OnResize: state.Resize, Invalidator: &invalidator, OnWake: state.Wake,
					TextInput: func(context app.TextInputContext) *app.TextInput {
						if editor := state.EditorFor(context.ID); editor != nil {
							return &app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
						}
						return nil
					},
				}, nil, headless.Options{Scale: scale})
				if err != nil {
					test.Fatal(err)
				}
				test.Cleanup(view.Close)
				must := func(err error) {
					test.Helper()
					if err != nil {
						test.Fatal(err)
					}
				}
				if scale > 1 {
					must(view.Driver().Resize(700, 650))
					must(view.Rest())
				}
				click := func(label string) {
					test.Helper()
					must(view.ClickLabel(label))
					must(view.Rest())
				}
				write := func(label, value string) {
					test.Helper()
					click(label)
					must(view.Type(value))
					must(view.Rest())
				}
				check := func() {
					test.Helper()
					for _, hit := range view.Snapshot().Hits {
						if hit.ID == hitFormSubmit || hit.ID == hitFormCancel || hit.ID == hitFormBack {
							if hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > state.viewport.Width || hit.Bounds.Y+hit.Bounds.Height > state.viewport.Height {
								test.Fatalf("navigation action is outside the window: %+v", hit)
							}
						}
					}
				}
				screenshot := func(name string) {
					test.Helper()
					if directory := os.Getenv("MOTTY_S3_NAV_SCREENSHOTS"); directory != "" && language == i18n.Chinese && scale == 1 {
						must(os.MkdirAll(directory, 0o755))
						must(view.Rest())
						must(view.Screenshot(filepath.Join(directory, name+".png")))
					}
				}
				messages := state.messages.Tools
				write(messages.S3Endpoint, server.URL)
				click(messages.S3Next)
				if state.form.count != 5 || state.form.dialogError == "" {
					test.Fatal("missing bucket advanced past validation")
				}
				write(messages.S3Bucket, "mybucket")
				write(messages.S3Region, "test-region")
				write(messages.S3Key, "custom/backup")
				check()
				screenshot("s3-bucket-next")
				click(messages.S3Next)
				if state.form.count != 3 || view.Snapshot().Focused != hitFormFieldList.At(0) {
					test.Fatal("Next did not open and focus the credentials page")
				}
				click(messages.SyncSave)
				if state.form.count != 3 || state.form.dialogError == "" {
					test.Fatal("missing credentials passed validation")
				}
				write(messages.S3AccessKey, "AKIATEST")
				write(messages.S3Secret, "synthetic-secret")
				write(messages.BackupPassword, testBackupPassword)
				check()
				screenshot("s3-credentials-back-save")
				click(messages.S3Back)
				if state.form.count != 5 || state.form.value(2) != "mybucket" || view.Snapshot().Focused != hitFormFieldList.At(0) {
					test.Fatal("Back lost bucket values or focus")
				}
				click(messages.S3Next)
				if state.form.value(1) != "synthetic-secret" || state.form.value(2) != testBackupPassword {
					test.Fatal("Back/Next lost unsaved credentials")
				}
				click(messages.SyncSave)
				if state.modal != modalNone || state.sync.s3.key != "custom/backup" {
					test.Fatal("final Save did not apply the S3 settings")
				}
				waitFor(test, state, func() bool { return state.status == messages.SyncTestOK })
			})
		}
	}
}
