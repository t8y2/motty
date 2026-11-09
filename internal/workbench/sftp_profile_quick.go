package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
	w.ui.profileQuickScroll = components.MenuScroll{BarID: hitSFTPProfileQuickBar}
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
	w.ui.profileQuickScroll = components.MenuScroll{BarID: hitSFTPProfileQuickBar}
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

func (w *sftpWindow) paintProfileQuickMenu(canvas *gfx.Canvas) {
	theme, m := w.owner.Theme(), w.owner.messages.SFTPWindow
	profiles := w.visibleProfiles()
	height := float32(min(360, 40+len(profiles)*32+42))
	anchor := w.ui.profileQuickAnchor
	bounds := gfx.Rect{X: anchor.X + anchor.Width - 300, Y: anchor.Y + anchor.Height + 4, Width: 300, Height: height}
	if bounds.Y+bounds.Height > w.ui.size.Height-8 {
		bounds.Y = max(8, anchor.Y-bounds.Height-4)
	}
	bounds.X = max(8, min(bounds.X, w.ui.size.Width-bounds.Width-8))
	menu := components.BeginDropdownMenu(canvas, theme, bounds, components.DropdownMenuOptions{
		Layer: popupLayer, Scope: hitSFTPProfileQuickMenu, DismissID: hitSFTPProfileQuickDismiss,
		Viewport: gfx.Rect{Width: w.ui.size.Width, Height: w.ui.size.Height}, Scroll: &w.ui.profileQuickScroll,
	})
	menu.AddLabel(m.ProfileChoose, true)
	if len(profiles) == 0 {
		menu.AddItem(components.DropdownMenuItem{Label: m.ProfileEmpty, Disabled: true})
	} else {
		for index, profile := range profiles {
			icon := lucideIcons[iconSFTPProfiles]
			if profile.sync {
				icon = lucideIcons[iconSFTPBrowseSync]
			}
			menu.AddItem(components.DropdownMenuItem{
				ID:    hitSFTPProfileQuickItems.At(index),
				Label: profileQuickLabel(profile),
				Hint:  profileQuickHint(profile, m.ProfileQuickSync),
				Shortcut: func() string {
					if profile.sync {
						return m.ProfileQuickSync
					}
					return ""
				}(),
				Icon: icon,
			})
		}
	}
	menu.AddSeparator()
	menu.AddItem(components.DropdownMenuItem{
		ID: hitSFTPLocationProfiles, Label: m.BookmarkTitle, Icon: lucideIcons[iconSFTPProfiles],
	})
	if w.ui.profileQuickFocus {
		focus := hitSFTPLocationProfiles
		if len(profiles) > 0 {
			focus = hitSFTPProfileQuickItems.At(0)
		}
		canvas.RequestFocus(focus)
		w.ui.profileQuickFocus = false
	}
	menu.End()
}

func (w *sftpWindow) paintProfileQuickPreview(canvas *gfx.Canvas, viewport gfx.Rect) {
	if w.bookmark.open || w.view.bookmarksOpen || w.ui.menuOpen || w.headerMenuOpen() || w.view.dialog != sftpDialogNone || w.closeConfirm || w.externalConflict != 0 {
		w.ui.profileHoverOpen = false
		return
	}
	hovered, focused := canvas.Hovered(), canvas.Focused()
	onTrigger := hovered == hitSFTPLocationProfiles || hovered == hitSFTPProfileQuick
	onPreview := hovered == hitSFTPProfileHover
	if _, ok := hovered.In(hitSFTPProfileHoverItems); ok {
		onPreview = true
	}
	if _, ok := focused.In(hitSFTPProfileHoverItems); ok {
		onPreview = true
	}
	if onTrigger {
		w.ui.profileHoverOpen = true
		w.ui.profileHoverTrigger = hovered
		if hovered == hitSFTPProfileQuick {
			w.ui.profileHoverAnchor = w.ui.profileQuickAnchor
		}
	} else if w.ui.profileHoverOpen && !onPreview {
		w.ui.profileHoverOpen = false
	}
	if !w.ui.profileHoverOpen {
		return
	}
	theme, m := w.owner.Theme(), w.owner.messages.SFTPWindow
	profiles := w.visibleProfiles()
	shown := min(len(profiles), 5)
	bodyHeight := max(float32(36), float32(shown)*36)
	card := components.BeginHoverCard(canvas, theme, viewport, w.ui.profileHoverAnchor, components.HoverCardOptions{
		Layer: tooltipLayer, ID: hitSFTPProfileHover, Side: components.HoverCardRight,
		Align: components.HoverCardAlignStart, SideOffset: 1, ContentSize: gfx.Size{Width: 320}, BodyHeight: bodyHeight,
	})
	layout := card.Begin(m.ProfileQuickTitle, m.ProfileQuickDescription)
	if shown == 0 {
		canvas.LabelCenter(m.ProfileEmpty, layout.Body, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Color: theme.MutedForeground})
	} else {
		for index := 0; index < shown; index++ {
			profile := profiles[index]
			id := hitSFTPProfileHoverItems.At(index)
			row := gfx.Rect{
				X: layout.Body.X, Y: layout.Body.Y + float32(index)*36,
				Width: layout.Body.Width, Height: 34,
			}
			if hovered == id || focused == id {
				canvas.Rounded(row, theme.Metrics.RadiusItem(), theme.Accent)
			}
			icon := uint8(iconSFTPProfiles)
			if profile.sync {
				icon = iconSFTPBrowseSync
			}
			canvas.DrawIcon(lucideIcons[icon], gfx.Rect{X: row.X + 4, Y: row.Y + 9, Width: 16, Height: 16}, theme.MutedForeground)
			nameWidth := max(0, row.Width-28)
			name := canvas.FitLine(profileQuickLabel(profile), theme.Metrics.FontSizeM, gfx.WeightMedium, nameWidth)
			canvas.Label(name, gfx.Rect{X: row.X + 26, Y: row.Y, Width: nameWidth, Height: row.Height}, gfx.TextStyle{
				Size: theme.Metrics.FontSizeM, Weight: gfx.WeightMedium, Color: theme.Foreground,
			})
			canvas.HitWithOptions(id, row, profileQuickLabel(profile), gfx.HitOptions{
				Role: gfx.RoleButton, Hint: profileQuickHint(profile, m.ProfileQuickSync),
			})
		}
	}
	card.End()
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
		if next, ok := w.ui.profileQuickScroll.Navigate(event.Key, event.Target, true); ok {
			result.Focus, result.FocusVisible = next, true
			return result, true
		}
		if !event.Repeat && event.Modifiers == 0 && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
			if !w.clickProfileQuick(event.Target) {
				w.closeProfileQuickMenu()
			}
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
