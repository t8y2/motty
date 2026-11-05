package workbench

import (
	"testing"

	"motty/internal/term"
)

func TestNormalizeWordSeparatorsKeepsOneOfEachVisibleCharacter(t *testing.T) {
	for value, want := range map[string]string{
		"":          "",
		" \t\n":     "",
		",;,;":      ",;",
		" , : ":     ",:",
		"，、，":       "，、",
		"a\x00\x1b": "a",
	} {
		if got := normalizeWordSeparators(value); got != want {
			t.Errorf("normalizeWordSeparators(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestWordSeparatorsFormSavesAndSurvivesARestart(t *testing.T) {
	options := testOptions(t)
	first := newTestState(t, options)
	if first.terminalWordSeparators != "" {
		t.Fatalf("default = %q, want the built-in rule", first.terminalWordSeparators)
	}
	before := first.wordSeparatorsDescription()
	first.Click(hitSettings)
	first.Click(hitSettingsWordSeparators)
	if first.modal != modalForm {
		t.Fatalf("modal = %v, want the form", first.modal)
	}
	first.form.set(0, " ,,: ")
	if message := first.form.run(first, &first.form); message != "" {
		t.Fatalf("form refused: %q", message)
	}
	if first.terminalWordSeparators != ",:" {
		t.Fatalf("saved %q", first.terminalWordSeparators)
	}
	if first.wordSeparatorsDescription() == before {
		t.Fatal("the row still describes the built-in rule")
	}
	second := newTestState(t, options)
	if second.terminalWordSeparators != ",:" {
		t.Fatalf("after a restart: %q", second.terminalWordSeparators)
	}
	// Emptying the field goes back to the built-in rule.
	second.openWordSeparatorsForm()
	if got := second.form.value(0); got != ",:" {
		t.Fatalf("the form opened with %q", got)
	}
	second.form.set(0, "")
	second.form.run(second, &second.form)
	if third := newTestState(t, options); third.terminalWordSeparators != "" {
		t.Fatalf("after clearing: %q", third.terminalWordSeparators)
	}
}

func TestADoubleClickDragGrowsByTheSameSeparators(t *testing.T) {
	screen := term.New(40, 2, 0)
	screen.Write([]byte("/usr/bin:/opt/app next"))
	selection := terminalSelection{}
	selection.beginUnit(screen, screen.PointAt(term.Position{Row: 0, Column: 2}), unitWord, ":")
	if got := selection.text(screen); got != "/usr/bin" {
		t.Fatalf("double click took %q", got)
	}
	if !selection.extendUnit(screen, screen.PointAt(term.Position{Row: 0, Column: 11})) {
		t.Fatal("the drag did not grow")
	}
	if got := selection.text(screen); got != "/usr/bin:/opt/app" {
		t.Fatalf("drag took %q", got)
	}
}
