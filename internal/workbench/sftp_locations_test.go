//go:build windows || darwin || linux

package workbench

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/configstore"
	"motty/internal/i18n"
)

func TestSFTPDirectoryFavoritesAndProfilesPersistIndependently(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1)
	w.session.host.CatalogID = "favorite-host"
	state.sftpBookmarks = []sftpBookmark{
		{host: "favorite-host", name: "project-a", local: dir, path: "/home/test"},
		{host: "favorite-host", name: "project-b", local: filepath.Join(dir, "tree"), path: "/home/test"},
	}
	w.click(hitSFTPLocalStar)
	w.click(hitSFTPBookmarkToggle)
	if w.bookmark.open || len(w.visibleProfiles()) != 2 || len(w.directoryFavorites()) != 1 || !w.directoryFavorite(true) || !w.directoryFavorite(false) {
		t.Fatal("directory stars changed paired profiles")
	}
	// Receiving shared configuration must never reset this computer's folders.
	shared := state.snapshot()
	shared.SFTPLocalBookmarks = nil
	state.restoreShared(shared)
	if !slices.Equal(state.sftpLocalBookmarks, []string{dir}) {
		t.Fatal("shared configuration erased local favorites")
	}
	file := filepath.Join(t.TempDir(), "config.db")
	if err := saveState(file, state.snapshot()); err != nil {
		t.Fatal(err)
	}
	restored, _, err := loadState(file)
	if err != nil || len(restored.SFTPBookmarks) != 3 || !slices.Equal(restored.SFTPLocalBookmarks, []string{dir}) {
		t.Fatalf("same-server-path profiles or favorites were lost: %+v, %v", restored.SFTPBookmarks, err)
	}
	state.restoreSFTPLocalFavorites([]string{dir, filepath.Join(dir, "."), "relative/folder", ""})
	if !slices.Equal(state.sftpLocalBookmarks, []string{dir}) {
		t.Fatal("restoring local favorites accepted duplicates or relative paths")
	}
	store, err := configstore.Open(file, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records, err := store.Records()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.ID == "sftp_local_bookmarks" || strings.Contains(string(record.Payload), `"sftp_local_bookmarks"`) {
			t.Fatal("local favorites leaked into synced records")
		}
	}
	w.click(hitSFTPBookmarkToggle)
	w.click(hitSFTPLocalStar)
	if len(w.visibleProfiles()) != 2 || len(w.directoryFavorites()) != 0 || len(state.sftpLocalBookmarks) != 0 {
		t.Fatal("unbookmarking removed a named profile")
	}
	w.openDirectoryPlaces(false)
	w.window.Invalidate()
	host.windows[0].view.Driver().Pump()
	host.windows[0].view.Rest()
	for _, label := range host.windows[0].view.Snapshot().Texts {
		if label.Value == state.messages.SFTPWindow.BookmarkAdd || label.Value == "project-a" {
			t.Fatal("server directory menu still contains paired profile actions")
		}
	}
}

func TestSFTPLocalPlacesNavigationAndMissingFavoritesUseGlobalNotices(t *testing.T) {
	state, w, _, host, dir := dualSFTPFixture(t, 1.25)
	state.setLanguage(i18n.Chinese)
	view := host.windows[0].view
	dualClickID(t, view, hitSFTPLocalStar)
	w.navigate(true, filepath.Join(dir, "tree"))
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	dualClickID(t, view, hitSFTPLocalPlaces)
	dualText(t, view, state.messages.SFTPWindow.CommonFolders)
	dualText(t, view, state.messages.SFTPWindow.DirectoryFavorites)
	dualClickID(t, view, sftpLocalFavorites.At(0))
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if w.view.lfolder.path != dir || w.view.path != "/home/test" || w.view.bookmarksOpen {
		t.Fatal("local favorite did not navigate only its own pane")
	}
	missing := filepath.Join(dir, "missing-directory")
	state.sftpLocalBookmarks = append(state.sftpLocalBookmarks, missing)
	dualClickID(t, view, hitSFTPLocalPlaces)
	dualClickID(t, view, sftpLocalFavorites.At(1))
	if w.view.lfolder.path != dir || len(w.notices.items) == 0 || w.view.errorText != "" {
		t.Fatal("missing local favorite did not preserve its pane and show a global notice")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("favorite navigation created a missing directory")
	}
	w.notices = fileNotices{}
	w.toggleBrowseSync()
	outside := t.TempDir()
	w.openDirectoryPlaces(true)
	w.view.places.locations = []sftpLocalPlace{{kind: localPlaceDocuments, path: outside}}
	w.window.Invalidate()
	view.Driver().Pump()
	view.Rest()
	dualClickID(t, view, sftpLocalPlaces.At(0))
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if w.browse.enabled || w.view.lfolder.path != outside || w.view.path != "/home/test" || len(w.notices.items) == 0 {
		t.Fatal("jumping outside a paired root did not pause synchronization")
	}
	if len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("favorite or quick-place navigation queued transfers")
	}
}

func TestSFTPLocalPlacesLayoutAtDifferentScales(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		state, w, _, host, dir := dualSFTPFixture(t, scale)
		state.setLanguage(i18n.Chinese)
		view := host.windows[0].view
		w.click(hitSFTPLocalStar)
		for _, light := range []bool{true, false} {
			if light {
				state.setAppearance(appearanceLight)
			} else {
				state.setAppearance(appearanceDark)
			}
			w.window.Invalidate()
			view.Driver().Pump()
			view.Rest()
			dualClickID(t, view, hitSFTPLocalPlaces)
			seen := map[gfx.ID]bool{}
			for _, hit := range view.Snapshot().Hits {
				if hit.ID.IsZero() || hit.Block {
					continue
				}
				if seen[hit.ID] {
					t.Fatal("places popup reused a hit identity", hit.ID)
				}
				seen[hit.ID] = true
			}
			if screenshotDir := os.Getenv("MOTTY_PLACES_SCREENSHOTS"); screenshotDir != "" && scale == 1 {
				if err := os.MkdirAll(screenshotDir, 0700); err != nil {
					t.Fatal(err)
				}
				name := "places-dark.png"
				if light {
					name = "places-light.png"
				}
				if err := view.Screenshot(filepath.Join(screenshotDir, name)); err != nil {
					t.Fatal(err)
				}
			}
			if err := view.Keys("escape"); err != nil {
				t.Fatal(err)
			}
			if w.view.bookmarksOpen || !w.directoryFavorite(true) || w.view.lfolder.path != dir {
				t.Fatal("Escape changed a favorite or directory")
			}
		}
	}
}

// The startup path must put the saved favorites back on the state, not only in
// the decoded snapshot; losing that call would silently empty the list.
func TestSFTPLocalFavoritesReturnAfterRestart(t *testing.T) {
	options := testOptions(t)
	folder := t.TempDir()
	if err := saveState(options.StatePath, savedState{SFTPLocalBookmarks: []string{folder}}); err != nil {
		t.Fatal(err)
	}
	saved, _, err := loadState(options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	restarted := newTestState(t, options)
	restarted.restore(saved)
	if !slices.Equal(restarted.sftpLocalBookmarks, []string{folder}) {
		t.Fatalf("local favorites after restart = %v", restarted.sftpLocalBookmarks)
	}
}
