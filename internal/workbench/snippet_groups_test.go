package workbench

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// "cd " and "ssh " are sent to be finished by hand: the space the user typed
// after them is part of the command, through saving, restoring and sending.
func TestSnippetCommandKeepsItsSurroundingSpace(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("  go to  ")
	state.snippets.inline.command.Insert(" cd ")
	state.Click(hitSnippetInlineRun)
	state.applyInlineSnippetEdits()
	state.flushInlineSnippetSave()
	if len(state.snippets.list) != 1 {
		t.Fatal("the snippet was not saved")
	}
	if got := state.snippets.list[0]; got.Name != "go to" || got.Command != " cd " {
		t.Fatalf("saved name %q, command %q", got.Name, got.Command)
	}

	state.focusInlineSnippet(0)
	if got := state.snippets.inline.command.String(); got != " cd " {
		t.Fatalf("editing shows command %q", got)
	}

	saved := savedState{Snippets: []savedSnippet{{Name: "ssh", Command: "ssh "}}}
	state.restoreTerminalTools(saved)
	if got := state.snippets.list[0].Command; got != "ssh " {
		t.Fatalf("restored command %q", got)
	}
}

func TestSnippetCommandOfOnlySpaceIsRefused(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("blank")
	state.snippets.inline.command.Insert("   ")
	state.applyInlineSnippetEdits()
	if !state.snippets.inline.draft || len(state.snippets.list) != 0 || state.snippets.inline.error == "" {
		t.Fatal("a command of only space was saved")
	}
}

func TestMultilineSnippetKeepsLinesAndSendsAsPaste(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("deploy")
	state.snippets.inline.command.Insert("echo one\r\necho two")
	if result := state.Key(gfx.KeyEvent{Target: hitSnippetInlineCommand, Key: gfx.KeyEnter}); result.Handled {
		t.Fatalf("Enter in multiline command: result=%+v", result)
	}
	state.applyInlineSnippetEdits()
	state.flushInlineSnippetSave()
	if len(state.snippets.list) != 1 || state.snippets.list[0].Command != "echo one\necho two" || state.snippets.list[0].cells[3] != "echo one ↵ echo two" {
		t.Fatalf("saved multiline snippet = %+v", state.snippets.list)
	}
	loaded := newTestState(t, options)
	if len(loaded.snippets.list) != 1 || loaded.snippets.list[0].Command != "echo one\necho two" {
		t.Fatalf("restored multiline snippet = %+v", loaded.snippets.list)
	}
	loaded.activeSection = sectionHosts
	world.connectFirst(t, loaded)
	shell := world.conns[0].shell(t)
	if !loaded.sendSnippetTo(0, &loaded.sessions[0]) || shell.typed() != "echo one\recho two\r" {
		t.Fatalf("plain shell received %q", shell.typed())
	}
	loaded.sessions[0].term.screen.Write([]byte("\x1b[?2004h"))
	if !loaded.sendSnippetTo(0, &loaded.sessions[0]) || !strings.HasSuffix(shell.typed(), "\x1b[200~echo one\recho two\x1b[201~\r") {
		t.Fatalf("bracketed paste shell received %q", shell.typed())
	}
}

func addTestSnippets(state *State, entries ...snippet) {
	state.snippets.list = append(state.snippets.list[:0], entries...)
	state.refreshSnippets()
}

