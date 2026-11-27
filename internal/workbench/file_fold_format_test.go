//go:build darwin || windows || linux

package workbench

import (
	"runtime"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestDocumentFormatButtonVisibilityAndUndo(t *testing.T) {
	// 普通文本文件不应显示格式化按钮
	txtDoc := newCodeDocument(t, "notes.txt", "hello world\n")
	txtView := openCodeDocumentWindow(t, txtDoc, 720, 480, 1)
	if err := txtView.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := txtView.ClickLabel(txtDoc.owner.messages.CodeEditor.Format); err == nil {
		t.Fatal("format button should not appear for .txt files")
	}

	// 无缩进的单行 JSON 文件应使用默认 2 空格格式化，不永久改写 TabWidth，且可一步撤销
	rawJSON := `{"b":1,"a":{"c":2}}`
	doc := newCodeDocument(t, "config.json", rawJSON)
	origTabWidth := doc.edit.options.TabWidth
	view := openCodeDocumentWindow(t, doc, 720, 480, 1)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}

	if err := view.ClickLabel(doc.owner.messages.CodeEditor.Format); err != nil {
		t.Fatalf("format button missing or not clickable for .json document: %v", err)
	}
	view.refresh(t)
	want := "{\n  \"b\": 1,\n  \"a\": {\n    \"c\": 2\n  }\n}\n"
	if got := doc.editor.String(); got != want {
		t.Fatalf("formatted JSON = %q, want %q", got, want)
	}
	if doc.edit.options.TabWidth != origTabWidth {
		t.Fatalf("TabWidth mutated from %d to %d", origTabWidth, doc.edit.options.TabWidth)
	}

	if !doc.editor.Undo() {
		t.Fatal("expected single-step Undo after format")
	}
	if got := doc.editor.String(); got != rawJSON {
		t.Fatalf("after Undo = %q, want original %q", got, rawJSON)
	}
}

func TestDocumentFormatPreservesExistingFourSpaceIndent(t *testing.T) {
	src := "services:\n    web:\n        image: nginx\n"
	doc := newCodeDocument(t, "compose.yaml", src)
	if doc.edit.options.TabWidth != 4 {
		t.Fatalf("expected detected TabWidth=4, got %d", doc.edit.options.TabWidth)
	}
	doc.formatDocument()
	if got := doc.editor.String(); got != src {
		t.Fatalf("4-space YAML should keep 4-space indent, got:\n%q", got)
	}
}

func TestDocumentFormatSyntaxErrorUnfoldsAndMovesCaret(t *testing.T) {
	badYAML := "server:\n  host: localhost\n  bad: [\n"
	doc := newCodeDocument(t, "app.yaml", badYAML)
	doc.toggleFold(0)
	if !doc.edit.folds.state.IsCollapsed(0) {
		t.Fatal("expected line 0 to be collapsed")
	}

	doc.formatDocument()
	if doc.editor.String() != badYAML {
		t.Fatal("syntax error should not modify document text")
	}
	if doc.edit.folds.state.IsCollapsed(0) {
		t.Fatal("syntax error at folded line should unfold enclosing block")
	}
	caretLine := text.PositionAt(doc.editor.String(), doc.editor.ByteOffset(doc.editor.Caret())).Line
	if caretLine < 2 {
		t.Fatalf("caretLine = %d, expected jump to error line >= 2", caretLine)
	}
}

