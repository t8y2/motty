//go:build darwin || linux || windows

package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
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
	hit := func(id ui.ID) (ui.Hit, bool) {
		for _, hit := range preview.Snapshot().Hits {
			if hit.ID == id {
				return hit, true
			}
		}
		return ui.Hit{}, false
	}
	click := func(id ui.ID, double bool) {
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
	if _, ok := hit(contextID(hitSFTPSearchList.At(2))); !ok {
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
		state.refreshSFTPSearchStatus(view, &view.sftpFolder)
		repaint()
		if err := preview.Screenshot(filepath.Join(dir, "search-zh.png")); err != nil {
			t.Fatal(err)
		}
	}

	click(contextID(hitSFTPSearchList.At(2)), true)
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
