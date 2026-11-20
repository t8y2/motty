package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func profileQuickLabel(profile sftpBookmark) string {
	if profile.name != "" {
		return profile.name
	}
	return profile.path
}

func profileQuickHint(profile sftpBookmark, syncLabel string) string {
	hint := profile.path
	if profile.local != "" {
		hint = fmt.Sprintf("%s ⇄ %s", profile.local, profile.path)
	}
	if profile.sync {
		hint += " · " + syncLabel
	}
	return hint
}

func (w *sftpWindow) closeProfileQuickMenu() {
	w.ui.profileQuickOpen = false
	w.ui.profileQuickFocus = false
}

func (w *sftpWindow) toggleProfileQuickMenu() {
	if w.ui.profileQuickOpen {
		w.closeProfileQuickMenu()
		return
	}
	w.cancelDrag()
	w.closeHeaderMenu()
	w.ui.menuOpen, w.view.bookmarksOpen = false, false
	w.ui.profileQuickOpen = true
	w.ui.profileQuickFocus = true
}

func (w *sftpWindow) clickProfileQuick(id gfx.ID) bool {
	if id == hitSFTPProfileQuick {
		w.activeLocal, w.view.local = true, true
		w.toggleProfileQuickMenu()
		return true
	}
	if id == hitSFTPProfileQuickDismiss {
		w.closeProfileQuickMenu()
		return true
	}
	// A page menu closes itself as it delivers the chosen row, so a row is
	// dispatched by the identity it carries, not by whether the menu still
	// reports itself open.
	if index, ok := id.In(hitSFTPProfileQuickItems); ok {
		profiles := w.visibleProfiles()
		w.closeProfileQuickMenu()
		if index < len(profiles) {
			w.openPairedBookmark(profiles[index])
		}
		return true
	}
	if w.ui.profileQuickOpen {
		if id == hitSFTPLocationProfiles {
			w.closeProfileQuickMenu()
			w.editBookmark(nil)
			return true
		}
		w.closeProfileQuickMenu()
		return false
	}
	return false
}

// declareProfileQuickMenu declares the profile quick menu as a page menu,
// hanging under the chevron that opened it. The rows keep the actions the
// window's handler dispatches.
func (w *sftpWindow) declareProfileQuickMenu(c ui.C) {
	if !w.ui.profileQuickOpen || w.bookmark.open || !w.ui.profileQuickBox.Valid() {
		return
	}
	m := w.owner.messages.SFTPWindow
	profiles := w.visibleProfiles()
	var rows menuRows
	rows.add(components.ContextMenuItem{Label: m.ProfileChoose, Disabled: true})
	if len(profiles) == 0 {
		rows.add(components.ContextMenuItem{Label: m.ProfileEmpty, Disabled: true})
	} else {
		for index, profile := range profiles {
			icon := lucideIcons[iconSFTPProfiles]
			if profile.sync {
				icon = lucideIcons[iconSFTPBrowseSync]
			}
			rows.add(components.ContextMenuItem{
				ID: hitSFTPProfileQuickItems.At(index), Label: profileQuickLabel(profile),
				Hint: profileQuickHint(profile, m.ProfileQuickSync), Icon: icon,
			})
		}
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitSFTPLocationProfiles, Label: m.BookmarkTitle, Icon: lucideIcons[iconSFTPProfiles]})
	w.owner.declareMenuOn(c, &w.ui.profileQuickOpen, w.ui.profileQuickBox, &rows, func(id gfx.ID) {
		w.clickProfileQuick(id)
	})
}

func (w *sftpWindow) profileQuickKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	result := gfx.EventResult{Handled: true, Repaint: true}
	if w.ui.profileQuickOpen {
		switch event.Key {
		case gfx.KeyEscape:
			if !event.Repeat {
				w.closeProfileQuickMenu()
				result.Focus = hitSFTPProfileQuick
				result.FocusVisible = true
			}
			return result, true
		case gfx.KeyTab:
			w.closeProfileQuickMenu()
			result.ContinueTraversal = true
			return result, true
		}
		return gfx.EventResult{}, false
	}
	if event.Target == hitSFTPProfileQuick && !event.Repeat && event.Modifiers == 0 && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace || event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		w.toggleProfileQuickMenu()
		return result, true
	}
	return gfx.EventResult{}, false
}
