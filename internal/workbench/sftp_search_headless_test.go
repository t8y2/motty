//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"motty/internal/sftpclient"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

func TestSFTPSearchHeadlessFlow(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	deepTree(remote)
	// The panel is set up before the window opens: what changes outside the
	// window's own input is painted only when something asks for a frame.
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "guide")
	waitSearchDone(t, state, session, false)

	var fallback app.TextInput
	preview, err := headless.Open(app.Config{
		Title: "Motty SFTP search", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if input := state.SFTPInput(context); input != nil {
				return input
			}
			if editor := state.EditorFor(context.ID); editor != nil {
				fallback = app.TextInput{Editor: editor}
				return &fallback
			}
			return nil
		},
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	hit := func(id gfx.ID) (gfx.Hit, bool) {
		for _, hit := range preview.Snapshot().Hits {
			if hit.ID == id {
				return hit, true
			}
		}
		return gfx.Hit{}, false
	}
	click := func(id gfx.ID, double bool) {
		t.Helper()
		target, ok := hit(id)
		if !ok {
			t.Fatalf("missing control %v", id)
		}
		x, y := target.Bounds.X+target.Bounds.Width/2, target.Bounds.Y+target.Bounds.Height/2
		if double {
			err = preview.DoubleClick(x, y)
		} else {
			err = preview.Click(x, y)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := preview.Wait(100 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}

	repaint := func() {
		t.Helper()
		size := preview.Driver().Frame()
		width, height := int(size.Width), int(size.Height)
		if err := preview.Resize(width+1, height); err != nil {
			t.Fatal(err)
		}
		if err := preview.Resize(width, height); err != nil {
			t.Fatal(err)
		}
	}
	if err := preview.Wait(150 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	toggle, ok := hit(contextID(hitSFTPSearchToggle))
	if !ok || toggle.Label != state.messages.Tools.FilesSearchSubfolders {
		t.Fatalf("switch hit = %+v, present %v", toggle, ok)
	}
	if _, ok := hit(contextID(hitSFTPEntryList.At(2))); !ok {
		t.Fatal("the third result is not on screen")
	}
	if dir := os.Getenv("MOTTY_SFTP_SEARCH_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := preview.Screenshot(filepath.Join(dir, "search-en.png")); err != nil {
			t.Fatal(err)
		}
		state.setLanguage(i18n.Chinese)
		state.refreshSFTPFilterStatus(view, &view.sftpFolder)
		repaint()
		if err := preview.Screenshot(filepath.Join(dir, "search-zh.png")); err != nil {
			t.Fatal(err)
		}
	}

	click(contextID(hitSFTPEntryList.At(2)), true)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs/api" })
	repaint()
	if view.selected < 0 || view.rows[view.selected].entry.Name != "Guide-v2.md" {
		t.Fatalf("the result was not selected in its folder: rows %v, selected %d", rowNames(view.rows), view.selected)
	}
	if dir := os.Getenv("MOTTY_SFTP_SEARCH_SCREENSHOTS"); dir != "" {
		if err := preview.Screenshot(filepath.Join(dir, "revealed.png")); err != nil {
			t.Fatal(err)
		}
	}
}

// The detached window's lists show no results, so a search on its behalf would
// only read the tree, over a channel, for nothing, and clear its selection.
func TestSFTPSearchDoesNotStartInTheDetachedWindow(t *testing.T) {
	state, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.view.sftpFolder.deep, w.view.lfolder.deep = true, true
	fake := state.sessions[0].conn.(*fakeConn)
	fake.mu.Lock()
	opens := fake.sftpOpens
	fake.mu.Unlock()

	input := dualFind(t, view, w, false)
	dualReplaceFilter(t, view, "NOTES")
	dualClickID(t, view, sftpWindowRows[1].At(0))
	dualClickID(t, view, input)
	dualReplaceFilter(t, view, "notes")
	if w.view.sftpFolder.search != nil || !w.canTransfer(false) {
		t.Fatalf("remote filter: search %v, transferable %v, selection %d", w.view.sftpFolder.search != nil, w.canTransfer(false), w.selection(false).count)
	}
	localInput := dualFind(t, view, w, true)
	dualReplaceFilter(t, view, "中文")
	dualClickID(t, view, sftpWindowRows[0].At(0))
	dualClickID(t, view, localInput)
	dualReplaceFilter(t, view, "中文上")
	if w.view.lfolder.search != nil || !w.canTransfer(true) {
		t.Fatal("typing in the local filter started a search or cleared the selection")
	}
	fake.mu.Lock()
	reopened := fake.sftpOpens - opens
	fake.mu.Unlock()
	if reopened != 0 {
		t.Fatalf("the filter opened %d channels", reopened)
	}
}

// A narrow panel has to show everything a search could not do: the notice that
// it stopped at its limit and the one about folders it could not read were the
// parts cut off with an ellipsis.
func TestSFTPSearchStatusShowsEveryNoticeInTheNarrowPanel(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	deepTree(remote)
	state.Click(hitSFTPFilterToggle)
	state.Click(hitSFTPSearchToggle)
	replaceSFTPFilter(t, state, session, false, "guide")
	search := waitSearchDone(t, state, session, false)
	search.summary = sftpclient.WalkSummary{Truncated: true, Failed: 2}
	search.matched = len(search.rows) + 4000

	for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
		state.setLanguage(language)
		state.refreshSFTPFilterStatus(view, &view.sftpFolder)
		preview, err := headless.Open(app.Config{
			Title: "Motty SFTP search status", Width: 1400, Height: 900, Tree: Tree(),
			Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
		}, nil, headless.Options{Scale: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := preview.Wait(100 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if dir := os.Getenv("MOTTY_SFTP_SEARCH_SCREENSHOTS"); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := preview.Screenshot(filepath.Join(dir, fmt.Sprintf("status-limit-and-unreadable-%d.png", language))); err != nil {
				t.Fatal(err)
			}
		}
		var painted []string
		for _, text := range preview.Snapshot().Texts {
			painted = append(painted, text.Value)
		}
		preview.Close()
		screen := strings.ReplaceAll(strings.Join(painted, ""), " ", "") // a notice may wrap where it has a space
		m := state.messages.Tools
		for _, notice := range []string{m.FilesSearchTruncated, fmt.Sprintf(m.FilesSearchFailedFmt, 2), fmt.Sprintf(m.FilesSearchShownFmt, len(search.rows))} {
			if !strings.Contains(screen, strings.ReplaceAll(notice, " ", "")) {
				t.Errorf("language %v: %q is not on screen: %q", language, notice, screen)
			}
		}
	}
}
