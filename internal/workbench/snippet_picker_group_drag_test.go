package workbench

import "testing"

func TestSnippetPickerWholeGroupRowDragPersistsSubtreeOrder(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	addTestSnippets(state, snippet{Name: "A cmd", Command: "a", Group: "A/Child"},
		snippet{Name: "B cmd", Command: "b", Group: "B/Child"})
	state.createSnippetGroup("Empty")
	state.openSnippetPicker()
	state.foldSnippetGroup("b", true)
	state.foldSnippetGroup("b/child", true)
	view := openPickerView(t, state, 1)
	source := dualHit(t, view, hitCommandPaletteItems.At(1))
	target := dualHit(t, view, hitCommandPaletteItems.At(0))
	x := source.X + source.Width*0.8
	if err := view.Press(x, source.Y+source.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x, target.Y+2); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(x, target.Y+2); err != nil {
		t.Fatal(err)
	}
	if !sameShape(pickerShape(state), []string{"#B", "#Child", "B cmd", "#A", "#Empty"}) {
		t.Fatalf("moved subtree: %v", pickerShape(state))
	}
	if state.snippets.list[0].Group != "A/Child" || state.snippets.list[1].Group != "B/Child" {
		t.Fatal("group ordering changed command membership")
	}
	restored := newTestState(t, options)
	restored.openSnippetPicker()
	if !sameShape(pickerShape(restored), []string{"#B", "#A", "#Empty"}) {
		t.Fatalf("restored groups: %v", pickerShape(restored))
	}
	// 从整行拖到末尾，也包括空分组。
	source = dualHit(t, view, hitCommandPaletteItems.At(3))
	target = dualHit(t, view, hitCommandPaletteItems.At(4))
	if err := view.Press(x, source.Y+source.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x, target.Y+target.Height-1); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(x, target.Y+target.Height-1); err != nil {
		t.Fatal(err)
	}
	if !sameShape(pickerShape(state), []string{"#B", "#Child", "B cmd", "#Empty", "#A"}) {
		t.Fatalf("moved after empty group: %v", pickerShape(state))
	}
}

func TestSnippetPickerNestedGroupOrderSurvivesRenameAndDelete(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.createSnippetGroup("Ops/Alpha")
	state.createSnippetGroup("Ops/Beta")
	state.openSnippetPicker()
	state.foldSnippetGroup("ops", true)
	view := openPickerView(t, state, 1)
	source := dualHit(t, view, hitCommandPaletteItems.At(2))
	target := dualHit(t, view, hitCommandPaletteItems.At(1))
	x := source.X + source.Width/2
	if err := view.Press(x, source.Y+source.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Move(x, target.Y+2); err != nil {
		t.Fatal(err)
	}
	if err := view.Release(x, target.Y+2); err != nil {
		t.Fatal(err)
	}
	if !sameShape(state.snippetPickerGroups(), []string{"Ops", "Ops/Beta", "Ops/Alpha"}) {
		t.Fatalf("nested order: %v", state.snippetPickerGroups())
	}
	state.renameSnippetGroup("Ops/Beta", "Ops/Zulu")
	restored := newTestState(t, options)
	if !sameShape(restored.snippetPickerGroups(), []string{"Ops", "Ops/Zulu", "Ops/Alpha"}) {
		t.Fatalf("renamed order: %v", restored.snippetPickerGroups())
	}
	state.deleteSnippetGroup("Ops/Zulu")
	restored = newTestState(t, options)
	if !sameShape(restored.snippetPickerGroups(), []string{"Ops", "Ops/Alpha"}) {
		t.Fatalf("remaining order: %v", restored.snippetPickerGroups())
	}
}
