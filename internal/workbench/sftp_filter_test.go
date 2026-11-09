package workbench

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/sftpclient"
	layoutcore "motty/internal/workspace"
)

func replaceSFTPFilter(t *testing.T, state *State, session *Session, local bool, value string) {
	t.Helper()
	folder := &session.files.sftpFolder
	if local {
		folder = &session.files.lfolder
	}
	folder.filterEditor.SetSelection(text.Range{End: folder.filterEditor.RuneCount()})
	state.sftpFilterText(session.ID, local, app.TextEvent{Kind: app.TextInsert, Text: value})
}

func TestSFTPFilterMatchesCaseAndUnicodeAndHighlightsTheMatch(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.showSFTPListing(view, view.path, []sftpclient.Entry{
		{Name: "Docs", Mode: os.ModeDir | 0o755},
		{Name: "Release-NOTES.txt", Mode: 0o644},
		{Name: "部署说明.md", Mode: 0o644},
	})
	state.Click(hitSFTPFilterToggle)
	input := state.SFTPInput(app.TextInputContext{ID: contextID(hitSFTPFilter)})
	if input == nil || input.OnEvent == nil {
		t.Fatal("the remote filter has no native text input")
	}
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "notes"}); err != nil {
		t.Fatal(err)
	}
	if len(view.rows) != 1 || view.rows[0].entry.Name != "Release-NOTES.txt" || len(view.allRows) != 3 {
		t.Fatalf("filtered rows=%v, complete=%d", rowNames(view.rows), len(view.allRows))
	}
	if view.rows[0].matchStart <= 0 || view.rows[0].matchEnd <= view.rows[0].matchStart {
		t.Fatalf("match range = %d:%d", view.rows[0].matchStart, view.rows[0].matchEnd)
	}
	wantStatus := fmt.Sprintf(state.messages.Tools.FilesFilterCountFmt, 1, 3)
	if view.status != wantStatus {
		t.Fatalf("status = %q, want %q", view.status, wantStatus)
	}

	replaceSFTPFilter(t, state, session, false, "部署")
	if len(view.rows) != 1 || view.rows[0].entry.Name != "部署说明.md" || view.rows[0].matchStart != 0 {
		t.Fatalf("Unicode filter rows=%v, range=%d:%d", rowNames(view.rows), view.rows[0].matchStart, view.rows[0].matchEnd)
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	filter, ok := hitByID(&canvas, contextID(hitSFTPFilter))
	if !ok || filter.Role != gfx.RoleTextField || filter.Label != state.messages.Tools.FilesFilterLabel {
		t.Fatalf("filter hit=%+v, present=%v", filter, ok)
	}
	foundAccent := false
	for _, text := range canvas.Texts() {
		foundAccent = foundAccent || text.Value == "部署" && text.Style.Color == state.pal.accent
	}
	if !foundAccent {
		t.Fatal("the matching name segment is not highlighted")
	}
}

func TestSFTPFilterKeepsLocalAndRemoteDraftsIndependentAndDoesNoRemoteIO(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	before := remote.readCount(view.path)
	state.Click(hitSFTPFilterToggle)
	replaceSFTPFilter(t, state, session, false, "notes")
	if remote.readCount(view.path) != before {
		t.Fatal("filtering requested another remote listing")
	}
	clickSFTPRow(t, state, view, "notes.txt")
	replaceSFTPFilter(t, state, session, false, "docs")
	if selectedSFTPCount(view) != 0 {
		t.Fatal("a filtered-out selection remained active")
	}

	openLocalPane(t, state, session, localFixture(t))
	state.Click(hitSFTPFilterToggle)
	replaceSFTPFilter(t, state, session, true, "REPORT")
	if len(view.lfolder.rows) != 1 || view.lfolder.rows[0].entry.Name != "report.txt" {
		t.Fatalf("local rows = %v", rowNames(view.lfolder.rows))
	}
	state.Click(hitSFTPPaneToggle)
	if view.sftpFolder.filterEditor.String() != "docs" || len(view.rows) != 1 || view.rows[0].entry.Name != "docs" {
		t.Fatalf("remote draft=%q rows=%v", view.sftpFolder.filterEditor.String(), rowNames(view.rows))
	}
	state.Click(hitSFTPFilterClear)
	if view.sftpFolder.filterEditor.String() != "" || len(view.rows) != len(view.allRows) {
		t.Fatalf("clear left query=%q rows=%d/%d", view.sftpFolder.filterEditor.String(), len(view.rows), len(view.allRows))
	}
	state.Click(hitSFTPPaneToggle)
	if view.lfolder.filterEditor.String() != "REPORT" || len(view.lfolder.rows) != 1 {
		t.Fatalf("local draft=%q rows=%v", view.lfolder.filterEditor.String(), rowNames(view.lfolder.rows))
	}
}

