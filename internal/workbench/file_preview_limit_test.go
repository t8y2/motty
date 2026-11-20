//go:build darwin || windows || linux

package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func TestFilePreviewLimitNormalizesToAUsableSize(t *testing.T) {
	for _, test := range []struct {
		value int
		want  int
	}{
		{20, 20}, {1, 1}, {21, 21}, {40, 40}, {999999, 999999},
		{0, defaultFilePreviewMB}, {-1, defaultFilePreviewMB},
	} {
		if got := normalizeFilePreviewMB(test.value); got != test.want {
			t.Errorf("normalizeFilePreviewMB(%d) = %d, want %d", test.value, got, test.want)
		}
	}
	if got := filePreviewBytes(20); got != 20<<20 {
		t.Errorf("20 MB = %d bytes", got)
	}
	// A damaged file could hold a number too large to shift; the bound must
	// saturate rather than wrap negative and refuse every file.
	if got := filePreviewBytes(int(^uint(0) >> 1)); got <= 0 {
		t.Errorf("the largest size wrapped to %d bytes", got)
	}
}

func TestFilePreviewLimitSurvivesTheStateFile(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.filePreviewMB = 40
	saved := state.snapshot()
	if saved.FilePreviewMB != 40 {
		t.Fatalf("40 MB saved as %d", saved.FilePreviewMB)
	}
	restored := newTestState(t, testOptions(t))
	restored.restoreShared(saved)
	if restored.filePreviewMB != 40 {
		t.Fatalf("40 MB restored as %d", restored.filePreviewMB)
	}
	damaged := newTestState(t, testOptions(t))
	damaged.restoreShared(savedState{FilePreviewMB: 0})
	if damaged.filePreviewMB != defaultFilePreviewMB {
		t.Fatalf("a damaged state file left the bound at %d", damaged.filePreviewMB)
	}
}

// The setting is the same stepper the assistant's step count uses: −, an
// editable number, +, and no upper bound.
func TestFilePreviewStepperStepsTypesAndPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsFiles)
	canvas := paintSettings(t, state)
	for _, id := range []gfx.ID{hitSettingsPreviewMinus, hitSettingsPreviewValue, hitSettingsPreviewPlus} {
		if _, ok := hitByID(canvas, id); !ok {
			t.Fatalf("stepper control %d is not painted", id)
		}
	}
	state.Click(hitSettingsPreviewPlus)
	if state.filePreviewMB != defaultFilePreviewMB+1 {
		t.Fatalf("+ left the size at %d", state.filePreviewMB)
	}
	state.Click(hitSettingsPreviewMinus)
	if state.filePreviewMB != defaultFilePreviewMB {
		t.Fatalf("- left the size at %d", state.filePreviewMB)
	}

	typeFilePreview(t, state, "64")
	if state.filePreviewMB != 64 {
		t.Fatalf("typed 64 left the size at %d", state.filePreviewMB)
	}
	if again := newTestState(t, options); again.filePreviewMB != 64 {
		t.Fatalf("the typed size did not survive a restart: %d", again.filePreviewMB)
	}

	// Escape discards what was typed and keeps the stored number.
	state.Click(hitSettingsPreviewValue)
	paintSettings(t, state)
	input := state.WindowConfig(app.Config{}).TextInput(app.TextInputContext{ID: hitSettingsPreviewValue})
	if input == nil {
		t.Fatal("the stepper has no page editor")
	}
	input.Editor.Insert("7")
	state.Key(gfx.KeyEvent{Target: hitSettingsPreviewValue, Key: gfx.KeyEscape})
	if state.filePreviewMB != 64 {
		t.Fatalf("Escape stored %d", state.filePreviewMB)
	}

	// A value below the minimum falls back to the default rather than zero.
	typeFilePreview(t, state, "0")
	if state.filePreviewMB != defaultFilePreviewMB {
		t.Fatalf("0 was stored as %d", state.filePreviewMB)
	}
}

