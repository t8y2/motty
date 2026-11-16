package workbench

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// "cd " and "ssh " are sent to be finished by hand: the space the user typed
// after them is part of the command, through saving, restoring and sending.
func TestSnippetCommandKeepsItsSurroundingSpace(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("  go to  ")
	state.snippets.command.Insert(" cd ")
	state.snippets.run = false
	state.Click(hitSnippetSubmit)
	if len(state.snippets.list) != 1 {
		t.Fatal("the snippet was not saved")
	}
	if got := state.snippets.list[0]; got.Name != "go to" || got.Command != " cd " {
		t.Fatalf("saved name %q, command %q", got.Name, got.Command)
	}

	state.openSnippetDialog(0)
	if got := state.snippets.command.String(); got != " cd " {
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
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("blank")
	state.snippets.command.Insert("   ")
	state.Click(hitSnippetSubmit)
	if state.modal != modalSnippet || len(state.snippets.list) != 0 || state.snippets.dialogError == "" {
		t.Fatal("a command of only space was saved")
	}
}

func TestMultilineSnippetKeepsLinesAndSendsAsPaste(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("deploy")
	state.snippets.command.Insert("echo one\r\necho two")
	if result := state.Key(gfx.KeyEvent{Target: hitSnippetCommand, Key: gfx.KeyEnter}); result.Handled || state.modal != modalSnippet {
		t.Fatalf("Enter in multiline command: result=%+v modal=%d", result, state.modal)
	}
	state.Click(hitSnippetSubmit)
	if len(state.snippets.list) != 1 || state.snippets.list[0].Command != "echo one\necho two" || state.snippets.list[0].cells[3] != "echo one ↵ echo two" {
		t.Fatalf("saved multiline snippet = %+v", state.snippets.list)
	}
	loaded := newTestState(t, options)
	if len(loaded.snippets.list) != 1 || loaded.snippets.list[0].Command != "echo one\necho two" {
		t.Fatalf("restored multiline snippet = %+v", loaded.snippets.list)
	}
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

func TestSnippetManualOrderPersistsThroughSortingAndFiltering(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "zulu", Command: "echo z", Group: "ops"},
		snippet{Name: "alpha", Command: "echo a", Group: "dev"},
		snippet{Name: "beta", Command: "echo b", Group: "ops"},
	)
	state.Click(hitSnippetSort.At(0))
	state.snippets.filter = snippetFilter{group: "ops"}
	state.refreshSnippets()
	if !state.moveSnippet(2, -1) || state.snippets.sort.Direction != 0 || state.snippets.selected != 0 {
		t.Fatalf("move in filtered table: visible=%v selected=%d sort=%+v", state.snippets.visible, state.snippets.selected, state.snippets.sort)
	}
	if got := []string{state.snippets.list[0].Name, state.snippets.list[1].Name, state.snippets.list[2].Name}; !slices.Equal(got, []string{"beta", "alpha", "zulu"}) {
		t.Fatalf("reordered list = %v", got)
	}
	state.snippets.filter = snippetFilter{}
	state.refreshSnippets()
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := canvasHit(canvas.Hits(), hitSnippetHandles.At(0)); !ok {
		t.Fatal("manual ordering did not expose a drag handle")
	}
	result, handled := state.snippetReorderKey(gfx.KeyEvent{Target: hitSnippetHandles.At(0), Key: gfx.KeyDown})
	if !handled || result.Focus != hitSnippetHandles.At(1) || state.snippets.list[1].Name != "beta" {
		t.Fatalf("keyboard reorder: handled=%v focus=%s order=%v", handled, tree.Text(result.Focus), state.snippets.list)
	}
	if err := state.saveWithResult(); err != nil {
		t.Fatalf("persist keyboard reorder: %v", err)
	}
	if !slices.Equal(state.snippets.visible, []int{0, 1, 2}) {
		t.Fatalf("manual order visible = %v", state.snippets.visible)
	}
	loaded := newTestState(t, options)
	if got := []string{loaded.snippets.list[0].Name, loaded.snippets.list[1].Name, loaded.snippets.list[2].Name}; !slices.Equal(got, []string{"alpha", "beta", "zulu"}) {
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

func TestSnippetTableSortKeepsActionsOnTheirOriginalRows(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "zeta", Command: "echo z", Group: "Ops"},
		snippet{Name: "alpha", Command: "echo a", Group: "dev"},
		snippet{Name: "beta", Command: "echo b", Group: "Ops"},
	)
	form := &state.snippets
	form.selected = 0
	state.Click(hitSnippetSort.At(0))
	if got := form.visible; !slices.Equal(got, []int{1, 2, 0}) || form.selected != 0 {
		t.Fatalf("ascending names: visible=%v selected=%d", got, form.selected)
	}
	state.Click(hitSnippetSort.At(0))
	if got := form.visible; !slices.Equal(got, []int{0, 2, 1}) || form.selected != 0 {
		t.Fatalf("descending names: visible=%v selected=%d", got, form.selected)
	}
	state.Click(hitSnippetSort.At(0))
	if got := form.visible; !slices.Equal(got, []int{0, 1, 2}) || form.sort.Direction != components.TableSortNone {
		t.Fatalf("restored manual order: visible=%v sort=%+v", got, form.sort)
	}
	state.Click(hitSnippetSort.At(0))
	state.Click(hitSnippetSort.At(0))
	form.filter = snippetFilter{group: "Ops"}
	state.refreshSnippets()
	if got := form.visible; !slices.Equal(got, []int{0, 2}) {
		t.Fatalf("filtered descending names: %v", got)
	}
	state.Click(hitSnippetSort.At(3))
	if got := form.visible; !slices.Equal(got, []int{2, 0}) || form.list[form.selected].Name != "zeta" {
		t.Fatalf("ascending commands: visible=%v selected=%d", got, form.selected)
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
	state.Click(hitSnippetFilter)
	if !state.snippets.filterOpen {
		t.Fatal("the filter did not open")
	}
	state.Click(hitSnippetFilterList.At(0))
	if state.snippets.filterOpen || state.snippets.filter.group != "Ops" || len(state.snippets.visible) != 1 {
		t.Fatalf("filter = %+v open=%v visible=%v", state.snippets.filter, state.snippets.filterOpen, state.snippets.visible)
	}
	state.Click(hitSnippetFilter)
	state.Click(hitSnippetFilterAll)
	if !state.snippets.filter.all() || len(state.snippets.visible) != 2 {
		t.Fatalf("All groups left filter %+v", state.snippets.filter)
	}
}

// A snippet saved while another group is filtered must not vanish from the page.
func TestSavedSnippetStaysVisibleUnderTheFilter(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state, snippet{Name: "a", Command: "a", Group: "Ops"}, snippet{Name: "b", Command: "b", Group: "dev"})
	state.snippets.filter = snippetFilter{group: "Ops"}
	state.refreshSnippets()

	state.openSnippetDialog(-1)
	state.snippets.name.Insert("c")
	state.snippets.command.Insert("c")
	state.Click(hitSnippetSubmit)
	if state.modal != modalNone {
		t.Fatalf("save failed: %q", state.snippets.dialogError)
	}
	if !containsInt(state.snippets.visible, state.snippets.selected) {
		t.Fatal("the snippet just saved is hidden by the filter")
	}
}

