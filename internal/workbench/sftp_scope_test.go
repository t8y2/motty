package workbench

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func scopeHint(canvas *gfx.Canvas) string {
	for _, hit := range canvas.Hits() {
		if strings.Contains(hit.Hint, "Filtering covers loaded items only") {
			return hit.Hint
		}
	}
	return ""
}

func paintLocalScope(state *State) *gfx.Canvas {
	canvas := new(gfx.Canvas)
	state.Paint(canvas, gfx.Size{Width: 1400, Height: 900})
	return canvas
}

// Windows may cache DirEntry.Info during enumeration. Use a fresh real stat so
// removing a fixture exercises the same filesystem failure on every platform.
type scopeDirEntry struct {
	os.DirEntry
	dir string
}

func (entry scopeDirEntry) Info() (os.FileInfo, error) {
	return os.Lstat(filepath.Join(entry.dir, entry.Name()))
}

func scopeDirEntries(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range items {
		items[index] = scopeDirEntry{DirEntry: item, dir: dir}
	}
	return items
}

func TestLocalScopeExactlyCapAndLookahead(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localManyFiles(t, maxLocalEntries)
	view := openLocalPane(t, state, session, dir)
	waitLocalRows(t, state, view, maxLocalEntries)
	if hint := scopeHint(paintLocalScope(state)); hint != "" {
		t.Fatalf("exactly capped directory was marked incomplete: %q", hint)
	}
	if err := os.WriteFile(filepath.Join(dir, "one-more.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := state.listLocal(view, dir); err != nil {
		t.Fatal(err)
	}
	// A refresh has the same row count; wait for its posted acceptance instead.
	deadline := time.Now().Add(5 * time.Second)
	for view.status == state.messages.SFTPLoading && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	hint := scopeHint(paintLocalScope(state))
	if !strings.Contains(hint, "20000") || !strings.Contains(hint, "entry limit") {
		t.Fatalf("lookahead did not expose bounded loaded scope: %q", hint)
	}
	if len(view.lfolder.allRows) != maxLocalEntries {
		t.Fatalf("loaded %d rows, want the raw entry cap", len(view.lfolder.allRows))
	}
	loaded := make(map[string]bool, maxLocalEntries)
	for _, row := range view.lfolder.allRows {
		loaded[row.entry.Name] = true
	}
	omitted := ""
	for _, item := range scopeDirEntries(t, dir) {
		if !loaded[item.Name()] {
			omitted = item.Name()
			break
		}
	}
	if omitted == "" {
		t.Fatal("fixture has no real entry outside the loaded subset")
	}
	view.lfolder.filterOpen = true
	state.applySFTPFilterText(view, &view.lfolder, app.TextEvent{Kind: app.TextInsert, Text: omitted})
	if len(view.lfolder.rows) != 0 {
		t.Fatal("unloaded file unexpectedly matched the loaded subset")
	}
	view.status = "Transfer completed"
	canvas := paintLocalScope(state)
	found := false
	for _, text := range canvas.Texts() {
		found = found || text.Value == "No matches in loaded items"
	}
	if !found || scopeHint(canvas) == "" {
		t.Fatal("bounded zero-result warning was hidden by ordinary feedback")
	}
	view.approximate = true
	state.toggleSFTPPane(view)
	if scopeHint(paintLocalScope(state)) != "" || view.status != fmt.Sprintf(state.messages.SCPItemsFmt, len(view.rows)) {
		t.Fatal("local coverage leaked into the independent remote SCP approximation")
	}
	view.local = true
	if err := state.listLocal(view, localFixture(t)); err != nil {
		t.Fatal(err)
	}
	if hint := scopeHint(paintLocalScope(state)); hint != "" {
		t.Fatalf("small complete listing retained stale scope: %q", hint)
	}
}

func TestLocalScopeSkippedStatIsIncomplete(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	items := scopeDirEntries(t, dir)
	if err := os.Remove(filepath.Join(dir, "report.txt")); err != nil {
		t.Fatal(err)
	}
	view := openLocalPane(t, state, session, dir)
	state.applyLocalListing(view, scanLocalListing(dir, items), view.localSeq)
	hint := scopeHint(paintLocalScope(state))
	if !strings.Contains(hint, "1 unreadable") || !strings.Contains(hint, "2") {
		t.Fatalf("stat failure silently became a complete directory: %q", hint)
	}
}

func TestLocalScopeStatFailuresDoNotExtendRawEntryCap(t *testing.T) {
	dir := localManyFiles(t, maxLocalEntries+1)
	items := scopeDirEntries(t, dir)
	if err := os.Remove(filepath.Join(dir, items[0].Name())); err != nil {
		t.Fatal(err)
	}
	listing := scanLocalListing(dir, items)
	if len(listing.rows) != maxLocalEntries-1 {
		t.Fatalf("stat skip scanned beyond raw-entry cap: loaded %d, want %d", len(listing.rows), maxLocalEntries-1)
	}
}

func TestLocalScopeStaleListingDoesNotReplaceCoverage(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	items := scopeDirEntries(t, dir)
	if err := os.Remove(filepath.Join(dir, "report.txt")); err != nil {
		t.Fatal(err)
	}
	stale := scanLocalListing(dir, items)
	seq := view.localSeq
	small := localFixture(t)
	if err := state.listLocal(view, small); err != nil {
		t.Fatal(err)
	}
	state.applyLocalListing(view, stale, seq)
	if view.lfolder.path != small || len(view.lfolder.rows) != 3 || scopeHint(paintLocalScope(state)) != "" {
		t.Fatal("stale listing replaced rows or completeness")
	}
	if want := fmt.Sprintf(state.messages.SFTPItemsFmt, 3); view.status != want {
		t.Fatalf("complete wording changed: %q, want %q", view.status, want)
	}
}

func TestLocalScopePartialReadErrorRetainsLoadedRowsAndWarning(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localManyFiles(t, localListingInlineEntries+2)
	file, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	items, err := file.ReadDir(localListingInlineEntries + 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	// A real closed directory returns os.ErrClosed on continuation. Preserve the
	// already read rows, without claiming the directory was fully loaded.
	listing := readLocalListing(dir, file, items, nil)
	view := session.files
	view.local = true
	state.applyLocalListing(view, listing, view.localSeq)
	if len(view.lfolder.rows) != localListingInlineEntries+1 || !strings.Contains(scopeHint(paintLocalScope(state)), "Directory read stopped:") {
		t.Fatal("partial directory read silently became complete or discarded readable rows")
	}
	// Inline partial errors are also honest; normal EOF is complete.
	state.applyLocalListing(view, readLocalListing(dir, file, items[:2], os.ErrPermission), view.localSeq)
	if hint := scopeHint(paintLocalScope(state)); !strings.Contains(hint, "Directory read stopped:") || !strings.Contains(hint, "permission") {
		t.Fatalf("inline partial error lost: %q", hint)
	}
	state.applyLocalListing(view, readLocalListing(dir, file, nil, io.EOF), view.localSeq)
	if hint := scopeHint(paintLocalScope(state)); hint != "" {
		t.Fatalf("EOF marked incomplete: %q", hint)
	}
}

func TestLocalScopeFailedNewerRequestDropsPendingAcceptance(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	items := scopeDirEntries(t, dir)
	if err := os.Remove(filepath.Join(dir, "report.txt")); err != nil {
		t.Fatal(err)
	}
	state.applyLocalListing(view, scanLocalListing(dir, items), view.localSeq)
	warning := scopeHint(paintLocalScope(state))
	big := localManyFiles(t, localListingInlineEntries+1)
	if err := state.listLocal(view, big); err != nil {
		t.Fatal(err)
	}
	seq := view.localSeq
	if err := state.listLocal(view, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing folder unexpectedly opened")
	}
	items = scopeDirEntries(t, big)
	// Deliver the overtaken callback deterministically, then pump the actual
	// asynchronous one. Neither may discard the displayed scope after failure.
	state.applyLocalListing(view, scanLocalListing(big, items), seq)
	state.Wake()
	if view.lfolder.path != dir || len(view.lfolder.rows) != 2 || scopeHint(paintLocalScope(state)) != warning || view.errorText == "" {
		t.Fatal("older background listing replaced the displayed folder or scope after a newer failed request")
	}
}

func TestLocalScopeAllStatFailuresDoNotClaimAnEmptyFolder(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localManyFiles(t, 2)
	items := scopeDirEntries(t, dir)
	for _, item := range items {
		if err := os.Remove(filepath.Join(dir, item.Name())); err != nil {
			t.Fatal(err)
		}
	}
	view := openLocalPane(t, state, session, dir)
	state.applyLocalListing(view, scanLocalListing(dir, items), view.localSeq)
	canvas := paintLocalScope(state)
	if hint := scopeHint(canvas); !strings.Contains(hint, "2 unreadable") || !strings.Contains(hint, "0 items loaded") {
		t.Fatalf("empty partial listing lost its cause/scope: %q", hint)
	}
	found := false
	for _, text := range canvas.Texts() {
		found = found || text.Value == "No readable items loaded"
		if text.Value == state.messages.SFTPEmpty {
			t.Fatal("all stat failures were described as an empty directory")
		}
	}
	if !found {
		t.Fatal("zero loaded rows have no bounded empty message")
	}
}

func TestLocalScopeReviewFailedPathRetiresOnlyItsLocalLoading(t *testing.T) {
	for _, failure := range []string{"open", "initial-read"} {
		for _, feedback := range []string{"none", "operation", "remote-listing"} {
			t.Run(failure+"/"+feedback, func(t *testing.T) {
				state, session, _, _ := openTestSFTP(t)
				dir := localFixture(t)
				view := openLocalPane(t, state, session, dir)
				big := localManyFiles(t, localListingInlineEntries+1)
				if err := state.listLocal(view, big); err != nil {
					t.Fatal(err)
				}
				if view.status != state.messages.SFTPLoading {
					t.Fatal("fixture did not start an asynchronous local listing")
				}
				seq := view.localSeq
				want := fmt.Sprintf(state.messages.SFTPItemsFmt, 3)
				if feedback == "operation" {
					view.status = "Upload completed"
					want = view.status
				} else if feedback == "remote-listing" {
					// Remote enumeration uses this same status helper and message.
					view.startListingStatus(state.messages.SFTPLoading)
					want = state.messages.SFTPLoading
				}
				destination := filepath.Join(dir, "missing")
				if failure == "initial-read" {
					destination = filepath.Join(dir, "report.txt")
				}
				*view.lfolder.pathInput() = *text.NewEditor(destination)
				state.submitSFTPPath(view)
				state.applyLocalListing(view, scanLocalListing(big, scopeDirEntries(t, big)), seq)
				state.Wake()
				if view.lfolder.path != dir || len(view.lfolder.rows) != 3 || view.errorText != "" || len(state.fileNotices.items) != 1 {
					t.Fatalf("failed submit changed the folder or lost its toast: path=%q rows=%d error=%q notices=%d", view.lfolder.path, len(view.lfolder.rows), view.errorText, len(state.fileNotices.items))
				}
				if view.status != want {
					t.Fatalf("failed local request status=%q, want %q", view.status, want)
				}
				wantListing := ""
				if feedback == "remote-listing" {
					wantListing = state.messages.SFTPLoading
				}
				if view.listingStatus != wantListing {
					t.Fatalf("listing message ownership=%q, want %q", view.listingStatus, wantListing)
				}
			})
		}
	}
}
