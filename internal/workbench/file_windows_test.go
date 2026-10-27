//go:build darwin || windows || linux

package workbench

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/filecontent"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

func documentFixture(t *testing.T) *fileDocument {
	t.Helper()
	owner := New(freshOptions(t))
	owner.setLanguage(i18n.Chinese)
	t.Cleanup(owner.Close)
	doc := &fileDocument{owner: owner, name: "配置.yaml", location: "/etc/很长的目录/" + strings.Repeat("路径/", 30) + "配置.yaml", source: "docker-ubuntu · 127.0.0.1", local: true}
	decoded, err := filecontent.Decode([]byte("name: 中文\n# configuration\nenabled: true\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	doc.setText(decoded, nil)
	return doc
}

func TestDocumentWindowNativeTextInputAndLayoutAtDisplayScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprintf("%.2f", scale), func(t *testing.T) {
			d := documentFixture(t)
			config := d.config()
			config.Width = 800
			config.Height = 600
			view, err := headless.Open(config, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			defer view.Close()
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			var field ui.Rect
			for _, hit := range view.Snapshot().Hits {
				if hit.ID == hitDocumentInput {
					field = hit.Bounds
				}
			}
			if field.Width <= 0 || field.Height < 100 {
				t.Fatal("missing editor", field)
			}
			if err := view.Click(field.X+30, field.Y+20); err != nil {
				t.Fatal(err)
			}
			modifier := ui.ModifierControl
			if runtime.GOOS == "darwin" {
				modifier = ui.ModifierMeta
			}
			// Headless.Keys routes Control chords as terminal bytes. Use the
			// driver's native text-shortcut path for the platform editing chord.
			if handled, err := view.Driver().ShortcutChord(modifier, 'a'); err != nil || !handled {
				t.Fatal(err)
			}
			if err := view.Type("name: 新配置\n"); err != nil {
				t.Fatal(err)
			}
			if d.editor.String() != "name: 新配置\n" {
				t.Fatalf("native full selection or Chinese insert failed: %q", d.editor.String())
			}
			input := d.textInput(app.TextInputContext{ID: hitDocumentInput})
			if _, err := input.OnEvent(app.TextEvent{Kind: app.TextPaste, Text: "next: yes\r\n"}); err != nil {
				t.Fatal(err)
			}
			if strings.ContainsRune(d.editor.String(), '\r') {
				t.Fatal("Windows paste changed normalized buffer")
			}
			closed := false
			d.requestClose(func() { closed = true })
			if closed || d.confirm != "close" {
				t.Fatal("unsaved changes lost")
			}
			d.click(hitDocumentCancel)
			if d.confirm != "" || !d.dirty() {
				t.Fatal("cancel discarded edits")
			}
			if dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR"); dir != "" {
				os.MkdirAll(dir, 0700)
				view.Resize(801, 601)
				view.Rest()
				if err := view.Screenshot(filepath.Join(dir, fmt.Sprintf("text-%.2f.png", scale))); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDocumentLocalSavePreservesBytesAndRejectsExternalChanges(t *testing.T) {
	for _, encoding := range []string{"UTF-8", "UTF-16LE", "GB18030", "Windows-1252"} {
		value := "name: 中文\nnext: yes\n"
		if encoding == "Windows-1252" {
			value = "name: café\nnext: yes\n"
		}
		raw, err := filecontent.Encode(value, filecontent.Format{Encoding: encoding, BOM: encoding == "UTF-16LE", LineEnding: "\r\n"})
		if err != nil {
			t.Fatal(err)
		}
		location := filepath.Join(t.TempDir(), "settings.yml")
		os.WriteFile(location, raw, 0600)
		revision := sftpclient.Revision(sha256.Sum256(raw))
		opened, err := readLocalDocument(location)
		if err != nil || !bytes.Equal(opened, raw) {
			t.Fatal("read changed bytes", err)
		}
		decoded, err := filecontent.Decode(opened, encoding)
		if err != nil {
			t.Fatal(err)
		}
		edited, err := filecontent.Encode(decoded.Value+"added: true\n", decoded.Format)
		if err != nil {
			t.Fatal(err)
		}
		if err := saveLocalDocument(location, edited, revision); err != nil {
			t.Fatal(err)
		}
		actual, _ := os.ReadFile(location)
		if !bytes.Equal(actual, edited) {
			t.Fatal("local save changed bytes")
		}
		if err := saveLocalDocument(location, raw, revision); !errors.Is(err, sftpclient.ErrRemoteChanged) {
			t.Fatal("external update overwritten", err)
		}
		files, _ := filepath.Glob(filepath.Join(filepath.Dir(location), ".motty-edit-*"))
		if len(files) != 0 {
			t.Fatal("temporary file leaked")
		}
	}
}

func TestDocumentSaveFailureRetainsEditsAndCloseCancellation(t *testing.T) {
	d := documentFixture(t)
	d.format.Encoding = "GBK"
	d.editor.Insert("😀")
	d.save(true)
	if d.busy || !d.dirty() || d.problem == "" {
		t.Fatal("unrepresentable edit accepted")
	}
	d.apply(fileDocumentEvent{saved: true, err: sftpclient.ErrRemoteChanged})
	if !d.dirty() || d.closed || d.closeAfter {
		t.Fatal("conflict discarded unsaved text")
	}
	d.ready = false
	d.setText(filecontent.Text{}, filecontent.ErrChooseEncoding)
	if d.ready || d.editor.String() != "" || d.format.Encoding != "" {
		t.Fatal("failed encoding choice exposed stale editable text")
	}
}

func TestDocumentUnsupportedFilesAndNoticesAreBounded(t *testing.T) {
	d := documentFixture(t)
	for _, data := range [][]byte{[]byte("%PDF-1.7"), []byte{'P', 'K', 3, 4, 0}, bytes.Repeat([]byte{'a'}, filecontent.MaxTextBytes+1)} {
		event := decodeFileEvent(d, data, nil)
		if event.err == nil {
			t.Fatal("unsupported content accepted")
		}
		d.owner.fileNotices.show("Preview", d.owner.fileProblem(event.err), components.ToastWarning)
	}
	notices := &d.owner.fileNotices
	count := len(notices.items)
	notices.show(notices.items[0].title, notices.items[0].detail, components.ToastWarning)
	if len(notices.items) != count {
		t.Fatal("duplicate toast accumulated")
	}
	for range 10 {
		notices.show("Preview", time.Now().String(), components.ToastWarning)
	}
	if notices.queue.Len() > components.ToastQueueLimit || len(notices.items) > components.ToastQueueLimit {
		t.Fatal("unbounded notifications")
	}
	id := notices.items[0].id
	if !notices.click(id) || notices.queue.Len() != len(notices.items) {
		t.Fatal("dismiss did not remove notice")
	}
}

type documentTestHost struct{ windows []*documentTestWindow }
type documentTestWindow struct {
	config app.Config
	closed func()
	done   bool
	shows  int
}

func (w *documentTestWindow) Close() {
	if !w.done {
		w.done = true
		if w.config.OnClosed != nil {
			w.config.OnClosed()
		}
		w.closed()
	}
}
func (w *documentTestWindow) Hide() {}
func (w *documentTestWindow) Show() { w.shows++ }
func (w *documentTestWindow) Frame() (ui.Rect, bool) {
	return ui.Rect{Width: float32(w.config.Width), Height: float32(w.config.Height)}, true
}
func (h *documentTestHost) OpenWindow(config app.Config, _ *ui.Canvas, closed func()) (app.WindowControl, error) {
	w := &documentTestWindow{config: config, closed: closed}
	h.windows = append(h.windows, w)
	return w, nil
}
func (h *documentTestHost) AddStatusItem(app.StatusItemConfig) (app.StatusItemControl, error) {
	return nil, errors.New("not used")
}
func (h *documentTestHost) Quit() {
	for _, w := range h.windows {
		w.Close()
	}
}

func TestDocumentWindowSaveStaysOnCapturedConnectionAfterTabSwitch(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
	if err := app.RunApplication(func(application *app.Application) error {
		state, session, _, remote := openTestSFTP(t)
		state.options.OpenContentWindow = application.OpenContentWindow
		state.openFileWindow(session.files, "/home/test/notes.txt", false)
		until := func(done func() bool) {
			t.Helper()
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				state.Wake()
				if done() {
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatal("document operation did not settle")
		}
		until(func() bool { return len(host.windows) == 1 })
		var document *fileDocument
		for _, d := range state.fileWindows.documents {
			document = d
		}
		if document == nil || !document.ready {
			t.Fatal("document did not load")
		}
		state.openFileWindow(session.files, "/home/test/notes.txt", false)
		if len(host.windows) != 1 || host.windows[0].shows != 1 {
			t.Fatal("same source opened duplicate window")
		}
		// Returning home or selecting a different tab cannot redirect a document save.
		state.activeSessionID = 0
		app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&document.editor)
		document.editor.Insert("updated original host\n")
		document.save(false)
		until(func() bool { return !document.busy })
		remote.mu.Lock()
		actual := string(remote.files["/home/test/notes.txt"])
		remote.mu.Unlock()
		if actual != "updated original host\n" || document.dirty() {
			t.Fatal("save lost source binding", actual, document.problem)
		}
		document.editor.Insert("unsaved")
		allow := false
		if !state.ConfirmDocumentClose(func() { allow = true }) || allow {
			t.Fatal("source close bypassed document")
		}
		document.click(hitDocumentCancel)
		if allow || !document.dirty() {
			t.Fatal("cancel lost edits")
		}
		document.click(hitDocumentReload)
		document.click(hitDocumentDiscard)
		until(func() bool { return !document.busy })
		if document.editor.String() != actual || document.dirty() {
			t.Fatal("reload did not restore source")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentImageAndUnsavedConfirmationAtDisplayScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 2} {
		for _, light := range []bool{false, true} {
			d := documentFixture(t)
			if light {
				d.owner.setAppearance(appearanceLight)
			}
			d.image = &filecontent.Image{Pix: bytes.Repeat([]byte{220, 120, 40, 255}, 64*48), Width: 64, Height: 48, Format: "png"}
			config := d.config()
			config.Width = 680
			config.Height = 480
			view, err := headless.Open(config, nil, headless.Options{Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			d.click(hitDocumentActual)
			view.Resize(681, 481)
			view.Rest()
			if math.Abs(float64(d.ui.imageGeometry.Image.Width)*scale-64) > .01 {
				t.Fatal("image actual size changed with DPI")
			}
			if dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR"); dir != "" {
				view.Screenshot(filepath.Join(dir, fmt.Sprintf("image-%.2f-%t.png", scale, light)))
			}
			d.image = nil
			d.editor.Insert("unsaved")
			d.requestClose(func() { t.Error("unconfirmed document closed") })
			view.Resize(682, 482)
			view.Rest()
			var actions []ui.Rect
			for _, hit := range view.Snapshot().Hits {
				if hit.ID == hitDocumentSaveClose || hit.ID == hitDocumentCancel || hit.ID == hitDocumentDiscard {
					actions = append(actions, hit.Bounds)
				}
			}
			if len(actions) != 3 {
				t.Fatal("missing save/discard/cancel action", actions)
			}
			for i, a := range actions {
				if a.X < 0 || a.Y < 0 || a.X+a.Width > 682 || a.Y+a.Height > 482 {
					t.Fatal("confirmation outside viewport", a)
				}
				for _, b := range actions[i+1:] {
					if a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height {
						t.Fatal("confirmation buttons overlap", a, b)
					}
				}
			}
			if dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR"); dir != "" {
				view.Screenshot(filepath.Join(dir, fmt.Sprintf("confirm-%.2f-%t.png", scale, light)))
			}
			d.click(hitDocumentCancel)
			view.Close()
		}
	}
}
