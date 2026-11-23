package workbench

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

var (
	hitSFTPLocalPlaces      = tree.ID(sftpHits, "local-places")
	hitSFTPLocalStar        = tree.ID(sftpHits, "local-favorite")
	sftpLocalFavorites      = tree.Sub(sftpHits, tree.Name("local-favorites"))
	sftpLocalFavoriteRemove = tree.Sub(sftpHits, tree.Name("local-favorite-remove"))
	sftpLocalPlaces         = tree.Sub(sftpHits, tree.Name("local-places-list"))
	hitSFTPPlacesBar        = tree.ID(sftpHits, "places-scroll")
)

type sftpLocalPlaceKind uint8

const (
	localPlaceHome sftpLocalPlaceKind = iota
	localPlaceDesktop
	localPlaceDownloads
	localPlaceDocuments
	localPlacePictures
	localPlaceMusic
	localPlaceVideos
	localPlaceMovies
	localPlaceApplications
	localPlaceDrive
)

type sftpLocalPlace struct {
	kind sftpLocalPlaceKind
	path string
}

type sftpPlaceMenu struct {
	local     bool
	locations []sftpLocalPlace
	anchors   [2]gfx.Rect
}

// A sidebar and a detached window share navigation shortcuts and favorites.
// The view owns the popup; a temporary painter never owns asynchronous work.
type sftpPlaceBrowser struct {
	owner   *State
	session *Session
	view    *sftpView
}

func (w *sftpWindow) placeBrowser() sftpPlaceBrowser {
	return sftpPlaceBrowser{owner: w.owner, session: &w.session, view: w.view}
}

func (w *sftpWindow) directoryFavorites() []sftpBookmark {
	return w.placeBrowser().directoryFavorites()
}
func (w *sftpWindow) directoryFavorite(local bool) bool {
	return w.placeBrowser().directoryFavorite(local)
}
func (w *sftpWindow) toggleDirectoryFavorite(local bool) {
	w.placeBrowser().toggleDirectoryFavorite(local)
}
func (w *sftpWindow) openDirectoryPlaces(local bool) { w.placeBrowser().openDirectoryPlaces(local) }
func (w *sftpWindow) clickDirectoryPlace(id gfx.ID)  { w.placeBrowser().clickDirectoryPlace(id) }

func (w sftpPlaceBrowser) folder(local bool) *sftpFolder {
	if local {
		return &w.view.lfolder
	}
	return &w.view.sftpFolder
}

func (w sftpPlaceBrowser) localBusy() bool {
	return w.view.detached != nil && w.view.detached.pendingLocal != ""
}
func (w sftpPlaceBrowser) navigationBusy() bool {
	if w.view.detached != nil {
		return w.view.detached.navigationBusy()
	}
	return !w.view.local && w.view.busy
}
func (w sftpPlaceBrowser) connected() bool {
	return w.session.hasServer() && w.view.worker != nil && w.session.phase == sshconn.PhaseConnected
}
func (w sftpPlaceBrowser) cancelDrag() {
	if w.view.detached != nil {
		w.view.detached.cancelDrag()
	}
	w.owner.sftpMenu.open = false
}
func (w sftpPlaceBrowser) closeHeaderMenu() {
	w.view.headerMenuOpen, w.view.lfolder.headerMenuOpen = false, false
}
func (w sftpPlaceBrowser) noticeLimit() {
	m := w.owner.messages.SFTPWindow
	w.owner.fileOpenNotice(w.view.detached, m.DirectoryFavorites, m.FavoriteLimit, components.ToastError)
}
func (w sftpPlaceBrowser) removeWindowBookmark(bookmark sftpBookmark) {
	if index := slices.Index(w.owner.sftpBookmarks, bookmark); index >= 0 {
		w.owner.sftpBookmarks = slices.Delete(w.owner.sftpBookmarks, index, index+1)
		w.owner.save()
	}
}
func (w sftpPlaceBrowser) navigate(local bool, directory string) {
	if w.view.detached != nil {
		w.view.detached.navigate(local, directory)
		return
	}
	if w.navigationBusy() || w.view.dialog != sftpDialogNone {
		return
	}
	w.view.local = local
	*w.folder(local).pathInput() = *text.NewEditor(directory)
	w.owner.submitSFTPPath(w.view)
}

func localFavoriteIndex(favorites []string, directory string) int {
	directory = filepath.Clean(directory)
	return slices.IndexFunc(favorites, func(other string) bool {
		if runtime.GOOS == "windows" {
			return strings.EqualFold(filepath.Clean(other), directory)
		}
		return filepath.Clean(other) == directory
	})
}

