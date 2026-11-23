package workbench

import (
	"sort"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// commandsHits is the identity-tree place of the persistent command sidebar.
// It shares the list with the Snippets page, so a chip and a row name the same
// command; only the presentation and the group filter differ.
var commandsHits = tree.Feature("commands")

var (
	hitCommandsAdd = tree.ID(commandsHits, "add")
	// hitCommandsClose hides the whole bar. Its tooltip points at the snippet
	// picker, which is where the way back lives while the bar is hidden.
	hitCommandsClose = tree.ID(commandsHits, "close")
	// hitCommandsBarToggle is the picker's own checkbox for the bar, declared
	// above the search field rather than inside the searchable list. It stays
	// available whether the bar is shown or hidden.
	hitCommandsBarToggle = tree.ID(commandsHits, "bar-toggle")
	// hitCommandsSort opens the order dropdown beside the filter, so the bar's
	// own order lives with the bar instead of in the settings page.
	hitCommandsSort = tree.ID(commandsHits, "sort")
	// hitCommandsSortChoice names an order in the dropdown; its index is the
	// position of the order in settingsCommandsSortChoices.
	hitCommandsSortChoice = tree.Sub(commandsHits, tree.Name("sort-choice"))
	// hitCommandsFilter opens the group-filter dropdown at the bar's left.
	hitCommandsFilter = tree.ID(commandsHits, "filter")
	// hitCommandsGroupUngrouped toggles the ungrouped commands in the filter.
	hitCommandsGroupUngrouped = tree.ID(commandsHits, "group-ungrouped")
	// hitCommandsGroupAll / None show or hide every group at once.
	hitCommandsGroupAll  = tree.ID(commandsHits, "group-all")
	hitCommandsGroupNone = tree.ID(commandsHits, "group-none")
	// hitCommandsGroupToggle names a group's checkbox in the filter; its index is
	// the position of the group in state.commandGroups().
	hitCommandsGroupToggle = tree.Sub(commandsHits, tree.Name("group-toggle"))
	// hitCommandsGroupHeading names a group's heading, which opens or closes the
	// group; its index is the group's position among the shown sections.
	hitCommandsGroupHeading = tree.Sub(commandsHits, tree.Name("group-heading"))
	// hitCommandsTag names a snippet's chip in the bar; its index is the
	// snippet's index in state.snippets.list.
	hitCommandsTag = tree.Sub(commandsHits, tree.Name("tag"))
	// hitCommandsResize is the strip along the bar's inner edge that drags its
	// width, the way the assistant panel's divider does.
	hitCommandsResize = tree.ID(commandsHits, "resize")
)

// commandsPlacement is where the quick command bar lives: the strip under the
// terminal panes, or the sidebar beside them. The zero value is the bottom bar,
// so stored settings that predate the sidebar restore the strip the user had.
type commandsPlacement uint8

const (
	commandsPlacementBottom commandsPlacement = iota
	commandsPlacementSide
)

var commandsPlacementChoices = [...]commandsPlacement{commandsPlacementBottom, commandsPlacementSide}

const (
	commandsBarHeight     float32 = 34 // the bottom bar's own height
	commandsChipInset     float32 = 5  // vertical inset of chips inside the bar
	commandsSidebarWidth  float32 = 224
	commandsSidebarMin    float32 = 160 // the narrowest the bar may be dragged
	commandsMinContent    float32 = 320 // the terminals keep at least this much beside it
	commandsHeaderHeight  float32 = 34
	commandsChipHeight    float32 = 26
	commandsDrawerPadding float32 = 8
	commandsTagGap        float32 = 6
	commandsTagPaddingX   float32 = 12
	commandsChipRadius    float32 = 8
	commandsMinTagWidth   float32 = 44
	commandsMaxTagWidth   float32 = 200
	commandsIconSize      float32 = 16
	commandsButtonGap     float32 = 4  // between the + and × buttons
	commandsLeadGap       float32 = 10 // between the filter and order buttons
	commandsFilterGap     float32 = 8  // between the leading buttons and the chips
	commandsButtonInset   float32 = 4  // hover padding around an icon button
	commandsScrollStep    float32 = 60 // pixels a wheel notch moves the chips
	// A group's heading: its row in the sidebar, the chevron that shows whether
	// it is open, and the gaps that set one group apart from the next.
	commandsHeadingHeight float32 = 22
	commandsChevronSize   float32 = 12
	commandsHeadingGap    float32 = 4  // between the chevron and the group's name
	commandsSectionGap    float32 = 6  // extra space above a group in the sidebar
	commandsSectionGapX   float32 = 14 // between one group and the next in the strip
	commandsMaxLabelWidth float32 = 160
)

// commandsUI is the command bar's state. The bar stays hidden until shown opts
// it in. placement is its dock: the strip under the panes or the sidebar beside
// them. hiddenGroups are the snippet groups (by lowercased name; "" means
// ungrouped) not shown in the bar, and sort is the order its groups and chips
// appear in.
type commandsUI struct {
	// shown is whether the user opted the bar in. The zero value hides it,
	// so a fresh install sees no bar until the picker's checkbox turns it on.
	shown bool
	// placement is where the bar docks while shown; the zero value is the
	// bottom strip.
	placement commandsPlacement
	// filterOpen is whether the group-filter dropdown is showing.
	filterOpen bool
	// sortOpen is whether the order dropdown is showing.
	sortOpen bool
	// hiddenGroups are the snippet groups hidden from the bar, keyed by their
	// lowercased name; the empty key means ungrouped commands. Absent = shown.
	// A fresh state (nil) shows every group.
	hiddenGroups map[string]bool
	// collapsedGroups are the groups whose heading is shown but whose commands
	// are folded away, keyed the same way. Unlike hiddenGroups, a collapsed group
	// stays one click from open, right where it is listed.
	collapsedGroups map[string]bool
	// sort is how the visible commands are ordered along the bar.
	sort commandsSort
	// offset is how far the chips are scrolled: right on the bottom strip,
	// down the sidebar. It is where the wheel puts them when more commands
	// exist than the bar can hold.
	offset float32
	// bar is the last painted strip and maxOffset the largest offset that still
	// shows a chip, both recorded so a wheel over the bar knows what it moves.
	bar       gfx.Rect
	maxOffset float32
	// width is the width the user dragged the sidebar to; zero means the default.
	width float32
	// resizing is set while the inner edge is dragged, with dragFrom and
	// dragStart recording where the gesture began so motion lands exactly.
	resizing  bool
	dragFrom  float32
	dragStart float32
}

// commandsBarAvailable reports whether the active page can host the bar at all.
// The picker only offers to show the bar where showing it would do something.
func (state *State) commandsBarAvailable() bool {
	if !state.terminalPage() {
		return false
	}
	// The bar types snippets into a terminal; a remote desktop (RDP/VNC)
	// session has no shell to receive them, so the sidebar hides on those pages.
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.host.isDesktop() {
		return false
	}
	return true
}

// commandsDrawerHeight is the bottom space the bar reserves, subtracted from
// the terminal panes so they do not sit under it. Zero unless the bar is shown
// and docked at the bottom.
func (state *State) commandsDrawerHeight() float32 {
	if !state.commandsBarAvailable() || !state.commands.shown || state.commands.placement != commandsPlacementBottom {
		return 0
	}
	return commandsBarHeight
}

// commandsBarRect is where the bottom bar sits: the lowest strip of the active
// workspace, under its panes. Both the page's menus and the canvas' chips are
// placed from it.
func (state *State) commandsBarRect() gfx.Rect {
	if state.commandsDrawerHeight() <= 0 {
		return gfx.Rect{}
	}
	// The strip is the content area's own bottom, so it spans the full width the
	// panes and their side panels share, not just the terminal column between
	// them. A side panel or a terminal toolbar rail ends above the strip;
	// spanning it here means nothing is left unpainted in the corners beside
	// the bar (#981).
	full := state.fullWorkspacePixelBounds(state.viewport)
	frame := state.toolbarDockFrame(state.viewport, true)
	return gfx.Rect{X: full.X, Y: frame.Y + frame.Height, Width: full.Width, Height: commandsBarHeight}
}

// commandsPlacementLabel is the name of one dock in the picker's placement
// control.
func (state *State) commandsPlacementLabel(placement commandsPlacement) string {
	if placement == commandsPlacementSide {
		return state.messages.Tools.CommandsPlaceSide
	}
	return state.messages.Tools.CommandsPlaceBottom
}

// commandsSidebarExtent is the width the bar takes from the terminals: what
// the user dragged it to, within the room the dock frame has, or the default.
// The terminals keep their own minimum, so the bar cannot swallow the pane it
// sits beside. Zero while the bar is docked at the bottom.
func (state *State) commandsSidebarExtent(size gfx.Size) float32 {
	if !state.commandsBarAvailable() || !state.commands.shown || state.commands.placement != commandsPlacementSide {
		return 0
	}
	frame := state.toolbarDockFrame(size, true)
	if frame.Width <= 0 {
		return 0
	}
	least := min(commandsSidebarMin, frame.Width)
	most := max(frame.Width-commandsMinContent, least)
	width := state.commands.width
	if width <= 0 {
		width = min(commandsSidebarWidth, frame.Width*0.24)
	}
	return min(max(width, least), most)
}

// commandsSidebarBounds places the bar inside the dock frame, so the terminal
// toolbar keeps the window edge and the bar sits on its inner side, as the
// assistant panel does.
func (state *State) commandsSidebarBounds(size gfx.Size) gfx.Rect {
	width := state.commandsSidebarExtent(size)
	if width <= 0 {
		return gfx.Rect{}
	}
	frame := state.toolbarDockFrame(size, true)
	return gfx.Rect{
		X: frame.X + frame.Width - width, Y: windowBarHeight,
		Width: width, Height: max(size.Height-windowBarHeight, 0),
	}
}

// commandGroupKey is how a snippet's group is told apart from another's: groups
// are the same when they differ only in case or surrounding space, and the empty
// key is the ungrouped commands.
func commandGroupKey(group string) string {
	return strings.ToLower(strings.TrimSpace(group))
}

// commandGroupVisible reports whether a snippet's group is shown in the bar.
func (state *State) commandGroupVisible(group string) bool {
	return !state.commands.hiddenGroups[commandGroupKey(group)]
}

// commandGroups returns the distinct groups the snippets name, each in the
// spelling of the first snippet that names it, sorted by name. hasUngrouped says
// whether any snippet names none.
func (state *State) commandGroups() (groups []string, hasUngrouped bool) {
	seen := make(map[string]int)
	for _, s := range state.snippets.list {
		if s.Group == "" {
			hasUngrouped = true
			continue
		}
		lower := strings.ToLower(s.Group)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = len(groups)
		groups = append(groups, s.Group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return strings.ToLower(groups[i]) < strings.ToLower(groups[j])
	})
	return
}

// commandSection is one group of the bar: its heading and the snippets listed
// under it, as indices into state.snippets.list.
type commandSection struct {
	// key is the group's commandGroupKey; "" is the ungrouped commands.
	key string
	// label is the heading: the group in the spelling of its first snippet, or
	// the language's word for ungrouped.
	label string
	// collapsed hides the commands and leaves the heading.
	collapsed bool
	items     []int
}

// commandSections groups the snippets the filter lets through, in the order the
// bar shows them. A group's place and its commands' order follow the chosen sort:
// the list order the user arranged by default, or by name. Ungrouped commands
// always come last, so the named groups stay at the top where they are looked for.
func (state *State) commandSections() []commandSection {
	list := state.snippets.list
	sections := make([]commandSection, 0, 4)
	at := make(map[string]int)
	for i := range list {
		if !state.commandGroupVisible(list[i].Group) {
			continue
		}
		key := commandGroupKey(list[i].Group)
		n, ok := at[key]
		if !ok {
			n = len(sections)
			at[key] = n
			label := strings.TrimSpace(list[i].Group)
			if key == "" {
				label = state.messages.Tools.CommandsUngrouped
			}
			sections = append(sections, commandSection{key: key, label: label, collapsed: state.commands.collapsedGroups[key]})
		}
		sections[n].items = append(sections[n].items, i)
	}
	byName := state.commands.sort != commandsSortCreated
	sort.SliceStable(sections, func(a, b int) bool {
		if (sections[a].key == "") != (sections[b].key == "") {
			return sections[b].key == ""
		}
		return byName && sections[a].key < sections[b].key
	})
	if state.commands.sort == commandsSortName {
		for i := range sections {
			items := sections[i].items
			sort.SliceStable(items, func(a, b int) bool {
				return strings.ToLower(list[items[a]].Name) < strings.ToLower(list[items[b]].Name)
			})
		}
	}
	return sections
}

// commandSectionsGrouped reports whether the bar shows group headings. Commands
// that all lack a group form one section that needs no name, so a list nobody
// has grouped keeps looking like a plain row of buttons.
func commandSectionsGrouped(sections []commandSection) bool {
	return len(sections) > 1 || len(sections) == 1 && sections[0].key != ""
}

// commandDisplayOrder returns the snippet indices the bar lists, in the order it
// lists them: the sections one after another, folded groups included.
func (state *State) commandDisplayOrder() []int {
	order := make([]int, 0, len(state.snippets.list))
	for _, section := range state.commandSections() {
		order = append(order, section.items...)
	}
	return order
}

// paintCommandsDrawer draws the quick command bar where the user docked it.
func (state *State) paintCommandsDrawer(canvas *gfx.Canvas) {
	if state.commands.placement == commandsPlacementSide {
		state.paintCommandsSidebar(canvas)
		return
	}
	state.paintCommandsBar(canvas)
}

// paintCommandsBar draws the bottom strip of quick commands under the panes.
func (state *State) paintCommandsBar(canvas *gfx.Canvas) {
	bar := state.commandsBarRect()
	state.commands.bar = bar
	// Nothing is painted, so a wheel must not think the old strip is there.
	state.commands.maxOffset = 0
	if bar.Width <= 0 {
		return
	}
	// A faint fill and a top hairline set the strip apart from the terminal.
	canvas.Fill(bar, withAlpha(state.pal.shell, 0.6))
	canvas.Fill(gfx.Rect{X: bar.X, Y: bar.Y, Width: bar.Width, Height: 1}, state.pal.line)

	m := state.messages.Tools
	iconColor := state.pal.muted
	cy := bar.Y + (commandsBarHeight-commandsIconSize)/2

	// Group-filter button on the left. It tints to the accent, on a soft pill,
	// while the dropdown is open or any group is hidden, so an active filter reads
	// at a glance.
	filterBtn := gfx.Rect{X: bar.X + commandsDrawerPadding, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
	filterActive := state.commands.filterOpen || len(state.commands.hiddenGroups) > 0
	state.paintCommandsLeadButton(canvas, filterBtn, hitCommandsFilter, iconSliders, m.CommandsFilter, filterActive)

	// Order button right beside it, lit the same way while its menu is open or a
	// non-default order is in effect.
	sortBtn := gfx.Rect{X: filterBtn.X + filterBtn.Width + commandsLeadGap, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
	sortActive := state.commands.sortOpen || state.commands.sort != commandsSortCreated
	state.paintCommandsLeadButton(canvas, sortBtn, hitCommandsSort, iconArrowUpDown, m.CommandsSortRow, sortActive)

	// Add (+) and close (×) grouped together on the right; each lifts or reddens
	// on hover so the pair reads as controls.
	closeX := bar.X + bar.Width - commandsDrawerPadding - commandsIconSize
	addX := closeX - commandsButtonGap - commandsIconSize
	add := gfx.Rect{X: addX, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
	addColor := iconColor
	if canvas.Hovered() == hitCommandsAdd {
		addColor = state.pal.text
	}
	canvas.DrawIcon(lucideIcons[iconPlus], add, addColor)
	canvas.HitWithOptions(hitCommandsAdd, add, m.CommandsAdd, gfx.HitOptions{Role: gfx.RoleButton})
	closeBtn := gfx.Rect{X: closeX, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
	closeColor := iconColor
	if canvas.Hovered() == hitCommandsClose {
		closeColor = state.pal.danger
	}
	canvas.DrawIcon(lucideIcons[iconClose], closeBtn, closeColor)
	canvas.HitWithOptions(hitCommandsClose, closeBtn, m.CommandsClose, gfx.HitOptions{Role: gfx.RoleButton})

	// Command chips occupy the middle, between the leading buttons and add/close.
	contentX := sortBtn.X + sortBtn.Width + commandsButtonInset + commandsFilterGap
	contentW := addX - commandsFilterGap - contentX
	if contentW > 0 {
		state.paintCommandChipsBar(canvas, bar, contentX, contentW)
	}
}

// paintCommandsSidebar draws the sidebar beside the terminal panes.
func (state *State) paintCommandsSidebar(canvas *gfx.Canvas) {
	bar := state.commandsSidebarBounds(state.viewport)
	state.commands.bar = bar
	state.commands.maxOffset = 0
	if bar.Width <= 0 || bar.Height <= 0 {
		return
	}
	canvas.Fill(bar, withAlpha(state.pal.shell, 0.6))
	canvas.Fill(gfx.Rect{X: bar.X, Y: bar.Y, Width: 1, Height: bar.Height}, state.pal.line)

	m := state.messages.Tools
	// The inner edge drags the bar's width. The strip is wider than the line it
	// sits on, so it is easy to grab without moving anything until it is pulled.
	if state.commands.resizing || canvas.Hovered() == hitCommandsResize {
		canvas.Fill(gfx.Rect{X: bar.X, Y: bar.Y, Width: 1, Height: bar.Height}, state.pal.accent)
	}
	canvas.HitWithOptions(hitCommandsResize, gfx.Rect{X: bar.X, Y: bar.Y, Width: 8, Height: bar.Height}, m.CommandsResize, gfx.HitOptions{Cursor: gfx.PointerCursorResizeHorizontal})
	iconColor := state.pal.muted
	cy := bar.Y + (commandsHeaderHeight-commandsIconSize)/2

	closeX := bar.X + bar.Width - commandsDrawerPadding - commandsIconSize
	addX := closeX - commandsButtonGap - commandsIconSize
	sortX := addX - commandsLeadGap - commandsIconSize
	filterX := sortX - commandsLeadGap - commandsIconSize
	title := gfx.Rect{X: bar.X + commandsDrawerPadding, Y: bar.Y, Width: filterX - bar.X - commandsDrawerPadding - commandsFilterGap, Height: commandsHeaderHeight}
	if title.Width > 24 {
		canvas.Label(m.SnippetsTitle, title, gfx.TextStyle{Size: state.Theme().Metrics.FontSizeS, Weight: gfx.WeightMedium, Color: state.pal.text})
	}

	filterBtn := gfx.Rect{X: filterX, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
	filterActive := state.commands.filterOpen || len(state.commands.hiddenGroups) > 0
	state.paintCommandsLeadButton(canvas, filterBtn, hitCommandsFilter, iconSliders, m.CommandsFilter, filterActive)

	// Order button right beside it, lit the same way while its menu is open or a
	// non-default order is in effect.
	sortBtn := gfx.Rect{X: sortX, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
	sortActive := state.commands.sortOpen || state.commands.sort != commandsSortCreated
	state.paintCommandsLeadButton(canvas, sortBtn, hitCommandsSort, iconArrowUpDown, m.CommandsSortRow, sortActive)

	// Add (+) and close (×) grouped together on the right; each lifts or reddens
	// on hover so the pair reads as controls.
	add := gfx.Rect{X: addX, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
	addColor := iconColor
	if canvas.Hovered() == hitCommandsAdd {
		addColor = state.pal.text
	}
	canvas.DrawIcon(lucideIcons[iconPlus], add, addColor)
	canvas.HitWithOptions(hitCommandsAdd, add, m.CommandsAdd, gfx.HitOptions{Role: gfx.RoleButton})
	closeBtn := gfx.Rect{X: closeX, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
	closeColor := iconColor
	if canvas.Hovered() == hitCommandsClose {
		closeColor = state.pal.danger
	}
	canvas.DrawIcon(lucideIcons[iconClose], closeBtn, closeColor)
	canvas.HitWithOptions(hitCommandsClose, closeBtn, m.CommandsClose, gfx.HitOptions{Role: gfx.RoleButton})

	list := gfx.Rect{
		X: bar.X + commandsDrawerPadding, Y: bar.Y + commandsHeaderHeight,
		Width: bar.Width - commandsDrawerPadding*2, Height: bar.Height - commandsHeaderHeight - commandsDrawerPadding,
	}
	if list.Width > 0 && list.Height > 0 {
		state.paintCommandChips(canvas, list)
	}
}

// paintCommandsLeadButton draws one of the bar's two leading icon buttons: a
// sliders glyph, and a sort glyph, each on a soft pill while its menu is open or
// its setting differs from the default.
func (state *State) paintCommandsLeadButton(canvas *gfx.Canvas, rect gfx.Rect, id gfx.ID, icon uint8, hint string, active bool) {
	if active || canvas.Hovered() == id {
		fill := withAlpha(state.pal.surfaceHot, 0.9)
		if active {
			fill = withAlpha(state.pal.accent, 0.16)
		}
		canvas.Rounded(gfx.Rect{
			X: rect.X - commandsButtonInset, Y: rect.Y - commandsButtonInset,
			Width: rect.Width + commandsButtonInset*2, Height: rect.Height + commandsButtonInset*2,
		}, 6, fill)
	}
	color := state.pal.muted
	if active {
		color = state.pal.accent
	}
	canvas.DrawIcon(lucideIcons[icon], rect, color)
	canvas.HitWithOptions(id, rect, hint, gfx.HitOptions{Role: gfx.RoleButton})
}

// paintCommandChips draws the scrollable list below the sidebar header: each
// group's heading, then its commands one to a row. A folded group shows only its
// heading.
func (state *State) paintCommandChips(canvas *gfx.Canvas, bounds gfx.Rect) {
	sections := state.commandSections()
	if len(sections) == 0 {
		state.commands.offset = 0
		return
	}
	grouped := commandSectionsGrouped(sections)
	// Lay the rows out twice: once to learn how far the list scrolls, once to
	// paint what the scroll leaves in view.
	extent := state.layoutCommandRows(sections, grouped, nil)
	state.commands.maxOffset = max(extent-bounds.Height, 0)
	state.commands.offset = min(max(state.commands.offset, 0), state.commands.maxOffset)
	canvas.Clip(bounds)
	state.layoutCommandRows(sections, grouped, func(section, index int, top, height float32) {
		y := bounds.Y + top - state.commands.offset
		if y >= bounds.Y+bounds.Height || y+height <= bounds.Y {
			return
		}
		rect := gfx.Rect{X: bounds.X, Y: y, Width: bounds.Width, Height: height}
		if index < 0 {
			state.paintCommandHeading(canvas, rect, sections[section], hitCommandsGroupHeading.At(section), true)
			return
		}
		id := hitCommandsTag.At(index)
		state.paintCommandTag(canvas, rect, state.snippets.list[index], id)
		state.openSnippetBarMenuOn(canvas, id, index)
	})
	canvas.Unclip()
}

// layoutCommandRows walks the sidebar's rows top to bottom and returns the
// height they add up to. visit, when given, is called for each row with its
// section, the snippet index (-1 for the group's heading), its top within the list
// and its height.
func (state *State) layoutCommandRows(sections []commandSection, grouped bool, visit func(section, index int, top, height float32)) float32 {
	y := float32(0)
	for si, section := range sections {
		if grouped {
			if si > 0 {
				y += commandsSectionGap
			}
			if visit != nil {
				visit(si, -1, y, commandsHeadingHeight)
			}
			y += commandsHeadingHeight + commandsTagGap
		}
		if grouped && section.collapsed {
			continue
		}
		for _, index := range section.items {
			if visit != nil {
				visit(si, index, y, commandsChipHeight)
			}
			y += commandsChipHeight + commandsTagGap
		}
	}
	return max(y-commandsTagGap, 0)
}

// paintCommandHeading draws a group's heading: a chevron that says whether its
// commands are listed, and the group's name. Clicking it folds or unfolds the
// group. The sidebar heading spans the row; the strip's is only as wide as its
// text.
func (state *State) paintCommandHeading(canvas *gfx.Canvas, rect gfx.Rect, section commandSection, id gfx.ID, hoverFill bool) {
	theme := state.Theme()
	color := state.pal.muted
	if canvas.Hovered() == id {
		color = state.pal.text
		if hoverFill {
			canvas.Rounded(rect, 6, withAlpha(state.pal.surfaceHot, 0.6))
		}
	}
	icon, hint := iconChevronDown, state.messages.Tools.CommandsCollapse
	if section.collapsed {
		icon, hint = iconChevronRight, state.messages.Tools.CommandsExpand
	}
	chevron := gfx.Rect{X: rect.X + 2, Y: rect.Y + (rect.Height-commandsChevronSize)/2, Width: commandsChevronSize, Height: commandsChevronSize}
	canvas.DrawIcon(lucideIcons[icon], chevron, color)
	textX := chevron.X + commandsChevronSize + commandsHeadingGap
	label := gfx.Rect{X: textX, Y: rect.Y, Width: max(rect.X+rect.Width-textX, 0), Height: rect.Height}
	canvas.Label(section.label, label, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightMedium, Color: color})
	canvas.HitWithOptions(id, rect, section.label, gfx.HitOptions{Role: gfx.RoleButton, Hint: hint})
}

// commandHeadingWidth is the strip heading's width: chevron, gap and name.
func (state *State) commandHeadingWidth(canvas *gfx.Canvas, label string) float32 {
	text := canvas.TextWidthWeight(label, state.Theme().Metrics.FontSizeS, gfx.WeightMedium)
	return min(commandsMaxLabelWidth, text+2+commandsChevronSize+commandsHeadingGap+2)
}

// paintCommandChipsBar draws the visible commands in the bottom strip's middle,
// each group's name followed by its commands. More than fit scroll sideways with
// the wheel, so a long list stays reachable without widening the bar; the clip
// keeps a half-scrolled chip from running under the buttons at either end.
func (state *State) paintCommandChipsBar(canvas *gfx.Canvas, bar gfx.Rect, x, width float32) {
	sections := state.commandSections()
	grouped := commandSectionsGrouped(sections)
	chipH := commandsBarHeight - commandsChipInset*2
	y := bar.Y + commandsChipInset
	extent := state.layoutCommandStrip(canvas, sections, grouped, nil)
	maxOffset := max(extent-width, 0)
	state.commands.maxOffset = maxOffset
	state.commands.offset = min(max(state.commands.offset, 0), maxOffset)
	if len(sections) == 0 {
		return
	}
	canvas.Clip(gfx.Rect{X: x, Y: bar.Y, Width: width, Height: bar.Height})
	state.layoutCommandStrip(canvas, sections, grouped, func(section, index int, left, w float32) {
		left += x - state.commands.offset
		if left+w <= x || left >= x+width {
			return
		}
		if index < 0 {
			state.paintCommandHeading(canvas, gfx.Rect{X: left, Y: y, Width: w, Height: chipH}, sections[section], hitCommandsGroupHeading.At(section), false)
			return
		}
		id := hitCommandsTag.At(index)
		state.paintCommandTag(canvas, gfx.Rect{X: left, Y: y, Width: w, Height: chipH}, state.snippets.list[index], id)
		state.openSnippetBarMenuOn(canvas, id, index)
	})
	canvas.Unclip()
}

// layoutCommandStrip walks the strip's items left to right and returns the width
// they add up to; visit receives each with its left edge and width, the way
// layoutCommandRows does for the sidebar.
func (state *State) layoutCommandStrip(canvas *gfx.Canvas, sections []commandSection, grouped bool, visit func(section, index int, left, width float32)) float32 {
	x := float32(0)
	for si, section := range sections {
		if si > 0 {
			x += commandsSectionGapX
		}
		if grouped {
			w := state.commandHeadingWidth(canvas, section.label)
			if visit != nil {
				visit(si, -1, x, w)
			}
			x += w + commandsTagGap
		}
		if grouped && section.collapsed {
			x -= commandsTagGap
			continue
		}
		for i, index := range section.items {
			if i > 0 {
				x += commandsTagGap
			}
			w := state.commandTagWidth(canvas, state.snippets.list[index].Name)
			if visit != nil {
				visit(si, index, x, w)
			}
			x += w
		}
	}
	return x
}

// commandsMenuWidth fits a dropdown to its longest row: group names and order
// labels come from the user's own snippets, so a fixed width would clip them.
// The minimum keeps short menus from looking cramped like a tooltip.
func (state *State) commandsMenuWidth(canvas *gfx.Canvas, items []components.DropdownMenuItem, minimum float32) float32 {
	theme := state.Theme()
	width := minimum
	for _, item := range items {
		width = max(width, components.DropdownMenuItemWidth(canvas, theme, item))
	}
	return min(width, max(160, state.windowRect().Width-24))
}

// commandsMenuHeading is a muted, non-interactive first row. It is a disabled
// item, not a menu label: a dropdown label is smaller and indented, which would
// make the menu's type uneven and its rows start at two different left edges.
func commandsMenuHeading(label string) components.DropdownMenuItem {
	return components.DropdownMenuItem{Label: label, Disabled: true}
}

// declareCommandsMenus declares the bar's open dropdown as a page menu. The
// bar itself is still painted by the window; the menu is an overlay of the
// page, so its rows keep the page's own hit, focus and keyboard behaviour.
func (state *State) declareCommandsMenus(c ui.C) {
	if state.commands.placement == commandsPlacementBottom {
		bar := state.commandsBarRect()
		if bar.Width <= 0 {
			return
		}
		state.commands.bar = bar
		cy := bar.Y + (commandsBarHeight-commandsIconSize)/2
		filterButton := gfx.Rect{X: bar.X + commandsDrawerPadding, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
		sortButton := gfx.Rect{X: filterButton.X + filterButton.Width + commandsLeadGap, Y: cy, Width: commandsIconSize, Height: commandsIconSize}
		if state.commands.filterOpen {
			state.declareCommandsFilterMenu(c, bar, filterButton)
		}
		if state.commands.sortOpen {
			state.declareCommandsSortMenu(c, bar, sortButton)
		}
		return
	}
	bar := state.commandsSidebarBounds(state.viewport)
	if bar.Width <= 0 {
		return
	}
	cy := bar.Y + (commandsHeaderHeight-commandsIconSize)/2
	closeX := bar.X + bar.Width - commandsDrawerPadding - commandsIconSize
	addX := closeX - commandsButtonGap - commandsIconSize
	sortX := addX - commandsLeadGap - commandsIconSize
	filterX := sortX - commandsLeadGap - commandsIconSize
	if state.commands.filterOpen {
		state.declareCommandsFilterMenu(c, bar, gfx.Rect{X: filterX, Y: cy, Width: commandsIconSize, Height: commandsIconSize})
	}
	if state.commands.sortOpen {
		state.declareCommandsSortMenu(c, bar, gfx.Rect{X: sortX, Y: cy, Width: commandsIconSize, Height: commandsIconSize})
	}
}

// declareCommandsMenu places a menu beside the sidebar header or above the
// bottom bar, depending on where the bar is docked, and declares its rows.
func (state *State) declareCommandsMenu(c ui.C, bar, trigger gfx.Rect, open *bool, width float32, rows func(*ui.MenuItems)) {
	if state.commands.placement == commandsPlacementSide {
		width = min(width, max(0, bar.X-18))
		anchor := gfx.Rect{X: bar.X - width - 6, Y: bar.Y + commandsHeaderHeight}
		ui.MenuAt(c, open, anchor, func(menu *ui.MenuItems) {
			menu.WidthRange(width, width)
			rows(menu)
		})
		return
	}
	// ui.Menu opens 4px over its anchor; the bar's menus have always kept 6px
	// from the strip, so the zero-size anchor sits 2px above the bar's top.
	viewport := state.windowRect()
	x := max(12, trigger.X-4)
	if x+width > viewport.Width-12 {
		x = max(12, viewport.Width-12-width)
	}
	anchor := ui.Slot(c).Absolute().Left(x).Top(bar.Y-2).Size(0, 0)
	ui.Menu(c, anchor, open, func(menu *ui.MenuItems) {
		menu.WidthRange(width, width)
		rows(menu)
	})
}

// declareCommandsFilterMenu declares the group-filter dropdown: a heading, a
// checkbox per group (and one for ungrouped), then Show all / Hide all. Its rows
// are states, not one-shot choices, so the application reopens the menu after a
// row is chosen: the page closes a menu on any choice, while the canvas menu kept
// this one open for the next toggle.
func (state *State) declareCommandsFilterMenu(c ui.C, bar, trigger gfx.Rect) {
	m := state.messages.Tools
	groups, hasUngrouped := state.commandGroups()
	choices := make([]components.DropdownMenuItem, 0, len(groups)+1)
	if hasUngrouped {
		choices = append(choices, components.DropdownMenuItem{
			ID: hitCommandsGroupUngrouped, Label: m.CommandsUngrouped,
			Kind: components.DropdownMenuCheckbox, Checked: state.commandGroupVisible(""),
		})
	}
	for i, g := range groups {
		choices = append(choices, components.DropdownMenuItem{
			ID: hitCommandsGroupToggle.At(i), Label: g,
			Kind: components.DropdownMenuCheckbox, Checked: state.commandGroupVisible(g),
		})
	}
	empty := len(choices) == 0
	var actions []components.DropdownMenuItem
	if empty {
		actions = []components.DropdownMenuItem{{Label: m.CommandsNoGroups, Disabled: true}}
	} else {
		actions = []components.DropdownMenuItem{
			{ID: hitCommandsGroupAll, Label: m.CommandsShowAll},
			{ID: hitCommandsGroupNone, Label: m.CommandsHideAll},
		}
	}
	items := make([]components.DropdownMenuItem, 0, 1+len(choices)+len(actions))
	items = append(items, commandsMenuHeading(m.CommandsFilter))
	items = append(items, choices...)
	items = append(items, actions...)
	width := state.commandsMenuWidth(c.Canvas(), items, 190)
	choose := func(id gfx.ID) {
		c.After(func() {
			state.clickCommandsDrawer(id)
			if state.commands.shown {
				state.commands.filterOpen = true
			}
		})
	}
	state.declareCommandsMenu(c, bar, trigger, &state.commands.filterOpen, width, func(menu *ui.MenuItems) {
		menu.Item(m.CommandsFilter).Disabled(true)
		for _, choice := range choices {
			item := menu.Item(choice.Label).Checked(choice.Checked)
			if item.Clicked() {
				choose(choice.ID)
			}
		}
		if !empty {
			menu.Separator()
		}
		for _, action := range actions {
			item := menu.Item(action.Label).Disabled(action.Disabled)
			if item.Clicked() {
				choose(action.ID)
			}
		}
	})
}

// declareCommandsSortMenu declares the order dropdown: a heading and one radio
// item per order. Picking an order closes it, since the bar behind already shows
// the result.
func (state *State) declareCommandsSortMenu(c ui.C, bar, trigger gfx.Rect) {
	items := []components.DropdownMenuItem{commandsMenuHeading(state.messages.Tools.CommandsSortRow)}
	for i, order := range settingsCommandsSortChoices {
		current := state.commands.sort == order
		items = append(items, components.DropdownMenuItem{
			ID: hitCommandsSortChoice.At(i), Label: state.settingsCommandsSortLabel(order),
			Kind: components.DropdownMenuRadio, Checked: current, Selected: current,
		})
	}
	width := state.commandsMenuWidth(c.Canvas(), items, 180)
	state.declareCommandsMenu(c, bar, trigger, &state.commands.sortOpen, width, func(menu *ui.MenuItems) {
		menu.Item(state.messages.Tools.CommandsSortRow).Disabled(true)
		for i, order := range settingsCommandsSortChoices {
			item := menu.Item(state.settingsCommandsSortLabel(order)).Chosen(state.commands.sort == order)
			if item.Clicked() {
				id := hitCommandsSortChoice.At(i)
				c.After(func() { state.clickCommandsDrawer(id) })
			}
		}
	})
}

// scrollCommandsBar moves the chips under a wheel that turns over the bar: to
// the side on the bottom strip, down the sidebar.
func (state *State) scrollCommandsBar(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	if !state.commandsBarAvailable() || !state.commands.shown || state.commands.maxOffset <= 0 ||
		state.commands.filterOpen || state.commands.sortOpen ||
		!pointInRect(event.X, event.Y, state.commands.bar) {
		return gfx.EventResult{}, false
	}
	delta := event.StepY(commandsScrollStep)
	if state.commands.placement == commandsPlacementBottom {
		// A horizontal delta is the user's own sideways gesture; a plain wheel
		// turns its vertical notches into the same sideways move, the way a tab
		// strip does.
		if horizontal := event.StepX(commandsScrollStep); horizontal != 0 {
			delta = horizontal
		}
	}
	if delta == 0 {
		return gfx.EventResult{}, false
	}
	before := state.commands.offset
	state.commands.offset = min(max(before+delta, 0), state.commands.maxOffset)
	return gfx.EventResult{Handled: true, Repaint: state.commands.offset != before}, true
}

// commandTagWidth measures a bottom bar tag's width from its label, clamped to
// sensible bounds.
func (state *State) commandTagWidth(canvas *gfx.Canvas, name string) float32 {
	theme := state.Theme()
	w := canvas.TextWidthWeight(name, theme.Metrics.FontSizeS, gfx.WeightMedium) + commandsTagPaddingX*2
	return max(commandsMinTagWidth, min(commandsMaxTagWidth, w))
}

// paintCommandTag draws one command chip: a rounded, bordered pill that sends on
// click. A hovered chip lifts to an accent tint so the pointer's target is clear.
func (state *State) paintCommandTag(canvas *gfx.Canvas, rect gfx.Rect, entry snippet, id gfx.ID) {
	theme := state.Theme()
	fill := state.pal.surfaceHot
	border := withAlpha(state.pal.line, 0.9)
	if canvas.Hovered() == id {
		fill = withAlpha(state.pal.accent, 0.16)
		border = withAlpha(state.pal.accent, 0.55)
	}
	canvas.RoundedBorder(rect, commandsChipRadius, 1, border, fill)
	label := gfx.Rect{X: rect.X + commandsTagPaddingX, Y: rect.Y, Width: rect.Width - commandsTagPaddingX*2, Height: rect.Height}
	canvas.Label(entry.Name, label, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightMedium, Color: state.pal.text})
	canvas.HitWithOptions(id, rect, entry.Name, gfx.HitOptions{Role: gfx.RoleButton, Hint: entry.Command})
}

// commandBarSwitchShown reports whether the picker's chrome offers the bar's
// checkbox. It is a control of the dialog, not a choice in its list, so the
// send-target picker (also a snippet picker) leaves it out.
func (state *State) commandBarSwitchShown() bool {
	return state.commandPalette.snippets && state.snippetSend.target != sendChosen && state.commandsBarAvailable()
}

// toggleCommandBarFromPicker flips the picker's checkbox. The picker deliberately
// stays open: a checkbox is a state, not a one-shot command, so the user can
// untick it right where they ticked it. Nothing in the searchable list changes —
// not the query, scroll or selection — because the switch is not a list entry.
// The choice is saved like the bar's other settings.
func (state *State) toggleCommandBarFromPicker() {
	state.commands.shown = !state.commands.shown
	if !state.commands.shown {
		state.commands.filterOpen = false
		state.commands.sortOpen = false
	}
	state.save()
}

// setCommandsPlacement docks the bar at the bottom strip or beside the panes.
// A dock change ends any menu or drag that belonged to the old shape, and the
// chips start from the beginning again because the room they have changed.
func (state *State) setCommandsPlacement(placement commandsPlacement) {
	previous := state.commands.placement
	state.commands.placement = placement
	state.commandsPlacementChanged(previous)
}

// commandsPlacementChanged is what follows the picker's dock control, which has
// already written state.commands.placement: only a real change resets the
// menus, drag and chip offset the old dock had.
func (state *State) commandsPlacementChanged(previous commandsPlacement) {
	if state.commands.placement == previous {
		return
	}
	state.commands.offset = 0
	state.commands.filterOpen = false
	state.commands.sortOpen = false
	state.commands.resizing = false
	state.save()
}

// clickCommandsDrawer dispatches a click in the command bar. It returns false
// when the id is not one of the bar's, so the central handler can try the next
// candidate.
func (state *State) clickCommandsDrawer(id gfx.ID) bool {
	if !state.commandsBarAvailable() || !state.commands.shown || state.modal != modalNone {
		return false
	}
	switch {
	case id == hitCommandsFilter:
		state.commands.filterOpen = !state.commands.filterOpen
		state.commands.sortOpen = false
	case id == hitCommandsSort:
		state.commands.sortOpen = !state.commands.sortOpen
		state.commands.filterOpen = false
	case id.Scope() == hitCommandsSortChoice:
		if index := item(id); index >= 0 && index < len(settingsCommandsSortChoices) {
			state.commands.sort = settingsCommandsSortChoices[index]
			state.commands.offset = 0
			state.commands.sortOpen = false
			state.save()
		}
	case id == hitCommandsGroupAll:
		state.commands.hiddenGroups = nil
		state.save()
	case id == hitCommandsGroupNone:
		hidden := map[string]bool{"": true}
		groups, _ := state.commandGroups()
		for _, group := range groups {
			hidden[strings.ToLower(group)] = true
		}
		state.commands.hiddenGroups = hidden
		state.save()
	case id == hitCommandsGroupUngrouped:
		state.commands.hiddenGroups[""] = !state.commands.hiddenGroups[""]
		state.save()
	case id.Scope() == hitCommandsGroupToggle:
		groups, _ := state.commandGroups()
		if index := item(id); index >= 0 && index < len(groups) {
			key := strings.ToLower(groups[index])
			state.commands.hiddenGroups[key] = !state.commands.hiddenGroups[key]
			state.save()
		}
	case id.Scope() == hitCommandsGroupHeading:
		sections := state.commandSections()
		if index := item(id); index >= 0 && index < len(sections) {
			key := sections[index].key
			if state.commands.collapsedGroups == nil {
				state.commands.collapsedGroups = make(map[string]bool)
			}
			state.commands.collapsedGroups[key] = !state.commands.collapsedGroups[key]
			state.save()
		}
	case id.Scope() == hitCommandsTag:
		// Sending a command leaves the keyboard on the bar. Ask for the
		// terminal back so the next keystroke types there, not at whatever
		// sits under the pointer.
		if state.sendSnippet(item(id)) {
			if focus := state.activeTerminalFocusHit(); !focus.IsZero() {
				state.pendingFocus = focus
			}
		}
	case id == hitCommandsAdd:
		state.openSnippetDialog(-1)
	case id == hitCommandsClose:
		state.commands.shown = false
		state.commands.filterOpen = false
		state.commands.sortOpen = false
		state.save()
	default:
		return false
	}
	return true
}

// commandsDrawerResize drags the bar's inner edge. It stores the width the
// user leaves and refits the terminals while the drag runs, so the panes follow
// the pointer instead of jumping when it is released.
func (state *State) commandsDrawerResize(event gfx.PointerEvent) gfx.EventResult {
	commands := &state.commands
	// A press elsewhere ends a gesture whose release the window never saw, and
	// is then handled normally.
	if event.Kind == gfx.PointerDown && event.Target != hitCommandsResize {
		commands.resizing = false
	}
	if !state.commandsBarAvailable() || !commands.shown || commands.placement != commandsPlacementSide || state.modal != modalNone {
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerDown && event.Target == hitCommandsResize && event.Button == gfx.PointerButtonPrimary {
		if bar := state.commandsSidebarBounds(event.Viewport); bar.Width > 0 {
			commands.resizing = true
			commands.dragFrom, commands.dragStart = event.X, bar.Width
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{}
	}
	if !commands.resizing {
		return gfx.EventResult{}
	}
	switch event.Kind {
	case gfx.PointerMove, gfx.PointerUp:
		frame := state.toolbarDockFrame(event.Viewport, true)
		least := min(commandsSidebarMin, frame.Width)
		most := max(frame.Width-commandsMinContent, least)
		width := min(max(commands.dragStart+commands.dragFrom-event.X, least), most)
		if width != commands.width {
			commands.width = width
			state.fitTerminals()
		}
	case gfx.PointerExit:
		commands.resizing = false
	}
	if event.Kind == gfx.PointerUp {
		commands.resizing = false
		state.save()
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}