func TestDocumentFoldMarkersBadgeAndSearchUnfold(t *testing.T) {
	tomlSrc := strings.Join([]string{
		"[server]",
		"host = \"127.0.0.1\"",
		"[server.tls]",
		"cert = \"/etc/ssl/cert.pem\"",
		"[logging]",
		"level = \"info\"",
	}, "\n")
	doc := newCodeDocument(t, "config.toml", tomlSrc)
	var canvas gfx.Canvas
	doc.paint(&canvas, gfx.Size{Width: 720, Height: 480})

	// 点击第 0 行左侧折叠箭头，应折叠 [server] 及其子表 [server.tls]
	doc.click(hitDocumentFoldList.At(0))
	if !doc.edit.folds.state.IsCollapsed(0) {
		t.Fatal("clicking fold gutter did not collapse [server]")
	}

	// 重绘后应出现 "..." 徽标的 PlaceholderID 命中区域，点击它应展开
	canvas.Reset()
	doc.paint(&canvas, gfx.Size{Width: 720, Height: 480})
	badgeHitFound := false
	for _, hit := range canvas.Hits() {
		if hit.ID == hitDocumentFoldBadgeList.At(0) {
			badgeHitFound = true
			break
		}
	}
	if !badgeHitFound {
		t.Fatal("expected fold ellipsis badge hit for collapsed header")
	}
	doc.click(hitDocumentFoldBadgeList.At(0))
	if doc.edit.folds.state.IsCollapsed(0) {
		t.Fatal("clicking ellipsis badge did not expand [server]")
	}

	// 再次折叠 [server]，然后搜索内部关键字 "cert"，应自动展开
	doc.click(hitDocumentFoldList.At(0))
	doc.openFind(false)
	doc.edit.find.Query = *text.NewEditor("cert")
	doc.queryChanged()
	if doc.edit.folds.state.IsCollapsed(0) {
		t.Fatal("finding text inside collapsed fold should auto-expand it")
	}

	// 再次折叠后，Go to Line 到第 4 行（1-based line 4）应自动展开
	doc.click(hitDocumentFoldList.At(0))
	doc.openGoToLine()
	doc.edit.goEditor = *text.NewEditor("4")
	doc.goToLine()
	if doc.edit.folds.state.IsCollapsed(0) {
		t.Fatal("GoToLine inside collapsed fold should auto-expand it")
	}
}

func TestDocumentFoldFollowsEditsAboveCollapsedBlock(t *testing.T) {
	yamlSrc := "top: 1\nserver:\n  host: localhost\n  port: 8080\n"
	doc := newCodeDocument(t, "config.yaml", yamlSrc)
	doc.toggleFold(1)
	if !doc.edit.folds.state.IsCollapsed(1) {
		t.Fatal("expected line 1 to collapse")
	}

	// 在折叠块上方插入两行，折叠状态应跟随平移到第 3 行
	doc.editor.SetSelection(text.Range{Start: 0, End: 0})
	doc.editor.Insert("first: 0\nsecond: 0\n")
	doc.edit.folds.sync(doc.editor.String())
	if doc.edit.folds.state.IsCollapsed(1) {
		t.Fatal("stale line 1 remained collapsed after editing above")
	}
	if !doc.edit.folds.state.IsCollapsed(3) {
		t.Fatal("expected fold to follow server: to line 3")
	}
}

func TestDocumentFormatAndFoldShortcuts(t *testing.T) {
	doc := newCodeDocument(t, "data.json", "{\n  \"a\": {\n    \"b\": 1\n  }\n}\n")
	blockMods := gfx.ModifierControl | gfx.ModifierShift
	if runtime.GOOS == "darwin" {
		blockMods = gfx.ModifierMeta | gfx.ModifierShift
	}

	// 光标位于第 1 行（"a": {），按 Cmd/Ctrl+Shift+[ 折叠
	doc.editor.MoveTo(doc.editor.RuneIndex(text.OffsetAt(doc.editor.String(), text.Position{Line: 1, Column: 2})), false)
	if res, ok := doc.codeShortcut(gfx.KeyEvent{Target: hitDocumentInput, Text: "[", Modifiers: blockMods}); !ok || !res.Handled {
		t.Fatal("fold block shortcut was not handled")
	}
	if !doc.edit.folds.state.IsCollapsed(1) {
		t.Fatal("expected line 1 to collapse via shortcut")
	}

	// 按 Cmd/Ctrl+Shift+] 展开
	if res, ok := doc.codeShortcut(gfx.KeyEvent{Target: hitDocumentInput, Text: "]", Modifiers: blockMods}); !ok || !res.Handled {
		t.Fatal("unfold block shortcut was not handled")
	}
	if doc.edit.folds.state.IsCollapsed(1) {
		t.Fatal("expected line 1 to expand via shortcut")
	}

	// Shift+Alt+F 触发格式化
	compactDoc := newCodeDocument(t, "data.json", `{"a":1}`)
	res, ok := compactDoc.codeShortcut(gfx.KeyEvent{
		Target:    hitDocumentInput,
		Text:      "f",
		Modifiers: gfx.ModifierShift | gfx.ModifierAlt,
	})
	if !ok || !res.Handled {
		t.Fatal("Shift+Alt+F shortcut was not handled")
	}
	if got := compactDoc.editor.String(); got != "{\n  \"a\": 1\n}\n" {
		t.Fatalf("formatted by shortcut = %q", got)
	}
}
