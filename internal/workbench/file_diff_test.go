//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func TestDocumentRemoteSaveRequiresConfirmation(t *testing.T) {
	d := documentFixture(t)
	d.local = false
	d.editor.Insert("local change\n")
	d.requestSave(false)
	if d.confirm != "save" || d.busy {
		t.Fatalf("remote save did not ask first: confirm=%q busy=%t", d.confirm, d.busy)
	}
	d.click(hitDocumentCancel)
	if d.confirm != "" || !d.dirty() {
		t.Fatal("canceling save confirmation changed the local edit")
	}
}

func TestDocumentRemoteSaveDirectAndSaveCloseUseOneConfirmation(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() {
		host.Quit()
		app.UseApplicationRunner(nil)
	})
	if err := app.RunApplication(func(application *app.Application) error {
		state, session, _, remote := openTestSFTP(t)
		state.options.OpenContentWindow = application.OpenContentWindow
		state.openFileWindow(session.files, "/home/test/notes.txt", false)
		waitDocument := func(done func(*fileDocument) bool) *fileDocument {
			t.Helper()
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				state.Wake()
				for _, document := range state.fileWindows.documents {
					if done(document) {
						return document
					}
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatal("document save did not settle")
			return nil
		}
		document := waitDocument(func(d *fileDocument) bool { return d.ready && !d.busy })
		app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&document.editor)
		document.editor.Insert("direct save\n")

		document.requestSave(false)
		if document.confirm != "save" || document.confirmCloseAfter {
			t.Fatalf("direct save opened the wrong confirmation: %q closeAfter=%t", document.confirm, document.confirmCloseAfter)
		}
		document.click(hitDocumentSaveDirect)
		document = waitDocument(func(d *fileDocument) bool { return !d.busy && !d.dirty() })
		remote.mu.Lock()
		actual := string(remote.files["/home/test/notes.txt"])
		remote.mu.Unlock()
		if actual != "direct save\n" {
			t.Fatalf("direct save wrote %q", actual)
		}

		app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&document.editor)
		document.editor.Insert("save and close\n")
		document.requestSave(true)
		if document.confirm != "save" || !document.confirmCloseAfter {
			t.Fatalf("save and close opened the wrong confirmation: %q closeAfter=%t", document.confirm, document.confirmCloseAfter)
		}
		document.click(hitDocumentSaveDirect)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			state.Wake()
			remote.mu.Lock()
			actual = string(remote.files["/home/test/notes.txt"])
			remote.mu.Unlock()
			if actual == "save and close\n" {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if actual != "save and close\n" {
			t.Fatalf("save and close wrote %q", actual)
		}
		for time.Now().Before(deadline) && !document.closed {
			state.Wake()
			time.Sleep(time.Millisecond)
		}
		if !document.closed {
			t.Fatal("save and close did not close the document")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentComparisonRejectsUndecodableServerText(t *testing.T) {
	d := documentFixture(t)
	d.local = false
	d.format.Encoding = "UTF-8"
	d.comparison.loading = true
	d.comparison.generation = 1
	d.busy = true
	d.apply(fileDocumentEvent{document: d, data: []byte{0xff}, compared: true})
	if d.busy || d.comparison.active || d.comparison.loading || d.problem == "" || d.editor.String() != "name: 中文\n# configuration\nenabled: true\n" {
		t.Fatalf("undecodable comparison changed the editor: %+v", d)
	}
}

func TestDocumentComparisonEnforcesFixedTextBudget(t *testing.T) {
	if !fileDiffWithinLimit(strings.Repeat("a", fileDiffMaxBytes)) {
		t.Fatal("the exact byte limit should be comparable")
	}
	if fileDiffWithinLimit(strings.Repeat("a", fileDiffMaxBytes+1)) {
		t.Fatal("text above the byte limit should be rejected")
	}
	exactLines := strings.Repeat("x\n", fileDiffMaxLines-1) + "x\n"
	if !fileDiffWithinLimit(exactLines) {
		t.Fatal("the exact line limit should be comparable")
	}
	if fileDiffWithinLimit(exactLines + "x\n") {
		t.Fatal("text above the line limit should be rejected")
	}
}

func TestDocumentComparisonRejectsOversizedLocalTextBeforeReading(t *testing.T) {
	d := documentFixture(t)
	d.local = false
	d.editor = text.Editor{}
	d.editor.Insert(strings.Repeat("a", fileDiffMaxBytes+1))

	d.beginComparison()
	if d.busy || d.comparison.active || d.comparison.loading {
		t.Fatalf("oversized local text started a comparison: %+v", d.comparison)
	}
	if d.problem != d.owner.messages.FileWindow.CompareLimit || len(d.notices.items) != 1 ||
		d.notices.items[0].kind != components.ToastWarning {
		t.Fatalf("oversized local comparison was not explained: problem=%q notices=%+v", d.problem, d.notices.items)
	}
}

func TestDocumentComparisonRejectsOversizedServerText(t *testing.T) {
	d := documentFixture(t)
	d.local = false
	d.format.Encoding = "UTF-8"
	d.comparison.loading = true
	d.comparison.generation = 1
	d.busy = true

	d.apply(fileDocumentEvent{
		document: d, data: []byte(strings.Repeat("a", fileDiffMaxBytes+1)), compared: true,
	})
	if d.busy || d.comparison.active || d.comparison.loading || d.comparison.window != nil {
		t.Fatalf("oversized server text started a comparison: %+v", d.comparison)
	}
	if d.problem != d.owner.messages.FileWindow.CompareLimit || len(d.notices.items) != 1 ||
		d.notices.items[0].kind != components.ToastWarning {
		t.Fatalf("oversized server comparison was not explained: problem=%q notices=%+v", d.problem, d.notices.items)
	}
}

func TestDocumentComparisonStopsUpdatingAboveTheTextBudgetAndResumes(t *testing.T) {
	d := documentFixture(t)
	d.local = false
	d.comparison.active = true
	d.comparison.baseline = d.original
	d.comparison.result = components.DiffCodeLines(d.comparison.baseline, d.editor.String())
	d.comparison.computedValue = d.editor.String()
	d.comparison.computedSet = true
	d.editor.Insert(strings.Repeat("a", fileDiffMaxBytes+1-len(d.editor.String())))

	d.scheduleDiff()
	if !d.comparison.oversized || d.comparison.pending || d.comparison.timer != nil {
		t.Fatalf("oversized edit remained eligible for comparison: %+v", d.comparison)
	}
	if d.canUploadComparison() || d.comparisonStatus() != d.owner.messages.FileWindow.CompareLimit {
		t.Fatalf("oversized edit remained saveable or had the wrong status: %q", d.comparisonStatus())
	}

	d.editor = text.Editor{}
	d.editor.Insert("small enough\n")
	d.scheduleDiff()
	if d.comparison.oversized || !d.comparison.pending || d.comparison.timer == nil {
		t.Fatalf("comparison did not resume below the budget: %+v", d.comparison)
	}
	d.stopComparison()
}

func TestDocumentSaveRemainsAvailableAboveComparisonLimit(t *testing.T) {
	d := documentFixture(t)
	d.local = false
	d.editor = text.Editor{}
	d.editor.Insert(strings.Repeat("a", fileDiffMaxBytes+1))

	d.requestSave(false)
	if d.confirm != "save" || d.busy {
		t.Fatalf("comparison limit unexpectedly blocked an ordinary save: confirm=%q busy=%t", d.confirm, d.busy)
	}
}

func TestDocumentComparisonCannotUploadAnEqualFile(t *testing.T) {
	d := documentFixture(t)
	d.local = false
	d.comparison.active = true
	d.comparison.baseline = d.editor.String()
	d.comparison.result = components.DiffCodeLines(d.comparison.baseline, d.editor.String())
	if d.canUploadComparison() || d.comparisonStatus() != d.owner.messages.FileWindow.CompareEqual {
		t.Fatalf("equal comparison enabled saving or reported the wrong status: %q", d.comparisonStatus())
	}
	d.stopComparison()
}

func TestDocumentComparisonCloseKeepsEditorAndCancelsPendingDocumentClose(t *testing.T) {
	d := documentFixture(t)
	d.local = false
	d.editor.Insert("unsaved local edit\n")
	d.comparison.active = true
	d.comparison.confirm = true
	d.saveDirect(true)
	if !d.closeAfter || !d.dirty() {
		t.Fatal("saving from a comparison did not retain the requested close")
	}

	firstClose := false
	d.requestComparisonClose(func() { firstClose = true })
	if firstClose || d.comparison.confirm || !d.comparison.active || !d.closeAfter {
		t.Fatal("the first close should dismiss the comparison save confirmation")
	}

	secondClose := false
	d.requestComparisonClose(func() { secondClose = true })
	if !secondClose || d.comparison.active || d.closeAfter || !d.dirty() || d.editor.String() != "name: 中文\n# configuration\nenabled: true\nunsaved local edit\n" {
		t.Fatal("closing the comparison window lost the local edit or kept the document close pending")
	}
}

func TestDocumentSaveConfirmationAndDiffLayoutAtDisplayScales(t *testing.T) {
	for _, light := range []bool{false, true} {
		for _, scale := range []float64{1, 1.25, 1.5, 2} {
			t.Run(fmt.Sprintf("light=%t/scale=%.2f", light, scale), func(t *testing.T) {
				d := documentFixture(t)
				if light {
					d.owner.setAppearance(appearanceLight)
				}
				d.local = false
				d.editor.Insert(strings.Repeat("changed: true\n", 40) + strings.Repeat("long value ", 80) + "\n")
				config := d.config()
				config.Width, config.Height = 640, 420
				view, err := headless.Open(config, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()

				d.requestSave(false)
				view.Resize(641, 421)
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				assertDocumentHitsInside(t, view, 641, 421, hitDocumentCancel, hitDocumentSaveDirect)
				if dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0o700); err != nil {
						t.Fatal(err)
					}
					appearance := "dark"
					if light {
						appearance = "light"
					}
					_ = view.Screenshot(filepath.Join(dir, fmt.Sprintf("%.2f-save-confirm-%s.png", scale, appearance)))
				}

				d.confirm = ""
				view.Resize(643, 423)
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				assertDocumentHitsInside(t, view, 643, 423, hitDocumentSave, hitDocumentSaveMenu)
				assertDocumentSplitSaveButton(t, view, false)
				d.ui.saveMenuOpen = true
				view.Resize(644, 424)
				if err := view.Rest(); err != nil {
					t.Fatal(err)
				}
				assertDocumentHitsInside(t, view, 644, 424, hitDocumentSaveMenuDismiss, hitDocumentCompare)
				assertDocumentSplitSaveButton(t, view, true)
				for _, hit := range view.Snapshot().Hits {
					if hit.ID == hitDocumentCompare && hit.Bounds.Height != 32 {
						t.Fatalf("save menu row was clipped: %+v", hit.Bounds)
					}
				}
				if dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR"); dir != "" {
					appearance := "dark"
					if light {
						appearance = "light"
					}
					_ = view.Screenshot(filepath.Join(dir, fmt.Sprintf("%.2f-save-menu-%s.png", scale, appearance)))
				}

				d.ui.saveMenuOpen = false
				d.comparison.active = true
				d.comparison.baseline = d.original
				d.comparison.result = components.DiffCodeLines(d.original, d.editor.String())
				comparison := d.comparisonConfig()
				comparison.Width, comparison.Height = 1000, 700
				diffView, err := headless.Open(comparison, nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer diffView.Close()
				diffView.Resize(1001, 701)
				if err := diffView.Rest(); err != nil {
					t.Fatal(err)
				}
				assertDocumentHitsInside(t, diffView, 1001, 701, hitDocumentInput, hitDocumentReturnToEdit, hitDocumentConfirmUpload)
				if scale == 1 {
					geometry := d.comparison.ui.diff.Geometry
					x := geometry.VerticalThumb.X + geometry.VerticalThumb.Width/2
					y := geometry.VerticalThumb.Y + geometry.VerticalThumb.Height/2
					if err := diffView.Driver().Press(x, y, ui.PointerButtonPrimary); err != nil {
						t.Fatal(err)
					}
					if err := diffView.Driver().Move(x, y+24); err != nil {
						t.Fatal(err)
					}
					firstOffset := d.comparison.ui.diff.Geometry.OffsetY
					if err := diffView.Driver().Move(x, y+48); err != nil {
						t.Fatal(err)
					}
					if d.comparison.ui.diff.Geometry.OffsetY <= firstOffset {
						t.Fatalf("scrollbar did not follow the captured pointer: first=%v second=%v", firstOffset, d.comparison.ui.diff.Geometry.OffsetY)
					}
					if err := diffView.Driver().Release(x, y+48, ui.PointerButtonPrimary); err != nil {
						t.Fatal(err)
					}
					d.comparison.ui.diff.ScrollTo(0, 0)
					if err := diffView.Rest(); err != nil {
						t.Fatal(err)
					}
				}
				if dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR"); dir != "" {
					var input ui.Rect
					for _, hit := range diffView.Snapshot().Hits {
						if hit.ID == hitDocumentInput {
							input = hit.Bounds
							break
						}
					}
					if input.Width <= 0 {
						t.Fatal("diff selection fixture cannot find the editable pane")
					}
					if err := diffView.Click(input.X+24, input.Y+10); err != nil {
						t.Fatal(err)
					}
					selectionEnd := min(320, len([]rune(d.editor.String())))
					app.TextEvent{Kind: app.TextSetSelection, Selection: app.TextRange{End: selectionEnd}}.Apply(&d.editor)
					if err := diffView.Resize(1002, 702); err != nil {
						t.Fatal(err)
					}
					if err := diffView.Rest(); err != nil {
						t.Fatal(err)
					}
					if selection := d.editor.Selection(); selection.Start != 0 || selection.End != selectionEnd {
						t.Fatalf("selection fixture = %+v, want 0..%d", selection, selectionEnd)
					}
					snapshot := diffView.Snapshot()
					if snapshot.Editor == nil || !snapshot.Editor.HideNativeOverlays {
						t.Fatalf("headless native editor = %+v, want self-painted overlays", snapshot.Editor)
					}
					selectionColor := d.owner.Theme().Selection
					selectionRects := make([]ui.Rect, 0, 2)
					for _, primitive := range snapshot.Primitives {
						if primitive.Kind == app.RectanglePrimitive && primitive.Color == selectionColor &&
							primitive.Bounds.Height >= 10 && primitive.Bounds.Height < 30 &&
							primitive.Bounds.Width > 0 && primitive.Bounds.Width < input.Width {
							selectionRects = append(selectionRects, primitive.Bounds)
						}
					}
					if len(selectionRects) < 2 {
						t.Fatalf("diff selection fixture painted %+v, want per-line selection backgrounds", selectionRects)
					}
					if err := os.MkdirAll(dir, 0o700); err != nil {
						t.Fatal(err)
					}
					name := "diff-light.png"
					if !light {
						name = "diff-dark.png"
					}
					_ = diffView.Screenshot(filepath.Join(dir, fmt.Sprintf("%.2f-%s", scale, name)))
				}
			})
		}
	}
}

func assertDocumentHitsInside(t *testing.T, view *headless.Session, width, height float32, ids ...ui.ID) {
	t.Helper()
	for _, wanted := range ids {
		found := false
		for _, hit := range view.Snapshot().Hits {
			if hit.ID != wanted {
				continue
			}
			found = true
			if hit.Bounds.X < 0 || hit.Bounds.Y < 0 || hit.Bounds.X+hit.Bounds.Width > width+.01 || hit.Bounds.Y+hit.Bounds.Height > height+.01 {
				t.Fatalf("%v escaped the viewport: %+v", wanted, hit.Bounds)
			}
		}
		if !found {
			available := make([]string, 0, len(view.Snapshot().Hits))
			for _, hit := range view.Snapshot().Hits {
				available = append(available, tree.Text(hit.ID))
			}
			t.Fatalf("missing hit %s (%v); available hits: %s", tree.Text(wanted), wanted, strings.Join(available, ", "))
		}
	}
}

func assertDocumentSplitSaveButton(t *testing.T, view *headless.Session, open bool) {
	t.Helper()
	var primary, menu *ui.Hit
	for _, hit := range view.Snapshot().Hits {
		switch hit.ID {
		case hitDocumentSave:
			hit := hit
			primary = &hit
		case hitDocumentSaveMenu:
			hit := hit
			menu = &hit
		}
	}
	if primary == nil || menu == nil {
		t.Fatalf("missing split save actions: primary=%v menu=%v", primary != nil, menu != nil)
	}
	if primary.Bounds.Y != menu.Bounds.Y || primary.Bounds.Height != menu.Bounds.Height ||
		primary.Bounds.X+primary.Bounds.Width != menu.Bounds.X || menu.Bounds.Width != 32 {
		t.Fatalf("save actions are not one connected split button: primary=%+v menu=%+v", primary.Bounds, menu.Bounds)
	}
	if primary.Role != ui.RoleButton || menu.Role != ui.RoleButton || !menu.Expandable || menu.Expanded != open {
		t.Fatalf("split save semantics: primary=%+v menu=%+v", primary, menu)
	}
}

func TestDocumentComparisonReadsLatestRevisionAndProtectsAgainstLaterChanges(t *testing.T) {
	host := &documentTestHost{}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() {
		host.Quit()
		app.UseApplicationRunner(nil)
	})
	if err := app.RunApplication(func(application *app.Application) error {
		state, session, _, remote := openTestSFTP(t)
		state.options.OpenContentWindow = application.OpenContentWindow
		state.openFileWindow(session.files, "/home/test/notes.txt", false)
		waitDocument := func(done func(*fileDocument) bool) *fileDocument {
			t.Helper()
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				state.Wake()
				for _, document := range state.fileWindows.documents {
					if done(document) {
						return document
					}
				}
				time.Sleep(time.Millisecond)
			}
			details := make([]string, 0, len(state.fileWindows.documents))
			for _, candidate := range state.fileWindows.documents {
				details = append(details, fmt.Sprintf(
					"ready=%t busy=%t dirty=%t active=%t loading=%t pending=%t conflict=%t saving=%t window=%p comparison-window=%p problem=%q",
					candidate.ready, candidate.busy, candidate.dirty(), candidate.comparison.active,
					candidate.comparison.loading, candidate.comparison.pending, candidate.comparison.conflict,
					candidate.comparison.saving, candidate.window, candidate.comparison.window, candidate.problem,
				))
			}
			t.Fatalf("document comparison did not settle: %s", strings.Join(details, "; "))
			return nil
		}
		document := waitDocument(func(d *fileDocument) bool { return d.ready && !d.busy })
		app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&document.editor)
		document.editor.Insert("local final\n")

		remote.mu.Lock()
		remote.files["/home/test/notes.txt"] = []byte("server current\n")
		remote.mu.Unlock()
		document.click(hitDocumentSaveMenu)
		if !document.ui.saveMenuOpen {
			t.Fatal("the save menu did not open")
		}
		document.click(hitDocumentCompare)
		document = waitDocument(func(d *fileDocument) bool {
			return d.comparison.active && !d.comparison.loading && !d.comparison.pending
		})
		if len(host.windows) != 2 || document.comparison.window == nil || document.window == nil ||
			document.comparison.window == document.window {
			t.Fatalf("comparison did not use an independent content window: windows=%d", len(host.windows))
		}
		if got, want := host.windows[1].config.Title, fmt.Sprintf(document.owner.messages.FileWindow.CompareWindowTitle, document.name); got != want {
			t.Fatalf("comparison window title = %q, want %q", got, want)
		}
		if document.comparison.baseline != "server current\n" ||
			document.comparison.result.Stats.Added == 0 || document.comparison.result.Stats.Removed == 0 {
			t.Fatalf("comparison did not read the latest server version: %+v", document.comparison)
		}
		if !document.canUploadComparison() {
			t.Fatal("a current comparison with changes cannot be uploaded")
		}

		document.clickComparison(hitDocumentConfirmUpload)
		if !document.comparison.confirm || document.confirm != "" {
			t.Fatal("comparison save skipped its final confirmation in the comparison window")
		}
		remote.mu.Lock()
		remote.files["/home/test/notes.txt"] = []byte("changed after compare\n")
		remote.mu.Unlock()
		document.clickComparison(hitDocumentUploadConfirm)
		document = waitDocument(func(d *fileDocument) bool { return !d.busy && d.comparison.conflict })
		if document.editor.String() != "local final\n" || !document.comparison.active || document.canUploadComparison() {
			t.Fatal("revision conflict discarded local text or left upload enabled")
		}
		remote.mu.Lock()
		actual := string(remote.files["/home/test/notes.txt"])
		remote.mu.Unlock()
		if actual != "changed after compare\n" {
			t.Fatalf("revision conflict overwrote the server: %q", actual)
		}

		comparisonWindow := document.comparison.window
		document.clickComparison(hitDocumentCompareRetry)
		document = waitDocument(func(d *fileDocument) bool {
			return d.comparison.active && !d.comparison.loading && !d.comparison.pending && !d.comparison.conflict
		})
		if document.comparison.window != comparisonWindow || len(host.windows) != 2 {
			t.Fatal("retrying the comparison replaced its independent window")
		}
		if document.comparison.baseline != "changed after compare\n" || !document.canUploadComparison() {
			t.Fatalf("retry did not refresh the comparison baseline: %+v", document.comparison)
		}
		document.clickComparison(hitDocumentConfirmUpload)
		document.clickComparison(hitDocumentUploadConfirm)
		document = waitDocument(func(d *fileDocument) bool {
			return !d.busy && !d.comparison.active && !d.dirty()
		})
		remote.mu.Lock()
		actual = string(remote.files["/home/test/notes.txt"])
		remote.mu.Unlock()
		if actual != "local final\n" {
			t.Fatalf("confirmed comparison upload wrote %q", actual)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentComparisonDebouncesEditsAndDropsStaleResults(t *testing.T) {
	d := documentFixture(t)
	d.owner.options.Invalidate = func() {}
	d.local = false
	d.comparison.active = true
	d.comparison.baseline = "base\n"
	d.comparison.result = components.CodeDiffResult{Left: strings.Split(d.comparison.baseline, "\n")}
	d.original = "base\n"
	d.editor = text.Editor{}
	d.editor.Insert("first edit\n")

	d.scheduleDiff()
	defer d.stopComparison()
	first := d.comparison.generation
	d.editor.Insert("second edit\n")
	d.scheduleDiff()
	second := d.comparison.generation
	if first == second || !d.comparison.pending {
		t.Fatal("editing did not arm a new comparison generation")
	}
	d.startDiff(first)
	d.finishDiff(fileDocumentEvent{diff: components.DiffCodeLines("base\n", "first edit\n"), diffGeneration: first})
	if !d.comparison.pending {
		t.Fatal("a stale comparison result cleared the pending state")
	}
	if len(d.comparison.result.Rows) != 0 {
		t.Fatal("a stale comparison result replaced the visible stable diff")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		d.owner.pumpFileWindows()
		if d.comparison.pending && d.comparison.generation == second {
			// The debounce event is delivered before the calculation result.
			break
		}
		time.Sleep(time.Millisecond)
	}
	for time.Now().Before(deadline) {
		d.owner.pumpFileWindows()
		if !d.comparison.pending {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if d.comparison.pending || d.comparison.generation != second {
		t.Fatal("the latest debounced comparison did not finish")
	}
}

func TestDocumentComparisonSkipsAReturnToTheComputedText(t *testing.T) {
	d := documentFixture(t)
	d.owner.options.Invalidate = func() {}
	d.local = false
	d.comparison.active = true
	d.comparison.baseline = "base\n"
	d.comparison.computedValue = "same\n"
	d.comparison.computedSet = true
	d.comparison.generation = 1
	d.editor = text.Editor{}
	d.editor.Insert("same\n")
	d.scheduleDiff()
	if d.comparison.timer != nil || d.comparison.pending {
		t.Fatal("restoring the already-computed text still scheduled a diff")
	}

	d.editor.Insert("changed")
	d.scheduleDiff()
	if d.comparison.timer == nil || !d.comparison.pending {
		t.Fatal("an actual edit did not schedule a diff")
	}
	d.editor = text.Editor{}
	d.editor.Insert("same\n")
	d.scheduleDiff()
	if d.comparison.timer != nil || d.comparison.pending {
		t.Fatal("undoing to the computed text left a queued diff")
	}
}
