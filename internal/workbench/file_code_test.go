//go:build darwin || windows || linux

package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"motty/internal/filecontent"
	"motty/internal/i18n"
)

// codeDocument is a document of a source file in the language of its name.
func newCodeDocument(t *testing.T, name, source string) *fileDocument {
	t.Helper()
	d := documentFixture(t)
	d.name = name
	decoded, err := filecontent.Decode([]byte(source), "")
	if err != nil {
		t.Fatal(err)
	}
	d.setText(decoded, nil)
	return d
}

// codeWindow is a headless window of a document. State the test changes on the
// document directly is not an event, so the window paints it again on refresh.
type codeWindow struct {
	*headless.Session
	width, height int
}

func (w codeWindow) refresh(t *testing.T) {
	t.Helper()
	if err := w.Resize(w.width, w.height+1); err != nil {
		t.Fatal(err)
	}
	if err := w.Resize(w.width, w.height); err != nil {
		t.Fatal(err)
	}
	if err := w.Rest(); err != nil {
		t.Fatal(err)
	}
}

// openCodeDocumentWindow opens the document in a headless window and focuses its editor.
func openCodeDocumentWindow(t *testing.T, d *fileDocument, width, height int, scale float64) codeWindow {
	t.Helper()
	config := d.config()
	config.Width, config.Height = width, height
	view, err := headless.Open(config, nil, headless.Options{Scale: scale})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { view.Close() })
	if err := view.Resize(width, height); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	window := codeWindow{Session: view, width: width, height: height}
	field := documentHitBounds(t, window, hitDocumentInput)
	if err := view.Click(field.X+field.Width/2, field.Y+field.Height-8); err != nil {
		t.Fatal(err)
	}
	return window
}

func documentHitBounds(t *testing.T, view codeWindow, id gfx.ID) gfx.Rect {
	t.Helper()
	for _, hit := range view.Snapshot().Hits {
		if hit.ID == id {
			return hit.Bounds
		}
	}
	t.Fatalf("no hit %v", id)
	return gfx.Rect{}
}

func documentHasHit(view codeWindow, id gfx.ID) bool {
	for _, hit := range view.Snapshot().Hits {
		if hit.ID == id {
			return true
		}
	}
	return false
}

// primary is the modifier of the platform's editing chords.
func documentPrimaryModifier() gfx.Modifiers {
	if runtime.GOOS == "darwin" {
		return gfx.ModifierMeta
	}
	return gfx.ModifierControl
}

func documentCaretAt(d *fileDocument, line, column int) {
	value := d.editor.String()
	d.editor.MoveTo(d.editor.RuneIndex(text.OffsetAt(value, text.Position{Line: line, Column: column})), false)
}

func TestDocumentOpensAtTheStartWithNothingToUndo(t *testing.T) {
	d := newCodeDocument(t, "main.go", "package main\n\nfunc main() {\n}\n")
	if d.editor.CanUndo() {
		t.Fatal("loading the file left an undo step that would empty it")
	}
	if d.editor.Caret() != 0 {
		t.Fatalf("the caret opens at %d, want the start", d.editor.Caret())
	}
	if d.editor.Undo() || d.editor.String() != "package main\n\nfunc main() {\n}\n" {
		t.Fatal("undo changed a file that was only opened")
	}
}

func TestDocumentCodeEditingFollowsTheLanguageAndTheFilesOwnStyle(t *testing.T) {
	cases := []struct {
		name, source string
		hard         bool
		width        int
		comment      string
	}{
		{"main.go", "package main\n", true, 4, "//"},
		{"app.py", "def f():\n    pass\n", false, 4, "#"},
		{"config.yaml", "a:\n  b: 1\n", false, 2, "#"},
		{"Makefile", "all:\n\techo\n", true, 4, "#"},
		{"notes.txt", "plain\n", false, 4, ""},
		{"data.json", "{\n  \"a\": 1\n}\n", false, 2, ""},
		{"query.sql", "select 1;\n", false, 4, "--"},
	}
	for _, c := range cases {
		options := codeOptionsFor(c.name, c.source)
		if options.HardTabs != c.hard || options.TabWidth != c.width || options.LineComment != c.comment {
			t.Errorf("%s: %+v, want hard=%v width=%d comment=%q", c.name, options, c.hard, c.width, c.comment)
		}
	}
	if options := codeOptionsFor("notes.txt", "it's 'quoted'\n"); options.AutoClose {
		t.Error("plain text pairs brackets and quotes")
	}
	if options := codeOptionsFor("main.go", ""); !options.AutoClose || !options.AutoIndent {
		t.Error("code is not edited as code")
	}
}