func TestSnippetManualOrderPersistsThroughTreeReorder(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "zulu", Command: "echo z", Group: "ops"},
		snippet{Name: "alpha", Command: "echo a", Group: "dev"},
		snippet{Name: "beta", Command: "echo b", Group: "ops"},
	)
	state.snippets.filter = snippetFilter{group: "ops"}
	state.refreshSnippets()
	if !state.moveSnippet(2, -1) || state.snippets.selected != 0 {
		t.Fatalf("move in filtered list: visible=%v selected=%d", state.snippets.visible, state.snippets.selected)
	}
	if got := []string{state.snippets.list[0].Name, state.snippets.list[1].Name, state.snippets.list[2].Name}; !slices.Equal(got, []string{"beta", "alpha", "zulu"}) {
		t.Fatalf("reordered list = %v", got)
	}
	state.snippets.filter = snippetFilter{}
	state.refreshSnippets()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if state.snippets.tree.RowID(0).IsZero() {
		t.Fatal("directory tree did not declare its rows")
	}
	// Tree rows: dev(0), alpha(1), ops(2), beta(3), zulu(4).
	// Dragging zulu before beta reorders within ops and persists across reloads.
	state.dropSnippetTreeRow(ui.TreeMove{From: 4, To: 3, Where: ui.TreeDropBefore})
	if got := []string{state.snippets.list[0].Name, state.snippets.list[1].Name, state.snippets.list[2].Name}; !slices.Equal(got, []string{"zulu", "beta", "alpha"}) {
		t.Fatalf("tree reorder: order=%v", got)
	}
	if !slices.Equal(state.snippets.visible, []int{0, 1, 2}) {
		t.Fatalf("manual order visible = %v", state.snippets.visible)
	}
	loaded := newTestState(t, options)
	if got := []string{loaded.snippets.list[0].Name, loaded.snippets.list[1].Name, loaded.snippets.list[2].Name}; !slices.Equal(got, []string{"zulu", "beta", "alpha"}) {
		t.Fatalf("restored order = %v", got)
	}
}

func TestSnippetOrderRestoresLegacyAndDuplicateNames(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.restoreTerminalTools(savedState{Snippets: []savedSnippet{
		{Name: "same", Command: "echo first"},
		{Name: "same", Command: "echo second"},
		{Name: "last", Command: "echo third"},
	}})
	if got := []string{state.snippets.list[0].Command, state.snippets.list[1].Command, state.snippets.list[2].Command}; !slices.Equal(got, []string{"echo first", "echo second", "echo third"}) {
		t.Fatalf("legacy order = %v", got)
	}
	state.restoreTerminalTools(savedState{Snippets: []savedSnippet{
		{Name: "last", Command: "echo third", Order: 2},
		{Name: "same", Command: "echo first", Order: 0},
		{Name: "same", Command: "echo second", Order: 1},
	}})
	if got := []string{state.snippets.list[0].Command, state.snippets.list[1].Command, state.snippets.list[2].Command}; !slices.Equal(got, []string{"echo first", "echo second", "echo third"}) {
		t.Fatalf("manual order with duplicate names = %v", got)
	}
}

func BenchmarkPaintSnippetsPage(b *testing.B) {
	state := newTestState(b, freshOptions(b))
	state.activeSection = sectionSnippets
	for index := range 30 {
		state.snippets.list = append(state.snippets.list, snippet{
			Name: fmt.Sprintf("Command %d", index), Command: "echo one\necho two",
		})
	}
	state.refreshSnippets()
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

// The page lists the snippets of one group at a time, and a group exists only
// while a snippet names it.
func TestSnippetFilterShowsOneGroup(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state,
		snippet{Name: "a", Command: "a", Group: "Ops"},
		snippet{Name: "b", Command: "b"},
		snippet{Name: "c", Command: "c", Group: "dev"},
		snippet{Name: "d", Command: "d", Group: "ops"},
	)
	form := &state.snippets
	if len(form.groups) != 2 {
		t.Fatalf("groups = %v, want Ops and dev once each", form.groups)
	}
	if len(form.visible) != 4 {
		t.Fatalf("unfiltered page shows %d rows", len(form.visible))
	}

	form.filter = snippetFilter{group: "OPS"}
	state.refreshSnippets()
	if len(form.visible) != 2 {
		t.Fatalf("filter by Ops shows %v", form.visible)
	}
	for _, index := range form.visible {
		if !sameSnippetGroup(form.list[index].Group, "ops") {
			t.Fatalf("row %d of group %q is shown under Ops", index, form.list[index].Group)
		}
	}
	if form.selected != form.visible[0] && !containsInt(form.visible, form.selected) {
		t.Fatalf("selected row %d is hidden by the filter", form.selected)
	}

	form.filter = snippetFilter{ungrouped: true}
	state.refreshSnippets()
	if len(form.visible) != 1 || form.list[form.visible[0]].Name != "b" {
		t.Fatalf("ungrouped filter shows %v", form.visible)
	}

	// Removing the last snippet of the filtered group must not leave an empty page.
	form.filter = snippetFilter{group: "dev"}
	state.refreshSnippets()
	state.deleteSnippet(2)
	if !form.filter.all() || len(form.visible) != 3 {
		t.Fatalf("after the group went away: filter %+v, %d rows", form.filter, len(form.visible))
	}
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSnippetFilterIsChosenFromThePage(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "a", Command: "a", Group: "Ops"},
		snippet{Name: "b", Command: "b"},
	)
	state.Click(hitSnippetTreeSearchToggle)
	state.snippets.treeSearch.Insert("Ops")
	state.rebuildSnippetTreeRows()
	if len(state.snippets.visible) != 1 || state.snippets.list[state.snippets.visible[0]].Name != "a" {
		t.Fatalf("search Ops: visible=%v", state.snippets.visible)
	}
	state.Click(hitSnippetTreeSearchToggle)
	if len(state.snippets.visible) != 2 {
		t.Fatalf("closing search left visible=%v", state.snippets.visible)
	}
}

