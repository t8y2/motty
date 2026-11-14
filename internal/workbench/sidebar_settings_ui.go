package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// The Sidebar group of Settings is one row, like the toolbar groups: it names
// the pages the sidebar shows and opens a dialog to choose them. Hiding is the
// entry only, so the page keeps its section, shortcuts and command entry.

var sidebarSettingsHits = tree.Feature("sidebar-settings")

var (
	hitSidebarSettingsOpen    = tree.ID(settingsHits, "sidebar-customize")
	hitSidebarSettingsClose   = tree.ID(sidebarSettingsHits, "close")
	hitSidebarSettingsDismiss = tree.ID(sidebarSettingsHits, "dismiss")
	hitSidebarSettingsShowAll = tree.ID(sidebarSettingsHits, "show-all")
	hitSidebarSettingsScroll  = tree.ID(sidebarSettingsHits, "scroll")
	hitSidebarSettingsToggles = tree.Sub(sidebarSettingsHits, tree.Name("toggle"))
)

type sidebarSettingsUI struct {
	scroll      float32
	geometry    components.ScrollAreaGeometry
	scrollDrag  bool
	thumbOffset float32
	// summary is the switch count under the settings row; the counts and the
	// format it was built from keep a paint from formatting it again.
	summary       string
	summaryShown  int
	summaryTotal  int
	summaryFormat string
}

// sidebarShown counts the sidebar pages on show and the pages the dialog offers.
func (state *State) sidebarShown() (shown, total int) {
	for _, item := range sidebarPages {
		if item.page == navPageHosts {
			continue
		}
		total++
		if !state.hiddenNav.has(item.page) {
			shown++
		}
	}
	return shown, total
}

// paintSidebarSettingsRow declares the General page's Sidebar row: the count the
// dialog's switches add up to, and the way in.
func (state *State) paintSidebarSettingsRow(page *settingsPage) {
	m, editor := state.messages, &state.sidebarEditor
	shown, total := state.sidebarShown()
	if editor.summary == "" || editor.summaryShown != shown || editor.summaryTotal != total || editor.summaryFormat != m.SettingsSidebarShownFmt {
		editor.summary = fmt.Sprintf(m.SettingsSidebarShownFmt, shown, total)
		editor.summaryShown, editor.summaryTotal, editor.summaryFormat = shown, total, m.SettingsSidebarShownFmt
	}
	page.actionRow(m.SettingsSidebar, editor.summary, false,
		components.ButtonAction{ID: hitSidebarSettingsOpen, Label: m.Toolbars.Customize, Variant: components.ButtonOutline})
}

func (state *State) openSidebarSettings() {
	state.sidebarEditor = sidebarSettingsUI{}
	state.modal = modalSidebarSettings
}

// clickSidebarSettings handles the dialog's buttons and its rows' switches.
func (state *State) clickSidebarSettings(id gfx.ID) {
	switch {
	case id == hitSidebarSettingsClose || id == hitSidebarSettingsDismiss:
		state.closeModal()
		return
	case id == hitSidebarSettingsShowAll:
		state.hiddenNav = 0
	case id.Scope() == hitSidebarSettingsToggles:
		page := navPage(item(id))
		if page == navPageHosts || page >= navPageCount {
			return
		}
		state.hiddenNav = state.hiddenNav.set(page, !state.hiddenNav.has(page))
	default:
		return
	}
	state.save()
}

