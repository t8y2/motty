//go:build windows || darwin || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	layoutcore "motty/internal/workspace"
)

func TestSFTPPanelPlacesNativeNavigationMatchesDetachedFavorites(t *testing.T) {
	for _, side := range []layoutcore.ContextSide{layoutcore.ContextLeft, layoutcore.ContextRight} {
		for _, scale := range []float64{1, 1.5, 2} {
			t.Run(fmt.Sprintf("%v/%v", side, scale), func(t *testing.T) {
				state, session, _, _ := openTestSFTP(t)
				state.setLanguage(i18n.Chinese)
				session.host.CatalogID = "sidebar-favorites"
				files := session.files
				dir := t.TempDir()
				child := filepath.Join(dir, "child")
				if err := os.Mkdir(child, 0700); err != nil {
					t.Fatal(err)
				}
				if err := state.listLocal(files, dir); err != nil {
					t.Fatal(err)
				}
				files.local = true
				context, _ := state.deck.Context(state.deck.ActiveWorkspace())
				context.Side = side
				if err := state.deck.SetContext(state.deck.ActiveWorkspace(), context); err != nil {
					t.Fatal(err)
				}
				preview, err := headless.Open(app.Config{Title: "SFTP places", Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize, OnWake: state.Wake, TextInput: state.SFTPInput}, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer preview.Close()
				click := func(id gfx.ID) {
					t.Helper()
					hit, ok := findSnapshotHit(preview.Snapshot(), contextID(id))
					if !ok {
						t.Fatal("missing panel control", id)
					}
					if err := preview.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
						t.Fatal(err)
					}
				}
				click(hitSFTPLocalStar)
				other := sftpPlaceBrowser{owner: state, session: session, view: &sftpView{lfolder: sftpFolder{path: dir}}}
				if !other.directoryFavorite(true) || files.path != "/home/test" {
					t.Fatal("local favorites were not shared without moving the server pane")
				}
				click(hitSFTPLocalPlaces)
				if !files.bookmarksOpen {
					t.Fatal("sidebar local locations did not open")
				}
				files.places.locations = []sftpLocalPlace{{kind: localPlaceDocuments, path: child}, {kind: localPlaceDrive, path: dir}}
				if err := preview.Rest(); err != nil {
					t.Fatal(err)
				}
				if output := os.Getenv("MOTTY_PANEL_PLACES_SCREENSHOTS"); output != "" && scale == 1 {
					if err := os.MkdirAll(output, 0700); err != nil {
						t.Fatal(err)
					}
					if err := preview.Screenshot(filepath.Join(output, fmt.Sprintf("panel-%v.png", side))); err != nil {
						t.Fatal(err)
					}
				}
				click(sftpLocalPlaces.At(0))
				if files.lfolder.path != child || files.path != "/home/test" || files.bookmarksOpen {
					t.Fatal("sidebar shortcut did not navigate its local pane")
				}
				click(hitSFTPLocalPlaces)
				click(sftpLocalFavorites.At(0))
				if files.lfolder.path != dir {
					t.Fatal("sidebar local favorite did not reopen")
				}
				click(hitSFTPLocalPlaces)
				if err := preview.Keys("escape"); err != nil {
					t.Fatal(err)
				}
				if files.bookmarksOpen {
					t.Fatal("Escape left the sidebar menu open")
				}
				click(hitSFTPPaneToggle)
				click(hitSFTPBookmarkToggle)
				if files.local || len((sftpPlaceBrowser{owner: state, session: session, view: files}).directoryFavorites()) != 1 {
					t.Fatal("server favorite was not independent of local favorites")
				}
				click(hitSFTPBookmarkList)
				terminal := state.workspacePixelBounds(gfx.Size{Width: 1400, Height: 900})
				if err := preview.Click(terminal.X+terminal.Width/2, terminal.Y+terminal.Height/2); err != nil {
					t.Fatal(err)
				}
				if files.bookmarksOpen {
					t.Fatal("clicking outside did not dismiss sidebar places")
				}
			})
		}
	}
}

func TestSFTPPanelPlacesScrollbarDoesNotScrollTheFileListOrDismiss(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	files := session.files
	files.local = true
	files.lfolder.path = t.TempDir()
	for i := range maxBookmarksPerHost {
		state.sftpLocalBookmarks = append(state.sftpLocalBookmarks, filepath.Join(files.lfolder.path, fmt.Sprintf("favorite-%02d", i)))
	}
	preview, err := headless.Open(app.Config{Title: "SFTP places scroll", Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	trigger, ok := findSnapshotHit(preview.Snapshot(), contextID(hitSFTPLocalPlaces))
	if !ok {
		t.Fatal("missing local places trigger")
	}
	if err := preview.Click(trigger.Bounds.X+trigger.Bounds.Width/2, trigger.Bounds.Y+trigger.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	bar, ok := findSnapshotHit(preview.Snapshot(), contextID(files.places.scroll.BarID))
	if !ok || files.places.scroll.BarID.IsZero() {
		t.Fatal("long location menu has no scrollbar")
	}
	x, y := bar.Bounds.X+bar.Bounds.Width/2, bar.Bounds.Y+1
	if err := preview.Press(x, y); err != nil {
		t.Fatal(err)
	}
	if !files.bookmarksOpen {
		t.Fatal("pressing a stationary thumb dismissed places")
	}
	if err := preview.Move(x, bar.Bounds.Y+bar.Bounds.Height-2); err != nil {
		t.Fatal(err)
	}
	if err := preview.Release(x, bar.Bounds.Y+bar.Bounds.Height-2); err != nil {
		t.Fatal(err)
	}
	if !files.bookmarksOpen || files.places.scroll.Offset <= 0 || files.lfolder.first != 0 {
		t.Fatal("scrollbar dismissed places or moved the underlying file list")
	}
}
