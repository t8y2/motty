package workbench

import (
	"sort"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// commandsHits is the identity-tree place of the bottom command drawer, a
// persistent strip of quick commands beside the terminal. It shares the snippet
// list with the Snippets page, so a chip here and a row there name the same
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
	// hitCommandsSortDismiss is that dropdown's backdrop.
	hitCommandsSortDismiss = tree.ID(commandsHits, "sort-dismiss")
	// hitCommandsSortChoice names an order in the dropdown; its index is the
	// position of the order in settingsCommandsSortChoices.
	hitCommandsSortChoice = tree.Sub(commandsHits, tree.Name("sort-choice"))
	// hitCommandsFilter opens the group-filter dropdown at the bar's left.
	hitCommandsFilter = tree.ID(commandsHits, "filter")
	// hitCommandsFilterDismiss is the backdrop of that dropdown: a press anywhere
	// but the menu reports it, closing the dropdown without changing the filter.
	hitCommandsFilterDismiss = tree.ID(commandsHits, "filter-dismiss")
	// hitCommandsGroupUngrouped toggles the ungrouped commands in the filter.
	hitCommandsGroupUngrouped = tree.ID(commandsHits, "group-ungrouped")
	// hitCommandsGroupAll / None show or hide every group at once.
	hitCommandsGroupAll  = tree.ID(commandsHits, "group-all")
	hitCommandsGroupNone = tree.ID(commandsHits, "group-none")
	// hitCommandsGroupToggle names a group's checkbox in the filter; its index is
	// the position of the group in state.commandGroups().
	hitCommandsGroupToggle = tree.Sub(commandsHits, tree.Name("group-toggle"))
	// hitCommandsTag names a snippet's chip in the bar; its index is the
	// snippet's index in state.snippets.list.
	hitCommandsTag = tree.Sub(commandsHits, tree.Name("tag"))
)

const (
	commandsBarHeight     float32 = 34 // the single, always-visible command bar
	commandsChipInset     float32 = 5  // vertical inset of chips inside the bar
	commandsDrawerPadding float32 = 8  // horizontal padding at the bar's ends
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
)

// commandsUI is the bottom command drawer's state. The single bar stays hidden
// until shown opts it in (zero height, no bar). hiddenGroups are the
// snippet groups (by lowercased name; "" means ungrouped) not shown in the bar,
// and sort is the order the visible commands appear in.
type commandsUI struct {
	// shown is whether the user opted the bar in. The zero value hides it,
	// so a fresh install sees no bar until the picker's checkbox turns it on.
	shown bool
	// filterOpen is whether the group-filter dropdown is showing.
	filterOpen bool
	// sortOpen is whether the order dropdown is showing.
	sortOpen bool
	// hiddenGroups are the snippet groups hidden from the bar, keyed by their
	// lowercased name; the empty key means ungrouped commands. Absent = shown.
	// A fresh state (nil) shows every group.
	hiddenGroups map[string]bool
	// sort is how the visible commands are ordered along the bar.
	sort commandsSort
	// offset is how far the chips are scrolled right; it is where the wheel puts
	// them when more commands exist than the bar can hold.
	offset float32
	// bar is the last painted strip and maxOffset the largest offset that still
	// shows a chip, both recorded so a wheel over the bar knows what it moves.
	bar       gfx.Rect
	maxOffset float32
}

// commandsBarAvailable reports whether the active page can host the bar at all.
// The picker only offers to show the bar where showing it would do something.
func (state *State) commandsBarAvailable() bool {
	if !state.terminalPage() {
		return false
	}
	// The bar types snippets into a terminal; a remote desktop (RDP/VNC)
	// session has no shell to receive them, so the strip hides on those pages.
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.host.isDesktop() {
		return false
	}
	return true
}

// commandsDrawerHeight is the bottom space the drawer reserves, subtracted from
// the terminal panes so they do not sit under it. Zero off the terminal page.
func (state *State) commandsDrawerHeight() float32 {
	if !state.commandsBarAvailable() || !state.commands.shown {
		return 0
	}
	return commandsBarHeight
}

