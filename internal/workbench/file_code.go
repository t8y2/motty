package workbench

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/text/find"
	"motty/internal/i18n"
)

// The code editor of the file window. The reusable parts, the grid, the line
// numbers, the editing operations and the search, are Viva's (components.CodeEditor,
// text.Editor's code operations, text/find, components.PaintFindBar); what is
// here is what only this application knows: which languages comment with what,
// how a remote file is searched and replaced inside, and which keys do what in
// a window that also saves over SFTP.

var (
	hitDocumentFind          = tree.ID(documentHits, "find")
	hitDocumentReplace       = tree.ID(documentHits, "replace")
	hitDocumentFindPrevious  = tree.ID(documentHits, "find-previous")
	hitDocumentFindNext      = tree.ID(documentHits, "find-next")
	hitDocumentFindClose     = tree.ID(documentHits, "find-close")
	hitDocumentFindCase      = tree.ID(documentHits, "find-case")
	hitDocumentFindWord      = tree.ID(documentHits, "find-word")
	hitDocumentFindRegexp    = tree.ID(documentHits, "find-regexp")
	hitDocumentFindToggle    = tree.ID(documentHits, "find-toggle")
	hitDocumentReplaceOne    = tree.ID(documentHits, "replace-one")
	hitDocumentReplaceAll    = tree.ID(documentHits, "replace-all")
	hitDocumentGoTo          = tree.ID(documentHits, "go-to-line")
	hitDocumentWrap          = tree.ID(documentHits, "wrap")
	hitDocumentEditorScrollX = tree.ID(documentHits, "editor-scroll-x")
)

// codeBarLayer puts the find and go to line bars above the text and below the
// menus and dialogs of the window.
const codeBarLayer = uint16(100)

// documentEditing is what the window keeps of editing code beyond the editor
// itself.
type documentEditing struct {
	options text.CodeOptions
	noWrap  bool

	find      find.Session
	findOpen  bool
	replacing bool
	// notice is what the last replacement said, shown in place of the count until
	// the query or the text changes.
	notice, noticeText string

	goOpen    bool
	goEditor  text.Editor
	goInvalid bool

	// focus is the bar field to hand the keyboard to at the next paint, which is
	// when its hit first exists.
	focus gfx.ID

	findInput, replaceInput, goInput app.TextInput
}

// codeLanguageRules is what the editor needs of a language: how it comments a
// line, and whether an opening bracket or a colon starts an indented block.
type codeLanguageRules struct {
	comment     string
	indentAfter string
	// code turns on the pairing of brackets and quotes.
	code bool
	// hardTabs is the indentation of a file whose own style cannot be read.
	hardTabs bool
}

func codeRulesFor(language string) codeLanguageRules {
	switch language {
	case "go":
		return codeLanguageRules{comment: "//", indentAfter: "{[(", code: true, hardTabs: true}
	case "js", "jsx", "mjs", "cjs", "ts", "tsx", "java", "c", "h", "cc", "cpp", "cxx", "hpp", "cs", "rs", "swift", "kt", "kts", "scala", "php", "dart", "groovy", "gradle", "proto", "zig", "v":
		return codeLanguageRules{comment: "//", indentAfter: "{[(", code: true}
	case "json", "jsonc", "json5":
		return codeLanguageRules{indentAfter: "{[", code: true}
	case "css", "scss", "less":
		return codeLanguageRules{indentAfter: "{", code: true}
	case "py", "pyw":
		return codeLanguageRules{comment: "#", indentAfter: "{[(:", code: true}
	case "yaml", "yml":
		return codeLanguageRules{comment: "#", indentAfter: ":", code: true}
	case "rb", "pl", "pm", "r", "tf", "hcl", "nix", "toml", "cr", "ex", "exs":
		return codeLanguageRules{comment: "#", indentAfter: "{[(", code: true}
	case "sh", "bash", "zsh", "fish", "ksh":
		return codeLanguageRules{comment: "#", indentAfter: "{([", code: true}
	case "mk", "make", "makefile":
		return codeLanguageRules{comment: "#", indentAfter: ":", code: true, hardTabs: true}
	case "ini", "conf", "config", "cfg", "env", "properties", "service", "socket", "timer", "toml-lock", "gitconfig":
		return codeLanguageRules{comment: "#", code: true}
	case "sql":
		return codeLanguageRules{comment: "--", indentAfter: "(", code: true}
	case "lua", "hs":
		return codeLanguageRules{comment: "--", indentAfter: "{[(", code: true}
	case "xml", "html", "htm", "svg", "vue":
		return codeLanguageRules{code: true}
	}
	return codeLanguageRules{}
}

