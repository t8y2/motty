package workbench

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// On this computer an application bundle shows the icon its own program has, as
// the file manager does, not the folder it is on disk. The system answers with
// pixels, which takes a read of the bundle and the icon database, so the
// listing is shown at once with the stand-in glyph (see sftpFileIcon) and the
// icons arrive from a goroutine behind it.

const (
	// appIconPixels is the size asked for: 16 points on a 2x display, which a 1x
	// display draws scaled down.
	appIconPixels = 32
	// maxAppIcons bounds the icons asked for in one listing; the bundles past it
	// keep the glyph. A folder of applications is a few dozen.
	maxAppIcons = 256
)

// appIconIDs numbers the bitmaps so the renderer keeps what it uploaded for one.
// The high bit set here keeps them apart from the other pictures the window draws.
var appIconIDs atomic.Uint64

// fileIconSlot is the icon of one local application bundle. It is written and
// read on the window thread only; the goroutine that fetches it posts the
// answer. Rows are copied by value (a filtered list holds copies of the full
// one), so they share the slot by pointer and all see the answer.
type fileIconSlot struct {
	path    string
	modTime time.Time  // of the bundle when listed: an updated one is a different icon
	bitmap  gfx.Bitmap // empty until the system has answered
}

func (slot *fileIconSlot) ready() bool { return slot != nil && slot.bitmap.Width > 0 }

// isApplicationBundle reports an entry that is an application bundle: a folder
// whose name ends in .app, the convention the system itself goes by. A link to
// one is a link, whatever it points at.
func isApplicationBundle(name string, mode os.FileMode) bool {
	return mode.IsDir() && mode&os.ModeSymlink == 0 && strings.EqualFold(path.Ext(name), ".app")
}

// attachAppIconSlots gives each application bundle of a freshly scanned local
// listing the slot its icon will arrive in. It touches no State, so the scan
// goroutine may run it.
func attachAppIconSlots(dir string, rows []sftpRow) {
	count := 0
	for index := range rows {
		entry := rows[index].entry
		if !isApplicationBundle(entry.Name, entry.Mode) {
			continue
		}
		if count++; count > maxAppIcons {
			return
		}
		rows[index].appIcon = &fileIconSlot{path: filepath.Join(dir, entry.Name), modTime: entry.ModTime}
	}
}

// loadAppIcons starts fetching the icons of the listing now on screen. What the
// listing it replaces already had, for a bundle that has not changed, is carried
// over, so a refresh does not flash the glyph.
func (state *State) loadAppIcons(view *sftpView, previous, rows []sftpRow) {
	var known map[string]*fileIconSlot
	var pending []*fileIconSlot
	for index := range rows {
		slot := rows[index].appIcon
		if slot == nil {
			continue
		}
		if known == nil {
			known = make(map[string]*fileIconSlot)
			for old := range previous {
				if previous[old].appIcon.ready() {
					known[previous[old].entry.Name] = previous[old].appIcon
				}
			}
		}
		if old := known[rows[index].entry.Name]; old != nil && old.path == slot.path && old.modTime.Equal(slot.modTime) {
			slot.bitmap = old.bitmap
			continue
		}
		pending = append(pending, slot)
	}
	if len(pending) == 0 {
		return
	}
	load := state.options.FileIcon
	go func() {
		for _, slot := range pending {
			bitmap, err := load(slot.path, appIconPixels)
			if errors.Is(err, app.ErrUnsupported) {
				return // this system has none to give: every other answer would be the same
			}
			if err != nil {
				continue // this bundle keeps the glyph
			}
			bitmap.ID = 1<<60 | appIconIDs.Add(1)
			if !state.aiPost(func(*State) {
				slot.bitmap = bitmap
				view.invalidateDetached()
			}) {
				return
			}
		}
	}()
}

// declareSFTPRowIcon declares what leads a row: the system's icon for an
// application bundle once it has arrived, otherwise the glyph of its kind.
func declareSFTPRowIcon(f *ui.Frame, row *sftpRow, tint gfx.Color) {
	switch {
	case row.appIcon.ready():
		f.Image(row.appIcon.bitmap)
	case row.icon != nil:
		f.Icon(*row.icon).Color(tint)
	}
}