// commandGroupVisible reports whether a snippet's group is shown in the bar.
func (state *State) commandGroupVisible(group string) bool {
	return !state.commands.hiddenGroups[strings.ToLower(strings.TrimSpace(group))]
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

// commandDisplayOrder returns the snippet indices to show in the bar: the ones
// whose group is visible, ordered by the chosen sort.
func (state *State) commandDisplayOrder() []int {
	list := state.snippets.list
	order := make([]int, 0, len(list))
	for i := range list {
		if state.commandGroupVisible(list[i].Group) {
			order = append(order, i)
		}
	}
	lower := func(text string) string { return strings.ToLower(text) }
	switch state.commands.sort {
	case commandsSortName:
		sort.SliceStable(order, func(a, b int) bool {
			return lower(list[order[a]].Name) < lower(list[order[b]].Name)
		})
	case commandsSortGroup:
		sort.SliceStable(order, func(a, b int) bool {
			groupA, groupB := lower(list[order[a]].Group), lower(list[order[b]].Group)
			if groupA != groupB {
				return groupA < groupB
			}
			return lower(list[order[a]].Name) < lower(list[order[b]].Name)
		})
	}
	return order
}

// paintCommandsDrawer draws the single command bar beneath the terminal panes,
// and its filter or order dropdown when open. A filter button and an order
// button sit at the left; the add and close buttons are grouped on the right;
// command chips fill the middle and scroll when they do not fit.
func (state *State) paintCommandsDrawer(canvas *gfx.Canvas, size gfx.Size) {
	height := state.commandsDrawerHeight()
	if height <= 0 {
		// Nothing is painted, so a wheel must not think the old strip is there.
		state.commands.bar = gfx.Rect{}
		state.commands.maxOffset = 0
		return
	}
	wb := state.workspacePixelBounds(size)
	bar := gfx.Rect{X: wb.X, Y: wb.Y + wb.Height, Width: wb.Width, Height: height}
	state.commands.bar = bar
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
		state.paintCommandChips(canvas, bar, contentX, contentW)
	}

	if state.commands.filterOpen {
		state.paintCommandsFilterMenu(canvas, bar, filterBtn)
	}
	if state.commands.sortOpen {
		state.paintCommandsSortMenu(canvas, bar, sortBtn)
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

// paintCommandChips draws the visible commands in the strip's middle. More
// commands than fit scroll sideways with the wheel, so a long list stays
// reachable without widening the bar; the clip keeps a half-scrolled chip from
// running under the buttons at either end.
func (state *State) paintCommandChips(canvas *gfx.Canvas, bar gfx.Rect, x, width float32) {
	order := state.commandDisplayOrder()
	chipH := commandsBarHeight - commandsChipInset*2
	y := bar.Y + commandsChipInset
	extent := float32(0)
	for i, index := range order {
		if i > 0 {
			extent += commandsTagGap
		}
		extent += state.commandTagWidth(canvas, state.snippets.list[index].Name)
	}
	maxOffset := max(extent-width, 0)
	state.commands.maxOffset = maxOffset
	state.commands.offset = min(max(state.commands.offset, 0), maxOffset)
	if len(order) == 0 {
		return
	}
	canvas.Clip(gfx.Rect{X: x, Y: bar.Y, Width: width, Height: bar.Height})
	x0 := x - state.commands.offset
	for _, index := range order {
		w := state.commandTagWidth(canvas, state.snippets.list[index].Name)
		if x0+w > x && x0 < x+width {
			state.paintCommandTag(canvas, gfx.Rect{X: x0, Y: y, Width: w, Height: chipH}, state.snippets.list[index].Name, hitCommandsTag.At(index))
		}
		x0 += w + commandsTagGap
		if x0 >= x+width {
			break
		}
	}
	canvas.Unclip()
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

// commandsMenuBounds places a dropdown above the bar, level with its trigger.
// The bar sits at the window's bottom, so its menus always open upwards; the
// menu's Side makes its entrance come from that same edge.
func (state *State) commandsMenuBounds(canvas *gfx.Canvas, bar, anchor gfx.Rect, width, height float32) gfx.Rect {
	viewport := state.windowRect()
	bounds := state.popupBelow(canvas, anchor, width, height, ui.Start)
	bounds.Y = max(12, bar.Y-height-6)
	// Line the surface up with the trigger's hover pill, not the glyph inside it.
	bounds.X = max(12, bounds.X-4)
	if bounds.X+width > viewport.Width-12 {
		bounds.X = max(12, viewport.Width-12-width)
	}
	return bounds
}

// commandsMenuHeight counts the rows and the divider, plus the surface's own
// padding. The heading is a plain (disabled) row like every other row, so it is
// one of rows; leaving any of them out clips the menu's last row.
func commandsMenuHeight(rows int, divider bool) float32 {
	const rowHeight = float32(32) // the dropdown component's standard row
	height := float32(rows)*rowHeight + 2*components.DropdownMenuPadding
	if divider {
		height += components.DropdownMenuSeparatorHeight
	}
	return height
}

// commandsMenuHeading is a muted, non-interactive first row. It uses AddItem,
// not AddLabel: a dropdown label is smaller and indented, which would make the
// menu's type uneven and its rows start at two different left edges.
func commandsMenuHeading(label string) components.DropdownMenuItem {
	return components.DropdownMenuItem{Label: label, Disabled: true}
}

// paintCommandsFilterMenu draws the group-filter dropdown: a heading, a checkbox
// per group (and one for ungrouped), then Show all / Hide all. It is the project's
// standard dropdown menu, opened upwards from the bar's left-hand button. Every
// label starts at the same x and uses the same size; only the check marks sit on
// the right.
func (state *State) paintCommandsFilterMenu(canvas *gfx.Canvas, bar gfx.Rect, anchor gfx.Rect) {
	m := state.messages.Tools
	heading := commandsMenuHeading(m.CommandsFilter)
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
	items = append(items, heading)
	items = append(items, choices...)
	items = append(items, actions...)
	width := state.commandsMenuWidth(canvas, items, 190)
	height := commandsMenuHeight(len(items), !empty)
	menu := components.BeginDropdownMenu(canvas, state.Theme(), state.commandsMenuBounds(canvas, bar, anchor, width, height), components.DropdownMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitCommandsFilterDismiss,
		Viewport: state.windowRect(), Scroll: state.menuScrollFor(hitCommandsFilterDismiss),
		Side: components.OverlayTop,
	})
	state.menuPlaced(menu.Bounds())
	menu.AddItem(heading)
	for _, choice := range choices {
		menu.AddItem(choice)
	}
	if !empty {
		menu.AddSeparator()
	}
	for _, action := range actions {
		menu.AddItem(action)
	}
	menu.End()
}

// paintCommandsSortMenu draws the order dropdown: a heading and one radio item
// per order. It is the same standard menu as the filter's, opened upwards from
// the order button. Picking an order closes it, since the bar behind already
// shows the result.
func (state *State) paintCommandsSortMenu(canvas *gfx.Canvas, bar gfx.Rect, anchor gfx.Rect) {
	items := []components.DropdownMenuItem{commandsMenuHeading(state.messages.Tools.CommandsSortRow)}
	for i, order := range settingsCommandsSortChoices {
		current := state.commands.sort == order
		items = append(items, components.DropdownMenuItem{
			ID: hitCommandsSortChoice.At(i), Label: state.settingsCommandsSortLabel(order),
			Kind: components.DropdownMenuRadio, Checked: current, Selected: current,
		})
	}
	width := state.commandsMenuWidth(canvas, items, 180)
	height := commandsMenuHeight(len(items), false)
	menu := components.BeginDropdownMenu(canvas, state.Theme(), state.commandsMenuBounds(canvas, bar, anchor, width, height), components.DropdownMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitCommandsSortDismiss,
		Viewport: state.windowRect(), Scroll: state.menuScrollFor(hitCommandsSortDismiss),
		Side: components.OverlayTop,
	})
	state.menuPlaced(menu.Bounds())
	for _, item := range items {
		menu.AddItem(item)
	}
	menu.End()
}

// scrollCommandsBar moves the chips under a wheel that turns over the bar. A
// horizontal delta is the user's own sideways gesture; a plain wheel turns its
// vertical notches into the same sideways move, the way a tab strip does.
func (state *State) scrollCommandsBar(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	if state.commandsDrawerHeight() <= 0 || state.commands.maxOffset <= 0 ||
		state.commands.filterOpen || state.commands.sortOpen ||
		!pointInRect(event.X, event.Y, state.commands.bar) {
		return gfx.EventResult{}, false
	}
	delta := event.StepX(commandsScrollStep)
	if delta == 0 {
		delta = event.StepY(commandsScrollStep)
	}
	before := state.commands.offset
	state.commands.offset = min(max(before+delta, 0), state.commands.maxOffset)
	return gfx.EventResult{Handled: true, Repaint: state.commands.offset != before}, true
}

// paintCommandTag draws one command chip: a rounded, bordered pill that sends on
// click. A hovered chip lifts to an accent tint so the pointer's target is clear.
func (state *State) paintCommandTag(canvas *gfx.Canvas, rect gfx.Rect, name string, id gfx.ID) {
	theme := state.Theme()
	fill := state.pal.surfaceHot
	border := withAlpha(state.pal.line, 0.9)
	if canvas.Hovered() == id {
		fill = withAlpha(state.pal.accent, 0.16)
		border = withAlpha(state.pal.accent, 0.55)
	}
	canvas.RoundedBorder(rect, commandsChipRadius, 1, border, fill)
	label := gfx.Rect{X: rect.X + commandsTagPaddingX, Y: rect.Y, Width: rect.Width - commandsTagPaddingX*2, Height: rect.Height}
	canvas.Label(name, label, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightMedium, Color: state.pal.text})
	canvas.HitWithOptions(id, rect, name, gfx.HitOptions{Role: gfx.RoleButton})
}

// commandTagWidth measures a tag's width from its label, clamped to sensible bounds.
func (state *State) commandTagWidth(canvas *gfx.Canvas, name string) float32 {
	theme := state.Theme()
	w := canvas.TextWidthWeight(name, theme.Metrics.FontSizeS, gfx.WeightMedium) + commandsTagPaddingX*2
	return max(commandsMinTagWidth, min(commandsMaxTagWidth, w))
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

// clickCommandsDrawer dispatches a click in the bottom command drawer. It returns
// false when the id is not one of the drawer's, so the central handler can try
// the next candidate.
func (state *State) clickCommandsDrawer(id gfx.ID) bool {
	if !state.terminalPage() || state.modal != modalNone {
		return false
	}
	switch {
	case id == hitCommandsFilter:
		state.commands.filterOpen = !state.commands.filterOpen
		state.commands.sortOpen = false
	case id == hitCommandsFilterDismiss:
		state.commands.filterOpen = false
	case id == hitCommandsSort:
		state.commands.sortOpen = !state.commands.sortOpen
		state.commands.filterOpen = false
	case id == hitCommandsSortDismiss:
		state.commands.sortOpen = false
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
	case id.Scope() == hitCommandsTag:
		state.sendSnippet(item(id))
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
