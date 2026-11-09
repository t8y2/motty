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
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/filecontent"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

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
			var field gfx.Rect
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
			modifier := gfx.ModifierControl
			if runtime.GOOS == "darwin" {
				modifier = gfx.ModifierMeta
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

func TestDocumentLongPathsAndFooterEncodingMenu(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
			for _, size := range []gfx.Size{{Width: 520, Height: 320}, {Width: 1000, Height: 760}} {
				d := documentFixture(t)
				d.owner.setLanguage(language)
				d.cacheKey = "long-path"
				d.name = strings.Repeat("配置", 40) + ".yaml"
				d.location = "/etc/" + strings.Repeat("很长的目录/", 80) + d.name
				d.format.Encoding = "Windows-1252" // widest selector value, independent of buffer content
				windows := &d.owner.fileWindows
				windows.documents = map[string]*fileDocument{d.cacheKey: d}
				windows.order = []string{d.cacheKey}
				windows.active = d.cacheKey
				config := windows.config()
				config.Width, config.Height = int(size.Width), int(size.Height)
				view, err := headless.Open(config, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				// A small screen can cap the native minimum below the requested
				// size. Drive that viewport after initial config normalization.
				if err := view.Resize(int(size.Width), int(size.Height)); err != nil {
					t.Fatal(err)
				}
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				checkBounds := func(bounds gfx.Rect) {
					t.Helper()
					if bounds.X < 0 || bounds.Y < 0 || bounds.Width <= 0 || bounds.Height <= 0 || bounds.X+bounds.Width > size.Width+.01 || bounds.Y+bounds.Height > size.Height+.01 {
						t.Fatalf("language %v scale %.2f viewport %+v: control escaped viewport: %+v", language, scale, size, bounds)
					}
				}
				foundTabHint := false
				for _, hit := range view.Snapshot().Hits {
					if hit.ID.Scope() == hitDocumentTabList {
						foundTabHint = true
						if hit.Hint != d.source+" · "+d.location {
							t.Fatal("long path hint was replaced by truncated display text")
						}
						checkBounds(hit.Bounds)
					}
					if hit.ID == hitDocumentLocation {
						t.Fatal("the document name was painted twice")
					}
					if hit.ID == hitDocumentEncoding || hit.ID == hitDocumentReload || hit.ID == hitDocumentSave || hit.ID == hitDocumentInput {
						checkBounds(hit.Bounds)
					}
				}
				if !foundTabHint {
					t.Fatal("missing document tab hint")
				}
				d.encodingOpen = true
				if err := view.Resize(int(size.Width), int(size.Height)+1); err != nil {
					t.Fatal(err)
				}
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				visible := 0
				for _, hit := range view.Snapshot().Hits {
					if hit.ID.Scope() == hitDocumentEncodings && !hit.Block {
						visible++
						checkBounds(hit.Bounds)
						if hit.Bounds.Y+hit.Bounds.Height > d.ui.encodingBounds.Y {
							t.Fatal("footer menu failed to open above the selector")
						}
					}
				}
				if visible == 0 {
					t.Fatal("no encoding choices visible")
				}
				d.encodingOpen = false
				view.Close()
			}
		}
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
	d.saveDirect(true)
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
		if config := host.windows[0].config; config.MinWidth != fileWindowMinWidth || config.MinHeight != fileWindowMinHeight {
			t.Fatalf("document window lost its native size limits: %+v", config)
		}
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
		readsBefore := remote.readCount("/home/test")
		document.saveDirect(false)
		until(func() bool { return !document.busy })
		remote.mu.Lock()
		actual := string(remote.files["/home/test/notes.txt"])
		remote.mu.Unlock()
		if actual != "updated original host\n" || document.dirty() {
			t.Fatal("save lost source binding", actual, document.problem)
		}
		if len(document.notices.items) == 0 || document.notices.items[len(document.notices.items)-1].kind != components.ToastSuccess {
			t.Fatalf("save did not report success: %+v", document.notices.items)
		}
		until(func() bool { return remote.readCount("/home/test") > readsBefore })
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

func TestDocumentWindowSharesTabsAndPreservesEachDocument(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() {
		host.Quit()
		app.UseApplicationRunner(nil)
	})
	if err := app.RunApplication(func(application *app.Application) error {
		state, session, _, _ := openTestSFTP(t)
		state.options.OpenContentWindow = application.OpenContentWindow
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

		state.openFileWindow(session.files, "/home/test/notes.txt", false)
		until(func() bool {
			return len(state.fileWindows.order) == 1 && state.fileWindows.activeDocument() != nil
		})
		state.openFileWindow(session.files, "/home/test/docs/guide.md", false)
		until(func() bool {
			return len(state.fileWindows.order) == 2 &&
				state.fileWindows.documents[state.fileWindows.order[0]].ready &&
				state.fileWindows.documents[state.fileWindows.order[1]].ready
		})
		if len(host.windows) != 1 {
			t.Fatalf("two documents opened %d native windows", len(host.windows))
		}

		notes := state.fileWindows.documents[state.fileWindows.order[0]]
		guide := state.fileWindows.documents[state.fileWindows.order[1]]
		if notes == nil || guide == nil {
			t.Fatal("missing shared-window documents")
		}
		notes.editor.Insert("!")
		state.fileWindows.activate(notes.cacheKey)
		config := host.windows[0].config
		config.Invalidator = nil
		view, err := headless.Open(config, nil, headless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer view.Close()
		view.Resize(1000, 700)
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		var tabs []gfx.Rect
		var closes []gfx.Rect
		foundDirty := false
		foundCloseLabel := false
		for _, hit := range view.Snapshot().Hits {
			switch hit.ID.Scope() {
			case hitDocumentTabList:
				tabs = append(tabs, hit.Bounds)
				foundDirty = foundDirty || hit.Label == "* "+notes.name
			case hitDocumentTabCloseList:
				closes = append(closes, hit.Bounds)
				foundCloseLabel = foundCloseLabel || hit.Label == fmt.Sprintf(state.messages.FileWindow.CloseTabFmt, notes.name)
			}
		}
		if len(tabs) != 2 || len(closes) < 1 || !foundDirty || !foundCloseLabel {
			t.Fatalf("tab strip tabs=%d closes=%d dirtyLabel=%v closeLabel=%v", len(tabs), len(closes), foundDirty, foundCloseLabel)
		}
		if dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR"); dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := view.Screenshot(filepath.Join(dir, "document-tabs.png")); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
				t.Fatal(err)
			}
		}

		shows := host.windows[0].shows
		if err := view.Click(tabs[1].X+20, tabs[1].Y+16); err != nil {
			t.Fatal(err)
		}
		if state.fileWindows.active != guide.cacheKey || guide.editor.String() != "guide\n" {
			t.Fatal("switching tabs changed the selected document or its text")
		}
		state.openFileWindow(session.files, "/home/test/notes.txt", false)
		if state.fileWindows.active != notes.cacheKey || host.windows[0].shows <= shows {
			t.Fatal("reopening an open document did not reveal its existing tab")
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}

		if err := view.Move(tabs[0].X+20, tabs[0].Y+16); err != nil {
			t.Fatal(err)
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		var notesClose gfx.Rect
		for _, hit := range view.Snapshot().Hits {
			if hit.ID.Scope() == hitDocumentTabCloseList {
				if index, ok := hit.ID.In(hitDocumentTabCloseList); ok && index == 0 {
					notesClose = hit.Bounds
				}
			}
		}
		if notesClose.Width <= 0 {
			t.Fatal("hovering an inactive tab did not reveal its close button")
		}
		if err := view.Click(notesClose.X+11, notesClose.Y+11); err != nil {
			t.Fatal(err)
		}
		if notes.confirm != "close" {
			t.Fatal("closing a dirty tab did not ask for confirmation")
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		var cancel gfx.Rect
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == hitDocumentCancel {
				cancel = hit.Bounds
			}
		}
		if err := view.Click(cancel.X+5, cancel.Y+5); err != nil {
			t.Fatal(err)
		}
		if notes.confirm != "" || len(state.fileWindows.order) != 2 {
			t.Fatal("canceling tab close removed the document")
		}
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		state.fileWindows.activate(guide.cacheKey)
		if err := view.Rest(); err != nil {
			t.Fatal(err)
		}
		if err := view.Click(notesClose.X+11, notesClose.Y+11); err != nil {
			t.Fatal(err)
		}
		if state.fileWindows.active != notes.cacheKey {
			t.Fatal("closing an inactive dirty tab did not reveal its confirmation")
		}
		notes.click(hitDocumentDiscard)
		if len(state.fileWindows.order) != 1 || state.fileWindows.activeDocument() != guide {
			t.Fatal("closing one tab did not activate the remaining document")
		}

		state.fileWindows.closeTab(0)
		if len(state.fileWindows.order) != 0 || !host.windows[0].done {
			t.Fatal("closing the last clean tab did not close the shared native window")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentTabsUseSessionChromeAndScrollWhenTheyOverflow(t *testing.T) {
	owner := New(freshOptions(t))
	owner.setLanguage(i18n.Chinese)
	t.Cleanup(owner.Close)
	manager := &owner.fileWindows
	manager.documents = make(map[string]*fileDocument)
	for index := range 8 {
		key := fmt.Sprintf("document-%d-with-a-long-name", index)
		document := &fileDocument{
			owner: owner, cacheKey: key,
			name:   fmt.Sprintf("service-%02d-production-configuration.yaml", index+1),
			source: "production · 192.168.31.2", local: true,
		}
		decoded, err := filecontent.Decode([]byte(fmt.Sprintf("service: %d\n", index+1)), "")
		if err != nil {
			t.Fatal(err)
		}
		document.setText(decoded, nil)
		manager.documents[key] = document
		manager.order = append(manager.order, key)
	}
	manager.active = manager.order[0]

	config := manager.config()
	config.Width, config.Height = 620, 480
	view, err := headless.Open(config, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Resize(620, 480); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if manager.tabStrip.max <= 0 || manager.tabStrip.tabWidth != barTabMin {
		t.Fatalf("long tabs did not overflow at the minimum width: max=%.1f width=%.1f", manager.tabStrip.max, manager.tabStrip.tabWidth)
	}

	tabs := make(map[int]gfx.Rect)
	for _, hit := range view.Snapshot().Hits {
		if hit.ID == hitDocumentLocation {
			t.Fatal("the filename is still painted in the editor content")
		}
		if hit.ID.Scope() == hitDocumentTabList {
			if index, ok := hit.ID.In(hitDocumentTabList); ok {
				tabs[index] = hit.Bounds
			}
		}
	}
	first, firstOK := tabs[0]
	second, secondOK := tabs[1]
	if !firstOK || !secondOK {
		t.Fatalf("not enough visible tabs for geometry check: %+v", tabs)
	}
	if math.Abs(float64(first.Height-barTabHeight)) > .01 ||
		math.Abs(float64(second.X-(first.X+first.Width+barGap))) > .01 ||
		math.Abs(float64(first.Height-second.Height)) > .01 {
		t.Fatalf("file tabs do not share session-tab geometry: first=%+v second=%+v", first, second)
	}
	if dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := view.Screenshot(filepath.Join(dir, "document-tabs-overflow.png")); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
			t.Fatal(err)
		}
	}

	if err := view.Move(first.X+12, first.Y+12); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	foundBar := false
	for _, hit := range view.Snapshot().Hits {
		if hit.ID == hitDocumentTabBar {
			foundBar = true
		}
	}
	if !foundBar || manager.tabs.geometry.MaxOffsetX <= 0 {
		t.Fatal("overflowing tabs did not expose a draggable horizontal scrollbar")
	}
	if dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR"); dir != "" {
		if err := view.Screenshot(filepath.Join(dir, "document-tabs-overflow-hover.png")); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
			t.Fatal(err)
		}
	}

	if err := view.Scroll(gfx.ScrollEvent{
		X: manager.tabStrip.area.X + 20, Y: manager.tabStrip.area.Y + 14,
		DeltaY: manager.tabStrip.max + 10,
	}); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if manager.tabStrip.offset != manager.tabStrip.max {
		t.Fatalf("the mouse wheel did not move the tab strip: %.1f of %.1f", manager.tabStrip.offset, manager.tabStrip.max)
	}

	geometry := manager.tabs.geometry
	if result := manager.pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, Target: hitDocumentTabBar,
		X: geometry.HorizontalThumb.X + geometry.HorizontalThumb.Width/2,
		Y: geometry.HorizontalThumb.Y + geometry.HorizontalThumb.Height/2,
	}); !result.Handled {
		t.Fatal("pressing the tab scrollbar was not handled")
	}
	if result := manager.pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, Target: hitDocumentTabBar, Captured: true,
		X: geometry.HorizontalTrack.X + 2, Y: geometry.HorizontalTrack.Y + 3,
	}); !result.Handled {
		t.Fatal("dragging the tab scrollbar was not handled")
	}
	manager.pointer(gfx.PointerEvent{Kind: gfx.PointerUp, Target: hitDocumentTabBar, Captured: true})
	if manager.tabStrip.offset > 2 {
		t.Fatalf("dragging the thumb to the start left the strip at %.1f", manager.tabStrip.offset)
	}

	if err := view.Resize(1800, 480); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if manager.tabStrip.max != 0 || manager.tabs.geometry.MaxOffsetX != 0 {
		t.Fatalf("the tab scrollbar remained after every tab fit: strip=%.1f bar=%.1f", manager.tabStrip.max, manager.tabs.geometry.MaxOffsetX)
	}
}