func (state *State) sidebarSettingsPointer(event gfx.PointerEvent) gfx.EventResult {
	if state.modal != modalSidebarSettings {
		return gfx.EventResult{}
	}
	editor := &state.sidebarEditor
	if event.Target == hitSidebarSettingsScroll || editor.scrollDrag {
		if event.Kind == gfx.PointerExit {
			editor.scrollDrag = false
			return gfx.EventResult{Handled: true}
		}
		switch event.Kind {
		case gfx.PointerDown:
			if event.Button != gfx.PointerButtonPrimary {
				return gfx.EventResult{}
			}
			editor.scrollDrag = true
			editor.thumbOffset = event.Y - editor.geometry.VerticalThumb.Y
			if editor.thumbOffset < 0 || editor.thumbOffset > editor.geometry.VerticalThumb.Height {
				editor.thumbOffset = editor.geometry.VerticalThumb.Height / 2
			}
		case gfx.PointerUp:
			editor.scrollDrag = false
		}
		editor.scroll = editor.geometry.VerticalOffsetForThumb(event.Y - editor.thumbOffset)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (state *State) sidebarSettingsKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalSidebarSettings {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEscape && !event.Release && !event.Repeat {
		state.closeModal()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

func (state *State) sidebarSettingsScroll(event gfx.ScrollEvent) gfx.EventResult {
	editor := &state.sidebarEditor
	if !pointInRect(event.X, event.Y, editor.geometry.Viewport) {
		return gfx.EventResult{Handled: true}
	}
	before := editor.scroll
	editor.scroll = min(max(before+event.DeltaY, 0), editor.geometry.MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: before != editor.scroll}
}

// paintSidebarSettings paints the dialog: one switch per page the sidebar may
// leave out, in the sidebar's order.
func (state *State) paintSidebarSettings(canvas *gfx.Canvas, viewport gfx.Rect) {
	editor, m, theme := &state.sidebarEditor, state.messages, state.Theme()
	const (
		rowHeight = float32(48)
		rowGap    = float32(4)
	)
	rows := 0
	for _, item := range sidebarPages {
		if item.page != navPageHosts {
			rows++
		}
	}
	contentHeight := float32(rows)*rowHeight + float32(rows-1)*rowGap
	bodyHeight := min(contentHeight, max(viewport.Height-250, 96))
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitSidebarSettingsDismiss, CloseID: hitSidebarSettingsClose,
		ContentSize: gfx.Size{Width: 520}, FitBody: true, BodyHeight: bodyHeight,
	})
	layout := dialog.Begin(m.SettingsSidebar, m.SettingsSidebarHint)
	area := components.BeginScrollArea(canvas, theme, layout.Body, gfx.Size{Width: layout.Body.Width, Height: contentHeight}).
		Offset(0, editor.scroll).VerticalScrollbar(hitSidebarSettingsScroll, "", "")
	geometry := area.BeginContent()
	editor.geometry, editor.scroll = geometry, geometry.OffsetY
	bounds := geometry.Content
	if geometry.MaxOffsetY > 0 {
		bounds.Width = max(bounds.Width-14, 0)
	}
	row := 0
	for _, item := range sidebarPages {
		if item.page == navPageHosts {
			continue
		}
		bounds := gfx.Rect{X: bounds.X, Y: bounds.Y + float32(row)*(rowHeight+rowGap), Width: bounds.Width, Height: rowHeight}
		row++
		canvas.RoundedBorder(bounds, theme.Metrics.RadiusControl, 1, theme.Border, theme.Background)
		icon := gfx.Rect{X: bounds.X + 12, Y: bounds.Y + (rowHeight-18)/2, Width: 18, Height: 18}
		canvas.DrawIcon(lucideIcons[item.icon], icon, theme.Foreground)
		label := state.navPageLabel(item.page)
		caption := gfx.Rect{X: bounds.X + 40, Y: bounds.Y, Width: max(bounds.Width-40-58, 0), Height: rowHeight}
		canvas.Label(canvas.FitLine(label, theme.Metrics.FontSizeM, gfx.WeightRegular, caption.Width), caption, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Color: theme.Foreground})
		components.BeginSwitch(canvas, state.switchTheme(), hitSidebarSettingsToggles.At(int(item.page)),
			gfx.Rect{X: bounds.X + bounds.Width - 50, Y: bounds.Y + (rowHeight-22)/2, Width: 38, Height: 22}, !state.hiddenNav.has(item.page)).Label(label).Paint()
	}
	area.EndContent()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitSidebarSettingsShowAll, Label: m.SettingsSidebarShowAll, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitSidebarSettingsClose, Label: m.Toolbars.Close, Variant: components.ButtonSecondary},
	)
	dialog.End()
}
