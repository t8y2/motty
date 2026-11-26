package workbench

import (
	"slices"
	"strings"
	"unicode"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// A snippet may belong to one group, a name or slash-separated path
// (e.g. "Clients/Acme/Production") the user types once and picks after that.
// Groups are not records of their own: one exists while a snippet names it or
// a descendant beneath it. The Snippets page filters its table by group
// (including descendants), and the command palette searches the group with the
// name and the command.

const maxSnippetGroup = 512

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
		return snippetGroupWithin(entry.Group, filter.group)
	}
	return true
}

func sameSnippetGroup(a, b string) bool { return strings.EqualFold(a, b) }

// normalizeSnippetGroupPath is the one form a snippet group path is kept in:
// its folder names, in order, joined with "/". Spaces around separators and
// empty segments are dropped, matching normalizeHostGroupPath.
func normalizeSnippetGroupPath(path string) string {
	if !strings.Contains(path, "/") {
		return strings.TrimSpace(path)
	}
	segments := make([]string, 0, 4)
	for _, segment := range strings.Split(path, "/") {
		if segment = strings.TrimSpace(segment); segment != "" {
			segments = append(segments, segment)
		}
	}
	return strings.Join(segments, "/")
}

func snippetGroupLeaf(path string) string {
	return path[strings.LastIndex(path, "/")+1:]
}

func snippetGroupParent(path string) string {
	if at := strings.LastIndex(path, "/"); at >= 0 {
		return path[:at]
	}
	return ""
}

func snippetGroupDepth(path string) uint8 {
	return uint8(min(strings.Count(path, "/"), 12))
}

// snippetGroupWithin reports whether path is parent or a descendant of parent,
// comparing segments case-insensitively.
func snippetGroupWithin(path, parent string) bool {
	if sameSnippetGroup(path, parent) {
		return true
	}
	if parent == "" {
		return false
	}
	for {
		p, prest, pmore := strings.Cut(path, "/")
		a, arest, amore := strings.Cut(parent, "/")
		if !strings.EqualFold(p, a) {
			return false
		}
		if !amore {
			return pmore
		}
		if !pmore {
			return false
		}
		path, parent = prest, arest
	}
}

// snippetGroupLess compares two group paths one segment at a time so
// punctuation in a sibling cannot split a subtree, matching hostGroupLess.
func snippetGroupLess(left, right string) bool {
	for {
		l, lrest, lmore := strings.Cut(left, "/")
		r, rrest, rmore := strings.Cut(right, "/")
		if order := strings.Compare(strings.ToLower(l), strings.ToLower(r)); order != 0 {
			return order < 0
		}
		if l != r {
			return l < r
		}
		if !lmore || !rmore {
			return !lmore && rmore
		}
		left, right = lrest, rrest
	}
}

// remapSnippetGroupPath rewrites a path in the from subtree to sit under to,
// preserving the spelling of any descendant segments.
func remapSnippetGroupPath(path, from, to string) string {
	if sameSnippetGroup(path, from) {
		return to
	}
	if !snippetGroupWithin(path, from) {
		return path
	}
	segments := strings.Count(from, "/") + 1
	rest := path
	for range segments {
		_, after, ok := strings.Cut(rest, "/")
		if !ok {
			return to
		}
		rest = after
	}
	if rest == "" {
		return to
	}
	return to + "/" + rest
}

func validSnippetGroup(value string) bool {
	value = normalizeSnippetGroupPath(value)
	return value != "" && len(value) <= maxSnippetGroup && !strings.ContainsFunc(value, unicode.IsControl)
}

// cleanSnippetGroup is the group a saved snippet names: a group that cannot be
// typed in the dialog is no group.
func cleanSnippetGroup(value string) string {
	value = normalizeSnippetGroupPath(cleanSnippetText(value, maxSnippetGroup))
	if !validSnippetGroup(value) {
		return ""
	}
	return value
}

