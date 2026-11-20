//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	sshknownhosts "golang.org/x/crypto/ssh/knownhosts"
)

// writeKnownHostsPages writes enough entries for more than two pages, so a click
// that ran twice would land two pages on instead of one.
func writeKnownHostsPages(t *testing.T, state *State) int {
	t.Helper()
	key := workbenchKnownHostKey(t)
	count := 2*knownHostsPageSize + 1
	var data strings.Builder
	for index := range count {
		data.WriteString(sshknownhosts.Line([]string{fmt.Sprintf("host-%02d.example.com", index)}, key))
		data.WriteByte('\n')
	}
	if err := os.WriteFile(state.options.KnownHostsPath, []byte(data.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return count
}

// The known-hosts chooser is a page Pagination: a pointer click on Next through
// the real window turns exactly one page, once, in the event pass the rest of the
// page uses.
func TestKnownHostsPaginationClicksThroughThePage(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	count := writeKnownHostsPages(t, state)
	state.Click(hitKnownHosts)
	if got := len(state.knownHosts.visible); got != count {
		t.Fatalf("fixture has %d visible entries, want %d", got, count)
	}
	view, err := headless.Open(state.WindowConfig(app.Config{
		Title: "Motty", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll,
	}), nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	next, ok := hitByIDSnapshot(view.Snapshot().Hits, hitKnownHostsNext)
	if !ok {
		t.Fatal("the footer has no Next control")
	}
	if err := view.Click(next.Bounds.X+next.Bounds.Width/2, next.Bounds.Y+next.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Wait(16 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if state.knownHosts.page != 1 {
		t.Fatalf("clicking Next moved to page %d, want 1", state.knownHosts.page)
	}
}