// A snippet saved while another group is filtered must not vanish from the page.
func TestSavedSnippetStaysVisibleUnderTheFilter(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state, snippet{Name: "a", Command: "a", Group: "Ops"}, snippet{Name: "b", Command: "b", Group: "dev"})
	state.snippets.filter = snippetFilter{group: "Ops"}
	state.refreshSnippets()

	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("c")
	state.snippets.inline.command.Insert("c")
	state.applyInlineSnippetEdits()
	state.flushInlineSnippetSave()
	if state.snippets.inline.error != "" {
		t.Fatalf("save failed: %q", state.snippets.inline.error)
	}
	if !containsInt(state.snippets.visible, state.snippets.selected) {
		t.Fatal("the snippet just saved is hidden by the filter")
	}
}

func TestSnippetGroupIsChosenOrCreatedInTheDialog(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state, snippet{Name: "a", Command: "a", Group: "Ops"})

	// An existing group, picked by a different spelling, keeps its one name.
	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("b")
	state.snippets.inline.command.Insert("b")
	state.snippets.inline.groupPicker.Query.Insert("ops")
	state.Key(gfx.KeyEvent{Target: hitSnippetInlineGroup, Key: gfx.KeyEnter})
	state.applyInlineSnippetEdits()
	state.flushInlineSnippetSave()
	if state.snippets.inline.draft || state.snippets.list[1].Group != "Ops" {
		t.Fatalf("group = %q, draft %v", state.snippets.list[1].Group, state.snippets.inline.draft)
	}

	// A new group is created by typing it and pressing Enter.
	state.startInlineSnippetDraft("")
	state.snippets.inline.name.Insert("c")
	state.snippets.inline.command.Insert("c")
	state.snippets.inline.groupPicker.Query.Insert("Dev")
	state.Key(gfx.KeyEvent{Target: hitSnippetInlineGroup, Key: gfx.KeyEnter})
	if state.snippets.inline.groupPicker.Value() != "Dev" {
		t.Fatalf("Enter in the group field: value %q", state.snippets.inline.groupPicker.Value())
	}
	state.applyInlineSnippetEdits()
	state.flushInlineSnippetSave()
	if state.snippets.inline.draft || state.snippets.list[2].Group != "Dev" {
		t.Fatalf("saving into Dev failed: %+v", state.snippets.list[2])
	}

	// Editing shows the group, and clearing it ungroups the snippet.
	state.focusInlineSnippet(2)
	if state.snippets.inline.groupPicker.Value() != "Dev" {
		t.Fatalf("editing shows group %q", state.snippets.inline.groupPicker.Value())
	}
	state.Click(hitSnippetInlineGroupClear)
	if state.snippets.list[2].Group != "" {
		t.Fatalf("group after clearing = %q", state.snippets.list[2].Group)
	}
}

func TestSnippetGroupSurvivesRestore(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.restoreTerminalTools(savedState{Snippets: []savedSnippet{{Name: "a", Command: "a", Group: " Ops "}}})
	if got := state.snippets.list[0].Group; got != "Ops" {
		t.Fatalf("restored group %q", got)
	}
}

