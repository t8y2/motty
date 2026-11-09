package workbench

import (
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/markdown"
	"motty/internal/workbench/icons"
)

var hitNoteCodeCopies = tree.Sub(notesHits, tree.Name("code-copy"))

const noteCodeCopiedFor = 1500 * time.Millisecond

type noteCodeCopy struct {
	token int
	text  string
}
type noteCodeActions struct {
	source   string
	next     int
	targets  []noteCodeCopy
	copied   gfx.ID
	copiedAt time.Duration
}

func (s *State) paintNoteCodeCopies(c *gfx.Canvas, n *notesUI, origin gfx.Point, clip gfx.Rect) {
	v := &n.view
	copies := &v.codeActions
	source := n.body.String()
	blocks := n.rendered.Codes()
	if copies.source != source || len(copies.targets) != len(blocks) {
		copies.source = source
		copies.copied = gfx.ID{}
		copies.targets = copies.targets[:0]
		for _, block := range n.doc.Blocks {
			if block.Kind != markdown.Code {
				continue
			}
			if len(copies.targets) == len(blocks) {
				break
			}
			copies.next++
			copies.targets = append(copies.targets, noteCodeCopy{token: copies.next, text: noteCodeText(source, block)})
		}
	}
	for i, block := range blocks {
		id := hitNoteCodeCopies.At(copies.targets[i].token)
		b := block.Bounds
		b.X += origin.X
		b.Y += origin.Y
		button := gfx.Rect{X: b.X + b.Width - 26, Y: b.Y + 3, Width: 22, Height: 22}
		visible := intersectRect(button, clip)
		if visible.Width < button.Width || visible.Height < button.Height {
			continue
		}
		label := s.noteText("Copy code", "复制代码")
		icon := icons.Copy
		copied := copies.copied == id && s.now()-copies.copiedAt < noteCodeCopiedFor
		if copied {
			icon = icons.Check
			label = s.noteText("Copied", "已复制")
			c.RequestFrameIn(noteCodeCopiedFor - (s.now() - copies.copiedAt))
		}
		hovered := v.pointerValid && pointInRect(v.pointer.X, v.pointer.Y, b) && pointInRect(v.pointer.X, v.pointer.Y, clip)
		if hovered || c.Focused() == id || copied {
			components.BeginButton(c, s.Theme(), id, button).Height(22).WithIcon(icon).IconSmall().Ghost(label)
		} else {
			// Keep keyboard traversal available while the pointer-only affordance is hidden.
			c.HitWithOptions(id, button, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label})
		}
	}
}

func (s *State) clickNoteCodeCopy(id gfx.ID, n *notesUI) bool {
	token, ok := id.In(hitNoteCodeCopies)
	if !ok {
		return false
	}
	v := &n.view.codeActions
	if v.source != n.body.String() {
		return true
	}
	for _, target := range v.targets {
		if target.token != token {
			continue
		}
		v.copied = gfx.ID{}
		if s.options.SetClipboardText == nil {
			n.error = s.noteText("Clipboard unavailable", "剪贴板不可用")
		} else if err := s.options.SetClipboardText(target.text); err != nil {
			v.copied = gfx.ID{}
			n.error = err.Error()
		} else {
			v.copied, v.copiedAt = id, s.now()
		}
		break
	}
	return true
}

// Viva omits the final code line break from Text. Restore that source break
// for clipboard use; keep Viva's quote/indent removal for nested fences.
func noteCodeText(source string, block markdown.Block) string {
	raw := source[block.Start:block.End]
	first := strings.IndexByte(raw, '\n')
	if first < 0 {
		return block.Text
	}
	start := block.Start + first + 1
	end := start + len(block.Text)
	if end <= block.End && source[start:end] == block.Text {
		if strings.HasPrefix(source[end:], "\r\n") {
			return block.Text + "\r\n"
		}
		if strings.HasPrefix(source[end:], "\n") {
			return block.Text + "\n"
		}
		return block.Text
	}
	last := raw[strings.LastIndexByte(raw, '\n')+1:]
	last = strings.TrimLeft(last, " >\t")
	if block.Text != "" && len(last) >= 3 && (last[0] == '`' || last[0] == '~') && strings.Trim(last, string(last[0])+" \t\r") == "" {
		return block.Text + "\n"
	}
	return block.Text
}
