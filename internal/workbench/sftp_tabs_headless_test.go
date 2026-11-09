//go:build darwin || linux || windows

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/sftpclient"
)

func TestSFTPFileTabHeadlessCompactThemesAndNativeInput(t *testing.T) {
	for _, light := range []bool{false, true} {
		for _, remoteLeft := range []bool{false, true} {
			t.Run(fmt.Sprintf("light-%t-remote-left-%t", light, remoteLeft), func(t *testing.T) {
				state, source, w, _, _ := openSFTPTabFixture(t)
				source.Title = strings.Repeat("long-host-name-", 12)
				w.session.Title = source.Title
				state.rebuildFileTabOptions()
				if light {
					state.setAppearance(appearanceLight)
				}
				if remoteLeft {
					state.sftpWindowOrder = sftpRemoteLeft
				}

				var invalidator app.Invalidator
				preview, err := headless.Open(app.Config{
					Title: "File manager tab", Width: 720, Height: 520, Tree: Tree(),
					Invalidator: &invalidator, Paint: state.Paint,
					OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
					OnScroll: state.Scroll, OnFileDrop: state.FileDrop, OnResize: state.Resize,
					TextInput: state.SFTPTabInput,
				}, nil, headless.Options{Scale: 1})
				if err != nil {
					t.Fatal(err)
				}
				defer preview.Close()
				if err := preview.Driver().Pump(); err != nil {
					t.Fatal(err)
				}

				snapshot := preview.Snapshot()
				if state.fileManagerRect.Width <= 0 || state.fileManagerRect.Height <= 0 {
					t.Fatal("the embedded file manager was not painted")
				}
				localText, localOK := textBoundsFromSnapshot(snapshot, "local.yml")
				remoteText, remoteOK := textBoundsFromSnapshot(snapshot, "notes.txt")
				if !localOK || !remoteOK {
					t.Fatalf("compact file manager rows: local=%t remote=%t", localOK, remoteOK)
				}
				if remoteLeft && remoteText.X >= localText.X || !remoteLeft && localText.X >= remoteText.X {
					t.Fatalf("pane order did not match setting: local=%+v remote=%+v remoteLeft=%t", localText, remoteText, remoteLeft)
				}
				tabHit, ok := hitByIDFromSnapshot(snapshot, sftpFileTabID(source.ID))
				if !ok || tabHit.X < 0 || tabHit.X+tabHit.Width > float32(snapshot.Width) {
					t.Fatalf("long file tab escaped the compact title bar: %+v, hit=%t", tabHit, ok)
				}

				mount := sftpWindowMount(w)
				modeHit, ok := hitByIDFromSnapshot(snapshot, tree.MountID(mount, hitSFTPWindowSudoMode))
				if !ok || modeHit.Width <= 0 || modeHit.Y+modeHit.Height > w.ui.list[1].Y || modeHit.X < 0 || modeHit.X+modeHit.Width > float32(snapshot.Width) {
					t.Fatalf("remote mode control escaped compact header: %+v, hit=%t", modeHit, ok)
				}
				pathID := tree.MountID(mount, sftpWindowPath[0])
				pathHit, ok := hitByIDFromSnapshot(snapshot, pathID)
				if !ok {
					t.Fatal("the local path field has no mounted hit identity")
				}
				if err := preview.Click(pathHit.X+pathHit.Width/2, pathHit.Y+pathHit.Height/2); err != nil {
					t.Fatal(err)
				}
				if err := preview.Driver().Pump(); err != nil {
					t.Fatal(err)
				}
				editor := preview.Snapshot().Editor
				if editor == nil {
					t.Fatal("the embedded local path field did not open a native editor")
				}
				if editor.Value != w.view.lfolder.path {
					t.Fatalf("native editor value = %q, want local path %q", editor.Value, w.view.lfolder.path)
				}
				if editor.Bounds.X < 0 || editor.Bounds.Y < 0 ||
					editor.Bounds.X+editor.Bounds.Width > float32(snapshot.Width) ||
					editor.Bounds.Y+editor.Bounds.Height > float32(snapshot.Height) {
					t.Fatalf("native editor escaped the main window: %+v", editor.Bounds)
				}

				if directory := os.Getenv("MOTTY_SFTP_TAB_SCREENSHOT_DIR"); directory != "" {
					if err := os.MkdirAll(directory, 0o755); err != nil {
						t.Fatal(err)
					}
					name := "file-tab-dark-local-left-compact.png"
					if light {
						name = "file-tab-light-local-left-compact.png"
					}
					if remoteLeft {
						name = strings.Replace(name, "local-left", "remote-left", 1)
					}
					if err := preview.Screenshot(filepath.Join(directory, name)); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestSFTPFileTabWheelAndScrollbarStayUnderThePointer(t *testing.T) {
	state, _, w, _, _ := openSFTPTabFixture(t)
	entries := make([]sftpclient.Entry, 240)
	for index := range entries {
		entries[index] = sftpclient.Entry{
			Name: fmt.Sprintf("entry-%03d.yml", index), Size: int64(index),
			ModTime: time.Unix(1_700_000_000+int64(index), 0),
		}
	}
	w.view.lfolder.rows = prepareSFTPRows(nil, entries)

	var invalidator app.Invalidator
	preview, err := headless.Open(app.Config{
		Title: "File manager scrolling", Width: 1100, Height: 720, Tree: Tree(),
		Invalidator: &invalidator, Paint: state.Paint, OnWake: state.Wake,
		OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut,
		OnScroll: state.Scroll, OnFileDrop: state.FileDrop, OnResize: state.Resize,
		TextInput: state.SFTPTabInput,
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}

	list := w.ui.list[0]
	for _, delta := range []float32{24, 48, 72, -24} {
		before := w.ui.offset[0]
		if err := preview.Scroll(gfx.ScrollEvent{
			X:      state.fileManagerRect.X + list.X + 24,
			Y:      state.fileManagerRect.Y + list.Y + 24,
			DeltaY: delta, Precise: true,
		}); err != nil {
			t.Fatal(err)
		}
		if delta > 0 && w.ui.offset[0] <= before {
			t.Fatalf("wheel delta %.0f did not move the list down: %.1f -> %.1f", delta, before, w.ui.offset[0])
		}
		if delta < 0 && w.ui.offset[0] >= before {
			t.Fatalf("wheel delta %.0f did not move the list up: %.1f -> %.1f", delta, before, w.ui.offset[0])
		}
	}

	geometry := w.ui.bars[0]
	thumb := geometry.VerticalThumb
	x := state.fileManagerRect.X + thumb.X + thumb.Width/2
	y := state.fileManagerRect.Y + thumb.Y + thumb.Height/2
	if err := preview.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if w.ui.barDrag != 1 {
		t.Fatalf("scrollbar press did not start a local drag: %d", w.ui.barDrag)
	}
	last := w.ui.offset[0]
	jitter := w.pointer(gfx.PointerEvent{
		Kind: gfx.PointerMove, Captured: true,
		X: x - state.fileManagerRect.X, Y: y - state.fileManagerRect.Y,
	})
	if jitter.Repaint || w.ui.offset[0] != last {
		t.Fatalf("a move that did not change the offset repainted the tab: offset %.1f -> %.1f, repaint %v", last, w.ui.offset[0], jitter.Repaint)
	}
	capturedExit := w.pointer(gfx.PointerEvent{
		Kind: gfx.PointerExit, Captured: true,
		X: x - state.fileManagerRect.X, Y: y - state.fileManagerRect.Y,
	})
	if w.ui.barDrag != 1 || !capturedExit.Handled {
		t.Fatalf("a captured exit stopped the scrollbar drag: local drag %d, result %+v", w.ui.barDrag, capturedExit)
	}
	for step := float32(1); step <= 4; step++ {
		if err := preview.Move(x, y+step*28); err != nil {
			t.Fatal(err)
		}
		if w.ui.offset[0] <= last {
			t.Fatalf("scrollbar drag step %.0f did not follow the pointer: %.1f -> %.1f", step, last, w.ui.offset[0])
		}
		last = w.ui.offset[0]
	}
	if err := preview.Release(x, y+4*28); err != nil {
		t.Fatal(err)
	}
	if w.ui.barDrag != 0 {
		t.Fatal("scrollbar release left the drag captured")
	}
}

func textBoundsFromSnapshot(snapshot app.Snapshot, value string) (gfx.Rect, bool) {
	for _, text := range snapshot.Texts {
		if text.Value == value {
			return text.Bounds, true
		}
	}
	return gfx.Rect{}, false
}

func hitByIDFromSnapshot(snapshot app.Snapshot, id gfx.ID) (gfx.Rect, bool) {
	for _, hit := range snapshot.Hits {
		if hit.ID == id && !hit.Block {
			return hit.Bounds, true
		}
	}
	return gfx.Rect{}, false
}