func (state *State) restoreSFTPLocalFavorites(favorites []string) {
	state.sftpLocalBookmarks = nil
	for _, directory := range favorites {
		if directory == "" || !validBookmarkLocal(directory) || len(state.sftpLocalBookmarks) >= maxBookmarksPerHost {
			continue
		}
		directory = filepath.Clean(directory)
		if localFavoriteIndex(state.sftpLocalBookmarks, directory) < 0 {
			state.sftpLocalBookmarks = append(state.sftpLocalBookmarks, directory)
		}
	}
}

// Directory favorites are independent of named, paired location profiles.
func (w sftpPlaceBrowser) directoryFavorites() []sftpBookmark {
	var result []sftpBookmark
	for _, bookmark := range w.owner.sftpBookmarks {
		if bookmark.host == w.session.host.CatalogID && !bookmark.shared && bookmark.local == "" && bookmark.name == "" {
			result = append(result, bookmark)
		}
	}
	return result
}

func (w sftpPlaceBrowser) directoryFavorite(local bool) bool {
	if local {
		return localFavoriteIndex(w.owner.sftpLocalBookmarks, w.view.lfolder.path) >= 0
	}
	for _, bookmark := range w.owner.sftpBookmarks {
		if bookmark.host == w.session.host.CatalogID && !bookmark.shared && bookmark.local == "" && bookmark.name == "" && bookmark.path == w.view.path {
			return true
		}
	}
	return false
}

func (w sftpPlaceBrowser) toggleDirectoryFavorite(local bool) {
	if w.folder(local).path == "" || local && w.localBusy() || !local && (w.view.busy || !w.connected() || w.session.host.CatalogID == "") {
		return
	}
	state := w.owner
	if local {
		if index := localFavoriteIndex(state.sftpLocalBookmarks, w.view.lfolder.path); index >= 0 {
			state.sftpLocalBookmarks = slices.Delete(state.sftpLocalBookmarks, index, index+1)
		} else if len(state.sftpLocalBookmarks) < maxBookmarksPerHost {
			state.sftpLocalBookmarks = append(state.sftpLocalBookmarks, filepath.Clean(w.view.lfolder.path))
		} else {
			w.noticeLimit()
			return
		}
	} else {
		bookmark := sftpBookmark{host: w.session.host.CatalogID, path: w.view.path}
		if index := slices.Index(state.sftpBookmarks, bookmark); index >= 0 {
			state.sftpBookmarks = slices.Delete(state.sftpBookmarks, index, index+1)
		} else if state.bookmarkScopeSize(bookmark.host, false) < maxBookmarksPerHost && len(state.sftpBookmarks) < maxBookmarks {
			state.sftpBookmarks = append(state.sftpBookmarks, bookmark)
		} else {
			w.noticeLimit()
			return
		}
	}
	state.save()
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}

func (w sftpPlaceBrowser) openDirectoryPlaces(local bool) {
	w.cancelDrag()
	w.closeHeaderMenu()
	if w.view.detached != nil {
		w.view.detached.activeLocal = local
		w.view.detached.ui.menuOpen = false
	}

	w.view.local = local
	w.view.places.local = local
	if local {
		// Snapshot native locations on opening, never during paint. No volumes
		// or network shares are scanned to populate the drive list.
		w.view.places.locations = nativeSFTPLocalPlaces()
	}
	w.view.bookmarksOpen = true
}

func (w sftpPlaceBrowser) clickDirectoryPlace(id gfx.ID) {
	local := w.view.places.local
	if id == hitSFTPBookmarkNew {
		w.toggleDirectoryFavorite(local)
		return
	}
	if local {
		if index, ok := id.In(sftpLocalFavoriteRemove); ok {
			if index < len(w.owner.sftpLocalBookmarks) {
				w.owner.sftpLocalBookmarks = slices.Delete(w.owner.sftpLocalBookmarks, index, index+1)
				w.owner.save()
			}
			return
		}
		if index, ok := id.In(sftpLocalFavorites); ok && index < len(w.owner.sftpLocalBookmarks) {
			w.view.bookmarksOpen = false
			w.navigate(true, w.owner.sftpLocalBookmarks[index])
			return
		}
		if index, ok := id.In(sftpLocalPlaces); ok && index < len(w.view.places.locations) {
			w.view.bookmarksOpen = false
			w.navigate(true, w.view.places.locations[index].path)
			return
		}
	} else {
		marks := w.directoryFavorites()
		if index, ok := id.In(hitSFTPBookmarkRemoveList); ok {
			if index < len(marks) {
				w.removeWindowBookmark(marks[index])
			}
			return
		}
		if index, ok := id.In(hitSFTPBookmarkItemList); ok && index < len(marks) {
			w.view.bookmarksOpen = false
			w.navigate(false, marks[index].path)
			return
		}
	}
	w.view.bookmarksOpen = false
}