func typeFilePreview(t *testing.T, state *State, value string) {
	t.Helper()
	state.Click(hitSettingsPreviewValue)
	paintSettings(t, state)
	input := state.WindowConfig(app.Config{}).TextInput(app.TextInputContext{ID: hitSettingsPreviewValue})
	if input == nil {
		t.Fatal("the stepper has no page editor")
	}
	input.Editor.Insert(value)
	state.Key(gfx.KeyEvent{Target: hitSettingsPreviewValue, Key: gfx.KeyEnter})
}

func paintSettings(t *testing.T, state *State) *gfx.Canvas {
	t.Helper()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	return &canvas
}

// The toast used to say text was limited to 1 MiB although the document window
// had long since moved past it, so the message must name the bound it refuses
// for.
func TestPreviewLimitMessageNamesTheDocumentsBound(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setLanguage(i18n.Chinese)
	document := &fileDocument{owner: state, name: "big.log", previewMB: 32}
	message := document.fileProblem(sftpclient.ErrPreviewFileTooLarge)
	if !strings.Contains(message, "32 MB") || strings.Contains(message, "1 MiB") {
		t.Fatalf("the refusal names the wrong limit: %q", message)
	}
	if text := previewLimitMessage(state.messages, 20); !strings.Contains(text, "20 MB") {
		t.Fatalf("the open refusal names the wrong limit: %q", text)
	}
}

func TestLocalDocumentReadHonorsTheLimit(t *testing.T) {
	location := filepath.Join(t.TempDir(), "document.txt")
	if err := os.WriteFile(location, []byte("123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocalDocument(location, 8); !errors.Is(err, sftpclient.ErrPreviewFileTooLarge) {
		t.Fatalf("read past the bound: %v", err)
	}
	if data, err := readLocalDocument(location, 9); err != nil || string(data) != "123456789" {
		t.Fatalf("refused a document at the bound: %q %v", data, err)
	}
}

// A raised bound must reach the document window: the same 24 MB entry is
// refused at the default 20 MB and opens once the setting is 40 MB.
func TestFilePreviewLimitDecidesWhatADocumentWindowOpens(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() {
		host.Quit()
		app.UseApplicationRunner(nil)
	})
	if err := app.RunApplication(func(application *app.Application) error {
		state, session, _, remote := openTestSFTP(t)
		state.options.OpenContentWindow = application.OpenContentWindow
		remote.mu.Lock()
		remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{
			name: "big.log", size: 24 << 20, mode: 0o644, when: time.Unix(1_700_000_300, 0),
		})
		remote.files["/home/test/big.log"] = []byte("log\n")
		remote.mu.Unlock()
		state.Click(hitSFTPRefresh)
		waitSFTP(t, state, session, func(view *sftpView) bool {
			for _, row := range view.sftpFolder.rows {
				if row.entry.Name == "big.log" {
					return !view.busy
				}
			}
			return false
		})
		view := session.files
		view.sftpFolder.selectRow(folderRowIndex(t, &view.sftpFolder, "big.log"), 0)
		state.openFileWindow(view, "/home/test/big.log", false)
		if len(state.fileWindows.documents) != 0 {
			t.Fatal("a file over the default limit opened")
		}
		notices := &state.fileNotices
		if len(notices.items) == 0 {
			t.Fatal("the refusal was not explained")
		}
		if detail := notices.items[len(notices.items)-1].detail; !strings.Contains(detail, "20 MB") {
			t.Fatalf("refusal names the wrong limit: %q", detail)
		}

		state.setFilePreviewMB(40)
		state.openFileWindow(view, "/home/test/big.log", false)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			state.Wake()
			for _, document := range state.fileWindows.documents {
				if document.ready && !document.busy {
					if document.previewMB != 40 {
						t.Fatalf("document bound = %d, want 40", document.previewMB)
					}
					return nil
				}
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("the document did not open under the raised limit")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
