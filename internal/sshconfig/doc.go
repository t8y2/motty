package sshconfig

import (
	"bytes"
	"os"
	"strings"
)

// docLine is one line of a configuration file and the line ending it came with,
// so a file that is read and written back unchanged is byte-identical.
type docLine struct {
	text string
	eol  string
}

// doc is one configuration file held as lines. It is the unit that is edited
// and written back; nothing but the lines a caller changed is ever touched.
type doc struct {
	path string
	// lines is the whole file. A file with no final line ending ends in a line
	// whose eol is empty.
	lines []docLine
	// exists is false for a file that is yet to be created.
	exists bool
	mode   os.FileMode
	// raw is the bytes read, to tell on saving whether the file changed under us.
	raw []byte
	// eol is the line ending new lines take: the one most lines of the file use.
	eol   string
	dirty bool
	// rev counts the edits, so what was worked out from the lines can be kept
	// until they change.
	rev      uint64
	scanned  uint64
	scan     []section
	scanDone bool
}

func newDoc(path string, data []byte, exists bool, mode os.FileMode) *doc {
	d := &doc{path: path, exists: exists, mode: mode, raw: data}
	crlf, lf := 0, 0
	for rest := data; len(rest) > 0; {
		end := bytes.IndexByte(rest, '\n')
		if end < 0 {
			d.lines = append(d.lines, docLine{text: string(rest)})
			break
		}
		if end > 0 && rest[end-1] == '\r' {
			d.lines = append(d.lines, docLine{text: string(rest[:end-1]), eol: "\r\n"})
			crlf++
		} else {
			d.lines = append(d.lines, docLine{text: string(rest[:end]), eol: "\n"})
			lf++
		}
		rest = rest[end+1:]
	}
	d.eol = "\n"
	if crlf > lf {
		d.eol = "\r\n"
	}
	return d
}

func (d *doc) bytes() []byte {
	size := 0
	for _, line := range d.lines {
		size += len(line.text) + len(line.eol)
	}
	out := make([]byte, 0, size)
	for _, line := range d.lines {
		out = append(out, line.text...)
		out = append(out, line.eol...)
	}
	return out
}

// insert puts lines before index, ending each with the file's line ending.
func (d *doc) insert(index int, texts ...string) {
	if len(texts) == 0 {
		return
	}
	if index == len(d.lines) && index > 0 && d.lines[index-1].eol == "" {
		d.lines[index-1].eol = d.eol
	}
	added := make([]docLine, len(texts))
	for i, text := range texts {
		added[i] = docLine{text: text, eol: d.eol}
	}
	d.lines = append(d.lines[:index], append(added, d.lines[index:]...)...)
	d.touch()
}

// remove deletes the lines in [from, to).
func (d *doc) remove(from, to int) {
	if from >= to {
		return
	}
	d.lines = append(d.lines[:from], d.lines[to:]...)
	d.touch()
}

// touch notes that the lines changed.
func (d *doc) touch() {
	d.dirty = true
	d.rev++
}

// setText replaces the text of one line, keeping its line ending.
func (d *doc) setText(index int, text string) {
	d.lines[index].text = text
	d.touch()
}

// section is one Host or Match block: its header line and the lines up to the
// next header.
type section struct {
	header int
	end    int
	match  bool
	args   []string
}

// directiveAt is a line that carries a directive.
type directiveAt struct {
	index int
	name  string
	args  []string
}

// sections lists the blocks of the file. The list is kept until the lines
// change, and must not be changed by the caller.
func (d *doc) sections() []section {
	if d.scanDone && d.scanned == d.rev {
		return d.scan
	}
	d.scan, d.scanDone, d.scanned = d.findSections(), true, d.rev
	return d.scan
}

// mayBeHeader is a cheap look at a line, to leave the lines that cannot start a
// block unparsed.
func mayBeHeader(text string) bool {
	text = strings.TrimLeft(text, " \t")
	if len(text) < 5 {
		return false
	}
	switch text[0] {
	case 'h', 'H', 'm', 'M':
		return true
	}
	return false
}

func (d *doc) findSections() []section {
	var sections []section
	for index, line := range d.lines {
		if !mayBeHeader(line.text) {
			continue
		}
		name, args, err := parseLine(line.text)
		if err != nil || (name != "host" && name != "match") {
			continue
		}
		if len(sections) > 0 {
			sections[len(sections)-1].end = index
		}
		sections = append(sections, section{header: index, end: len(d.lines), match: name == "match", args: args})
	}
	return sections
}

// directives lists the directive lines strictly inside (header, end).
func (d *doc) directives(header, end int) []directiveAt {
	var found []directiveAt
	for index := header + 1; index < end && index < len(d.lines); index++ {
		name, args, err := parseLine(d.lines[index].text)
		if err != nil || name == "" {
			continue
		}
		found = append(found, directiveAt{index: index, name: name, args: args})
	}
	return found
}

func isComment(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "#")
}

func isBlank(text string) bool {
	return strings.TrimSpace(text) == ""
}

func leadingSpace(text string) string {
	return text[:len(text)-len(strings.TrimLeft(text, " \t"))]
}
