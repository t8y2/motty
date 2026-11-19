package workbench

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// profileHoverDelay is how long the pointer rests on a profiles button before
// the preview opens, so a press that follows the pointer does not miss it.
const profileHoverDelay = 150 * time.Millisecond

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
	if w.ui.profileQuickOpen {
		if index, ok := id.In(hitSFTPProfileQuickItems); ok {
			profiles := w.visibleProfiles()
			w.closeProfileQuickMenu()
			if index < len(profiles) {
				w.openPairedBookmark(profiles[index])
			}
			return true
		}
		if id == hitSFTPLocationProfiles {
			w.closeProfileQuickMenu()
			w.editBookmark(nil)
			return true
		}
		w.closeProfileQuickMenu()
		return false
	}
	if index, ok := id.In(hitSFTPProfileHoverItems); ok {
		profiles := w.visibleProfiles()
		w.ui.profileHoverOpen = false
		if index < len(profiles) {
			w.openPairedBookmark(profiles[index])
		}
		return true
	}
	return false
}

// declareProfileQuickMenu declares the profile quick menu as a page menu,
// hanging under the chevron that opened it. The rows keep the actions the
// window's handler dispatches.
func (w *sftpWindow) declareProfileQuickMenu(c ui.C) {
	if !w.ui.profileQuickOpen || w.bookmark.open {
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
	w.owner.declareMenuAt(c, &w.ui.profileQuickOpen, w.ui.profileQuickAnchor, false, hitSFTPProfileQuickDismiss, &rows, func(id gfx.ID) {
		w.clickProfileQuick(id)
	})
}

// declareProfileHover declares the profile preview that follows the pointer on
// the profiles button: a page popover whose rows carry the identities the
// window's handler chooses. It also decides whether the card is open, from what
// the pointer and the keyboard rest on.
func (w *sftpWindow) declareProfileHover(c ui.C) {
	if w.bookmark.open || w.view.bookmarksOpen || w.ui.menuOpen || w.headerMenuOpen() || w.view.dialog != sftpDialogNone || w.closeConfirm || w.externalConflict != 0 {
		w.ui.profileHoverOpen = false
		return
	}
	hovered, focused := c.Canvas().Hovered(), c.Canvas().Focused()
	onTrigger := hovered == hitSFTPLocationProfiles || hovered == hitSFTPProfileQuick
	onPreview := hovered == hitSFTPProfileHover
	if _, ok := hovered.In(hitSFTPProfileHoverItems); ok {
		onPreview = true
	}
	if _, ok := focused.In(hitSFTPProfileHoverItems); ok {
		onPreview = true
	}
	if onTrigger {
		// The preview waits for the pointer to rest, so moving to the button to
		// press it does not open a card that would take the press.
		if !w.ui.profileHoverArmed || w.ui.profileHoverTrigger != hovered {
			w.ui.profileHoverArmed, w.ui.profileHoverSince = true, c.Canvas().Now()
			w.ui.profileHoverOpen = false
		} else if c.Canvas().Now()-w.ui.profileHoverSince >= profileHoverDelay {
			w.ui.profileHoverOpen = true
		}
		w.ui.profileHoverTrigger = hovered
	} else {
		w.ui.profileHoverArmed = false
		if w.ui.profileHoverOpen && !onPreview {
			w.ui.profileHoverOpen = false
		}
	}
	if !w.ui.profileHoverOpen {
		return
	}
	m := w.owner.messages.SFTPWindow
	profiles := w.visibleProfiles()
	shown := min(len(profiles), 5)
	anchor := w.ui.profileHoverBox
	if w.ui.profileHoverTrigger == hitSFTPProfileQuick {
		anchor = w.ui.profileQuickBox
	}
	if !anchor.Valid() {
		return
	}
	spec := ui.PopoverSpec{
		Scope: hitSFTPProfileHover, Side: components.PopoverRight, Align: ui.Start,
		Offset: 1, Width: 320, KeepOpenOnAnchor: true, ConsumeOutsidePress: false,
		Title: m.ProfileQuickTitle, Description: m.ProfileQuickDescription,
	}
	ui.PopoverWith(c, anchor, spec, &w.ui.profileHoverOpen, func() {
		if shown == 0 {
			ui.Text(c, m.ProfileEmpty).FontSize(w.owner.Theme().Metrics.FontSizeS).Color(w.owner.pal.muted)
			return
		}
		for index := 0; index < shown; index++ {
			profile := profiles[index]
			icon := uint8(iconSFTPProfiles)
			if profile.sync {
				icon = iconSFTPBrowseSync
			}
			row := ui.Row(c.KeyInt(index)).Height(34).PadX(4).Gap(8).Items(ui.Center).
				HitWith(hitSFTPProfileHoverItems.At(index), profileQuickLabel(profile), gfx.HitOptions{Role: gfx.RoleButton, Hint: profileQuickHint(profile, m.ProfileQuickSync)})
			row.Children(func() {
				ui.Icon(c, lucideIcons[icon]).Size(16, 16).Color(w.owner.pal.muted)
				ui.Text(c, profileQuickLabel(profile)).FontSize(w.owner.Theme().Metrics.FontSizeM).Weight(gfx.WeightMedium).Color(w.owner.pal.text).Ellipsis().Grow(1)
			})
		}
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
	if event.Target == hitSFTPProfileHover && event.Key == gfx.KeyEscape {
		w.ui.profileHoverOpen = false
		return result, true
	}
	if _, ok := event.Target.In(hitSFTPProfileHoverItems); ok {
		if event.Key == gfx.KeyEscape {
			w.ui.profileHoverOpen = false
			result.Focus = w.ui.profileHoverTrigger
			if result.Focus.IsZero() {
				result.Focus = hitSFTPLocationProfiles
			}
			result.FocusVisible = true
			return result, true
		}
		if !event.Repeat && event.Modifiers == 0 && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
			w.clickProfileQuick(event.Target)
			return result, true
		}
	}
	return gfx.EventResult{}, false
}