func TestSFTPFilterRefreshesTheCompleteListingWhileFiltered(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	state.Click(hitSFTPFilterToggle)
	replaceSFTPFilter(t, state, session, false, "notes")
	remote.mu.Lock()
	remote.entries[view.path] = append(remote.entries[view.path], workbenchFileInfo{
		name: "notes-2.txt", size: 2, mode: 0o644, when: time.Unix(1_700_000_300, 0),
	})
	remote.mu.Unlock()
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.allRows) == 3 })
	if len(view.rows) != 2 || fmt.Sprint(rowNames(view.rows)) != "[notes-2.txt notes.txt]" {
		t.Fatalf("filtered refreshed rows = %v; complete=%v", rowNames(view.rows), rowNames(view.allRows))
	}
	state.Click(hitSFTPFilterClear)
	if len(view.rows) != 3 {
		t.Fatalf("cleared refreshed rows = %v", rowNames(view.rows))
	}
}

func TestSFTPFilterShortcutUsesTheFileListFocusAndLeavesTerminalFindAlone(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	find := state.shortcuts.chords.Chord(shortcutFind)
	result := state.Shortcut(gfx.KeyEvent{
		Target: contextID(hitSFTPEntryList.At(0)), Modifiers: find.Modifiers, Text: string(find.Char),
	})
	if !result.Handled || result.Focus != contextID(hitSFTPFilter) || !session.files.active().filterOpen || session.term.find.open {
		t.Fatalf("file-list find result=%+v filter=%v terminal=%v", result, session.files.active().filterOpen, session.term.find.open)
	}

	result = state.Shortcut(gfx.KeyEvent{
		Target: contextID(hitTerminalList.At(int(session.ID))), Modifiers: find.Modifiers, Text: string(find.Char),
	})
	if !result.Handled || result.Focus != hitTerminalFindField || !session.term.find.open {
		t.Fatalf("terminal find result=%+v open=%v", result, session.term.find.open)
	}
}

func TestSFTPHiddenLocalDefaultsOffAndToggleHasVisibleState(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := t.TempDir()
	for _, name := range []string{"visible.txt", ".secret"} {
		if err := os.WriteFile(dir+string(os.PathSeparator)+name, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		if name == ".secret" {
			markLocalHidden(t, dir+string(os.PathSeparator)+name)
		}
	}
	if err := os.Mkdir(dir+string(os.PathSeparator)+".config", 0o755); err != nil {
		t.Fatal(err)
	}
	markLocalHidden(t, dir+string(os.PathSeparator)+".config")
	view := openLocalPane(t, state, session, dir)
	if view.lfolder.showHidden || fmt.Sprint(rowNames(view.lfolder.rows)) != "[visible.txt]" || len(view.lfolder.allRows) != 3 {
		t.Fatalf("local hidden default=%v rows=%v complete=%v", view.lfolder.showHidden, rowNames(view.lfolder.rows), rowNames(view.lfolder.allRows))
	}
	assertSFTPHiddenToggleLabel(t, state, state.messages.Tools.FilesShowHidden)
	state.Click(contextID(hitSFTPHeaderHidden))
	if !view.lfolder.showHidden || len(view.lfolder.rows) != 3 {
		t.Fatalf("shown hidden=%v rows=%v", view.lfolder.showHidden, rowNames(view.lfolder.rows))
	}
	assertSFTPHiddenToggleLabel(t, state, state.messages.Tools.FilesHideHidden)
	clickSFTPLocalRow(t, state, view, ".secret")
	state.Click(hitSFTPHiddenToggle)
	if view.lfolder.showHidden || selectedSFTPCount(view) != 0 || fmt.Sprint(rowNames(view.lfolder.rows)) != "[visible.txt]" {
		t.Fatalf("hidden again=%v selected=%d rows=%v", view.lfolder.showHidden, selectedSFTPCount(view), rowNames(view.lfolder.rows))
	}
}

func TestSFTPHiddenRemoteDefaultsOnAndStaysIndependentWithoutIO(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	remote.mu.Lock()
	remote.entries[view.path] = append(remote.entries[view.path], workbenchFileInfo{name: ".env", size: 3, mode: 0o600})
	remote.mu.Unlock()
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.allRows) == 3 })
	if !view.sftpFolder.showHidden || sftpRowIndexOrNegative(view, ".env") < 0 {
		t.Fatalf("remote hidden default=%v rows=%v", view.sftpFolder.showHidden, rowNames(view.rows))
	}
	before := remote.readCount(view.path)
	clickSFTPRow(t, state, view, ".env")
	state.Click(hitSFTPHiddenToggle)
	if view.sftpFolder.showHidden || sftpRowIndexOrNegative(view, ".env") >= 0 || selectedSFTPCount(view) != 0 {
		t.Fatalf("remote hidden off=%v selected=%d rows=%v", view.sftpFolder.showHidden, selectedSFTPCount(view), rowNames(view.rows))
	}
	if remote.readCount(view.path) != before {
		t.Fatal("changing remote hidden visibility requested a listing")
	}

	state.Click(hitSFTPFilterToggle)
	replaceSFTPFilter(t, state, session, false, ".")
	want := fmt.Sprintf(state.messages.Tools.FilesFilterCountFmt, 1, 2)
	if view.status != want || fmt.Sprint(rowNames(view.rows)) != "[notes.txt]" {
		t.Fatalf("hidden filter status=%q rows=%v, want %q", view.status, rowNames(view.rows), want)
	}
	state.Click(hitSFTPHiddenToggle)
	want = fmt.Sprintf(state.messages.Tools.FilesFilterCountFmt, 2, 3)
	if view.status != want || fmt.Sprint(rowNames(view.rows)) != "[.env notes.txt]" || remote.readCount(view.path) != before {
		t.Fatalf("shown filter status=%q rows=%v reads=%d, want %q", view.status, rowNames(view.rows), remote.readCount(view.path), want)
	}

	openLocalPane(t, state, session, localFixture(t))
	if view.lfolder.showHidden {
		t.Fatal("remote visibility leaked into the local side")
	}
	state.Click(hitSFTPHiddenToggle)
	state.Click(hitSFTPPaneToggle)
	if !view.sftpFolder.showHidden || !view.lfolder.showHidden {
		t.Fatalf("independent visibility remote=%v local=%v", view.sftpFolder.showHidden, view.lfolder.showHidden)
	}
}

