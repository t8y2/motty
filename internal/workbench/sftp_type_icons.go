package workbench

import (
	"errors"
	"path"
	"runtime"
	"strings"

	"github.com/vivaui/viva/app"
)

// A file's system icon is chosen by its extension, so every remote file of the
// same kind shares one slot and one question to the system. The icon is loaded
// behind the built-in glyph; a platform with no system icons (or an extension
// it does not know) keeps that glyph.

const (
	// maxTypeIcons bounds the extensions asked about. A listing with more kinds
	// than this is rare; the rest keep their glyph.
	maxTypeIcons = 128
	// maxTypeIconExt bounds a usable extension. A longer tail is not a file type.
	maxTypeIconExt = 16
)

// sftpTypeIconPath is an absolute path with the wanted extension. FileIcon is
// documented to answer with the type's icon for a path that does not exist, so
// nothing is created and a remote file needs no local copy.
func sftpTypeIconPath(ext string) string {
	if runtime.GOOS == "windows" {
		return `C:\motty-file-type\file` + ext
	}
	return "/tmp/motty-file-type/file" + ext
}

// validTypeIconExtension accepts the short, lower-case tails that name a file
// type: letters, digits, dot, underscore and hyphen. The whole name of a
// dotfile is not an extension.
func validTypeIconExtension(name, ext string) bool {
	if len(ext) < 2 || len(ext) > maxTypeIconExt || ext[0] != '.' {
		return false
	}
	if strings.EqualFold(name, ext) {
		return false
	}
	for _, r := range ext[1:] {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// loadTypeIcons gives every file row with a usable extension its shared icon
// slot and starts one load per extension not asked for yet. It runs on the
// window thread after a listing is put on screen.
func (state *State) loadTypeIcons(view *sftpView, rows []sftpRow) {
	if state.sftpTypeIcons == nil {
		state.sftpTypeIcons = make(map[string]*fileIconSlot)
	}
	var pending []*fileIconSlot
	for index := range rows {
		row := &rows[index]
		if row.entry.IsDir() || row.appIcon != nil {
			continue
		}
		slot, load := state.typeIconSlot(row.entry.Name)
		if slot == nil {
			continue
		}
		row.typeIcon = slot
		if load {
			pending = append(pending, slot)
		}
	}
	state.loadTypeIconSlots(view, pending)
}

// typeIconSlot returns the shared system-icon slot for a file name. The bool
// says a new request must be started; an in-flight or answered slot is reused.
func (state *State) typeIconSlot(name string) (*fileIconSlot, bool) {
	if state.sftpTypeIcons == nil {
		state.sftpTypeIcons = make(map[string]*fileIconSlot)
	}
	ext := strings.ToLower(path.Ext(name))
	// An application bundle has its own icon from its bundle path; a file that
	// merely ends in .app keeps the built-in glyph.
	if ext == ".app" || !validTypeIconExtension(name, ext) {
		return nil, false
	}
	slot := state.sftpTypeIcons[ext]
	if slot == nil {
		if len(state.sftpTypeIcons) >= maxTypeIcons {
			return nil, false
		}
		slot = &fileIconSlot{path: sftpTypeIconPath(ext)}
		state.sftpTypeIcons[ext] = slot
	}
	if slot.ready() || slot.loading || slot.failed {
		return slot, false
	}
	slot.loading = true
	return slot, true
}

// attachTypeIcon gives the transfer center the same system icon slot the SFTP
// file list uses. Both surfaces therefore follow the operating system's icon
// database together, or keep the same built-in fallback where it has none.
func (state *State) attachTypeIcon(name string, directory bool) *fileIconSlot {
	if directory {
		return nil
	}
	slot, load := state.typeIconSlot(name)
	if slot == nil {
		return nil
	}
	if load {
		state.loadTypeIconSlots(nil, []*fileIconSlot{slot})
	}
	return slot
}

func (state *State) loadTypeIconSlots(view *sftpView, pending []*fileIconSlot) {
	if len(pending) == 0 {
		return
	}
	load := state.options.FileIcon
	invalidate := state.options.Invalidate
	go func() {
		for _, slot := range pending {
			slot := slot
			bitmap, err := load(slot.path, appIconPixels)
			if errors.Is(err, app.ErrUnsupported) {
				// The platform has no system icons: every later answer would be
				// the same, so stop asking for the whole group.
				state.aiPostWithInvalidator(func(*State) {
					for _, slot := range pending {
						slot.loading, slot.failed = false, true
					}
					if view != nil {
						view.invalidateDetached()
					}
				}, invalidate)
				return
			}
			if err != nil {
				state.aiPostWithInvalidator(func(*State) {
					slot.loading, slot.failed = false, true
					if view != nil {
						view.invalidateDetached()
					}
				}, invalidate)
				continue
			}
			bitmap.ID = 1<<60 | appIconIDs.Add(1)
			if !state.aiPostWithInvalidator(func(*State) {
				slot.bitmap, slot.loading = bitmap, false
				if view != nil {
					view.invalidateDetached()
				}
			}, invalidate) {
				return
			}
		}
	}()
}