func pickerShape(state *State) []string {
	palette := &state.commandPalette
	var shape []string
	for _, index := range palette.visible {
		entry := palette.entries[index]
		if entry.header {
			shape = append(shape, "#"+entry.label)
		} else {
			shape = append(shape, entry.label)
		}
	}
	return shape
}

func pickerHeaderRow(t *testing.T, state *State, label string) int {
	t.Helper()
	palette := &state.commandPalette
	for row, index := range palette.visible {
		if entry := palette.entries[index]; entry.header && entry.label == label {
			return row
		}
	}
	t.Fatalf("no group row %q in %v", label, pickerShape(state))
	return -1
}

func sameShape(a, b []string) bool {
	return len(a) == len(b) && (len(a) == 0 || strings.Join(a, "\x00") == strings.Join(b, "\x00"))
}

// The terminal's picker lists its groups closed, so a long catalog is found by
// opening one group; clicking a group row opens and closes it, and a search
// looks inside every group, closed or not, and shows the matches alone.
func TestSnippetPickerGroupsFoldAndStartClosed(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state,
		snippet{Name: "a", Command: "a", Group: "Ops"},
		snippet{Name: "b", Command: "b"},
		snippet{Name: "c", Command: "c", Group: "Dev"},
	)
	state.openSnippetPicker()
	palette := &state.commandPalette
	none := state.messages.Tools.SnippetFilterNone
	if got, want := pickerShape(state), []string{"#Dev", "#Ops", "#" + none}; !sameShape(got, want) {
		t.Fatalf("picker rows %v, want every group closed: %v", got, want)
	}

	state.Click(hitCommandPaletteItems.At(pickerHeaderRow(t, state, "Ops")))
	if state.modal != modalCommandPalette {
		t.Fatal("opening a group closed the picker")
	}
	if got, want := pickerShape(state), []string{"#Dev", "#Ops", "a", "#" + none}; !sameShape(got, want) {
		t.Fatalf("after opening Ops: %v, want %v", got, want)
	}
	if palette.entries[palette.visible[palette.selected]].label != "Ops" {
		t.Fatal("the group row lost the selection when it opened")
	}
	state.Click(hitCommandPaletteItems.At(pickerHeaderRow(t, state, "Ops")))
	if got, want := pickerShape(state), []string{"#Dev", "#Ops", "#" + none}; !sameShape(got, want) {
		t.Fatalf("after closing Ops: %v, want %v", got, want)
	}

	palette.search.Insert("c")
	state.refreshCommandPalette()
	if got, want := pickerShape(state), []string{"c"}; !sameShape(got, want) {
		t.Fatalf("a search must find a snippet inside a closed group without opening it: %v, want %v", got, want)
	}
	palette.search = text.Editor{}
	palette.search.Insert("ops")
	state.refreshCommandPalette()
	if got, want := pickerShape(state), []string{"a"}; !sameShape(got, want) {
		t.Fatalf("search by group name: %v, want %v", got, want)
	}

	state.closeCommandPalette()
	state.openSnippetPicker()
	if got, want := pickerShape(state), []string{"#Dev", "#Ops", "#" + none}; !sameShape(got, want) {
		t.Fatalf("a reopened picker shows %v, want every group closed again", got)
	}
}

// The keyboard reaches a group too: the arrows step onto its row, Right opens it
// and Left closes it, and Enter on a snippet inside still sends it.
func TestSnippetPickerGroupsFoldFromTheKeyboard(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state, snippet{Name: "a", Command: "a", Group: "Ops"})
	state.openSnippetPicker()
	palette := &state.commandPalette
	press := func(key gfx.Key) {
		t.Helper()
		if _, handled := state.commandPaletteKey(gfx.KeyEvent{Key: key, Target: hitCommandPaletteSearch}); !handled {
			t.Fatalf("key %v not handled", key)
		}
	}
	if palette.entries[palette.visible[palette.selected]].label != "Ops" {
		t.Fatal("the first choice is not the group row")
	}
	press(gfx.KeyRight)
	if got, want := pickerShape(state), []string{"#Ops", "a"}; !sameShape(got, want) {
		t.Fatalf("Right: %v, want %v", got, want)
	}
	press(gfx.KeyLeft)
	if got, want := pickerShape(state), []string{"#Ops"}; !sameShape(got, want) {
		t.Fatalf("Left: %v, want %v", got, want)
	}
	press(gfx.KeyEnter)
	if got, want := pickerShape(state), []string{"#Ops", "a"}; !sameShape(got, want) {
		t.Fatalf("Enter on a group: %v, want %v", got, want)
	}
}

