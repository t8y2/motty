package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/ai/store"
	layoutcore "motty/internal/workspace"
)

// paintedTabNumber is the text a paint put in a tab's number slot, between its
// badge and its name.
func paintedTabNumber(canvas *gfx.Canvas, tab barTab) string {
	badge, title := tab.badge.Rect(), tab.title.Rect()
	for _, text := range canvas.Texts() {
		bounds := text.Bounds
		if math.Abs(float64(bounds.Y-title.Y)) > 1 || bounds.X < badge.X+badge.Width || bounds.X >= title.X {
			continue
		}
		return text.Value
	}
	return ""
}

// paintedAt is paintedTexts at a height that shows a whole settings page.
func paintedAt(state *State, width, height float32) []string {
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: width, Height: height})
	var texts []string
	for _, text := range canvas.Texts() {
		texts = append(texts, text.Value)
	}
	return texts
}

func TestTabsShowNoNumbersUntilTheSettingIsOn(t *testing.T) {
	state, _ := threeTabs(t)
	canvas, _ := paintedTexts(state)
	if len(state.chrome.bar.tabs) != 3 {
		t.Fatalf("%d tabs on the bar", len(state.chrome.bar.tabs))
	}
	for index, tab := range state.chrome.bar.tabs {
		if got := paintedTabNumber(canvas, tab); got != "" {
			t.Fatalf("tab %d shows the number %q while the setting is off", index, got)
		}
	}

	state.setTabNumbers(true)
	canvas, _ = paintedTexts(state)
	for index, tab := range state.chrome.bar.tabs {
		if got, want := paintedTabNumber(canvas, tab), tabNumber(index+1); got != want {
			t.Fatalf("tab %d shows %q, want %q", index, got, want)
		}
	}
}

func TestVerticalTabsCarryTheirNumbers(t *testing.T) {
	state, _ := threeTabs(t)
	state.setTabOrientation(tabsVertical)
	state.setTabNumbers(true)
	canvas, _ := paintedTexts(state)
	if len(state.verticalTabs.tabs) != 3 {
		t.Fatalf("%d vertical tabs", len(state.verticalTabs.tabs))
	}
	for index, tab := range state.verticalTabs.tabs {
		if got, want := paintedTabNumber(canvas, tab), tabNumber(index+1); got != want {
			t.Fatalf("vertical tab %d shows %q, want %q", index, got, want)
		}
	}
}

func TestGroupedTabsKeepTheNumberOfTheirPlace(t *testing.T) {
	state := restoredGroupedTabs(t, groupingEntries()...)
	state.setTabNumbers(true)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 900})
	if len(state.verticalTabs.tabs) == 0 {
		t.Fatal("no grouped tabs were painted")
	}
	numbers := make(map[layoutcore.WorkspaceID]string, len(state.verticalTabs.tabs))
	for _, tab := range state.verticalTabs.tabs {
		numbers[tab.workspace.ID] = tab.number
	}
	for index, workspace := range state.visualWorkspaceTabs() {
		if got, want := numbers[workspace.ID], tabNumber(index+1); got != want {
			t.Fatalf("tab %d shows %q, want the number of its place %q", workspace.ID, got, want)
		}
	}

	// A collapsed group keeps the numbers of the tabs it hides: the keys count
	// the tabs, not the rows in sight.
	first := state.verticalTabs.grouped.groups[0]
	state.setVerticalTabGroupCollapsed(first.key, true)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 900})
	order := state.verticalTabs.grouped.order
	if len(state.verticalTabs.tabs) == 0 || state.verticalTabs.tabs[0].number != tabNumber(first.count+1) {
		t.Fatalf("after collapsing %d tabs the first visible row shows %q", first.count, state.verticalTabs.tabs[0].number)
	}
	for _, row := range state.verticalTabs.grouped.rows {
		if row.tab < 0 {
			continue
		}
		if order[row.number-1].ID != state.workspaceTabs[row.tab].ID {
			t.Fatalf("row %d is numbered %d, which names another tab", row.tab, row.number)
		}
	}
}

func TestNumberKeysDoNothingWhileTheNumbersAreHidden(t *testing.T) {
	state, _ := threeTabs(t)
	state.Click(hitHosts)
	before := tabIndex(state)
	press := func(digit string) gfx.EventResult {
		return state.Shortcut(gfx.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: keymap.Primary, Text: digit})
	}
	if result := press("1"); result.Handled {
		t.Fatal("a hidden number still took its key from the terminal")
	}
	if got := tabIndex(state); got != before {
		t.Fatalf("a hidden number moved to tab %d, from %d", got, before)
	}

	state.setTabNumbers(true)
	if result := press("1"); !result.Handled {
		t.Fatal("the number key did not move to its tab")
	}
	if got := tabIndex(state); got != 0 {
		t.Fatalf("the first digit went to tab %d", got)
	}
	if result := press("2"); !result.Handled || tabIndex(state) != 1 {
		t.Fatalf("the second digit did not reach the second tab: %+v", result)
	}

	state.setTabNumbers(false)
	if result := press("1"); result.Handled || tabIndex(state) != 1 {
		t.Fatalf("hiding the numbers again left the key bound: handled=%v tab=%d", result.Handled, tabIndex(state))
	}
}

