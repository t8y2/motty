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
	hitSFTPProfileHover        = tree.ID(sftpWindowHits, "profile-hover")
	hitSFTPProfileHoverItems   = tree.Sub(sftpWindowHits, tree.Name("profile-hover-items"))
)

type sftpBookmarkEditor struct {
	open, editing, shared, sync        bool
	focusName                          bool
	original                           sftpBookmark
	fields                             [3]text.Editor // name, local directory, remote directory
	layout                             ui.View
	errorText                          string
	list                               gfx.Rect
	area                               components.ScrollAreaGeometry
	offset, grab                       float32
	dragging                           bool
	scopeOpen, focusScope, focusOption bool
	scopeMenu                          components.MenuScroll
	scopeBounds                        gfx.Rect
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
		w.bookmark.scopeMenu = components.MenuScroll{}
		w.bookmark.focusOption = w.bookmark.scopeOpen
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

func (w *sftpWindow) paintBookmarkDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	b, theme, m := &w.bookmark, w.owner.Theme(), w.owner.messages.SFTPWindow
	bodyHeight := min(float32(400), max(float32(292), viewport.Height-208))
	if b.errorText != "" {
		bodyHeight += 24
	}
	focus := hitSFTPBookmarkFields.At(2)
	if w.activeLocal {
		focus = hitSFTPBookmarkFields.At(1)
	}
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{Layer: dialogLayer, Scope: hitSFTPBookmarkScope, CloseID: hitSFTPBookmarkCancel, InitialFocusID: focus, ContentSize: gfx.Size{Width: 760}, FitBody: true, BodyHeight: bodyHeight})
	layout := dialog.Begin(m.BookmarkTitle, m.BookmarkDescription)
	var fields [3]ui.Box
	var browse, list, scope, add, save, remove, sync ui.Box
	f := b.layout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().Gap(8).Children(func() {
		f.Row().Height(56).Gap(8).Items(ui.End).Children(func() {
			fields[1] = f.Slot().Grow(1).Height(56)
			browse = f.Slot().Size(32, 32)
		})
		fields[2] = f.Slot().Height(56)
		f.Row().Height(56).Gap(8).Items(ui.End).Children(func() {
			fields[0] = f.Slot().Grow(1).Height(56)
			scopeWidth := max(canvas.TextWidth(m.ProfileSite, theme.Metrics.FontSizeM), canvas.TextWidth(m.ProfileShared, theme.Metrics.FontSizeM)) + 36
			scope = f.Slot().Width(max(132, scopeWidth)).Height(56)
			add = f.Slot().Width(max(56, canvas.TextWidth(m.ProfileNew, theme.Metrics.FontSizeM)+24)).Height(32)
			save = f.Slot().Width(max(56, canvas.TextWidth(m.BookmarkSave, theme.Metrics.FontSizeM)+24)).Height(32)
			remove = f.Slot().Width(max(56, canvas.TextWidth(m.ProfileDelete, theme.Metrics.FontSizeM)+24)).Height(32)
		})
		sync = f.Slot().Height(32)
		list = f.Slot().Grow(1)
		if b.errorText != "" {
			f.Text(b.errorText).FontSize(theme.Metrics.FontSizeS).Color(w.owner.pal.danger).Ellipsis().Height(24)
		}
	})
	f.End()
	components.BeginButton(canvas, theme, hitSFTPProfileNew, add.Rect()).Small().Outline(m.ProfileNew)
	saveTheme := theme
	saveTheme.Primary, saveTheme.PrimaryForeground = gfx.RGB(24, 24, 27), gfx.RGB(250, 250, 250)
	components.BeginButton(canvas, saveTheme, hitSFTPBookmarkSave, save.Rect()).Small().Default(m.BookmarkSave)
	deleteButton := components.BeginButton(canvas, theme, hitSFTPProfileDelete, remove.Rect()).Small()
	if !b.editing {
		deleteButton = deleteButton.Disabled()
	}
	deleteButton.Destructive(m.ProfileDelete)
	for i, label := range [3]string{m.BookmarkName, m.BookmarkLocal, m.BookmarkRemote} {
		id := hitSFTPBookmarkFields.At(i)
		input := components.BeginField(canvas, theme, fields[i].Rect()).For(id).Paint(label, "", "")
		value, focused := b.fields[i].String(), canvas.Focused() == id
		components.PaintInput(canvas, theme, input, components.InputSnapshot{ID: id, Label: label, Value: value, Focused: focused, PlatformEditing: focused && value != ""})
	}
	syncBounds := sync.Rect()
	checkboxSide := components.CheckboxExtent(theme)
	components.BeginCheckbox(canvas, theme, hitSFTPBookmarkSync, gfx.Rect{
		X: syncBounds.X + 4,
		Y: syncBounds.Y + (syncBounds.Height-checkboxSide)/2,
	}, b.sync).Label(m.ProfileSync).HitBounds(syncBounds).Paint()
	canvas.Label(m.ProfileSync, gfx.Rect{
		X: syncBounds.X + 32, Y: syncBounds.Y, Width: max(0, syncBounds.Width-32), Height: syncBounds.Height,
	}, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Color: theme.Foreground})
	w.paintProfileList(canvas, list.Rect())
	w.paintProfileScope(canvas, scope.Rect(), viewport)
	if b.focusName {
		canvas.RequestFocus(hitSFTPBookmarkFields.At(0))
		b.focusName = false
	}
	w.owner.paintIconButton(canvas, hitSFTPBookmarkBrowse, iconFolderOpen, browse.Rect(), m.BookmarkChooseLocal, false, iconButtonQuiet, theme.Background)
	if b.focusScope {
		canvas.RequestFocus(hitSFTPProfileScope)
		b.focusScope = false
	}
	dialog.Actions(layout, components.ButtonAction{ID: hitSFTPBookmarkCancel, Label: w.owner.messages.Cancel, Variant: components.ButtonSecondary}, components.ButtonAction{ID: hitSFTPProfileOpen, Label: m.ProfileOpen})
	dialog.End()
}

