package workbench

import (
	"errors"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

var (
	hitSFTPBrowseSync          = tree.ID(sftpWindowHits, "sync-browsing")
	hitSFTPBookmarkNew         = tree.ID(sftpHits, "bookmark-new")
	hitSFTPBookmarkSave        = tree.ID(sftpWindowHits, "bookmark-save")
	hitSFTPBookmarkCancel      = tree.ID(sftpWindowHits, "bookmark-cancel")
	hitSFTPBookmarkScope       = tree.ID(sftpWindowHits, "bookmark-editor")
	hitSFTPBookmarkFields      = tree.Sub(sftpWindowHits, tree.Name("bookmark-fields"))
	hitSFTPBookmarkBrowse      = tree.ID(sftpWindowHits, "bookmark-browse-local")
	hitSFTPLocationProfiles    = tree.ID(sftpWindowHits, "location-profiles")
	hitSFTPProfileOpen         = tree.ID(sftpWindowHits, "profile-open")
	hitSFTPProfileScope        = tree.ID(sftpWindowHits, "profile-scope-select")
	hitSFTPProfileScopeDismiss = tree.ID(sftpWindowHits, "profile-scope-dismiss")
	hitSFTPProfileSite         = tree.ID(sftpWindowHits, "profile-site")
	hitSFTPProfileShared       = tree.ID(sftpWindowHits, "profile-shared")
	hitSFTPBookmarkSync        = tree.ID(sftpWindowHits, "bookmark-sync")
	hitSFTPProfileNew          = tree.ID(sftpWindowHits, "profile-new")
	hitSFTPProfileDelete       = tree.ID(sftpWindowHits, "profile-delete")
	hitSFTPProfileItems        = tree.Sub(sftpWindowHits, tree.Name("profile-items"))
	hitSFTPProfileList         = tree.ID(sftpWindowHits, "profile-list")
	hitSFTPProfileBar          = tree.ID(sftpWindowHits, "profile-scroll")
	hitSFTPProfileQuick        = tree.ID(sftpWindowHits, "profile-quick")
	hitSFTPProfileQuickDismiss = tree.ID(sftpWindowHits, "profile-quick-dismiss")
	hitSFTPProfileQuickMenu    = tree.ID(sftpWindowHits, "profile-quick-menu")
	hitSFTPProfileQuickItems   = tree.Sub(sftpWindowHits, tree.Name("profile-quick-items"))
	hitSFTPProfileQuickBar     = tree.ID(sftpWindowHits, "profile-quick-scroll")
)

type sftpBookmarkEditor struct {
	open, editing, shared, sync bool
	focusName                   bool
	original                    sftpBookmark
	fields                      [3]text.Editor // name, local directory, remote directory
	// fieldRows and fieldTexts are the page fields over fields, bound when first declared.
	fieldRows  [3]ui.FormFieldState
	fieldTexts [3]ui.Field
	fieldBound [3]bool
	// scopeRow is the “Available to” field around the scope trigger, as the
	// editor painted it before the profile list became a page scroll area.
	scopeRow           ui.FormFieldState
	viewport           gfx.Rect
	errorText          string
	errorTextSelection ui.TextSelectionState // what the user selected of errorText, to copy it
	scroll             *ui.ScrollState
	// emptyDraw paints the centered empty state over the list's viewport; it is
	// made once so a paint does not allocate a closure.
	emptyDraw             func(*ui.DrawContext)
	scopeOpen, focusScope bool
}

func cleanBookmarkName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) > 256 || strings.ContainsFunc(name, unicode.IsControl) {
		return ""
	}
	return name
}

func validBookmarkLocal(directory string) bool {
	return directory == "" || filepath.IsAbs(directory) && len(directory) <= 2048 && !strings.ContainsFunc(directory, unicode.IsControl)
}

