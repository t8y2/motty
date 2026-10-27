//go:build dev

package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

// The Debug entry is a sidebar page like the host list: it shows the showcase
// button group, and pressing an action of the Viva control is remembered by the
// page.
func TestDebugPageShowsTheButtonGroupAndRemembersTheLastAction(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.Click(hitDebug)
	if state.activeSection != sectionDebug {
		t.Fatalf("Debug did not open its page: section = %d", state.activeSection)
	}

	canvas, texts := paintedTexts(state)
	navigation, ok := hitByID(canvas, hitDebug)
	if !ok || !navigation.Selected {
		t.Fatalf("Debug navigation item = %+v, ok=%v", navigation, ok)
	}
	if !contains(texts, state.messages.DebugComponents) || !contains(texts, state.messages.DebugButtonGroupTitle) {
		t.Fatalf("the component gallery is not painted: %v", texts)
	}
	for index, label := range []string{state.messages.DebugAlignLeft, state.messages.DebugAlignCenter, state.messages.DebugAlignRight} {
		hit, ok := hitByID(canvas, hitDebugActionList.At(index))
		if !ok || hit.Role != ui.RoleButton || hit.Label != label {
			t.Fatalf("action %d = %+v, ok=%v; want a button labelled %q", index, hit, ok, label)
		}
	}

	state.Click(hitDebugActionList.At(2))
	canvas, texts = paintedTexts(state)
	if state.dev.action != 2 {
		t.Fatalf("last action = %d, want the third", state.dev.action)
	}
	if countText(texts, state.messages.DebugAlignRight) != 2 {
		t.Fatalf("the chosen action is not named beside %q: %v", state.messages.DebugButtonGroupChoice, texts)
	}
	if hit, ok := hitByID(canvas, hitDebugActionList.At(2)); !ok || hit.Label != state.messages.DebugAlignRight {
		t.Fatalf("the chosen action is no longer painted: %+v, ok=%v", hit, ok)
	}

	// Opening the Debug page is a workspace choice, so it comes back the way the
	// other management pages do.
	restarted := newTestState(t, options)
	if restarted.activeSection != sectionDebug {
		t.Fatalf("restarted section = %d, want Debug", restarted.activeSection)
	}
}

// The selector is the Viva Select again: its trigger shows the page's value, its
// list is a real popup with options, and choosing one is remembered.
func TestDebugSelectOpensChoosesAndTypesAhead(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitDebug)

	canvas, texts := paintedTexts(state)
	trigger, ok := hitByID(canvas, hitDebugSelect)
	if !ok || trigger.Role != ui.RoleCombobox || trigger.Label != state.messages.DebugSelectTitle ||
		trigger.Value != state.messages.DebugEnvironmentDevelopment || trigger.Expanded {
		t.Fatalf("select trigger = %+v, ok=%v", trigger, ok)
	}
	if !contains(texts, state.messages.DebugSelectTitle) {
		t.Fatalf("the selector card is not painted: %v", texts)
	}

	state.Click(hitDebugSelect)
	canvas, _ = paintedTexts(state)
	if !state.dev.selectOpen {
		t.Fatal("pressing the trigger did not open the list")
	}
	if trigger, _ := hitByID(canvas, hitDebugSelect); !trigger.Expanded {
		t.Fatalf("the open trigger is not expanded: %+v", trigger)
	}
	for index, label := range state.debugSelectLabels() {
		hit, ok := hitByID(canvas, hitDebugSelectItemList.At(index))
		if !ok || hit.Role != ui.RoleOption || hit.Label != label {
			t.Fatalf("option %d = %+v, ok=%v; want %q", index, hit, ok, label)
		}
	}

	state.Click(hitDebugSelectItemList.At(2))
	canvas, texts = paintedTexts(state)
	if state.dev.selectOpen || state.dev.selectOption != 2 {
		t.Fatalf("after choosing Production: open=%v option=%d", state.dev.selectOpen, state.dev.selectOption)
	}
	if trigger, _ := hitByID(canvas, hitDebugSelect); trigger.Value != state.messages.DebugEnvironmentProduction {
		t.Fatalf("trigger value = %q", trigger.Value)
	}
	if countText(texts, state.messages.DebugEnvironmentProduction) != 2 {
		t.Fatalf("the chosen value is not named beside %q: %v", state.messages.DebugSelectChoice, texts)
	}

	// Typing a letter on the closed trigger chooses its option without opening.
	result := state.Key(ui.KeyEvent{Target: hitDebugSelect, Text: "s"})
	if !result.Handled || state.dev.selectOpen || state.dev.selectOption != 1 {
		t.Fatalf("type-ahead result=%+v open=%v option=%d", result, state.dev.selectOpen, state.dev.selectOption)
	}
}