func assertSFTPHiddenToggleLabel(t *testing.T, state *State, want string) {
	t.Helper()
	var canvas gfx.Canvas
	if want == state.messages.Tools.FilesShowHidden {
		state.Click(contextID(hitSFTPHeaderMore))
	}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	id := contextID(hitSFTPHiddenToggle)
	if want == state.messages.Tools.FilesShowHidden {
		id = contextID(hitSFTPHeaderHidden)
	}
	hit, ok := hitByID(&canvas, id)
	if !ok || hit.Label != want {
		t.Fatalf("hidden toggle=%+v, present=%v, want label %q", hit, ok, want)
	}
}

func TestSFTPFilterEditorOnlyEventsPreserveSelectionAndScroll(t *testing.T) {
	state, session := openLargeSFTP(t, 5000)
	view := session.files
	view.sftpFolder.filterOpen = true
	view.first, view.anchor, view.selected = 2400, 2410, 2410
	view.rows[2410].selected = true
	view.rows[2410].cells[0] = sftpSelectionCell(true)
	state.sftpFilterText(session.ID, false, app.TextEvent{Kind: app.TextSetSelection, Selection: app.TextRange{}})
	if view.first != 2400 || view.anchor != 2410 || view.selected != 2410 || !view.rows[2410].selected {
		t.Fatalf("selection-only edit reset first=%d anchor=%d selected=%d row=%v", view.first, view.anchor, view.selected, view.rows[2410].selected)
	}
	state.sftpFilterText(session.ID, false, app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextMoveLeft})
	if view.first != 2400 || view.anchor != 2410 || view.selected != 2410 || !view.rows[2410].selected {
		t.Fatalf("caret move reset first=%d anchor=%d selected=%d row=%v", view.first, view.anchor, view.selected, view.rows[2410].selected)
	}
}

func TestSFTPFilterBackgroundUpdatePreservesScrollSelectionAndAnchor(t *testing.T) {
	state, session := openLargeSFTP(t, 5000)
	view := session.files
	view.first, view.anchor, view.selected = 2400, 2410, 2410
	view.rows[2410].selected = true
	view.rows[2410].cells[0] = sftpSelectionCell(true)
	entries := make([]sftpclient.Entry, len(view.allRows)+1)
	for index := range view.allRows {
		entries[index] = view.allRows[index].entry
	}
	entries[len(entries)-1] = sftpclient.Entry{Name: "zz-new.txt", Mode: 0o644}
	state.updateSFTPRows(view, entries)
	if view.first != 2400 || view.anchor != 2410 || view.selected != 2410 || !view.rows[2410].selected || view.rows[2410].entry.Name != "entry-02410.txt" {
		t.Fatalf("background update reset first=%d anchor=%d selected=%d row=%q selected-row=%v", view.first, view.anchor, view.selected, view.rows[2410].entry.Name, view.rows[2410].selected)
	}
}

func rowNames(rows []sftpRow) []string {
	names := make([]string, len(rows))
	for index := range rows {
		names[index] = rows[index].entry.Name
	}
	return names
}