// Profiles are shared configuration. Preserve a path from another operating
// system on restore; native validation still applies before opening or saving.
func validSavedBookmarkLocal(directory string) bool {
	if directory == "" {
		return true
	}
	if len(directory) > 2048 || strings.ContainsFunc(directory, unicode.IsControl) {
		return false
	}
	if strings.HasPrefix(directory, `\\`) {
		parts := strings.Split(strings.TrimPrefix(directory, `\\`), `\`)
		return len(parts) >= 2 && parts[0] != "" && parts[1] != ""
	}
	if len(directory) >= 3 && (directory[0] >= 'A' && directory[0] <= 'Z' || directory[0] >= 'a' && directory[0] <= 'z') && directory[1] == ':' && (directory[2] == '\\' || directory[2] == '/') {
		return true
	}
	return filepath.IsAbs(directory) || path.IsAbs(directory)
}

// bookmarkBelongsTo is the one rule for which saved location profiles a window
// may offer: this connection's own, and the shared ones.
func bookmarkBelongsTo(bookmark sftpBookmark, session *Session) bool {
	return bookmark.host == session.host.CatalogID || bookmark.shared
}

func (state *State) sftpHostBookmarks(session *Session) []sftpBookmark {
	var bookmarks []sftpBookmark
	for _, bookmark := range state.sftpBookmarks {
		if bookmarkBelongsTo(bookmark, session) {
			bookmarks = append(bookmarks, bookmark)
		}
	}
	return bookmarks
}

func (state *State) bookmarkScopeSize(host string, shared bool) int {
	count := 0
	for _, bookmark := range state.sftpBookmarks {
		if bookmark.shared == shared && (shared || bookmark.host == host) {
			count++
		}
	}
	return count
}

// profileVisible is the one rule for which saved location profiles a window
// offers. visibleProfiles and visibleProfileCount both ask it, so the drawn
// list and the count taken while painting a button cannot disagree.
func profileVisible(bookmark sftpBookmark) bool {
	return bookmark.name != "" || bookmark.local != "" || bookmark.shared
}

func (w *sftpWindow) visibleProfiles() []sftpBookmark {
	var profiles []sftpBookmark
	for _, bookmark := range w.owner.sftpHostBookmarks(&w.session) {
		if profileVisible(bookmark) {
			profiles = append(profiles, bookmark)
		}
	}
	return profiles
}

// visibleProfileCount counts the same profiles without building the slice: the
// pane header asks while it paints, and painting must not allocate.
func (w *sftpWindow) visibleProfileCount() int {
	count := 0
	for _, bookmark := range w.owner.sftpBookmarks {
		if bookmarkBelongsTo(bookmark, &w.session) && profileVisible(bookmark) {
			count++
		}
	}
	return count
}

func (w *sftpWindow) selectProfile(bookmark sftpBookmark) {
	b := &w.bookmark
	b.original, b.editing, b.shared, b.sync = bookmark, true, bookmark.shared, bookmark.sync
	name := bookmark.name
	if name == "" {
		name = path.Base(bookmark.path)
	}
	for i, value := range [3]string{name, bookmark.local, bookmark.path} {
		b.fields[i] = *text.NewEditor(value)
	}
	b.errorText = ""
}

func (w *sftpWindow) currentBookmark() *sftpBookmark {
	for i := range w.owner.sftpBookmarks {
		bookmark := &w.owner.sftpBookmarks[i]
		if (bookmark.name != "" || bookmark.local != "" || bookmark.shared) && (bookmark.host == w.session.host.CatalogID || bookmark.shared) && bookmark.path == w.view.path && (bookmark.local == "" || bookmark.local == w.view.lfolder.path) {
			return bookmark
		}
	}
	return nil
}

func (w *sftpWindow) editBookmark(bookmark *sftpBookmark) {
	if w.browse.pending != nil {
		return
	}
	w.cancelDrag()
	w.closeHeaderMenu()
	w.closeProfileQuickMenu()
	w.view.bookmarksOpen, w.ui.menuOpen = false, false
	b := &w.bookmark
	*b = sftpBookmarkEditor{open: true, shared: w.session.host.CatalogID == ""}
	values := [3]string{path.Base(w.view.path), w.view.lfolder.path, w.view.path}
	if bookmark != nil {
		b.original, b.editing, b.shared, b.sync = *bookmark, true, bookmark.shared, bookmark.sync
		values = [3]string{bookmark.name, bookmark.local, bookmark.path}
		if values[0] == "" {
			values[0] = path.Base(bookmark.path)
		}
	}
	for i, value := range values {
		b.fields[i] = *text.NewEditor(value)
	}
}

func (w *sftpWindow) saveBookmark() {
	b := &w.bookmark
	if !b.open {
		return
	}
	for i := range b.fields {
		if _, composing := b.fields[i].Composition(); composing {
			return
		}
	}
	name := cleanBookmarkName(b.fields[0].String())
	local, remote := strings.TrimSpace(b.fields[1].String()), strings.TrimSpace(b.fields[2].String())
	if name == "" || !validBookmarkLocal(local) || b.sync && local == "" || !validSyncDirectory(remote) || len(remote) > 2048 {
		b.errorText = w.owner.messages.SFTPWindow.BookmarkInvalid
		return
	}
	if local != "" {
		local = filepath.Clean(local)
	}
	bookmark := sftpBookmark{host: w.session.host.CatalogID, name: name, local: local, path: path.Clean(remote), shared: b.shared, sync: b.sync}
	if b.shared {
		bookmark.host = ""
	}
	if bookmark.host == "" && !bookmark.shared {
		b.errorText = w.owner.messages.SFTPWindow.BookmarkInvalid
		return
	}
	index := -1
	if b.editing {
		index = slices.Index(w.owner.sftpBookmarks, b.original)
	}
	if slices.ContainsFunc(w.owner.sftpBookmarks, func(other sftpBookmark) bool {
		return other.host == bookmark.host && other.shared == bookmark.shared && other.name == bookmark.name && (!b.editing || other != b.original)
	}) {
		b.errorText = w.owner.messages.SFTPWindow.ProfileNameExists
		return
	}
	changingScope := index >= 0 && (b.original.shared != bookmark.shared || b.original.host != bookmark.host)
	if (index < 0 || changingScope) && w.owner.bookmarkScopeSize(bookmark.host, bookmark.shared) >= maxBookmarksPerHost {
		b.errorText = w.owner.messages.SFTPWindow.BookmarkLimit
		return
	}
	if index >= 0 {
		w.owner.sftpBookmarks[index] = bookmark
	} else {
		if w.owner.bookmarkScopeSize(bookmark.host, bookmark.shared) >= maxBookmarksPerHost || len(w.owner.sftpBookmarks) >= maxBookmarks {
			b.errorText = w.owner.messages.SFTPWindow.BookmarkLimit
			return
		}
		w.owner.sftpBookmarks = append(w.owner.sftpBookmarks, bookmark)
	}
	w.owner.save()
	b.original, b.editing, b.errorText = bookmark, true, ""
	if w.owner.options.Invalidate != nil {
		w.owner.options.Invalidate()
	}
}

func (w *sftpWindow) removeWindowBookmark(bookmark sftpBookmark) {
	if index := slices.Index(w.owner.sftpBookmarks, bookmark); index >= 0 {
		w.owner.sftpBookmarks = slices.Delete(w.owner.sftpBookmarks, index, index+1)
		w.owner.save()
	}
}

func (w *sftpWindow) clickBookmarkEditor(id gfx.ID) {
	switch id {
	case hitSFTPBookmarkCancel:
		w.bookmark.open = false
	case hitSFTPBookmarkSave:
		w.saveBookmark()
	case hitSFTPProfileOpen:
		w.openProfileDraft()
	case hitSFTPProfileNew:
		w.bookmark.editing, w.bookmark.focusName = false, true
		w.bookmark.fields[0] = text.Editor{}
		w.bookmark.errorText = ""
	case hitSFTPProfileDelete:
		if w.bookmark.editing {
			w.removeWindowBookmark(w.bookmark.original)
			w.bookmark.editing = false
			w.bookmark.fields[0] = text.Editor{}
		}
	case hitSFTPProfileScope:
		w.bookmark.scopeOpen = !w.bookmark.scopeOpen
	case hitSFTPProfileScopeDismiss:
		w.bookmark.scopeOpen = false
		w.bookmark.focusScope = true
	case hitSFTPProfileSite, hitSFTPProfileShared:
		if id == hitSFTPProfileSite && w.session.host.CatalogID == "" {
			return
		}
		w.bookmark.shared = id == hitSFTPProfileShared
		w.bookmark.scopeOpen = false
		w.bookmark.focusScope = true
		w.bookmark.errorText = ""
	case hitSFTPBookmarkSync:
		w.bookmark.sync = !w.bookmark.sync
	case hitSFTPBookmarkBrowse:
		directory, err := w.owner.options.OpenDirectory(app.OpenDirectoryOptions{Title: w.owner.messages.SFTPWindow.BookmarkLocal})
		if err == nil {
			w.bookmark.fields[1] = *text.NewEditor(directory)
		} else if !errors.Is(err, app.ErrCanceled) {
			w.notices.show(w.owner.messages.SFTPWindow.BookmarkTitle, err.Error(), components.ToastError)
		}
	default:
		if index, ok := id.In(hitSFTPProfileItems); ok {
			profiles := w.visibleProfiles()
			if index < len(profiles) {
				w.selectProfile(profiles[index])
			}
		}
	}
}

func (w *sftpWindow) openProfileDraft() {
	b := &w.bookmark
	for i := range b.fields {
		if _, composing := b.fields[i].Composition(); composing {
			return
		}
	}
	local, remote := strings.TrimSpace(b.fields[1].String()), strings.TrimSpace(b.fields[2].String())
	if !validBookmarkLocal(local) || b.sync && local == "" || !validSyncDirectory(remote) || len(remote) > 2048 {
		b.errorText = w.owner.messages.SFTPWindow.BookmarkInvalid
		return
	}
	if w.navigationBusy() || !w.connected() {
		return
	}
	b.open = false
	w.openPairedBookmark(sftpBookmark{local: local, path: path.Clean(remote), sync: b.sync})
}

// declareBookmarkDialog declares the saved profile editor as a dialog of the
// page that hosts the window: the standalone window's page, or the workbench
// page's file tab. Its fields and buttons keep the application's identities and
// are painted into the slots the dialog leaves.
func (w *sftpWindow) declareBookmarkDialog(c ui.C, viewport gfx.Rect) {
	b, theme, m := &w.bookmark, w.owner.Theme(), w.owner.messages.SFTPWindow
	bodyHeight := min(float32(400), max(float32(292), viewport.Height-208))
	if b.errorText != "" {
		bodyHeight += 24
	}
	focus := hitSFTPBookmarkFields.At(2)
	if w.activeLocal {
		focus = hitSFTPBookmarkFields.At(1)
	}
	if b.focusName {
		focus, b.focusName = hitSFTPBookmarkFields.At(0), false
	}
	if b.focusScope {
		focus, b.focusScope = hitSFTPProfileScope, false
	}
	// The surface is as tall as the body the editor asks for; the dialog layout
	// adds the header and footer around it.
	b.viewport = viewport
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.BookmarkTitle, Description: m.BookmarkDescription, Width: 760,
		Height: dialogContentHeight(c.Canvas(), theme, viewport, 760, m.BookmarkDescription, bodyHeight),
		Layer:  dialogLayer, Scope: hitSFTPBookmarkScope, CloseID: hitSFTPBookmarkCancel, InitialFocusID: focus,
	})
	dialog.Body(func() {
		w.declareBookmarkBody(c)
	})
	dialog.Footer(func() {
		row := ui.Row(c).Fill().Gap((*c.Theme()).Metrics.GapItem).Justify(ui.End).Items(ui.Center)
		row.Children(func() {
			ui.Button(c, w.owner.messages.Cancel).Secondary().Hit(hitSFTPBookmarkCancel, w.owner.messages.Cancel).Shrink(0)
			ui.Button(c, m.ProfileOpen).Hit(hitSFTPProfileOpen, m.ProfileOpen).Shrink(0)
		})
	})
}

// declareBookmarkBody declares the editor's fields, the actions beside them, the
// scope choice and the profile list. They are page controls now: the window's
// click handler keeps their identities, and the page owns their keys and wheel.
func (w *sftpWindow) declareBookmarkBody(c ui.C) {
	b, theme, m := &w.bookmark, *c.Theme(), w.owner.messages.SFTPWindow
	canvas := c.Canvas()
	column := ui.Column(c).Fill().Gap(8)
	column.Children(func() {
		ui.Row(c).Height(56).Gap(8).Items(ui.End).Children(func() {
			w.declareBookmarkField(c, 1).Grow(1)
			w.owner.declareIconButton(c, hitSFTPBookmarkBrowse, iconFolderOpen, m.BookmarkChooseLocal, false, iconButtonQuiet).Size(32, 32)
		})
		w.declareBookmarkField(c, 2)
		ui.Row(c).Height(56).Gap(8).Items(ui.End).Children(func() {
			w.declareBookmarkField(c, 0).Grow(1)
			scopeWidth := max(canvas.TextWidth(m.ProfileSite, theme.Metrics.FontSizeM), canvas.TextWidth(m.ProfileShared, theme.Metrics.FontSizeM)) + 36
			value := m.ProfileSite
			if b.shared {
				value = m.ProfileShared
			}
			b.scopeRow.For = hitSFTPProfileScope
			scopeField := ui.FormField(c, &b.scopeRow, m.ProfileScope).
				Width(max(132, scopeWidth)).Height(56).Shrink(0)
			scopeField.Children(func() {
				trigger := ui.Button(c, value).Small().Outline().Hit(hitSFTPProfileScope, value).Height(32)
				w.declareBookmarkScopeMenu(c, trigger)
			})
			room := func(label string) float32 { return max(56, canvas.TextWidth(label, theme.Metrics.FontSizeM)+24) }
			bookmarkButtonRoom(c, room(m.ProfileNew), func() { ui.Button(c, m.ProfileNew).Small().Outline().Hit(hitSFTPProfileNew, m.ProfileNew).FillWidth() })
			bookmarkButtonRoom(c, room(m.BookmarkSave), func() {
				ui.Button(c, m.BookmarkSave).Small().ButtonRecipe(darkButtonRecipe).Hit(hitSFTPBookmarkSave, m.BookmarkSave).FillWidth()
			})
			bookmarkButtonRoom(c, room(m.ProfileDelete), func() {
				ui.Button(c, m.ProfileDelete).Small().Destructive().Disabled(!b.editing).Hit(hitSFTPProfileDelete, m.ProfileDelete).FillWidth()
			})
		})
		ui.Row(c).Height(32).Items(ui.Center).Children(func() {
			ui.Checkbox(c, w.owner.pageFlag(b.sync), m.ProfileSync).Hit(hitSFTPBookmarkSync, m.ProfileSync).Grow(1)
		})
		w.declareBookmarkProfileList(c)
		if b.errorText != "" {
			ui.TextSelectionArea(c, &b.errorTextSelection).Children(func() {
				ui.Text(c, b.errorText).FontSize(theme.Metrics.FontSizeS).Color(w.owner.pal.danger).Ellipsis().Height(24).LineHeight(24)
			})
		}
	})
}

// declareBookmarkScopeMenu declares the scope choice's popup under its trigger:
// this connection's profiles, or the shared ones.
func (w *sftpWindow) declareBookmarkScopeMenu(c ui.C, trigger ui.Box) {
	b, m := &w.bookmark, w.owner.messages.SFTPWindow
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitSFTPProfileSite, Label: m.ProfileSite, Kind: components.ContextMenuRadio, Selected: !b.shared, Disabled: w.session.host.CatalogID == ""})
	rows.add(components.ContextMenuItem{ID: hitSFTPProfileShared, Label: m.ProfileShared, Kind: components.ContextMenuRadio, Selected: b.shared})
	ui.Menu(c, trigger, &b.scopeOpen, func(menu *ui.MenuItems) {
		rows.declare(c, menu, func(id gfx.ID) { w.clickBookmarkEditor(id) })
	})
}

// declareBookmarkProfileList declares the saved profiles as a page scroll area:
// one row each, named by the identity the editor's handler knows. Scrolling,
// the wheel and the scrollbar are the page's.
func (w *sftpWindow) declareBookmarkProfileList(c ui.C) {
	b, theme, m := &w.bookmark, *c.Theme(), w.owner.messages.SFTPWindow
	profiles := w.visibleProfiles()
	// The archive list keeps the border and the two-pixel inset the editor was
	// painted with, so the list does not sit bare inside the dialog. The border
	// eats one pixel of the inset on each side.
	frame := ui.Column(c).Grow(1).Pad(1).Background(theme.Background).
		Radius(theme.Metrics.RadiusItem()).Border(1, theme.Border.Mix(theme.MutedForeground, .25))
	frame.Children(func() {
		area := ui.Scroll(c).Grow(1).AlwaysScrollbars().ScrollbarIDs(hitSFTPProfileBar, gfx.ID{}).ScrollbarLabels(m.BookmarkTitle, "")
		b.scroll = area.ScrollState()
		if len(profiles) == 0 {
			if b.emptyDraw == nil {
				b.emptyDraw = func(d *ui.DrawContext) {
					d.Canvas.LabelCenter(m.ProfileEmpty, d.Rect, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Color: theme.MutedForeground})
				}
			}
			area = area.DrawOver(b.emptyDraw)
		}
		area.Children(func() {
			if len(profiles) == 0 {
				return
			}
			for index, profile := range profiles {
				name := profile.name
				if name == "" {
					name = profile.path
				}
				description := profile.path
				if profile.local != "" {
					description = elideMiddleRunes(profile.local, 34) + "  ⇄  " + elideMiddleRunes(profile.path, 34)
				}
				if profile.sync {
					description += " · " + m.ProfileQuickSync
				}
				selected := b.editing && b.original == profile
				// The row is inset four pixels from the list's left and keeps the
				// scrollbar's room on the right, as the painted row did.
				row := ui.Row(c.KeyInt(index)).Height(52).MarginSides(0, 14, 0, 4).PadX(8).Gap(8).Items(ui.Center).
					HitWith(hitSFTPProfileItems.At(index), name, gfx.HitOptions{Role: gfx.RoleOption, Selected: selected}).
					Hover(ui.Look{}.Background(theme.Accent))
				if selected {
					row = row.Background(theme.Accent)
				}
				row.Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, name).FontSize(theme.Metrics.FontSizeM).Weight(gfx.WeightMedium).Color(theme.Foreground).Ellipsis()
						ui.Text(c, description).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis()
					})
					badge := m.ProfileSite
					background, foreground, border := theme.Background, theme.Foreground, theme.Border
					if profile.shared {
						badge, background, foreground = m.ProfileShared, theme.Secondary, theme.SecondaryForeground
					}
					ui.Text(c, badge).FontSize(theme.Metrics.FontSizeS).Weight(gfx.WeightMedium).
						Color(foreground).Background(background).Border(1, border).Radius(999).
						PadSides(6, 6, 2, 2).Shrink(0)
				})
			}
		})
	})
}

// declareBookmarkField declares text field i of the editor (name, local directory, remote
// directory): a labelled row around a page text bound to the editor, whose title is the
// field's label.
func (w *sftpWindow) declareBookmarkField(c ui.C, i int) ui.Box {
	b, m := &w.bookmark, w.owner.messages.SFTPWindow
	label := [3]string{m.BookmarkName, m.BookmarkLocal, m.BookmarkRemote}[i]
	id := hitSFTPBookmarkFields.At(i)
	if !b.fieldBound[i] {
		b.fieldTexts[i].Bind(&b.fields[i])
		b.fieldBound[i] = true
	}
	b.fieldRows[i].For = id
	field := ui.FormField(c, &b.fieldRows[i], label).FillWidth().Height(56)
	field.Children(func() { ui.TextField(c, &b.fieldTexts[i], label, "", ui.WithID(id)) })
	return field
}

// darkButtonRecipe is the Save button's look: a near-black primary with a white label, whatever the
// theme's primary is.
func darkButtonRecipe(theme gfx.Theme, variant components.ButtonVariant, state gfx.State) components.ButtonLook {
	theme.Primary, theme.PrimaryForeground = gfx.RGB(24, 24, 27), gfx.RGB(250, 250, 250)
	return components.ButtonRecipe(theme, variant, state)
}

// bookmarkButtonRoom declares a button in the room the editor's rows give it: width wide and 32 tall,
// the button at its own height from the top, as the buttons were painted in their slots.
func bookmarkButtonRoom(c ui.C, width float32, button func()) {
	ui.Column(c).Size(width, 32).Shrink(0).Children(button)
}

// bookmarkPage is the page that declares the bookmark dialog: the window's own,
// or the workbench page when the file manager is a tab.
func (w *sftpWindow) bookmarkPage() *ui.Page {
	if w.window == nil {
		return w.owner.windowPage()
	}
	return w.windowPage()
}