// editingLanguage is the language whose editing rules a file follows: its
// highlighting language, or for the files that are known by their names, not
// their extensions, the language they are written in.
func editingLanguage(name string) string {
	switch strings.ToLower(filepath.Base(name)) {
	case "makefile", "gnumakefile":
		return "mk"
	case "dockerfile", "containerfile":
		return "sh"
	case ".gitconfig", ".editorconfig":
		return "ini"
	case ".bashrc", ".zshrc", ".profile", ".bash_profile", ".bash_aliases", ".zprofile":
		return "sh"
	}
	return documentLanguage(name)
}

// codeOptionsFor is how the file called name, whose text is value, is edited:
// the language's rules, and the file's own indentation when it shows one.
func codeOptionsFor(name, value string) text.CodeOptions {
	rules := codeRulesFor(editingLanguage(name))
	options := text.CodeOptions{
		TabWidth: 4, AutoIndent: true, AutoClose: rules.code,
		IndentAfter: rules.indentAfter, LineComment: rules.comment, HardTabs: rules.hardTabs,
	}
	if hard, width := text.DetectIndent(value); strings.TrimSpace(value) != "" && hasIndentation(value) {
		options.HardTabs, options.TabWidth = hard, width
	}
	return options
}

// hasIndentation reports whether any of the first lines of value is indented,
// which is when DetectIndent has something to go by.
func hasIndentation(value string) bool {
	for lines := 0; value != "" && lines < 2000; lines++ {
		if value[0] == ' ' || value[0] == '\t' {
			if strings.TrimLeft(value[:lineEnd(value)], " \t") != "" {
				return true
			}
		}
		next := lineEnd(value)
		if next >= len(value) {
			break
		}
		value = value[next+1:]
	}
	return false
}

func lineEnd(value string) int {
	if end := strings.IndexByte(value, '\n'); end >= 0 {
		return end
	}
	return len(value)
}

// loadEditor makes the editor of a document from its text: the caret at the
// start, no undo history to step back into an empty file, and the language's
// editing rules.
func (d *fileDocument) loadEditor(value string) {
	d.editor = *text.NewEditor(value)
	d.editor.SetSelection(text.Range{})
	d.edit.options = codeOptionsFor(d.name, value)
	d.editor.SetCode(d.edit.options)
	d.ui.code = components.CodeEditor{}
	d.edit.notice = ""
}

// canEditCode reports whether the keyboard may change the text of the document.
func (d *fileDocument) canEditCode() bool {
	return d.image == nil && d.ready && !d.busy && d.confirm == "" && !d.comparison.active && !d.ui.saveMenuOpen && !d.encodingOpen
}

// codeBarsAvailable reports whether the find and go to line bars may be open.
func (d *fileDocument) codeBarsAvailable() bool {
	return d.image == nil && d.ready && d.confirm == "" && !d.comparison.active
}

// paintCodeEditor paints the editor in bounds and, over its top right corner, the
// bar that is open.
func (d *fileDocument) paintCodeEditor(canvas *gfx.Canvas, theme gfx.Theme, bounds gfx.Rect) {
	m, code := d.owner.messages.FileWindow, d.owner.messages.CodeEditor
	value := d.editor.String()
	selection := d.editor.Selection()
	start, end, caret := d.editor.ByteOffset(selection.Start), d.editor.ByteOffset(selection.End), d.editor.ByteOffset(d.editor.Caret())
	focused := canvas.Focused() == hitDocumentInput
	d.ui.caret.Rest()
	snapshot := components.CodeEditorSnapshot{
		ID: hitDocumentInput, Label: m.Title, Value: value, Placeholder: m.ChooseEncoding,
		ReadOnly: d.busy || !d.ready, Focused: focused, ShowCaret: focused && selection.Start == selection.End,
		SelectionStart: start, SelectionEnd: end, Caret: caret,
		CaretColor: d.owner.pal.cursor,
		FontSize:   d.owner.terminalFontSize, MonoFace: d.owner.terminalFontFace, TabWidth: d.edit.options.TabWidth,
		Wrap: !d.edit.noWrap, LineNumbers: true, CurrentLine: true, MatchBrackets: true,
		Language: documentLanguage(d.name), Highlight: d.ui.highlighter,
		ScrollbarID: hitDocumentScroll, HorizontalScrollbarID: hitDocumentEditorScrollX,
	}
	if marked, ok := d.editor.Composition(); ok {
		snapshot.CompositionStart, snapshot.CompositionEnd = d.editor.ByteOffset(marked.Start), d.editor.ByteOffset(marked.End)
	}
	bar := d.codeBarsAvailable() && d.edit.findOpen
	if bar {
		d.edit.find.Update(value)
		snapshot.Marks = d.edit.find.Matches()
		if index := find.Index(snapshot.Marks, start, end); index >= 0 {
			snapshot.CurrentMark = snapshot.Marks[index]
		}
	}
	d.ui.code.Paint(canvas, theme, bounds, snapshot)

	area := gfx.Rect{X: bounds.X + 8, Y: bounds.Y + 6, Width: bounds.Width - 16 - 14}
	switch {
	case bar:
		d.paintFindBar(canvas, theme, area, code)
	case d.codeBarsAvailable() && d.edit.goOpen:
		d.paintGoToLine(canvas, theme, area, code)
	}
}