func TestNestedSnippetGroupPathsNormalizeMaterializeAndFilterDescendants(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	for _, invalid := range []string{"", "   ", "/", " / / "} {
		if validSnippetGroup(invalid) || cleanSnippetGroup(invalid) != "" {
			t.Fatalf("invalid group path %q was accepted", invalid)
		}
	}
	if got := cleanSnippetGroup(" Clients / Acme / Production "); got != "Clients/Acme/Production" {
		t.Fatalf("normalized group path = %q, want %q", got, "Clients/Acme/Production")
	}

	state.restoreTerminalTools(savedState{Snippets: []savedSnippet{
		{Name: "deploy-prod", Command: "deploy prod", Group: " Clients / Acme / Production "},
		{Name: "deploy-stage", Command: "deploy stage", Group: "Clients/Acme/Staging"},
		{Name: "other", Command: "echo other", Group: "Clients-2/Beta"},
	}})
	form := &state.snippets
	wantGroups := []string{
		"Clients",
		"Clients/Acme",
		"Clients/Acme/Production",
		"Clients/Acme/Staging",
		"Clients-2",
		"Clients-2/Beta",
	}
	if !slices.Equal(form.groups, wantGroups) {
		t.Fatalf("groups = %v, want %v", form.groups, wantGroups)
	}
	if form.groupCounts["Clients"] != 2 || form.groupCounts["Clients/Acme"] != 2 || form.groupCounts["Clients/Acme/Production"] != 1 {
		t.Fatalf("group counts = %v", form.groupCounts)
	}

	// Filtering by a parent group includes all of its descendants.
	form.filter = snippetFilter{group: "Clients"}
	state.refreshSnippets()
	if !slices.Equal(form.visible, []int{0, 1}) {
		t.Fatalf("parent filter visible = %v, want [0 1]", form.visible)
	}
	form.filter = snippetFilter{group: "Clients/Acme"}
	state.refreshSnippets()
	if !slices.Equal(form.visible, []int{0, 1}) {
		t.Fatalf("intermediate filter visible = %v, want [0 1]", form.visible)
	}
	form.filter = snippetFilter{group: "Clients/Acme/Production"}
	state.refreshSnippets()
	if !slices.Equal(form.visible, []int{0}) {
		t.Fatalf("leaf filter visible = %v, want [0]", form.visible)
	}

	// The inline editor's group picker uses "/" hierarchy navigation like hosts.
	state.initInlineGroupPicker("Clients/Acme/Production")
	if state.snippets.inline.groupPicker.Separator != "/" {
		t.Fatalf("snippet group picker separator = %q, want /", state.snippets.inline.groupPicker.Separator)
	}
}