func TestDocumentEditorIndentsPairsAndCommentsLikeAnEditor(t *testing.T) {
	d := newCodeDocument(t, "main.go", "package main\n\nfunc main() {}\n")
	view := openCodeDocumentWindow(t, d, 900, 600, 1)
	driver := view.Driver()

	// Enter between the braces opens a block with the caret on an indented line.
	documentCaretAt(d, 2, len("func main() {"))
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got, want := d.editor.String(), "package main\n\nfunc main() {\n\t\n}\n"; got != want {
		t.Fatalf("Enter between braces gave %q, want %q", got, want)
	}
	if position := text.PositionAt(d.editor.String(), d.editor.ByteOffset(d.editor.Caret())); position != (text.Position{Line: 3, Column: 1}) {
		t.Fatalf("caret at %+v, want the indented line", position)
	}

	// Typing an opener pairs it, and the closer typed over it is stepped over.
	for _, r := range "fmt.Println(" { // keys arrive one character at a time
		if err := view.Type(string(r)); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.editor.String(); !strings.Contains(got, "\tfmt.Println()\n}") {
		t.Fatalf("an opener did not pair: %q", got)
	}
	if err := view.Type(")"); err != nil {
		t.Fatal(err)
	}
	if got := d.editor.String(); !strings.Contains(got, "\tfmt.Println()\n}") || strings.Contains(got, "))") {
		t.Fatalf("the closer was typed twice: %q", got)
	}

	// Comment and uncomment with the editor chord.
	if handled, err := driver.ShortcutChord(documentPrimaryModifier(), '/'); err != nil || !handled {
		t.Fatalf("the comment chord was not handled: %v", err)
	}
	if got := d.editor.String(); !strings.Contains(got, "\t// fmt.Println()\n") {
		t.Fatalf("comment gave %q", got)
	}
	if _, err := driver.ShortcutChord(documentPrimaryModifier(), '/'); err != nil {
		t.Fatal(err)
	}
	if got := d.editor.String(); !strings.Contains(got, "\tfmt.Println()\n") || strings.Contains(got, "//") {
		t.Fatalf("uncomment gave %q", got)
	}

	// Tab with several lines selected indents them; Shift+Tab takes it back, each as one undo.
	d.editor.SetSelection(text.Range{Start: d.editor.RuneIndex(strings.Index(d.editor.String(), "func")), End: d.editor.RuneCount()})
	before := d.editor.String()
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if got := d.editor.String(); !strings.Contains(got, "\tfunc main() {\n\t\t") {
		t.Fatalf("Tab over lines gave %q", got)
	}
	if err := driver.Key(gfx.KeyTab, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if d.editor.String() != before {
		t.Fatalf("Shift+Tab did not undo the indent: %q", d.editor.String())
	}
	if !d.editor.Undo() {
		t.Fatal("nothing to undo")
	}
	if !d.dirty() {
		// Typing and commenting are still undone separately from the indent.
		t.Fatal("undoing the last indent left no earlier edits")
	}
}

func TestDocumentTabInsertsToTheNextStopAndNeverLeavesTheEditor(t *testing.T) {
	d := newCodeDocument(t, "app.py", "x = 1\n")
	view := openCodeDocumentWindow(t, d, 900, 600, 1)
	driver := view.Driver()
	d.editor.MoveTo(1, false)
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if got := d.editor.String(); got != "x    = 1\n" {
		t.Fatalf("Tab from column 1 gave %q, want three spaces, to the stop at column 4", got)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	focused := view.Snapshot().Focused
	if focused != hitDocumentInput {
		t.Fatalf("Tab moved the keyboard focus to %v", focused)
	}
}

func TestDocumentReadOnlyStatesRefuseCodeEditing(t *testing.T) {
	d := newCodeDocument(t, "main.go", "a\nb\n")
	view := openCodeDocumentWindow(t, d, 900, 600, 1)
	d.busy = true
	for _, key := range []struct {
		key       gfx.Key
		modifiers gfx.Modifiers
	}{{gfx.KeyTab, 0}, {gfx.KeyUp, gfx.ModifierAlt}, {gfx.KeyDown, gfx.ModifierAlt | gfx.ModifierShift}} {
		d.key(gfx.KeyEvent{Key: key.key, Modifiers: key.modifiers, Target: hitDocumentInput})
	}
	d.shortcut(gfx.KeyEvent{Modifiers: documentPrimaryModifier(), Text: "/", Target: hitDocumentInput})
	if d.editor.String() != "a\nb\n" {
		t.Fatalf("a busy document was edited: %q", d.editor.String())
	}
	_ = view
}

func TestDocumentFindSelectsStepsAndReplaces(t *testing.T) {
	d := newCodeDocument(t, "config.yaml", "name: a\nport: 1\nname: b\nsurname: c\n")
	view := openCodeDocumentWindow(t, d, 900, 600, 1)
	driver := view.Driver()

	if handled, err := driver.ShortcutChord(documentPrimaryModifier(), 'f'); err != nil || !handled {
		t.Fatalf("the find chord was not handled: %v", err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !d.edit.findOpen || !documentHasHit(view, hitDocumentFind) {
		t.Fatal("the find bar did not open")
	}
	if view.Snapshot().Focused != hitDocumentFind {
		t.Fatalf("the keyboard is on %v, want the query field", view.Snapshot().Focused)
	}
	if documentHasHit(view, hitDocumentReplace) {
		t.Fatal("find opened the replace row")
	}

	if err := view.Type("name"); err != nil {
		t.Fatal(err)
	}
	if got := len(d.edit.find.Matches()); got != 3 {
		t.Fatalf("%d matches of %q in %q, want 3 (surname counts)", got, "name", d.editor.String())
	}
	selection := d.editor.Selection()
	if selection != (text.Range{Start: 0, End: 4}) {
		t.Fatalf("typing selected %+v, want the first match", selection)
	}

	// Enter goes to the next match, Shift+Enter back.
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if got := d.editor.Selection(); got != (text.Range{Start: 16, End: 20}) {
		t.Fatalf("Enter selected %+v, want the second match", got)
	}
	if err := driver.Key(gfx.KeyEnter, gfx.ModifierShift); err != nil {
		t.Fatal(err)
	}
	if got := d.editor.Selection(); got != (text.Range{Start: 0, End: 4}) {
		t.Fatalf("Shift+Enter selected %+v, want the first match", got)
	}

	// The whole-word option drops "surname" and the bar says so.
	d.click(hitDocumentFindWord)
	if got := len(d.edit.find.Matches()); got != 2 {
		t.Fatalf("whole words: %d matches, want 2", got)
	}

	// Replace one, then all, each a single undo.
	d.click(hitDocumentFindToggle)
	view.refresh(t)
	if !documentHasHit(view, hitDocumentReplace) {
		t.Fatal("the toggle did not show the replace row")
	}
	d.edit.find.Replacement.Insert("title")
	d.editor.SetSelection(text.Range{Start: 0, End: 4})
	d.click(hitDocumentReplaceOne)
	if got := d.editor.String(); got != "title: a\nport: 1\nname: b\nsurname: c\n" {
		t.Fatalf("replace gave %q", got)
	}
	if got := d.editor.Selection(); got != (text.Range{Start: 17, End: 21}) {
		t.Fatalf("after replacing, the selection is %+v, want the next match", got)
	}
	d.click(hitDocumentReplaceAll)
	if got := d.editor.String(); got != "title: a\nport: 1\ntitle: b\nsurname: c\n" {
		t.Fatalf("replace all gave %q", got)
	}
	if !d.editor.Undo() || d.editor.String() != "title: a\nport: 1\nname: b\nsurname: c\n" {
		t.Fatalf("replace all was not one undo: %q", d.editor.String())
	}

	// Escape closes the bar and gives the keyboard back to the text.
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if d.edit.findOpen || view.Snapshot().Focused != hitDocumentInput {
		t.Fatalf("Escape left the bar open (%v) with the keyboard on %v", d.edit.findOpen, view.Snapshot().Focused)
	}
}

func TestDocumentFindSeedsFromTheSelectionAndSearchesRegularExpressions(t *testing.T) {
	d := newCodeDocument(t, "main.go", "alpha beta\nalpha1 beta22\n")
	view := openCodeDocumentWindow(t, d, 900, 600, 1)
	d.editor.SetSelection(text.Range{Start: 0, End: 5})
	if _, err := view.Driver().ShortcutChord(documentPrimaryModifier(), 'f'); err != nil {
		t.Fatal(err)
	}
	if got := d.edit.find.Query.String(); got != "alpha" {
		t.Fatalf("the query is %q, want the selected word", got)
	}
	d.edit.find.Query = *text.NewEditor(`beta\d+`)
	d.click(hitDocumentFindRegexp)
	view.refresh(t)
	if got := d.edit.find.Matches(); len(got) != 1 || got[0] != (text.Span{Start: 18, End: 24}) {
		t.Fatalf("regexp matches = %+v", got)
	}
	d.edit.find.Query = *text.NewEditor(`(`)
	d.edit.find.Update(d.editor.String())
	if d.edit.find.Err() == nil {
		t.Fatal("an invalid expression compiled")
	}
	view.refresh(t)
	invalid := d.owner.messages.CodeEditor.FindInvalid
	found := false
	for _, shown := range view.Texts() {
		found = found || shown == invalid
	}
	if !found {
		t.Fatalf("the bar does not say the expression is invalid (%q); it shows %q", invalid, view.Texts())
	}
}

func TestDocumentGoToLineMovesTheCaret(t *testing.T) {
	d := newCodeDocument(t, "main.go", "one\ntwo\n中文 three\nfour\n")
	view := openCodeDocumentWindow(t, d, 900, 600, 1)
	driver := view.Driver()
	if handled, err := driver.ShortcutChord(gfx.ModifierControl, 'g'); err != nil || !handled {
		t.Fatalf("Control+G was not handled: %v", err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if view.Snapshot().Focused != hitDocumentGoTo {
		t.Fatalf("the keyboard is on %v, want the line field", view.Snapshot().Focused)
	}
	if err := view.Type("9"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if !d.edit.goOpen || !d.edit.goInvalid {
		t.Fatal("a line that does not exist closed the bar or was accepted")
	}
	d.edit.goEditor = *text.NewEditor("3:4")
	d.edit.goInvalid = false
	if err := driver.Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if d.edit.goOpen {
		t.Fatal("the bar stayed open after a jump")
	}
	if position := text.PositionAt(d.editor.String(), d.editor.ByteOffset(d.editor.Caret())); position != (text.Position{Line: 2, Column: 3}) {
		t.Fatalf("the caret is at %+v, want line 3 column 4 (counted from zero: 2, 3)", position)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if view.Snapshot().Focused != hitDocumentInput {
		t.Fatalf("the keyboard is on %v after the jump", view.Snapshot().Focused)
	}
}

func TestParseLineColumn(t *testing.T) {
	cases := []struct {
		in           string
		line, column int
		ok           bool
	}{
		{"12", 12, 1, true}, {" 12 ", 12, 1, true}, {"12:5", 12, 5, true}, {"12,5", 12, 5, true}, {"12:", 12, 1, true},
		{"", 0, 0, false}, {"x", 0, 0, false}, {"12:x", 0, 0, false}, {"12:0", 0, 0, false}, {"-3", -3, 1, true},
	}
	for _, c := range cases {
		line, column, ok := parseLineColumn(c.in)
		if ok != c.ok || ok && (line != c.line || column != c.column) {
			t.Errorf("parseLineColumn(%q) = %d, %d, %v; want %d, %d, %v", c.in, line, column, ok, c.line, c.column, c.ok)
		}
	}
}

func TestDocumentStatusLineShowsPositionIndentAndEnding(t *testing.T) {
	d := newCodeDocument(t, "main.go", "one\ntwo中\n")
	d.owner.setLanguage(i18n.English)
	documentCaretAt(d, 1, 3)
	if got := d.footerStatus(); !strings.HasPrefix(got, "Ln 2, Col 4 · Tabs · LF") {
		t.Fatalf("status = %q", got)
	}
	d.editor.SetSelection(text.Range{Start: 1, End: 6})
	if got := d.positionStatus(); !strings.Contains(got, "(5 selected)") {
		t.Fatalf("selection status = %q", got)
	}
}

func TestDocumentCodeBarsStayInsideTheWindowAtEveryScale(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		for _, scale := range []float64{1, 1.25, 1.5, 2} {
			for _, size := range []gfx.Size{{Width: 520, Height: 320}, {Width: 1000, Height: 700}} {
				d := newCodeDocument(t, "main.go", strings.Repeat("func f() { return }\n", 40))
				d.owner.setLanguage(language)
				view := openCodeDocumentWindow(t, d, int(size.Width), int(size.Height), scale)
				d.openFind(true)
				d.edit.find.Query = *text.NewEditor("func")
				view.refresh(t)
				editor := documentHitBounds(t, view, hitDocumentInput)
				bar := []gfx.ID{hitDocumentFind, hitDocumentReplace, hitDocumentFindPrevious, hitDocumentFindNext, hitDocumentFindClose, hitDocumentFindCase, hitDocumentFindWord, hitDocumentFindRegexp, hitDocumentFindToggle, hitDocumentReplaceOne, hitDocumentReplaceAll}
				seen := map[gfx.ID]gfx.Rect{}
				for _, hit := range view.Snapshot().Hits {
					seen[hit.ID] = hit.Bounds
				}
				for _, id := range bar {
					b, ok := seen[id]
					if !ok {
						t.Errorf("%v %.2f %v: control %v missing", language, scale, size, id)
						continue
					}
					if b.X < editor.X-0.01 || b.Y < editor.Y-0.01 || b.X+b.Width > editor.X+editor.Width+0.01 || b.Y+b.Height > editor.Y+editor.Height+0.01 {
						t.Errorf("%v %.2f %v: %v at %+v leaves the editor %+v", language, scale, size, id, b, editor)
					}
				}
				for _, hit := range view.Snapshot().Hits {
					if hit.Bounds.X+hit.Bounds.Width > size.Width+0.01 || hit.Bounds.Y+hit.Bounds.Height > size.Height+0.01 {
						t.Errorf("%v %.2f %v: %v at %+v leaves the window", language, scale, size, hit.ID, hit.Bounds)
					}
				}
				d.openGoToLine()
				view.refresh(t)
				if !documentHasHit(view, hitDocumentGoTo) || documentHasHit(view, hitDocumentFind) {
					t.Errorf("%v %.2f %v: go to line did not replace the find bar", language, scale, size)
				}
			}
		}
	}
}

func TestDocumentWrapToggleChangesTheLayoutNotTheText(t *testing.T) {
	long := strings.Repeat("word ", 60) + "\n"
	d := newCodeDocument(t, "notes.txt", long)
	view := openCodeDocumentWindow(t, d, 700, 500, 1)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if documentHasHit(view, hitDocumentEditorScrollX) {
		t.Fatal("a wrapping editor shows a horizontal scrollbar")
	}
	d.click(hitDocumentWrap)
	view.refresh(t)
	if !d.edit.noWrap || !documentHasHit(view, hitDocumentEditorScrollX) {
		t.Fatal("without wrapping a long line should scroll sideways")
	}
	if d.editor.String() != long {
		t.Fatal("toggling the wrap changed the text")
	}
}

// With MOTTY_DOCUMENT_SCREENSHOT_DIR set, TestDocumentCodeEditorScreenshots
// writes what the editor looks like in its states, to be looked at; without it
// the test only checks that the windows paint.
func TestDocumentCodeEditorScreenshots(t *testing.T) {
	source := "package main\n\nimport \"fmt\"\n\n// greet says hello to every name, 你好.\nfunc greet(names []string) {\n\tfor i, name := range names {\n\t\tif name == \"\" {\n\t\t\tcontinue\n\t\t}\n\t\tfmt.Printf(\"%d: hello, %s\\n\", i+1, name)\n\t}\n}\n\nfunc main() {\n\tgreet([]string{\"Viva\", \"中文\", \"Motty\"})\n}\n"
	dir := os.Getenv("MOTTY_DOCUMENT_SCREENSHOT_DIR")
	if dir != "" {
		os.MkdirAll(dir, 0o700)
	}
	shoot := func(view codeWindow, name string) {
		t.Helper()
		if dir == "" {
			return
		}
		if err := view.Screenshot(filepath.Join(dir, name+".png")); err != nil && !errors.Is(err, headless.ErrNoRenderer) {
			t.Fatal(err)
		}
	}
	for _, scale := range []float64{1, 2} {
		d := newCodeDocument(t, "main.go", source)
		d.owner.setLanguage(i18n.English)
		view := openCodeDocumentWindow(t, d, 900, 620, scale)
		documentCaretAt(d, 7, 12)
		view.refresh(t)
		shoot(view, fmt.Sprintf("editor-%.0fx", scale))

		d.editor.SetSelection(text.Range{Start: d.editor.RuneIndex(strings.Index(source, "greet(names")), End: d.editor.RuneIndex(strings.Index(source, "greet(names")) + 5})
		d.openFind(true)
		d.edit.find.Query = *text.NewEditor("name")
		d.edit.find.Replacement = *text.NewEditor("label")
		d.queryChanged()
		view.refresh(t)
		shoot(view, fmt.Sprintf("find-%.0fx", scale))

		d.closeFind()
		d.openGoToLine()
		view.refresh(t)
		shoot(view, fmt.Sprintf("goto-%.0fx", scale))

		d.closeFind()
		d.click(hitDocumentWrap)
		documentCaretAt(d, 15, 40)
		view.refresh(t)
		shoot(view, fmt.Sprintf("nowrap-%.0fx", scale))

		// The same file compared with what the server has, on the same grid.
		d.local = false
		d.editor.SetSelection(text.Range{Start: d.editor.RuneIndex(strings.Index(source, "name == ")), End: d.editor.RuneIndex(strings.Index(source, "name == ")) + 4})
		d.editor.Insert("title")
		d.comparison.active = true
		d.comparison.baseline = source
		d.comparison.result = components.DiffCodeLines(source, d.editor.String())
		config := d.comparisonConfig()
		config.Width, config.Height = 1100, 620
		compare, err := headless.Open(config, nil, headless.Options{Scale: scale})
		if err != nil {
			t.Fatal(err)
		}
		if err := compare.Rest(); err != nil {
			t.Fatal(err)
		}
		pane := documentHitBounds(t, codeWindow{Session: compare}, hitDocumentInput)
		if err := compare.Click(pane.X+120, pane.Y+80); err != nil {
			t.Fatal(err)
		}
		shoot(codeWindow{Session: compare}, fmt.Sprintf("compare-%.0fx", scale))
		compare.Close()
	}
}

func TestDocumentControlHOpensReplaceOnlyWhereItIsNotABackspace(t *testing.T) {
	d := newCodeDocument(t, "main.go", "alpha\n")
	view := openCodeDocumentWindow(t, d, 900, 600, 1)
	handled, err := view.Driver().ShortcutChord(gfx.ModifierControl, 'h')
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		if d.edit.findOpen || d.edit.replacing {
			t.Fatal("Control+H, a backspace on macOS, opened the replace bar")
		}
		return
	}
	if !handled || !d.edit.findOpen || !d.edit.replacing {
		t.Fatalf("Control+H did not open the replace form (handled %v, open %v, replacing %v)", handled, d.edit.findOpen, d.edit.replacing)
	}
}

func TestDocumentComparisonPaneEditsLikeTheEditor(t *testing.T) {
	d := newCodeDocument(t, "main.go", "package main\n\nfunc main() {\n\tprintln(1)\n}\n")
	d.local = false
	d.comparison.active = true
	d.comparison.baseline = d.original
	d.comparison.result = components.DiffCodeLines(d.original, d.editor.String())
	config := d.comparisonConfig()
	config.Width, config.Height = 1000, 600
	view, err := headless.Open(config, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	pane := documentHitBounds(t, codeWindow{Session: view}, hitDocumentInput)
	if err := view.Click(pane.X+pane.Width/2, pane.Y+10); err != nil {
		t.Fatal(err)
	}
	driver := view.Driver()
	documentCaretAt(d, 3, 1)
	d.editor.SetSelection(text.Range{Start: d.editor.RuneIndex(strings.Index(d.editor.String(), "func")), End: d.editor.RuneIndex(strings.Index(d.editor.String(), "\n}"))})
	if err := driver.Key(gfx.KeyTab, 0); err != nil {
		t.Fatal(err)
	}
	if got := d.editor.String(); !strings.Contains(got, "\t\tprintln(1)") || !strings.Contains(got, "\tfunc main()") {
		t.Fatalf("Tab in the comparison gave %q", got)
	}
	if handled, err := driver.ShortcutChord(documentPrimaryModifier(), '/'); err != nil || !handled {
		t.Fatalf("the comment chord was not handled in the comparison: %v", err)
	}
	if got := d.editor.String(); !strings.Contains(got, "\t// func main() {\n\t// \tprintln(1)") {
		t.Fatalf("comment in the comparison gave %q", got)
	}
}