// declareDirectoryPlaces declares the places menu under the address bar as a
// page menu: the favorite action, the folders kept, and, on this computer, the
// common folders and drives. The rows carry the identities the browser's click
// handler dispatches, and the remove buttons are trailing controls that do not
// choose their row.
func (w sftpPlaceBrowser) declareDirectoryPlaces(c ui.C, anchor ui.Box) {
	m := w.owner.messages.SFTPWindow
	local := w.view.places.local
	marks := w.directoryFavorites()
	count := len(marks)
	if local {
		count = len(w.owner.sftpLocalBookmarks)
	}
	label, glyph := m.FavoriteAdd, uint8(iconStar)
	if w.directoryFavorite(local) {
		label, glyph = m.FavoriteRemove, iconStarSolid
	}
	ui.Menu(c, anchor, &w.view.bookmarksOpen, func(menu *ui.MenuItems) {
		item := menu.Item(label).Icon(lucideIcons[glyph]).
			Disabled(w.folder(local).path == "" || local && w.localBusy() || !local && (w.view.busy || !w.connected() || w.session.host.CatalogID == ""))
		if item.Clicked() {
			c.After(func() { w.clickDirectoryPlace(hitSFTPBookmarkNew) })
		}
		menu.Separator()
		menu.Label(m.DirectoryFavorites)
		if count == 0 {
			menu.Item(w.owner.messages.Tools.BookmarksEmpty).Disabled(true)
		}
		for index := range count {
			id, remove, directory := hitSFTPBookmarkItemList.At(index), hitSFTPBookmarkRemoveList.At(index), ""
			if local {
				id, remove, directory = sftpLocalFavorites.At(index), sftpLocalFavoriteRemove.At(index), w.owner.sftpLocalBookmarks[index]
			} else {
				directory = marks[index].path
			}
			row := menu.Item(directory).Icon(lucideIcons[iconFolderOpen]).
				Disabled(w.navigationBusy() || !local && !w.connected())
			row.Trailing(24, func(canvas *gfx.Canvas, bounds gfx.Rect, theme gfx.Theme) {
				w.owner.paintIconButton(canvas, remove, iconClose, bounds, w.owner.messages.Tools.BookmarkRemove, false, iconButtonQuiet, w.owner.pal.surface)
			})
			if row.Clicked() {
				c.After(func() { w.clickDirectoryPlace(id) })
			}
		}
		if local {
			lastKind := sftpLocalPlaceKind(255)
			for index, place := range w.view.places.locations {
				kind := localPlaceHome
				if place.kind == localPlaceDrive {
					kind = localPlaceDrive
				}
				if kind != lastKind {
					menu.Separator()
					heading := m.CommonFolders
					if kind == localPlaceDrive {
						heading = m.LocalDrives
					}
					menu.Label(heading)
					lastKind = kind
				}
				label, glyph := place.path, uint8(iconHardDrive)
				switch place.kind {
				case localPlaceHome:
					label, glyph = m.LocalHome, uint8(iconFolderOpen)
				case localPlaceDesktop:
					label, glyph = m.LocalDesktop, uint8(iconDesktop)
				case localPlaceDownloads:
					label, glyph = m.LocalDownloads, uint8(iconDownload)
				case localPlaceDocuments:
					label, glyph = m.LocalDocuments, uint8(iconFile)
				case localPlacePictures:
					label, glyph = m.LocalPictures, uint8(iconFolderOpen)
				case localPlaceMusic:
					label, glyph = m.LocalMusic, uint8(iconFolderOpen)
				case localPlaceVideos:
					label, glyph = m.LocalVideos, uint8(iconFilm)
				case localPlaceMovies:
					label, glyph = m.LocalMovies, uint8(iconFilm)
				case localPlaceApplications:
					label, glyph = m.LocalApplications, uint8(iconHome)
				}
				id := sftpLocalPlaces.At(index)
				row := menu.Item(label).Icon(lucideIcons[glyph]).Disabled(w.navigationBusy())
				if row.Clicked() {
					c.After(func() { w.clickDirectoryPlace(id) })
				}
			}
		}
	})
}
