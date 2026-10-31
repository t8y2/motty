package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"motty/internal/highlight"
)

// importFile points a fresh state's file picker at body and imports it.
func importFile(t *testing.T, body string) *State {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.txt")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, options := newWorld(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	state.Click(hitSettingsHighlightImport)
	return state
}

// Importing a file of rules appends the ones that parse, skips the rest, and
// reports the counts in a notice: the settings page does not paint state.status
// (#173).
func TestImportHighlightRules(t *testing.T) {
	state := importFile(t, "# the build log\n"+
		"ERROR | red\n"+
		"WARN | yellow | case\n"+
		"no color here\n"+
		"wrong | purple\n")
	if len(state.highlighting.rules) != 2 {
		t.Fatalf("rules = %d, want the two that parse", len(state.highlighting.rules))
	}
	if state.highlighting.rules[0].Pattern != "ERROR" || state.highlighting.rules[0].Slot.Name() != "red" {
		t.Fatalf("first rule = %+v", state.highlighting.rules[0])
	}
	if state.highlighting.rules[1].Pattern != "WARN" || !state.highlighting.rules[1].CaseSensitive {
		t.Fatalf("second rule = %+v", state.highlighting.rules[1])
	}
	if len(state.fileNotices.items) != 1 {
		t.Fatalf("notices = %d, want the import's result", len(state.fileNotices.items))
	}
	notice := state.fileNotices.items[0]
	if !strings.Contains(notice.detail, "2") || notice.kind != components.ToastWarning {
		t.Fatalf("notice = %+v, want two imported and two skipped", notice)
	}
}

// A file whose lines are all unusable says so; it must not claim the list is
// full when the user may still add a rule.
func TestImportingOnlyUnusableLinesDoesNotClaimTheCap(t *testing.T) {
	state := importFile(t, "no color here\nwrong | purple\n")
	if len(state.highlighting.rules) != 0 {
		t.Fatalf("rules = %d, want none", len(state.highlighting.rules))
	}
	if want := fmt.Sprintf(state.messages.Tools.HighlightImportAllSkippedFmt, 2); state.status != want {
		t.Fatalf("status = %q, want %q", state.status, want)
	}
}

// A file with nothing to read is a warning, not silence.
func TestImportingAFileWithoutRulesWarns(t *testing.T) {
	state := importFile(t, "# nothing\n\n")
	if len(state.highlighting.rules) != 0 {
		t.Fatalf("rules = %d, want none", len(state.highlighting.rules))
	}
	if state.status != state.messages.Tools.HighlightImportNothing {
		t.Fatalf("status = %q, want %q", state.status, state.messages.Tools.HighlightImportNothing)
	}
	if notice := state.fileNotices.items[0]; notice.kind != components.ToastWarning {
		t.Fatalf("notice = %+v, want a warning", notice)
	}
}

// What the file holds beyond the room left under the cap is skipped and
// counted, and the rules already kept are not touched.
func TestImportingFillsOnlyTheRoomLeftUnderTheCap(t *testing.T) {
	_, options := newWorld(t)
	path := filepath.Join(t.TempDir(), "rules.txt")
	if err := os.WriteFile(path, []byte("FULL | red\nALSO | blue\nMORE | green\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)
	for len(state.highlighting.rules) < maxCustomHighlightRules-1 {
		state.highlighting.rules = append(state.highlighting.rules, highlightRule{Pattern: "seed", Slot: highlight.Red})
	}
	state.Click(hitSettingsHighlightImport)
	if got := len(state.highlighting.rules); got != maxCustomHighlightRules {
		t.Fatalf("rules = %d, want the cap of %d", got, maxCustomHighlightRules)
	}
	if last := state.highlighting.rules[len(state.highlighting.rules)-1]; last.Pattern != "FULL" {
		t.Fatalf("the room left took %+v, want the file's first rule", last)
	}
	notice := state.fileNotices.items[0]
	if !strings.Contains(notice.detail, "1") || !strings.Contains(notice.detail, "2") {
		t.Fatalf("notice = %+v, want one imported and two skipped", notice)
	}
}

// A file a Windows editor wrote opens with a byte-order mark; the first rule
// must still match what the terminal prints.
func TestImportingAFileWithAByteOrderMarkKeepsTheFirstRuleClean(t *testing.T) {
	state := importFile(t, "\ufeffZZTOP | red\n")
	if len(state.highlighting.rules) != 1 || state.highlighting.rules[0].Pattern != "ZZTOP" {
		t.Fatalf("rules = %+v, want ZZTOP without the mark", state.highlighting.rules)
	}
}

// A file larger than the bound is refused instead of being read whole on the
// interface thread.
func TestImportingAnOversizedFileIsRefused(t *testing.T) {
	state := importFile(t, strings.Repeat("# a line\n", maxHighlightImportBytes/9+1))
	if len(state.highlighting.rules) != 0 {
		t.Fatalf("rules = %d, want none", len(state.highlighting.rules))
	}
	if state.status != state.messages.Tools.HighlightImportTooLarge {
		t.Fatalf("status = %q, want the file-too-large warning", state.status)
	}
}