func TestSharedWindowCloseHandlesDirtyTabsInOrder(t *testing.T) {
	owner := New(freshOptions(t))
	t.Cleanup(owner.Close)
	owner.setLanguage(i18n.Chinese)
	newDocument := func(key, name string) *fileDocument {
		d := &fileDocument{owner: owner, cacheKey: key, name: name, local: true}
		decoded, err := filecontent.Decode([]byte("original\n"), "")
		if err != nil {
			t.Fatal(err)
		}
		d.setText(decoded, nil)
		d.editor.Insert("changed")
		owner.fileWindows.documents[key] = d
		owner.fileWindows.order = append(owner.fileWindows.order, key)
		return d
	}
	owner.fileWindows.documents = make(map[string]*fileDocument)
	first := newDocument("first", "first.txt")
	second := newDocument("second", "second.txt")
	owner.fileWindows.active = first.cacheKey

	done := false
	owner.fileWindows.requestClose(func() { done = true })
	if !owner.fileWindows.closingAll || first.confirm != "close" {
		t.Fatal("shared window did not start with the first dirty tab")
	}
	first.click(hitDocumentDiscard)
	if len(owner.fileWindows.order) != 1 || !first.closed || second.closed || second.confirm != "close" {
		t.Fatal("shared window did not advance to the next dirty tab")
	}
	second.click(hitDocumentCancel)
	if done || owner.fileWindows.closingAll || second.confirm != "" || second.closed {
		t.Fatal("canceling a later tab closed the shared window or lost edits")
	}

	owner.fileWindows.requestClose(func() { done = true })
	second.click(hitDocumentDiscard)
	if !done || !second.closed || len(owner.fileWindows.order) != 0 {
		t.Fatal("confirming every dirty tab did not finish the shared close")
	}
}