// editorField is the snapshot of a one-line field of a bar, backed by editor.
func editorField(canvas *gfx.Canvas, id gfx.ID, label, placeholder string, editor *text.Editor, readOnly bool) components.InputSnapshot {
	value := editor.String()
	focused := canvas.Focused() == id
	selection := editor.Selection()
	snapshot := components.InputSnapshot{
		ID: id, Label: label, Placeholder: placeholder, Value: value, ReadOnly: readOnly,
		Focused: focused, PlatformEditing: focused && value != "", NavigationKeys: false,
		ShowCaret: focused && selection.Start == selection.End,
		Caret:     editor.ByteOffset(editor.Caret()), SelectionStart: editor.ByteOffset(selection.Start), SelectionEnd: editor.ByteOffset(selection.End),
	}
	if marked, ok := editor.Composition(); ok {
		snapshot.CompositionStart, snapshot.CompositionEnd = editor.ByteOffset(marked.Start), editor.ByteOffset(marked.End)
	}
	return snapshot
}

func (d *fileDocument) paintFindBar(canvas *gfx.Canvas, theme gfx.Theme, area gfx.Rect, code i18n.CodeEditorMessages) {
	session, edit := &d.edit.find, &d.edit
	readOnly := !d.canEditCode()
	current, total := session.Position(&d.editor)
	status, none := "", false
	switch {
	case session.Err() != nil:
		status, none = code.FindInvalid, true
	case session.Empty():
	case total == 0:
		status, none = code.FindNone, true
	case edit.notice != "" && edit.noticeText == d.editor.String():
		status = edit.notice
	case session.Truncated():
		status = fmt.Sprintf(code.FindMoreFmt, total)
	case current == 0:
		status = fmt.Sprintf(code.FindTotalFmt, total)
	default:
		status = fmt.Sprintf(code.FindCountFmt, current, total)
	}
	snapshot := components.FindBarSnapshot{
		Find:        editorField(canvas, hitDocumentFind, code.FindLabel, code.FindPlaceholder, &session.Query, false),
		Replacement: editorField(canvas, hitDocumentReplace, code.ReplaceLabel, code.ReplacePlaceholder, &session.Replacement, readOnly),
		Replacing:   edit.replacing,
		PreviousID:  hitDocumentFindPrevious, NextID: hitDocumentFindNext, CloseID: hitDocumentFindClose,
		MatchCaseID: hitDocumentFindCase, WholeWordID: hitDocumentFindWord, RegexpID: hitDocumentFindRegexp,
		ToggleReplaceID: hitDocumentFindToggle, ReplaceOneID: hitDocumentReplaceOne, ReplaceAllID: hitDocumentReplaceAll,
		MatchCase: session.Options.CaseSensitive, WholeWord: session.Options.WholeWord, Regexp: session.Options.Regexp,
		Status: status, NoResults: none, Disabled: total == 0 || session.Err() != nil,
		Labels: components.FindBarLabels{
			Previous: code.FindPrevious, Next: code.FindNext, Close: code.FindClose,
			MatchCase: code.FindMatchCase, WholeWord: code.FindWholeWord, Regexp: code.FindRegexp,
			ToggleReplace: code.FindToggleReplace, ReplaceOne: code.FindReplaceOne, ReplaceAll: code.FindReplaceAll,
		},
		ReplaceOneText: code.FindReplaceOne, ReplaceAllText: code.FindReplaceAll,
	}
	if readOnly {
		// A replacement changes the text, which a document that is loading or
		// saving cannot take.
		snapshot.ReplaceOneID, snapshot.ReplaceAllID = gfx.ID{}, gfx.ID{}
	}
	area.Width = min(max(area.Width, 0), 480)
	area.X = area.X + max(0, d.ui.editorBounds.Width-16-14-area.Width)
	canvas.PushLayer(codeBarLayer)
	components.PaintFindBar(canvas, theme, area, snapshot)
	canvas.PopLayer()
	d.takeBarFocus(canvas)
}