// Typing a filter must keep the checkmarks of the rows it leaves visible and
// drop the ones it hides, instead of clearing every selection.
func TestSFTPFilterKeepsVisibleSelectionAndDropsHiddenRows(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.showSFTPListing(view, view.path, []sftpclient.Entry{
		{Name: "alpha.txt", Mode: 0o644},
		{Name: "beta.txt", Mode: 0o644},
		{Name: "gamma.log", Mode: 0o644},
	})
	folder := &view.sftpFolder
	folder.selectRow(0, 0)
	folder.selectRow(len(folder.rows)-1, gfx.ModifierShift)
	replaceSFTPFilter(t, state, session, false, "beta")
	if len(view.rows) != 1 || view.rows[0].entry.Name != "beta.txt" {
		t.Fatalf("filtered rows = %v", rowNames(view.rows))
	}
	if !view.rows[0].selected {
		t.Fatal("the visible matching row lost its checkmark")
	}
	for _, row := range folder.allRows {
		if row.entry.Name != "beta.txt" && row.selected {
			t.Fatalf("%s stayed selected after being filtered out", row.entry.Name)
		}
	}
}

func TestSFTPHeaderOverflowUsesMoreMenuForSecondaryFolderControls(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	session.files.sftpFolder.showHidden = false
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, contextID(hitSFTPHeaderMore)); !ok {
		t.Fatal("narrow file panel has no more button")
	}
	if _, ok := hitByID(canvas, contextID(hitSFTPFilterToggle)); ok {
		t.Fatal("filter toggle still consumes the narrow header")
	}
	if _, ok := hitByID(canvas, contextID(hitSFTPHiddenToggle)); ok {
		t.Fatal("disabled hidden toggle still consumes the narrow header")
	}
	if !containsCanvasText(canvas, state.messages.SFTPFiles) {
		t.Fatal("file panel title was clipped out of the narrow header")
	}

	state.Click(contextID(hitSFTPHeaderMore))
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, contextID(hitSFTPHeaderFilter)); !ok {
		t.Fatal("more menu has no filter action")
	}
	if _, ok := hitByID(canvas, contextID(hitSFTPHeaderHidden)); !ok {
		t.Fatal("more menu has no hidden-file action")
	}
	state.Click(contextID(hitSFTPHeaderFilter))
	if !session.files.sftpFolder.filterOpen {
		t.Fatal("more menu filter action did not open the filter")
	}
	state.Click(contextID(hitSFTPHeaderMore))
	state.Click(contextID(hitSFTPHeaderHidden))
	if !session.files.sftpFolder.showHidden {
		t.Fatal("more menu hidden-file action did not enable hidden entries")
	}
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, contextID(hitSFTPHiddenToggle)); !ok {
		t.Fatal("enabled hidden state is not visible in the header")
	}
}

func containsCanvasText(canvas *gfx.Canvas, value string) bool {
	for _, text := range canvas.Texts() {
		if text.Value == value {
			return true
		}
	}
	return false
}

func TestSFTPFilterEscapeClearsThenClosesWithoutClosingThePanel(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.showSFTPListing(view, view.path, []sftpclient.Entry{
		{Name: "notes.txt", Mode: 0o644},
		{Name: "other.txt", Mode: 0o644},
	})
	state.Click(hitSFTPFilterToggle)
	replaceSFTPFilter(t, state, session, false, "notes")
	if len(view.sftpFolder.rows) != 1 {
		t.Fatalf("filter shows %d rows, want 1", len(view.sftpFolder.rows))
	}

	panelOpen := func() bool {
		context, ok := state.deck.Context(state.deck.ActiveWorkspace())
		return ok && context.Open && context.Kind == layoutcore.ContextSFTP
	}

	result := state.Key(gfx.KeyEvent{Target: contextID(hitSFTPFilter), Key: gfx.KeyEscape})
	if !result.Handled || !panelOpen() {
		t.Fatalf("Escape in the filter closed the panel: %+v", result)
	}
	if !view.sftpFolder.filterOpen || view.sftpFolder.filterInputEditor().String() != "" || len(view.sftpFolder.rows) != 2 {
		t.Fatalf("first Escape did not clear the query: open=%v query=%q rows=%d",
			view.sftpFolder.filterOpen, view.sftpFolder.filterInputEditor().String(), len(view.sftpFolder.rows))
	}
	if result.Focus != contextID(hitSFTPFilter) {
		t.Fatalf("focus left the filter: %v", result.Focus)
	}

	result = state.Key(gfx.KeyEvent{Target: contextID(hitSFTPFilter), Key: gfx.KeyEscape})
	if !result.Handled || !panelOpen() || view.sftpFolder.filterOpen {
		t.Fatalf("second Escape did not close the filter or closed the panel: %+v open=%v", result, view.sftpFolder.filterOpen)
	}
	if result.Focus != contextID(hitSFTPEntryList.At(0)) {
		t.Fatalf("closing the filter left focus at %v, want the first file", result.Focus)
	}
}