func (w *sftpWindow) paintProfileScope(canvas *gfx.Canvas, bounds, viewport gfx.Rect) {
	b, theme, m := &w.bookmark, w.owner.Theme(), w.owner.messages.SFTPWindow
	b.scopeBounds = components.BeginField(canvas, theme, bounds).For(hitSFTPProfileScope).Paint(m.ProfileScope, "", "")
	content := gfx.Rect{X: b.scopeBounds.X, Y: b.scopeBounds.Y + b.scopeBounds.Height + 4, Width: b.scopeBounds.Width, Height: 72}
	if content.Y+content.Height > viewport.Y+viewport.Height-8 {
		content.Y = b.scopeBounds.Y - content.Height - 4
	}
	value := m.ProfileSite
	if b.shared {
		value = m.ProfileShared
	}
	control := components.BeginSelect(canvas, theme, b.scopeBounds, components.SelectOptions{TriggerID: hitSFTPProfileScope, Label: m.ProfileScope, Value: value, Open: b.scopeOpen, Content: content, Layer: dialogLayer + 2, Scope: hitSFTPProfileScope, DismissID: hitSFTPProfileScopeDismiss, Viewport: viewport, Scroll: &b.scopeMenu})
	control.AddItem(components.SelectItem{ID: hitSFTPProfileSite, Label: m.ProfileSite, Selected: !b.shared, Disabled: w.session.host.CatalogID == ""})
	control.AddItem(components.SelectItem{ID: hitSFTPProfileShared, Label: m.ProfileShared, Selected: b.shared})
	control.End()
	if b.scopeOpen && b.focusOption {
		focus := hitSFTPProfileSite
		if b.shared || w.session.host.CatalogID == "" {
			focus = hitSFTPProfileShared
		}
		canvas.RequestFocus(focus)
		b.focusOption = false
	}
}

