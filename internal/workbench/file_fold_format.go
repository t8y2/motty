package workbench

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/configformat"
	"motty/internal/i18n"
)

var (
	hitDocumentFormat        = tree.ID(documentHits, "format")
	hitDocumentFoldList      = tree.Sub(documentHits, tree.Name("fold"))
	hitDocumentFoldBadgeList = tree.Sub(documentHits, tree.Name("fold-badge"))
)

// documentFolds 将通用折叠状态追踪与缓存委托给 Viva 的 CodeEditorFoldState，
// 自身仅负责根据当前语言调用 configformat 解析区间。
type documentFolds struct {
	kind  configformat.Kind
	state components.CodeEditorFoldState
}

func (f *documentFolds) reset(kind configformat.Kind) {
	f.kind = kind
	f.state.Reset()
}

func (f *documentFolds) sync(value string) {
	if f.kind == configformat.KindNone {
		f.state.Reset()
		return
	}
	f.state.Sync(value, func(src string) []components.CodeEditorFoldRange {
		raw := configformat.FoldRanges(f.kind, src)
		if len(raw) == 0 {
			return nil
		}
		out := make([]components.CodeEditorFoldRange, len(raw))
		for i, r := range raw {
			out[i] = components.CodeEditorFoldRange{Header: r.Header, LastLine: r.LastLine}
		}
		return out
	})
}

func (f *documentFolds) snapshotData(labels i18n.CodeEditorMessages) ([]components.CodeEditorFoldRange, []components.CodeEditorFoldMarker) {
	return f.state.Snapshot(
		labels.FoldCollapse,
		labels.FoldExpand,
		func(line int) gfx.ID { return hitDocumentFoldList.At(line) },
		func(line int) gfx.ID { return hitDocumentFoldBadgeList.At(line) },
	)
}

func (d *fileDocument) canFormatConfig() bool {
	return d.edit.folds.kind != configformat.KindNone
}

func (d *fileDocument) unfoldContainingOffset(offset int) {
	value := d.editor.String()
	d.edit.folds.sync(value)
	pos := text.PositionAt(value, offset)
	d.edit.folds.state.UnfoldContaining(pos.Line)
}

func (d *fileDocument) toggleFold(line int) {
	d.edit.folds.sync(d.editor.String())
	if d.edit.folds.state.Toggle(line) {
		// 若当前光标落入刚折叠的隐藏区间内，将光标收敛到折叠头行末，避免不可见编辑。
		value := d.editor.String()
		caretOffset := d.editor.ByteOffset(d.editor.Caret())
		caretLine := text.PositionAt(value, caretOffset).Line
		if d.edit.folds.state.IsCollapsed(line) {
			if r, ok := d.edit.folds.state.RangeForHeader(line); ok && caretLine > r.Header && caretLine <= r.LastLine {
				headerEnd := text.OffsetAt(value, text.Position{Line: r.Header, Column: 1 << 20})
				d.editor.MoveTo(utf8.RuneCountInString(value[:headerEnd]), false)
			}
		}
		d.invalidateWindows()
	}
}

// formatIndentWidth 返回格式化时应沿用的文档缩进宽度；无明确空格缩进时默认 2 空格。
func (d *fileDocument) formatIndentWidth() int {
	if !d.edit.options.HardTabs && d.edit.options.TabWidth > 0 && hasIndentation(d.editor.String()) {
		return d.edit.options.TabWidth
	}
	return 2
}

// formatDocument 沿用文件自身缩进格式化 YAML/TOML/JSON，支持一步撤销与语法错误定位。
func (d *fileDocument) formatDocument() {
	if !d.canEditCode() || !d.canFormatConfig() {
		return
	}
	value := d.editor.String()
	formatted, err := configformat.Format(d.edit.folds.kind, value, d.formatIndentWidth())
	codeMsgs := d.owner.messages.CodeEditor
	if err != nil {
		line, col := 1, 1
		msg := err.Error()
		var ferr *configformat.FormatError
		if errors.As(err, &ferr) {
			if ferr.Line > 0 {
				line = ferr.Line
			}
			if ferr.Column > 0 {
				col = ferr.Column
			}
			msg = ferr.Message
		}
		targetOffset := text.OffsetAt(value, text.Position{Line: max(0, line-1), Column: max(0, col-1)})
		d.unfoldContainingOffset(targetOffset)
		d.editor.MoveTo(utf8.RuneCountInString(value[:targetOffset]), false)
		d.ui.code.Reveal(targetOffset)
		d.notices.show(d.name, fmt.Sprintf(codeMsgs.FormatErrorFmt, line, col, msg), components.ToastError)
		d.invalidateWindows()
		return
	}

	if formatted == value {
		d.notices.show(d.name, codeMsgs.FormatNoChanges, components.ToastInfo)
		return
	}

	caretOffset := d.editor.ByteOffset(d.editor.Caret())
	caretPos := text.PositionAt(value, caretOffset)

	// 以单次替换事务写入，确保用户可通过 Cmd/Ctrl+Z 一步撤销格式化。
	d.editor.SetSelection(text.Range{Start: 0, End: d.editor.RuneCount()})
	d.editor.Insert(formatted)

	newOffset := text.OffsetAt(formatted, caretPos)
	d.editor.MoveTo(utf8.RuneCountInString(formatted[:newOffset]), false)

	d.edit.folds.state.UnfoldAll()
	d.edit.folds.sync(formatted)
	d.ui.code.Reveal(newOffset)
	d.scheduleDiffIfComparing()
	d.notices.show(d.name, codeMsgs.Formatted, components.ToastSuccess)
	d.invalidateWindows()
}

// foldOrFormatShortcut 处理配置文件格式化（Shift+Alt+F）与折叠（Ctrl/Cmd+Shift+[ / ]）快捷键。
func (d *fileDocument) foldOrFormatShortcut(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Target != hitDocumentInput || !d.ready {
		return gfx.EventResult{}, false
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}

	// Shift+Alt+F：全平台统一格式化快捷键
	if strings.EqualFold(event.Text, "f") && event.Modifiers == gfx.ModifierShift|gfx.ModifierAlt {
		if d.canEditCode() && d.canFormatConfig() {
			d.formatDocument()
		}
		return handled, true
	}

	if !d.canFormatConfig() {
		return gfx.EventResult{}, false
	}

	// 折叠/展开当前光标所在块：macOS Cmd+Shift+[/]，Win/Linux Ctrl+Shift+[/]
	blockMods := gfx.ModifierControl | gfx.ModifierShift
	if runtime.GOOS == "darwin" {
		blockMods = gfx.ModifierMeta | gfx.ModifierShift
	}
	if event.Modifiers == blockMods && (event.Text == "[" || event.Text == "]") {
		value := d.editor.String()
		d.edit.folds.sync(value)
		caretLine := text.PositionAt(value, d.editor.ByteOffset(d.editor.Caret())).Line
		if header, ok := d.edit.folds.state.FoldContainingAt(caretLine); ok {
			collapse := event.Text == "["
			if d.edit.folds.state.IsCollapsed(header) != collapse {
				d.toggleFold(header)
			}
		}
		return handled, true
	}

	return gfx.EventResult{}, false
}