func TestTheNumbersSettingOwnsTheNumberKeys(t *testing.T) {
	state, _ := threeTabs(t)
	state.Click(hitSettings)
	if state.tabNumbers {
		t.Fatal("tab numbers must default to off")
	}
	if contains(paintedAt(state, 1400, 3400), state.messages.SettingsTabNumberKeys) {
		t.Fatal("the key row is drawn while the numbers are hidden")
	}

	state.Click(hitSettingsTabNumbers)
	if !state.tabNumbers {
		t.Fatal("the switch did not turn the numbers on")
	}
	if !contains(paintedAt(state, 1400, 3400), state.messages.SettingsTabNumberKeys) {
		t.Fatal("the number keys row is missing while the numbers are shown")
	}

	state.Click(hitSettingsTabNumbersAlt)
	want := keymap.Chord{Modifiers: gfx.ModifierAlt, Char: '1'}
	if got := state.shortcuts.chords.Chord(shortcutTabNumber); got != want {
		t.Fatalf("choosing Alt bound %+v, want %+v", got, want)
	}
	if label := state.shortcuts.labels[shortcutTabNumber]; label != "Alt+1…9" {
		t.Fatalf("label = %q", label)
	}

	state.save()
	restored := newTestState(t, state.options)
	if !restored.tabNumbers || restored.shortcuts.chords.Chord(shortcutTabNumber) != want {
		t.Fatalf("restored numbers=%v chord=%+v", restored.tabNumbers, restored.shortcuts.chords.Chord(shortcutTabNumber))
	}
}

func TestTheNumberKeysSettingTakesTheKeyboard(t *testing.T) {
	state, _ := threeTabs(t)
	state.setTabNumbers(true)
	state.Click(hitSettings)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 3400})
	for _, id := range []gfx.ID{hitSettingsTabNumbersPrimary, hitSettingsTabNumbersAlt, hitSettingsTabNumbersShift} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("the number keys row is missing option %v", id)
		}
	}
	result := state.Key(gfx.KeyEvent{Target: hitSettingsTabNumbersPrimary, Key: gfx.KeyRight})
	if !result.Handled || result.Focus != hitSettingsTabNumbersAlt {
		t.Fatalf("keyboard choice: %+v", result)
	}
	if got := state.shortcuts.chords.Chord(shortcutTabNumber).Modifiers; got != gfx.ModifierAlt {
		t.Fatalf("the keyboard chose %v, want Alt", got)
	}
}

func TestTheNumberKeysAreAbleToFireInATerminal(t *testing.T) {
	state, _ := threeTabs(t)
	state.setTabNumbers(true)
	state.Click(hitSettings)
	// Shift alone cannot be told apart from typing its symbol, so the third
	// choice is the platform's primary key with Shift.
	state.Click(hitSettingsTabNumbersShift)
	want := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierShift, Char: '1'}
	if got := state.shortcuts.chords.Chord(shortcutTabNumber); got != want {
		t.Fatalf("the third choice bound %+v, want %+v", got, want)
	}
	result := state.Shortcut(gfx.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: want.Modifiers, Text: "2"})
	if !result.Handled || tabIndex(state) != 1 {
		t.Fatalf("the third choice did not reach the second tab: %+v", result)
	}
}

// A hidden number has to reach the terminal behind the CLI login dialog too:
// the dialog swallows every action it recognises, so it must recognise none of
// the numbers while the setting is off.
func TestHiddenNumberKeysStayWithTheTerminalDuringCLILogin(t *testing.T) {
	w := newSettingsWorld(t, newLister())
	state := w.state
	auth := &fakeShell{done: make(chan struct{})}
	local := newTerminal(80, 24)
	local.shell = auth
	state.ai.login = &aiCLILogin{provider: store.Provider{ID: "auth"}, term: local, cancel: func() {}}
	state.ai.cfg.dialog = aiDialogCLILogin
	digit := func(text string) gfx.EventResult {
		return state.Shortcut(gfx.KeyEvent{Target: hitAICLILoginTerminal, Modifiers: keymap.Primary, Text: text})
	}
	if state.tabNumbers {
		t.Fatal("tab numbers must default to off")
	}
	if result := digit("1"); result.Handled {
		t.Fatal("the login dialog took a number key while the numbers are hidden")
	}
	state.setTabNumbers(true)
	if result := digit("1"); !result.Handled {
		t.Fatal("the login dialog left the number key with the terminal while the numbers are shown")
	}
}