func TestSnippetPickerRendersNestedGroupsAsCollapsibleTreeAndSearchesInside(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state,
		snippet{Name: "deploy-prod", Command: "kubectl apply -f prod/", Group: "Clients/Acme/Prod"},
		snippet{Name: "deploy-stage", Command: "kubectl apply -f stage/", Group: "Clients/Acme/Staging"},
		snippet{Name: "top", Command: "top", Group: "Ops"},
	)

	state.openSnippetPicker()
	// Initially only top-level groups are visible, both collapsed.
	if got, want := pickerShape(state), []string{"#Clients", "#Ops"}; !sameShape(got, want) {
		t.Fatalf("initial nested picker = %v, want %v", got, want)
	}
	if first := state.commandPalette.entries[state.commandPalette.visible[0]]; first.detail != "2" || first.depth != 0 {
		t.Fatalf("Clients header = detail:%q depth:%d, want detail:2 depth:0", first.detail, first.depth)
	}

	// Search still finds deeply nested snippets while their ancestors are collapsed.
	app.TextEvent{Kind: app.TextInsert, Text: "prod/"}.Apply(state.EditorFor(hitCommandPaletteSearch))
	state.refreshCommandPalette()
	if got, want := pickerShape(state), []string{"deploy-prod"}; !sameShape(got, want) {
		t.Fatalf("search inside collapsed nested tree = %v, want %v", got, want)
	}
	state.commandPalette.search = text.Editor{}
	state.refreshCommandPalette()

	// Expanding Clients reveals Acme (depth 1), keeping Prod/Staging hidden.
	state.Click(hitCommandPaletteItems.At(0))
	if got, want := pickerShape(state), []string{"#Clients", "#Acme", "#Ops"}; !sameShape(got, want) {
		t.Fatalf("after expanding Clients = %v, want %v", got, want)
	}
	if acme := state.commandPalette.entries[state.commandPalette.visible[1]]; acme.depth != 1 || acme.detail != "2" {
		t.Fatalf("Acme header = detail:%q depth:%d, want detail:2 depth:1", acme.detail, acme.depth)
	}

	// Expanding Acme reveals Prod and Staging (depth 2).
	state.Click(hitCommandPaletteItems.At(1))
	if got, want := pickerShape(state), []string{"#Clients", "#Acme", "#Prod", "#Staging", "#Ops"}; !sameShape(got, want) {
		t.Fatalf("after expanding Acme = %v, want %v", got, want)
	}

	// Expanding Prod reveals deploy-prod (depth 3).
	state.Click(hitCommandPaletteItems.At(2))
	if got, want := pickerShape(state), []string{"#Clients", "#Acme", "#Prod", "deploy-prod", "#Staging", "#Ops"}; !sameShape(got, want) {
		t.Fatalf("after expanding Prod = %v, want %v", got, want)
	}
	if cmd := state.commandPalette.entries[state.commandPalette.visible[3]]; cmd.depth != 3 {
		t.Fatalf("deploy-prod depth = %d, want 3", cmd.depth)
	}

	// Collapsing Clients hides the entire subtree; re-expanding restores inner state.
	state.Click(hitCommandPaletteItems.At(0))
	if got, want := pickerShape(state), []string{"#Clients", "#Ops"}; !sameShape(got, want) {
		t.Fatalf("after collapsing Clients = %v, want %v", got, want)
	}
	state.Click(hitCommandPaletteItems.At(0))
	if got, want := pickerShape(state), []string{"#Clients", "#Acme", "#Prod", "deploy-prod", "#Staging", "#Ops"}; !sameShape(got, want) {
		t.Fatalf("after re-expanding Clients = %v, want %v", got, want)
	}
}

func TestLegacyFlatSnippetGroupNamesMigrateWithoutLoss(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.restoreTerminalTools(savedState{Snippets: []savedSnippet{
		{Name: "ci", Command: "make test", Group: "CI/CD"},
		{Name: "spaced", Command: "echo hi", Group: " Clients / Acme "},
		{Name: "dot", Command: "pwd", Group: "."},
		{Name: "dotdot", Command: "cd ..", Group: ".."},
		{Name: "slash", Command: "ls /", Group: "/"},
		{Name: "slashes", Command: "ls //", Group: "//"},
		{Name: "rel", Command: "ls", Group: "a/../b"},
	}})
	want := []string{"CI/CD", "Clients/Acme", ".", "..", "／", "／／", "a/../b"}
	for index, expected := range want {
		if got := state.snippets.list[index].Group; got != expected {
			t.Fatalf("snippet %d (%s) group = %q, want %q", index, state.snippets.list[index].Name, got, expected)
		}
	}
	if state.snippets.hasUngrouped {
		t.Fatal("migrating legacy flat group names dropped a group to ungrouped")
	}
	// Saving and reloading preserves every migrated group identically.
	state.save()
	reloaded := newTestState(t, options)
	for index, expected := range want {
		if got := reloaded.snippets.list[index].Group; got != expected {
			t.Fatalf("reloaded snippet %d group = %q, want %q", index, got, expected)
		}
	}
}
