package workbench

import (
	"slices"
	"strings"
	"unicode"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// A snippet may belong to one group, a name the user types once and picks after
// that. Groups are not records of their own: one exists while a snippet names
// it. The Snippets page filters its table by group, and the command palette
// searches the group with the name and the command.

const maxSnippetGroup = 80

var (
	hitSnippetFilter = tree.ID(snippetsHits, "snippet-filter")

	hitSnippetGroup        = tree.ID(snippetsHits, "snippet-group")
	hitSnippetGroupTrigger = tree.ID(snippetsHits, "snippet-group-trigger")
	hitSnippetGroupClear   = tree.ID(snippetsHits, "snippet-group-clear")
	hitSnippetGroupDismiss = tree.ID(snippetsHits, "snippet-group-dismiss")
	hitSnippetGroupScroll  = tree.ID(snippetsHits, "snippet-group-scroll")
	snippetGroupScope      = tree.ID(snippetsHits, "snippet-group-scope")
	hitSnippetGroupChoices = tree.Sub(snippetsHits, tree.Name("snippet-group-option"))
)

// snippetFilter is which snippets the page shows: all of them, one group's, or
// the ones that belong to none.
type snippetFilter struct {
	group     string
	ungrouped bool
}

func (filter snippetFilter) all() bool { return filter.group == "" && !filter.ungrouped }

func (filter snippetFilter) matches(entry *snippet) bool {
	switch {
	case filter.ungrouped:
		return entry.Group == ""
	case filter.group != "":
		return sameSnippetGroup(entry.Group, filter.group)
	}
	return true
}

func sameSnippetGroup(a, b string) bool { return strings.EqualFold(a, b) }

func validSnippetGroup(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= maxSnippetGroup && !strings.ContainsFunc(value, unicode.IsControl)
}

// cleanSnippetGroup is the group a saved snippet names: a group that cannot be
// typed in the dialog is no group.
func cleanSnippetGroup(value string) string {
	value = cleanSnippetText(value, maxSnippetGroup)
	if !validSnippetGroup(value) {
		return ""
	}
	return value
}

// snippetDetail is what the command palette shows beside a snippet's name: its
// group and its shortcut.
func snippetDetail(entry *snippet) string {
	switch {
	case entry.Group != "" && entry.Shortcut.Valid():
		return entry.Group + " · " + chordLabel(entry.Shortcut)
	case entry.Group != "":
		return entry.Group
	case entry.Shortcut.Valid():
		return chordLabel(entry.Shortcut)
	}
	return ""
}

// refreshSnippetGroups makes the groups the snippets name and the rows the page
// shows under its filter. A group has one spelling, that of the snippet that
// names it first.
func (form *snippetsUI) refreshSnippetGroups() {
	form.groups = form.groups[:0]
	ungrouped := false
	for index := range form.list {
		group := form.list[index].Group
		switch {
		case group == "":
			ungrouped = true
		case !slices.ContainsFunc(form.groups, func(known string) bool { return sameSnippetGroup(known, group) }):
			form.groups = append(form.groups, group)
		}
	}
	slices.SortStableFunc(form.groups, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	if form.groupCounts == nil {
		form.groupCounts = make(map[string]int, len(form.groups))
	}
	clear(form.groupCounts)
	for index := range form.list {
		group := form.list[index].Group
		if group == "" {
			continue
		}
		if at := slices.IndexFunc(form.groups, func(known string) bool { return sameSnippetGroup(known, group) }); at >= 0 {
			form.groupCounts[form.groups[at]]++
		}
	}
	form.hasUngrouped = ungrouped
	// A filter whose group is gone shows everything rather than nothing.
	if filter := form.filter; filter.ungrouped && !ungrouped || filter.group != "" && !slices.ContainsFunc(form.groups, func(known string) bool { return sameSnippetGroup(known, filter.group) }) {
		form.filter = snippetFilter{}
	}
	form.visible = form.visible[:0]
	for index := range form.list {
		if form.filter.matches(&form.list[index]) {
			form.visible = append(form.visible, index)
		}
	}
	if form.sort.Direction != components.TableSortNone {
		column := form.sort.Column
		slices.SortStableFunc(form.visible, func(a, b int) int {
			comparison := strings.Compare(strings.ToLower(form.list[a].cells[column]), strings.ToLower(form.list[b].cells[column]))
			if form.sort.Direction == components.TableSortDescending {
				return -comparison
			}
			return comparison
		})
	}
	if len(form.visible) > 0 && !slices.Contains(form.visible, form.selected) {
		form.selected = form.visible[0]
	}
}

// showSnippet makes the page show the snippet at index, which the filter may
// have hidden: a snippet just saved must not vanish from the table.
func (form *snippetsUI) showSnippet(index int) {
	if index >= 0 && index < len(form.list) && !form.filter.matches(&form.list[index]) {
		form.filter = snippetFilter{}
	}
}

// The page's group filter is a Select over the groups there are: a negative
// choice is all groups or the ones in none, and a group's index is itself.
const (
	snippetFilterAllChoice       = -2
	snippetFilterUngroupedChoice = -1
)

// declareSnippetFilter declares the select that narrows the table to one group.
func (state *State) declareSnippetFilter(c ui.C) ui.Box {
	form := &state.snippets
	label := state.messages.FieldGroup
	choices := append(form.filterChoices[:0], snippetFilterAllChoice)
	if form.hasUngrouped {
		choices = append(choices, snippetFilterUngroupedChoice)
	}
	for index := range form.groups {
		choices = append(choices, index)
	}
	form.filterChoices = choices
	form.filterChoice = snippetFilterAllChoice
	switch {
	case form.filter.ungrouped:
		form.filterChoice = snippetFilterUngroupedChoice
	case form.filter.group != "":
		for index, group := range form.groups {
			if sameSnippetGroup(group, form.filter.group) {
				form.filterChoice = index
			}
		}
	}
	selector := ui.Select(c, &form.filterChoice, choices, state.snippetFilterChoiceLabel).
		Label(label)
	if selector.Changed() {
		choice := form.filterChoice
		c.After(func() { state.chooseSnippetFilter(choice) })
	}
	return selector
}

// snippetFilterChoiceLabel names one choice of the filter.
func (state *State) snippetFilterChoiceLabel(choice int) string {
	form, m := &state.snippets, state.messages.Tools
	switch {
	case choice == snippetFilterAllChoice:
		return m.SnippetFilterAll
	case choice == snippetFilterUngroupedChoice:
		return m.SnippetFilterNone
	case choice >= 0 && choice < len(form.groups):
		return form.groups[choice]
	}
	return ""
}

// chooseSnippetFilter narrows the page to the chosen group, once the paint that
// delivered the choice is over.
func (state *State) chooseSnippetFilter(choice int) {
	form := &state.snippets
	switch {
	case choice == snippetFilterUngroupedChoice:
		form.filter = snippetFilter{ungrouped: true}
	case choice >= 0 && choice < len(form.groups):
		form.filter = snippetFilter{group: form.groups[choice]}
	default:
		form.filter = snippetFilter{}
	}
	state.refreshSnippets()
}

// The group field of the dialog: pick one of the groups, or type a new one.

// snippetGroupPicker is the dialog's group control, set up to the groups there
// are and to the group of the snippet being edited.
func (state *State) snippetGroupPicker(value string) *components.SingleSelectState {
	control := &state.snippets.groupPicker
	control.AllowCreate = true
	// A group has one name: typing "ops" over "Ops" selects the existing group.
	control.MatchCaseInsensitive = true
	control.SetValidateCreate(validSnippetGroup)
	options := make([]components.SingleSelectOption, len(state.snippets.groups))
	for index, group := range state.snippets.groups {
		options[index] = components.SingleSelectOption{Value: group}
	}
	control.SetOptions(options)
	control.SetValue(value)
	control.Open = false
	return control
}

// refreshSnippetGroupPicker rebuilds the editor's group choices after the
// manager changed a group, keeping what the user typed or chose.
func (state *State) refreshSnippetGroupPicker() {
	control := &state.snippets.groupPicker
	value, query := control.Value(), control.Query.String()
	state.snippetGroupPicker(value)
	if query != "" {
		control.Query.Insert(query)
	}
}

func (state *State) syncSnippetGroupError() {
	form := &state.snippets
	if form.groupPicker.Query.String() == "" && form.dialogError == state.messages.ErrHostGroupInvalid {
		form.dialogError = ""
	}
}

// commitSnippetGroupQuery makes the group that was typed the one chosen. It is
// false, with the reason shown, when what was typed cannot name a group.
func (state *State) commitSnippetGroupQuery() bool {
	form := &state.snippets
	if _, composing := form.groupPicker.Query.Composition(); composing {
		return true
	}
	if strings.TrimSpace(form.groupPicker.Query.String()) == "" {
		return true
	}
	if !form.groupPicker.CommitQuery() {
		form.dialogError = state.messages.ErrHostGroupInvalid
		return false
	}
	return true
}

func isSnippetGroupTarget(id gfx.ID) bool {
	return id == hitSnippetGroup || id == hitSnippetGroupTrigger || id == hitSnippetGroupClear || id == hitSnippetGroupScroll || id.Scope() == hitSnippetGroupChoices
}

func (state *State) clickSnippetGroup(id gfx.ID) bool {
	control := &state.snippets.groupPicker
	switch {
	case id == hitSnippetGroup:
		control.Open = true
	case id == hitSnippetGroupTrigger:
		control.Open = !control.Open
	case id == hitSnippetGroupClear:
		control.Clear()
	case id == hitSnippetGroupDismiss:
		control.Open = false
	case id.Scope() == hitSnippetGroupChoices:
		control.Choose(item(id))
	default:
		return false
	}
	state.syncSnippetGroupError()
	return true
}

// snippetGroupPointer keeps a press outside the open list from reaching the
// dialog: it only closes the list.
func (state *State) snippetGroupPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	control := &state.snippets.groupPicker
	if state.modal != modalSnippet || !control.Open {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && !isSnippetGroupTarget(event.Target) {
		control.Open = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if result := control.Pointer(event); result.Handled {
		return result, true
	}
	return gfx.EventResult{}, false
}

func (state *State) snippetGroupKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	form := &state.snippets
	control := &form.groupPicker
	if state.modal != modalSnippet || form.capturing {
		return gfx.EventResult{}, false
	}
	if event.Target != hitSnippetGroup && (!control.Open || event.Key != gfx.KeyEscape && event.Key != gfx.KeyTab) {
		return gfx.EventResult{}, false
	}
	if event.Target == hitSnippetGroup && event.Key == gfx.KeyEnter && event.Modifiers == 0 && !event.Release {
		// Enter takes what is typed; with nothing typed it is the dialog's Enter
		// and saves, which the list must not consume for a group it did not choose.
		if _, composing := control.Query.Composition(); composing || event.Repeat {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if strings.TrimSpace(control.Query.String()) == "" {
			if control.Open {
				control.Dismiss()
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
			return gfx.EventResult{}, false
		}
		state.commitSnippetGroupQuery()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	result := control.Key(event)
	state.syncSnippetGroupError()
	return result, result.Handled || result.Repaint
}

func (state *State) paintSnippetGroupField(canvas *gfx.Canvas, bounds gfx.Rect) {
	form, theme := &state.snippets, state.Theme()
	control := &form.groupPicker
	input := components.BeginField(canvas, theme, bounds).For(hitSnippetGroup).Paint(state.messages.FieldGroup, "", "")
	query := strings.TrimSpace(control.Query.String())
	components.PaintSingleSelect(canvas, theme, input, control, components.SingleSelectOptions{
		InputID: hitSnippetGroup, TriggerID: hitSnippetGroupTrigger, ClearID: hitSnippetGroupClear, DismissID: hitSnippetGroupDismiss, Scope: snippetGroupScope, ScrollID: hitSnippetGroupScroll, OptionIDs: hitSnippetGroupChoices,
		Label: state.messages.FieldGroup, Placeholder: state.messages.FieldGroupPlaceholder, EmptyLabel: state.messages.HostGroupsEmpty, CreatePrefix: state.messages.HostGroupCreatePrefix, ClearLabel: state.messages.MoveGroupNone,
		Viewport: gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height}, Layer: dialogLayer + 2, Invalid: query != "" && !validSnippetGroup(query),
	})
}
