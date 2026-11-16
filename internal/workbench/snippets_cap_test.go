package workbench

import (
	"fmt"
	"testing"
)

// The cap bounds what a damaged state file may carry, not what a user may
// keep: a migration from another tool arrives with hundreds of commands
// (#211), and restoring takes as many as the cap allows.
func TestSnippetsTakeUpToTheCapWhenRestored(t *testing.T) {
	state := newTestState(t, testOptions(t))
	saved := savedState{}
	for i := 0; i < maxSnippets+40; i++ {
		saved.Snippets = append(saved.Snippets, savedSnippet{Name: fmt.Sprintf("cmd %d", i), Command: fmt.Sprintf("echo %d", i)})
	}
	state.restoreTerminalTools(saved)
	if len(state.snippets.list) != maxSnippets {
		t.Fatalf("restored %d snippets, want the cap of %d", len(state.snippets.list), maxSnippets)
	}
	if state.snippets.list[maxSnippets-1].Name != fmt.Sprintf("cmd %d", maxSnippets-1) {
		t.Fatalf("the cap kept the wrong end: last is %q", state.snippets.list[maxSnippets-1].Name)
	}
}