func TestSnippetGroupIsChosenOrCreatedInTheDialog(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state, snippet{Name: "a", Command: "a", Group: "Ops"})

	// An existing group, picked by a different spelling, keeps its one name.
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("b")
	state.snippets.command.Insert("b")
	state.snippets.groupPicker.Query.Insert("ops")
	state.Click(hitSnippetSubmit)
	if state.modal != modalNone || state.snippets.list[1].Group != "Ops" {
		t.Fatalf("group = %q, modal %v", state.snippets.list[1].Group, state.modal)
	}

	// A new group is created by typing it, with Enter or Save.
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("c")
	state.snippets.command.Insert("c")
	state.snippets.groupPicker.Query.Insert("Dev")
	state.Key(gfx.KeyEvent{Target: hitSnippetGroup, Key: gfx.KeyEnter})
	if state.modal != modalSnippet || state.snippets.groupPicker.Value() != "Dev" {
		t.Fatalf("Enter in the group field: modal %v, value %q", state.modal, state.snippets.groupPicker.Value())
	}
	state.Key(gfx.KeyEvent{Target: hitSnippetGroup, Key: gfx.KeyEnter})
	if state.modal != modalNone || state.snippets.list[2].Group != "Dev" {
		t.Fatalf("second Enter did not save into Dev: %+v", state.snippets.list[2])
	}

	// Editing shows the group, and clearing it ungroups the snippet.
	state.openSnippetDialog(2)
	if state.snippets.groupPicker.Value() != "Dev" {
		t.Fatalf("editing shows group %q", state.snippets.groupPicker.Value())
	}
	state.Click(hitSnippetGroupClear)
	state.Click(hitSnippetSubmit)
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
