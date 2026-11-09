package workbench

import (
	"regexp"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// Match whole inline constructs first so their contents are not reinterpreted
// as Markdown punctuation. Offsets remain bytes, as required by CodeEditor.
var noteMarkdownTokens = regexp.MustCompile(
	`\\.|` +
		`^ {0,3}(?:\x60{3,}.*|~{3,}.*|(?:-[ 	]*){3,}$|(?:\*[ 	]*){3,}$|(?:_[ 	]*){3,}$)|` +
		`^ {0,3}(?:#{1,6}(?:[ 	]+|$)|>+[ 	]*|[-+*][ 	]+|[0-9]+[.)][ 	]+)|` +
		`\x60+[^\x60]*\x60+|!?\[[^]\n]*\]\([^\n)]*\)|` +
		`\*\*[^*]+\*\*|__[^_]+__|~~[^~]+~~|\*[^*]+\*|_[^_]+_|\[[ xX]\]|\|`)

func (s *State) highlightNoteMarkdown(_ string, line string, dst []components.CodeEditorRun) []components.CodeEditorRun {
	color := func(index int) gfx.Color {
		v := s.pal.ansi[index]
		return gfx.RGB(v[0], v[1], v[2])
	}
	for _, span := range noteMarkdownTokens.FindAllStringIndex(line, -1) {
		token := line[span[0]:span[1]]
		if token[0] == '\\' {
			continue
		}
		shade := color(5)
		switch token[0] {
		case '`':
			shade = color(2)
		case '[', '!':
			shade = color(4)
		case '|':
			shade = s.pal.muted
		}
		dst = append(dst, components.CodeEditorRun{Start: span[0], End: span[1], Color: shade})
	}
	return dst
}