func TestSharedWindowAsyncReloadFailureCancelsWindowClose(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() {
		host.Quit()
		app.UseApplicationRunner(nil)
	})
	if err := app.RunApplication(func(application *app.Application) error {
		state, session, _, _ := openTestSFTP(t)
		state.options.OpenContentWindow = application.OpenContentWindow
		state.openFileWindow(session.files, "/home/test/notes.txt", false)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			state.Wake()
			if len(state.fileWindows.order) == 1 && state.fileWindows.activeDocument() != nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		document := state.fileWindows.activeDocument()
		if document == nil {
			t.Fatal("document did not open")
		}

		document.busy = true
		done := false
		state.fileWindows.requestClose(func() { done = true })
		if !state.fileWindows.closingAll || !document.closeAfter || document.allowClose == nil {
			t.Fatal("shared window did not start closing while the document was busy")
		}

		state.fileWindows.mu.Lock()
		state.fileWindows.events = append(state.fileWindows.events, fileDocumentEvent{
			document: document, err: errors.New("reload failed"),
		})
		state.fileWindows.mu.Unlock()
		state.pumpFileWindows()

		if done || state.fileWindows.closingAll || state.fileWindows.closeDone != nil || len(state.fileWindows.closeQueue) != 0 {
			t.Fatal("a failed asynchronous reload left the shared window stuck in its close flow")
		}
		if document.closed || document.busy || document.closeAfter || document.allowClose != nil || document.problem == "" {
			t.Fatalf("failed reload did not cancel cleanly: busy=%v closeAfter=%v allow=%v problem=%q", document.busy, document.closeAfter, document.allowClose != nil, document.problem)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSharedWindowActivatesBusyTabBeforeClosePrompt(t *testing.T) {
	newManager := func(t *testing.T) (*fileWindowManager, *fileDocument, *fileDocument) {
		t.Helper()
		owner := New(freshOptions(t))
		t.Cleanup(owner.Close)
		manager := &owner.fileWindows
		manager.documents = make(map[string]*fileDocument)
		makeDocument := func(key string, dirty, busy bool) *fileDocument {
			document := &fileDocument{owner: owner, cacheKey: key, name: key + ".txt", local: true, busy: busy}
			decoded, err := filecontent.Decode([]byte("original\n"), "")
			if err != nil {
				t.Fatal(err)
			}
			document.setText(decoded, nil)
			document.busy = busy
			if dirty {
				document.editor.Insert("changed")
			}
			manager.documents[key] = document
			manager.order = append(manager.order, key)
			return document
		}
		first := makeDocument("first", false, false)
		second := makeDocument("second", true, true)
		manager.active = first.cacheKey
		return manager, first, second
	}
	finishBusyOperation := func(document *fileDocument) {
		allow := document.allowClose
		document.allowClose = nil
		document.busy = false
		document.closeAfter = false
		document.requestClose(allow)
	}

	t.Run("window close", func(t *testing.T) {
		manager, _, second := newManager(t)
		manager.requestClose(func() {})
		finishBusyOperation(second)
		if manager.active != second.cacheKey || second.confirm != "close" {
			t.Fatalf("window close left a busy tab's prompt behind: active=%q confirm=%q", manager.active, second.confirm)
		}
	})

	t.Run("source close", func(t *testing.T) {
		manager, _, second := newManager(t)
		owner := second.owner
		if !owner.ConfirmDocumentClose(func() {}) {
			t.Fatal("source close did not defer to the busy document")
		}
		finishBusyOperation(second)
		if manager.active != second.cacheKey || second.confirm != "close" {
			t.Fatalf("source close left a busy tab's prompt behind: active=%q confirm=%q", manager.active, second.confirm)
		}
	})
}

func TestSharedWindowIgnoresCloseCallbackFromPreviousWindow(t *testing.T) {
	owner := New(freshOptions(t))
	t.Cleanup(owner.Close)
	doc := &fileDocument{owner: owner, cacheKey: "notes", name: "notes.txt", local: true}
	decoded, err := filecontent.Decode([]byte("notes\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	doc.setText(decoded, nil)
	owner.fileWindows.documents = map[string]*fileDocument{doc.cacheKey: doc}
	owner.fileWindows.order = []string{doc.cacheKey}
	owner.fileWindows.active = doc.cacheKey

	const oldEpoch, currentEpoch = 7, 8
	owner.fileWindows.windowEpoch = currentEpoch
	owner.fileWindows.onWindowClosed(oldEpoch)
	if doc.closed || owner.fileWindows.windowEpoch != currentEpoch {
		t.Fatal("a previous native window closed the current tab set")
	}
	owner.fileWindows.onWindowClosed(currentEpoch)
	if !doc.closed || owner.fileWindows.documents != nil || len(owner.fileWindows.order) != 0 {
		t.Fatal("the current native window did not dispose its tab set")
	}
}

func TestClosingLastTabDiscardsPendingDocumentLoads(t *testing.T) {
	owner := New(freshOptions(t))
	t.Cleanup(owner.Close)
	ready := &fileDocument{owner: owner, cacheKey: "ready", name: "ready.txt", local: true}
	decoded, err := filecontent.Decode([]byte("ready\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	ready.setText(decoded, nil)
	pending := &fileDocument{owner: owner, cacheKey: "pending", name: "pending.txt", local: true}
	owner.fileWindows.documents = map[string]*fileDocument{ready.cacheKey: ready, pending.cacheKey: pending}
	owner.fileWindows.order = []string{ready.cacheKey}
	owner.fileWindows.active = ready.cacheKey

	owner.fileWindows.forgetDocument(ready)
	if !pending.closed || len(owner.fileWindows.documents) != 0 || len(owner.fileWindows.order) != 0 {
		t.Fatal("closing the last visible tab left a pending load that could reopen the window")
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
			var actions []gfx.Rect
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