func (w *sftpWindow) paintProfileList(canvas *gfx.Canvas, bounds gfx.Rect) {
	b, theme, m := &w.bookmark, w.owner.Theme(), w.owner.messages.SFTPWindow
	profiles := w.visibleProfiles()
	canvas.RoundedBorder(bounds, theme.Metrics.RadiusItem(), 1, theme.Border.Mix(theme.MutedForeground, .25), theme.Background)
	b.list = bounds.Inset(gfx.All(2))
	canvas.Container(b.list, gfx.RoleListBox, m.BookmarkTitle)
	area := components.BeginScrollArea(canvas, theme, b.list, gfx.Size{Width: b.list.Width, Height: float32(len(profiles)) * 52}).Offset(0, b.offset).Viewport(hitSFTPProfileList, m.BookmarkTitle).VerticalScrollbar(hitSFTPProfileBar, m.BookmarkTitle, "")
	b.area = area.BeginContent()
	b.offset = b.area.OffsetY
	if len(profiles) == 0 {
		canvas.LabelCenter(m.ProfileEmpty, b.list, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Color: theme.MutedForeground})
	}
	for i, profile := range profiles {
		row := gfx.Rect{X: b.list.X + 4, Y: b.list.Y + float32(i)*52 - b.offset, Width: max(0, b.list.Width-18), Height: 52}
		if row.Y+row.Height < b.list.Y || row.Y > b.list.Y+b.list.Height {
			continue
		}
		id := hitSFTPProfileItems.At(i)
		selected := b.editing && b.original == profile
		if selected || canvas.Hovered() == id {
			canvas.Rounded(row, theme.Metrics.RadiusItem(), theme.Accent)
		}
		name := profile.name
		if name == "" {
			name = profile.path
		}
		scopeLabel, variant := m.ProfileSite, components.BadgeOutline
		if profile.shared {
			scopeLabel, variant = m.ProfileShared, components.BadgeSecondary
		}
		badgeWidth, _ := components.BadgeExtent(canvas, theme, scopeLabel)
		components.PaintBadge(canvas, theme, gfx.Rect{X: row.X + row.Width - badgeWidth - 8, Y: row.Y + 5}, scopeLabel, variant)
		description := profile.path
		if profile.local != "" {
			description = elideMiddleRunes(profile.local, 34) + "  ⇄  " + elideMiddleRunes(profile.path, 34)
		}
		if profile.sync {
			description += " · " + m.ProfileQuickSync
		}
		nameWidth := max(0, row.Width-badgeWidth-32)
		canvas.Label(canvas.FitLine(name, theme.Metrics.FontSizeM, gfx.WeightMedium, nameWidth), gfx.Rect{X: row.X + 8, Y: row.Y + 4, Width: nameWidth, Height: 22}, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Weight: gfx.WeightMedium, Color: theme.Foreground})
		canvas.Label(canvas.FitLine(description, theme.Metrics.FontSizeS, gfx.WeightRegular, row.Width-16), gfx.Rect{X: row.X + 8, Y: row.Y + 26, Width: row.Width - 16, Height: 20}, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Color: theme.MutedForeground})
		canvas.HitWithOptions(id, row, name, gfx.HitOptions{Role: gfx.RoleOption, Selected: selected})
	}
	area.EndContent()
}

func (w *sftpWindow) profilePointer(event gfx.PointerEvent) gfx.EventResult {
	b := &w.bookmark
	if b.scopeOpen {
		return gfx.EventResult{}
	}
	if b.dragging {
		if event.Kind == gfx.PointerMove {
			b.offset = b.area.VerticalOffsetForThumb(event.Y - b.grab)
		} else if event.Kind == gfx.PointerUp || event.Kind == gfx.PointerExit {
			b.dragging = false
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Kind == gfx.PointerDown && event.Target == hitSFTPProfileBar {
		b.grab = b.area.VerticalThumb.Height / 2
		if event.Y >= b.area.VerticalThumb.Y && event.Y < b.area.VerticalThumb.Y+b.area.VerticalThumb.Height {
			b.grab = event.Y - b.area.VerticalThumb.Y
		}
		b.dragging = true
		b.offset = b.area.VerticalOffsetForThumb(event.Y - b.grab)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && event.ClickCount == 2 {
		if index, ok := event.Target.In(hitSFTPProfileItems); ok {
			profiles := w.visibleProfiles()
			if index < len(profiles) {
				w.selectProfile(profiles[index])
				w.openProfileDraft()
				return gfx.EventResult{Handled: true, Repaint: true}
			}
		}
	}
	return gfx.EventResult{}
}

func (w *sftpWindow) profileScopeKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	b := &w.bookmark
	if !b.scopeOpen && event.Target != hitSFTPProfileScope {
		return gfx.EventResult{}, false
	}
	result := gfx.EventResult{Handled: true, Repaint: true}
	if b.scopeOpen {
		switch event.Key {
		case gfx.KeyEscape:
			if !event.Repeat {
				b.scopeOpen = false
				b.focusScope = true
			}
			return result, true
		case gfx.KeyTab:
			b.scopeOpen = false
			result.ContinueTraversal = true
			return result, true
		}
		if event.Modifiers == 0 {
			if next, ok := b.scopeMenu.Navigate(event.Key, event.Target, true); ok {
				result.Focus, result.FocusVisible = next, true
				return result, true
			}
			if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat {
				id := event.Target
				if id != hitSFTPProfileSite && id != hitSFTPProfileShared {
					id = hitSFTPProfileSite
					if b.shared {
						id = hitSFTPProfileShared
					}
				}
				w.clickBookmarkEditor(id)
				return result, true
			}
		}
		return gfx.EventResult{}, false
	}
	if event.Modifiers == 0 && !event.Repeat && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace || event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		w.clickBookmarkEditor(hitSFTPProfileScope)
		return result, true
	}
	return gfx.EventResult{}, false
}