// The open list is the frame's menu: arrow keys move through its rows, and Tab
// closes it and leaves. That needs the select painted inside the frame's menu
// pass, not before it.
func TestDebugSelectKeyboardTakesTheOpenList(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitDebug)
	state.Click(hitDebugSelect)
	paintedTexts(state)
	if !state.dev.selectOpen {
		t.Fatal("the list did not open")
	}

	result := state.Key(ui.KeyEvent{Target: hitDebugSelect, Key: ui.KeyDown})
	if !result.Handled || result.Focus != hitDebugSelectItemList.At(0) {
		t.Fatalf("Down result=%+v, want focus on the first option", result)
	}
	// The runtime delivers Enter on the focused row as its click.
	state.Click(result.Focus)
	if state.dev.selectOpen || state.dev.selectOption != 0 {
		t.Fatalf("after choosing with the keyboard: open=%v option=%d", state.dev.selectOpen, state.dev.selectOption)
	}

	state.Click(hitDebugSelect)
	paintedTexts(state)
	result = state.Key(ui.KeyEvent{Target: hitDebugSelect, Key: ui.KeyTab})
	if !result.Handled || !result.ContinueTraversal || state.dev.selectOpen {
		t.Fatalf("Tab result=%+v, want the list closed and traversal continuing", result)
	}
}

func TestDebugPagePaintDoesNotAllocateOnceWarm(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitDebug)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	for _, open := range []bool{false, true} {
		state.dev.selectOpen = open
		state.Paint(&canvas, size)
		if allocations := testing.AllocsPerRun(50, func() {
			canvas.Reset()
			state.Paint(&canvas, size)
		}); allocations != 0 {
			t.Fatalf("Debug page paint with select open=%v allocated %v times", open, allocations)
		}
	}
}

// The palette is Viva's ColorSwatch: each color is a radio choice with its own
// ring, pointer selection is remembered, the disabled choice cannot be chosen,
// and the arrow keys move through the enabled colors.
func TestDebugPaletteChoosesAndMovesBetweenColors(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitDebug)

	canvas, texts := paintedTexts(state)
	labels := state.debugSwatchLabels()
	for index, label := range labels {
		hit, ok := hitByID(canvas, hitDebugSwatchList.At(index))
		if !ok || hit.Role != ui.RoleRadio || hit.Label != label ||
			hit.Checked != (index == 0) || hit.Disabled != (index == debugSwatches-1) {
			t.Fatalf("swatch %d = %+v, ok=%v; want %q", index, hit, ok, label)
		}
	}
	if !contains(texts, state.messages.DebugColorSwatchTitle) {
		t.Fatalf("the palette card is not painted: %v", texts)
	}

	state.Click(hitDebugSwatchList.At(1))
	canvas, texts = paintedTexts(state)
	if state.dev.swatch != 1 {
		t.Fatalf("selected swatch = %d, want green", state.dev.swatch)
	}
	if hit, ok := hitByID(canvas, hitDebugSwatchList.At(1)); !ok || !hit.Checked {
		t.Fatalf("the chosen color is not checked: %+v, ok=%v", hit, ok)
	}
	if countText(texts, labels[1]) != 1 {
		t.Fatalf("the chosen color is not named beside %q: %v", state.messages.DebugColorSwatchChoice, texts)
	}

	// A disabled swatch is a hit for accessibility but chooses nothing.
	state.Click(hitDebugSwatchList.At(debugSwatches - 1))
	if state.dev.swatch != 1 {
		t.Fatalf("pressing the disabled swatch chose %d, want green", state.dev.swatch)
	}

	// Right from blue skips the disabled red and wraps to no color.
	result := state.Key(ui.KeyEvent{Target: hitDebugSwatchList.At(2), Key: ui.KeyRight})
	if !result.Handled || !result.Repaint || result.Focus != hitDebugSwatchList.At(0) || state.dev.swatch != 0 {
		t.Fatalf("Right from blue: result=%+v swatch=%d", result, state.dev.swatch)
	}
	result = state.Key(ui.KeyEvent{Target: hitDebugSwatchList.At(0), Key: ui.KeyLeft})
	if !result.Handled || result.Focus != hitDebugSwatchList.At(2) || state.dev.swatch != 2 {
		t.Fatalf("Left from no color: result=%+v swatch=%d", result, state.dev.swatch)
	}
}

func countText(texts []string, value string) int {
	count := 0
	for _, text := range texts {
		if text == value {
			count++
		}
	}
	return count
}