func (d *fileDocument) paintGoToLine(canvas *gfx.Canvas, theme gfx.Theme, area gfx.Rect, code i18n.CodeEditorMessages) {
	lines := text.LineCount(d.editor.String())
	area.Width = min(max(area.Width, 0), 220)
	area.X = area.X + max(0, d.ui.editorBounds.Width-16-14-area.Width)
	canvas.PushLayer(codeBarLayer)
	components.PaintGoToLine(canvas, theme, area, components.GoToLineSnapshot{
		Input:   editorField(canvas, hitDocumentGoTo, code.GoToLineLabel, fmt.Sprintf(code.GoToLinePlaceholderFmt, lines), &d.edit.goEditor, false),
		Invalid: d.edit.goInvalid,
	})
	canvas.PopLayer()
	d.takeBarFocus(canvas)
}

// takeBarFocus hands the keyboard to the field a bar was opened for, once its hit
// exists.
func (d *fileDocument) takeBarFocus(canvas *gfx.Canvas) {
	if !d.edit.focus.IsZero() {
		canvas.RequestFocus(d.edit.focus)
		d.edit.focus = gfx.ID{}
	}
}

// openFind opens the find bar, or its replace form, on the selection when it is
// a word or a short line, and puts the keyboard in the query field.
func (d *fileDocument) openFind(replacing bool) {
	if !d.codeBarsAvailable() {
		return
	}
	edit := &d.edit
	edit.goOpen = false
	edit.findOpen = true
	edit.replacing = edit.replacing && replacing || replacing
	if selection := d.editor.Selection(); selection.Start != selection.End && selection.End-selection.Start <= 256 {
		seed := d.editor.String()[d.editor.ByteOffset(selection.Start):d.editor.ByteOffset(selection.End)]
		if !strings.ContainsAny(seed, "\r\n") {
			edit.find.Query = *text.NewEditor(seed)
		}
	}
	edit.find.Query.SetSelection(text.Range{End: edit.find.Query.RuneCount()})
	edit.focus = hitDocumentFind
	edit.notice = ""
	edit.find.Update(d.editor.String())
}

func (d *fileDocument) closeFind() gfx.EventResult {
	d.edit.findOpen, d.edit.goOpen = false, false
	d.edit.notice = ""
	return gfx.EventResult{Handled: true, Repaint: true, Focus: hitDocumentInput}
}

func (d *fileDocument) openGoToLine() {
	if !d.codeBarsAvailable() {
		return
	}
	d.edit.findOpen = false
	d.edit.goOpen = true
	d.edit.goInvalid = false
	d.edit.goEditor = text.Editor{}
	d.edit.focus = hitDocumentGoTo
}