// restoreSnippetGroup migrates a snippet group loaded from saved state into a
// normalized slash-separated path without dropping previously valid flat group
// names to ungrouped. Names containing "/" become nested paths; names composed
// solely of "/" and whitespace (which were valid as flat strings before nested
// groups existed) keep their slashes as fullwidth "／" so no group is lost on
// the next save.
func restoreSnippetGroup(value string) string {
	cleaned := cleanSnippetText(value, maxSnippetGroup)
	if cleaned == "" {
		return ""
	}
	if normalized := normalizeSnippetGroupPath(cleaned); normalized != "" {
		return normalized
	}
	// Preserve slash-only legacy flat group names (e.g. "/", "//") without
	// exceeding maxSnippetGroup bytes.
	migrated := strings.ReplaceAll(cleaned, "/", "／")
	return cleanSnippetText(migrated, maxSnippetGroup)
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

// refreshSnippetGroups makes the groups the snippets name (plus every ancestor
// folder in their slash-separated paths) and the rows the page shows under its
// filter. A group has one spelling, that of the snippet that names it first.
func (form *snippetsUI) refreshSnippetGroups() {
	form.groups = form.groups[:0]
	if form.groupCounts == nil {
		form.groupCounts = make(map[string]int)
	}
	clear(form.groupCounts)
	canonical := make(map[string]string, len(form.list))
	add := func(group string) string {
		key := strings.ToLower(group)
		if known, ok := canonical[key]; ok {
			return known
		}
		canonical[key] = group
		form.groups = append(form.groups, group)
		return group
	}
	ungrouped := false
	for index := range form.list {
		group := form.list[index].Group
		if group == "" {
			ungrouped = true
			continue
		}
		for path := group; path != ""; path = snippetGroupParent(path) {
			known := add(path)
			form.groupCounts[known]++
		}
	}
	for _, group := range form.customGroups {
		for path := group; path != ""; path = snippetGroupParent(path) {
			add(path)
		}
	}
	slices.SortStableFunc(form.groups, func(a, b string) int {
		switch {
		case snippetGroupLess(a, b):
			return -1
		case snippetGroupLess(b, a):
			return 1
		}
		return 0
	})
	form.hasUngrouped = ungrouped
	// A filter whose group is gone shows everything rather than nothing.
	if filter := form.filter; filter.ungrouped && !ungrouped {
		form.filter = snippetFilter{}
	} else if filter.group != "" {
		if _, ok := canonical[strings.ToLower(filter.group)]; !ok {
			form.filter = snippetFilter{}
		}
	}
	form.visible = form.visible[:0]
	for index := range form.list {
		if form.filter.matches(&form.list[index]) {
			form.visible = append(form.visible, index)
		}
	}
	if len(form.visible) > 0 && !slices.Contains(form.visible, form.selected) {
		form.selected = form.visible[0]
	}
}

// showSnippet makes the page show the snippet at index, which the filter may
// have hidden: a snippet just saved must not vanish from the tree.
func (form *snippetsUI) showSnippet(index int) {
	if index >= 0 && index < len(form.list) && !form.filter.matches(&form.list[index]) {
		form.filter = snippetFilter{}
	}
}

// The group field of the inline editor: pick one of the groups, or type a new one.

func isSnippetInlineGroupTarget(id gfx.ID) bool {
	return id == hitSnippetInlineGroup || id == hitSnippetInlineGroupTrigger || id == hitSnippetInlineGroupClear || id == hitSnippetInlineGroupScroll || id.Scope() == hitSnippetInlineGroupChoices
}

func (state *State) clickSnippetInlineGroup(id gfx.ID) bool {
	control := &state.snippets.inline.groupPicker
	changed := false
	switch {
	case id == hitSnippetInlineGroup:
		control.Open = true
	case id == hitSnippetInlineGroupTrigger:
		control.Open = !control.Open
	case id == hitSnippetInlineGroupClear:
		control.Clear()
		changed = true
	case id == hitSnippetInlineGroupDismiss:
		control.Open = false
	case id.Scope() == hitSnippetInlineGroupChoices:
		control.Choose(item(id))
		changed = true
	default:
		return false
	}
	if state.snippets.inline.groupPicker.Query.String() == "" && state.snippets.inline.error == state.messages.ErrHostGroupInvalid {
		state.snippets.inline.error = ""
	}
	if changed {
		state.applyInlineSnippetGroup()
	}
	return true
}

// snippetGroupPointer keeps a press outside the open list from reaching the
// page: it only closes the list.
func (state *State) snippetGroupPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.modal == modalNone && state.activeSection == sectionSnippets {
		control := &state.snippets.inline.groupPicker
		if !control.Open {
			return gfx.EventResult{}, false
		}
		if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && !isSnippetInlineGroupTarget(event.Target) {
			control.Open = false
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		if result := control.Pointer(event); result.Handled {
			return result, true
		}
	}
	return gfx.EventResult{}, false
}

func (state *State) snippetGroupKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	form := &state.snippets
	if state.modal != modalNone || state.activeSection != sectionSnippets || form.inline.capturing {
		return gfx.EventResult{}, false
	}
	control := &form.inline.groupPicker
	if event.Target != hitSnippetInlineGroup && (!control.Open || event.Key != gfx.KeyEscape && event.Key != gfx.KeyTab) {
		return gfx.EventResult{}, false
	}
	if event.Target == hitSnippetInlineGroup && event.Key == gfx.KeyEnter && event.Modifiers == 0 && !event.Release {
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
		if !control.CommitQuery() {
			form.inline.error = state.messages.ErrHostGroupInvalid
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		form.inline.error = ""
		state.applyInlineSnippetGroup()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	before := control.Value()
	result := control.Key(event)
	if control.Query.String() == "" && form.inline.error == state.messages.ErrHostGroupInvalid {
		form.inline.error = ""
	}
	if control.Value() != before {
		state.applyInlineSnippetGroup()
	}
	return result, result.Handled || result.Repaint
}

func (state *State) paintSnippetInlineGroupField(canvas *gfx.Canvas, bounds gfx.Rect) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	form, theme := &state.snippets, state.Theme()
	control := &form.inline.groupPicker
	input := components.BeginField(canvas, theme, bounds).For(hitSnippetInlineGroup).Paint(state.messages.FieldGroup, "", "")
	invalid := false
	if control.Query.RuneCount() > 0 {
		query := strings.TrimSpace(control.Query.String())
		invalid = query != "" && !validSnippetGroup(query)
	}
	components.PaintSingleSelect(canvas, theme, input, control, components.SingleSelectOptions{
		InputID: hitSnippetInlineGroup, TriggerID: hitSnippetInlineGroupTrigger, ClearID: hitSnippetInlineGroupClear, DismissID: hitSnippetInlineGroupDismiss, Scope: snippetInlineGroupScope, ScrollID: hitSnippetInlineGroupScroll, OptionIDs: hitSnippetInlineGroupChoices,
		Label: state.messages.FieldGroup, Placeholder: state.messages.FieldGroupPlaceholder, EmptyLabel: state.messages.HostGroupsEmpty, CreatePrefix: state.messages.HostGroupCreatePrefix, ClearLabel: state.messages.MoveGroupNone,
		Viewport: gfx.Rect{Width: state.viewport.Width, Height: state.viewport.Height}, Layer: dialogLayer + 2, Invalid: invalid,
	})
}
