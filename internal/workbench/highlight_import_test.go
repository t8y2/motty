package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
)

// Importing a file of rules appends the ones that parse, skips the rest, and
// never goes past the cap the dialog's own rule obeys (#173).
func TestImportHighlightRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.txt")
	body := "# the build log\n" +
		"ERROR | red\n" +
		"WARN | yellow | case\n" +
		"no color here\n" +
		"wrong | purple\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, options := newWorld(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	state := newTestState(t, options)

	state.Click(hitSettingsHighlightImport)
	if len(state.highlighting.rules) != 2 {
		t.Fatalf("rules = %d, want the two that parse", len(state.highlighting.rules))
	}
	if state.highlighting.rules[0].Pattern != "ERROR" || state.highlighting.rules[0].Slot.Name() != "red" {
		t.Fatalf("first rule = %+v", state.highlighting.rules[0])
	}
	if state.highlighting.rules[1].Pattern != "WARN" || !state.highlighting.rules[1].CaseSensitive {
		t.Fatalf("second rule = %+v", state.highlighting.rules[1])
	}
	if !strings.Contains(state.status, "2") {
		t.Fatalf("status = %q, want the import's count", state.status)
	}

	// A file without a single usable rule is a warning, not silence.
	empty := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(empty, []byte("# nothing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, options2 := newWorld(t)
	options2.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{empty}, nil }
	state2 := newTestState(t, options2)
	state2.Click(hitSettingsHighlightImport)
	if len(state2.highlighting.rules) != 0 {
		t.Fatalf("rules = %d, want none", len(state2.highlighting.rules))
	}
	if state2.status != state2.messages.Tools.HighlightImportNothing {
		t.Fatalf("status = %q, want %q", state2.status, state2.messages.Tools.HighlightImportNothing)
	}
}