// goToLine moves the caret to the line, or line and column, the field holds, as
// "12" or "12:5", and closes the bar. A number that is no line leaves it open and
// marks the field.
func (d *fileDocument) goToLine() gfx.EventResult {
	line, column, ok := parseLineColumn(d.edit.goEditor.String())
	value := d.editor.String()
	if !ok || line < 1 || line > text.LineCount(value) {
		d.edit.goInvalid = true
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	offset := text.OffsetAt(value, text.Position{Line: line - 1, Column: max(column-1, 0)})
	d.editor.MoveTo(d.editor.RuneIndex(offset), false)
	d.ui.code.Reveal(offset)
	d.edit.goOpen = false
	return gfx.EventResult{Handled: true, Repaint: true, Focus: hitDocumentInput}
}

// parseLineColumn reads "12" or "12:5" (or "12,5"), with spaces around either.
// A line without a column is column 1.
func parseLineColumn(value string) (line, column int, ok bool) {
	linePart, columnPart, found := strings.Cut(value, ":")
	if !found {
		linePart, columnPart, _ = strings.Cut(value, ",")
	}
	line, err := strconv.Atoi(strings.TrimSpace(linePart))
	if err != nil {
		return 0, 0, false
	}
	if strings.TrimSpace(columnPart) == "" {
		return line, 1, true
	}
	column, err = strconv.Atoi(strings.TrimSpace(columnPart))
	if err != nil || column < 1 {
		return 0, 0, false
	}
	return line, column, true
}

// stepFind selects the next match, or the previous, and brings it into view.
func (d *fileDocument) stepFind(forward bool) gfx.EventResult {
	if !d.edit.findOpen {
		d.openFind(false)
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	d.edit.find.Update(d.editor.String())
	if d.edit.find.Step(&d.editor, forward) {
		d.ui.code.Reveal(d.editor.ByteOffset(d.editor.Caret()))
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}

// queryChanged searches again after the query was edited and selects the first
// match from where the editor was.
func (d *fileDocument) queryChanged() {
	d.edit.notice = ""
	d.edit.find.Update(d.editor.String())
	if d.edit.find.Seek(&d.editor) {
		d.ui.code.Reveal(d.editor.ByteOffset(d.editor.Caret()))
	}
}

func (d *fileDocument) replaceCurrent() {
	if !d.canEditCode() {
		return
	}
	d.edit.find.Update(d.editor.String())
	if d.edit.find.ReplaceCurrent(&d.editor) {
		d.ui.code.Reveal(d.editor.ByteOffset(d.editor.Caret()))
	}
	d.afterReplace()
}

func (d *fileDocument) replaceAll() {
	if !d.canEditCode() {
		return
	}
	d.edit.find.Update(d.editor.String())
	count := d.edit.find.ReplaceAll(&d.editor)
	if count > 0 {
		d.edit.notice = fmt.Sprintf(d.owner.messages.CodeEditor.FindReplacedFmt, count)
		d.edit.noticeText = d.editor.String()
		d.ui.code.Reveal(d.editor.ByteOffset(d.editor.Caret()))
	}
	d.afterReplace()
}

// afterReplace is what follows a change of the text made from the bar: the
// comparison, when it is open, hears of it as of any edit.
func (d *fileDocument) afterReplace() {
	d.invalidateWindows()
}

// findOption flips one option of the search and searches again.
func (d *fileDocument) findOption(id gfx.ID) {
	options := &d.edit.find.Options
	switch id {
	case hitDocumentFindCase:
		options.CaseSensitive = !options.CaseSensitive
	case hitDocumentFindWord:
		options.WholeWord = !options.WholeWord
	case hitDocumentFindRegexp:
		options.Regexp = !options.Regexp
	}
	d.edit.notice = ""
	d.edit.find.Update(d.editor.String())
}

// clickCode handles the clicks on the controls this file paints.
func (d *fileDocument) clickCode(id gfx.ID) bool {
	switch id {
	case hitDocumentFindPrevious:
		d.stepFind(false)
	case hitDocumentFindNext:
		d.stepFind(true)
	case hitDocumentFindClose:
		d.closeFind()
	case hitDocumentFindCase, hitDocumentFindWord, hitDocumentFindRegexp:
		d.findOption(id)
	case hitDocumentFindToggle:
		d.edit.replacing = !d.edit.replacing
	case hitDocumentReplaceOne:
		d.replaceCurrent()
	case hitDocumentReplaceAll:
		d.replaceAll()
	case hitDocumentWrap:
		d.edit.noWrap = !d.edit.noWrap
	default:
		return false
	}
	return true
}

// barInput connects a field of a bar to its editor. changed is called after an
// event that changed the text. A single line has no line break: a paste folds.
func barInput(input *app.TextInput, editor *text.Editor, changed func()) *app.TextInput {
	input.Editor = nil
	input.State = app.TextStateOf(editor)
	input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
		before := editor.String()
		if event.Kind == app.TextInsert || event.Kind == app.TextPaste {
			event.Text = text.SingleLine(event.Text)
		}
		event.Apply(editor)
		if changed != nil && editor.String() != before {
			changed()
		}
		return app.TextStateOf(editor), nil
	}
	return input
}

// barTextInput is the text input of the bar field the hit names, or nil.
func (d *fileDocument) barTextInput(id gfx.ID) *app.TextInput {
	if !d.codeBarsAvailable() {
		return nil
	}
	edit := &d.edit
	switch {
	case id == hitDocumentFind && edit.findOpen:
		return barInput(&edit.findInput, &edit.find.Query, d.queryChanged)
	case id == hitDocumentReplace && edit.findOpen && edit.replacing && d.canEditCode():
		return barInput(&edit.replaceInput, &edit.find.Replacement, nil)
	case id == hitDocumentGoTo && edit.goOpen:
		return barInput(&edit.goInput, &edit.goEditor, func() { edit.goInvalid = false })
	}
	return nil
}

// codeKey is what the keys of the code editor and of its bars do. It reports
// false for a key that is none of its business.
func (d *fileDocument) codeKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	handled := gfx.EventResult{Handled: true, Repaint: true}
	target := event.Target
	inFind := target == hitDocumentFind || target == hitDocumentReplace
	switch {
	case target == hitDocumentGoTo:
		switch event.Key {
		case gfx.KeyEnter:
			return d.goToLine(), true
		case gfx.KeyEscape:
			d.edit.goOpen = false
			return gfx.EventResult{Handled: true, Repaint: true, Focus: hitDocumentInput}, true
		}
		return gfx.EventResult{}, false
	case inFind || target == hitDocumentInput:
		if command, ok := components.FindBarCommandForKey(event, inFind); ok {
			switch command {
			case components.FindBarOpen:
				d.openFind(false)
			case components.FindBarOpenReplace:
				d.openFind(true)
			case components.FindBarNext, components.FindBarPrevious:
				if target == hitDocumentReplace && command == components.FindBarNext {
					d.replaceCurrent()
					return handled, true
				}
				return d.stepFind(command == components.FindBarNext), true
			case components.FindBarCloseCommand:
				return d.closeFind(), true
			}
			return handled, true
		}
	}
	if target != hitDocumentInput || !d.ready {
		return gfx.EventResult{}, false
	}
	if command, ok := components.CodeEditorCommandForKey(event); ok && d.canEditCode() {
		if command.Apply(&d.editor) {
			d.ui.code.Reveal(d.editor.ByteOffset(d.editor.Caret()))
			d.scheduleDiffIfComparing()
			return handled, true
		}
	}
	if movement, extend := components.TextareaMovementForKey(event.Key, event.Modifiers); movement != components.TextareaMoveNone {
		value := d.editor.String()
		offset := d.ui.code.Move(value, d.editor.ByteOffset(d.editor.Caret()), movement)
		d.editor.MoveTo(utf8.RuneCountInString(value[:offset]), extend)
		return handled, true
	}
	return gfx.EventResult{}, false
}

func (d *fileDocument) scheduleDiffIfComparing() {
	if d.comparison.active {
		d.scheduleDiff()
	}
}

// codeShortcut handles the chords that open the bars. They reach the window
// before the text system does.
func (d *fileDocument) codeShortcut(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if !d.codeBarsAvailable() {
		return gfx.EventResult{}, false
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	inFind := d.edit.findOpen && (event.Target == hitDocumentFind || event.Target == hitDocumentReplace)
	if command, ok := components.FindBarCommandForKey(event, inFind); ok {
		switch command {
		case components.FindBarOpen:
			d.openFind(false)
			return handled, true
		case components.FindBarOpenReplace:
			d.openFind(true)
			return handled, true
		}
	}
	// Control+H opens the replace form on Windows and Linux; on macOS it is a
	// backspace and stays one.
	if runtime.GOOS != "darwin" && strings.EqualFold(event.Text, "h") && event.Modifiers == gfx.ModifierControl {
		d.openFind(true)
		return handled, true
	}
	if strings.EqualFold(event.Text, "g") && event.Modifiers&gfx.ModifierControl != 0 {
		d.openGoToLine()
		return handled, true
	}
	// Command or Control+/, [ and ] edit the text; they are chords, so they
	// arrive here before they would arrive as keys.
	if event.Target == hitDocumentInput && d.canEditCode() {
		if command, ok := components.CodeEditorCommandForKey(event); ok && command.Apply(&d.editor) {
			d.ui.code.Reveal(d.editor.ByteOffset(d.editor.Caret()))
			d.scheduleDiffIfComparing()
			return handled, true
		}
	}
	return gfx.EventResult{}, false
}

// positionStatus is the caret's line and column, and the size of the selection,
// for the status line.
func (d *fileDocument) positionStatus() string {
	code := d.owner.messages.CodeEditor
	value := d.editor.String()
	selection := d.editor.Selection()
	position := text.PositionAt(value, d.editor.ByteOffset(d.editor.Caret()))
	status := fmt.Sprintf(code.PositionFmt, position.Line+1, position.Column+1)
	if selection.End > selection.Start {
		status += " (" + fmt.Sprintf(code.SelectedFmt, selection.End-selection.Start) + ")"
	}
	if d.edit.options.HardTabs {
		return status + " · " + code.IndentTabs
	}
	return status + " · " + fmt.Sprintf(code.IndentSpaces, d.edit.options.TabWidth)
}
