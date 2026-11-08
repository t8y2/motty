package attach

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
)

// Word, Excel and PowerPoint files (Office Open XML) are zip archives of XML.
// Their text is read with the standard library alone. Only the text is taken:
// pictures, charts, styles, comments, notes and headers are not, and a number in
// a spreadsheet is the value stored, not the date or currency a format shows.

const (
	// maxPartBytes bounds one XML part after decompression, so a small archive
	// cannot expand into memory.
	maxPartBytes = 64 << 20
	zipMagic     = "PK\x03\x04"
)

// errNotOffice is returned for an archive that is none of the three.
var errNotOffice = errors.New("attach: not an Office document")

// officeText returns the text of data when it is a docx, xlsx or pptx.
func officeText(data []byte, maxTextBytes int) (string, error) {
	if !bytes.HasPrefix(data, []byte(zipMagic)) {
		return "", errNotOffice
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errNotOffice
	}
	parts := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		parts[file.Name] = file
	}
	var text string
	switch {
	case parts["word/document.xml"] != nil:
		text, err = docxText(parts, maxTextBytes)
	case parts["ppt/presentation.xml"] != nil:
		text, err = pptxText(parts, maxTextBytes)
	case parts["xl/workbook.xml"] != nil:
		text, err = xlsxText(parts, maxTextBytes)
	default:
		return "", errNotOffice
	}
	if err != nil {
		return "", err
	}
	if len(text) > maxTextBytes {
		return "", ErrTextTooLarge
	}
	if strings.TrimSpace(text) == "" {
		return "", ErrEmpty
	}
	return text, nil
}

// openPart returns a decoder over one part of the archive.
func openPart(parts map[string]*zip.File, name string) (*xml.Decoder, io.Closer, error) {
	file := parts[name]
	if file == nil {
		return nil, nil, ErrUnsupported
	}
	if file.UncompressedSize64 > maxPartBytes {
		return nil, nil, ErrTooLarge
	}
	reader, err := file.Open()
	if err != nil {
		return nil, nil, ErrUnsupported
	}
	decoder := xml.NewDecoder(io.LimitReader(reader, maxPartBytes))
	return decoder, reader, nil
}

// walk calls visit with every start and end element of a part until visit says
// stop or the part ends.
func walk(parts map[string]*zip.File, name string, visit func(token xml.Token) (stop bool)) error {
	decoder, closer, err := openPart(parts, name)
	if err != nil {
		return err
	}
	defer closer.Close()
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return ErrUnsupported
		}
		if visit(token) {
			return nil
		}
	}
}

// textCollector gathers the text of a run of paragraphs. Every element is named
// by its local part: the word and drawing namespaces differ, the structure
// does not.
type textCollector struct {
	b      bytes.Buffer
	limit  int
	inText bool
	cells  int // how many table cells are open: a paragraph in one does not end its line
}

func (c *textCollector) full() bool { return c.b.Len() > c.limit }

// trimEnd drops the spaces and tabs at the end of what is collected.
func (c *textCollector) trimEnd() {
	c.b.Truncate(len(bytes.TrimRight(c.b.Bytes(), " \t")))
}

func docxText(parts map[string]*zip.File, maxTextBytes int) (string, error) {
	c := textCollector{limit: maxTextBytes}
	err := walk(parts, "word/document.xml", func(token xml.Token) bool {
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				c.inText = true
			case "tab":
				c.b.WriteByte('\t')
			case "br", "cr":
				c.b.WriteByte('\n')
			case "tc":
				c.cells++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				c.inText = false
			case "p":
				if c.cells > 0 {
					c.b.WriteByte(' ')
				} else {
					c.b.WriteByte('\n')
				}
			case "tc":
				c.cells--
				c.trimEnd()
				c.b.WriteByte('\t')
			case "tr":
				c.trimEnd()
				c.b.WriteByte('\n')
			}
		case xml.CharData:
			if c.inText {
				c.b.Write(t)
			}
		}
		return c.full()
	})
	return tidy(c.b.String()), err
}

// tidy trims the tab that closes every table row and runs of blank lines.
func tidy(text string) string {
	lines := strings.Split(text, "\n")
	out := lines[:0]
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, "\t ")
		if line == "" {
			if blank++; blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n")) + "\n"
}

// relationships maps a relationship id to its target, resolved against the
// directory of the part that owns the .rels file.
func relationships(parts map[string]*zip.File, rels, base string) map[string]string {
	targets := map[string]string{}
	_ = walk(parts, rels, func(token xml.Token) bool {
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "Relationship" {
			var id, target string
			for _, attr := range start.Attr {
				switch attr.Name.Local {
				case "Id":
					id = attr.Value
				case "Target":
					target = attr.Value
				}
			}
			if strings.HasPrefix(target, "/") {
				target = strings.TrimPrefix(target, "/")
			} else {
				target = path.Join(base, target)
			}
			targets[id] = target
		}
		return false
	})
	return targets
}

// relID is the r:id of an element: the attribute named id that has a namespace,
// which tells it from the plain id beside it.
func relID(start xml.StartElement) string {
	for _, attr := range start.Attr {
		if attr.Name.Local == "id" && attr.Name.Space != "" {
			return attr.Value
		}
	}
	return ""
}

func attribute(start xml.StartElement, local string) string {
	for _, attr := range start.Attr {
		if attr.Name.Local == local {
			return attr.Value
		}
	}
	return ""
}

func pptxText(parts map[string]*zip.File, maxTextBytes int) (string, error) {
	targets := relationships(parts, "ppt/_rels/presentation.xml.rels", "ppt")
	var order []string
	err := walk(parts, "ppt/presentation.xml", func(token xml.Token) bool {
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "sldId" {
			if target := targets[relID(start)]; target != "" {
				order = append(order, target)
			}
		}
		return false
	})
	if err != nil {
		return "", err
	}
	c := textCollector{limit: maxTextBytes}
	for index, slide := range order {
		if c.full() {
			break
		}
		c.b.WriteString("--- Slide " + strconv.Itoa(index+1) + " ---\n")
		err := walk(parts, slide, func(token xml.Token) bool {
			switch t := token.(type) {
			case xml.StartElement:
				if t.Name.Local == "t" {
					c.inText = true
				}
				if t.Name.Local == "br" {
					c.b.WriteByte('\n')
				}
			case xml.EndElement:
				switch t.Name.Local {
				case "t":
					c.inText = false
				case "p":
					c.b.WriteByte('\n')
				}
			case xml.CharData:
				if c.inText {
					c.b.Write(t)
				}
			}
			return c.full()
		})
		if err != nil {
			return "", err
		}
	}
	return tidy(c.b.String()), nil
}

// sharedStrings is the workbook's table of strings, which cells refer to by
// index. The pronunciation guide (rPh) inside an entry is not part of it.
func sharedStrings(parts map[string]*zip.File, maxTextBytes int) ([]string, error) {
	if parts["xl/sharedStrings.xml"] == nil {
		return nil, nil
	}
	var table []string
	var current strings.Builder
	inText, phonetic, size := false, 0, 0
	err := walk(parts, "xl/sharedStrings.xml", func(token xml.Token) bool {
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				current.Reset()
			case "rPh":
				phonetic++
			case "t":
				inText = phonetic == 0
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				table = append(table, current.String())
				size += current.Len()
			case "rPh":
				phonetic--
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText {
				current.Write(t)
			}
		}
		return size > 4*maxTextBytes
	})
	return table, err
}

// columnIndex turns the letters of a cell reference ("AB12") into a zero-based
// column, or -1.
func columnIndex(ref string) int {
	column := 0
	for i := 0; i < len(ref); i++ {
		ch := ref[i]
		if ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		if ch < 'A' || ch > 'Z' {
			if i == 0 {
				return -1
			}
			return column - 1
		}
		column = column*26 + int(ch-'A'+1)
	}
	return column - 1
}

type sheetRef struct{ name, part string }

func xlsxText(parts map[string]*zip.File, maxTextBytes int) (string, error) {
	targets := relationships(parts, "xl/_rels/workbook.xml.rels", "xl")
	var sheets []sheetRef
	err := walk(parts, "xl/workbook.xml", func(token xml.Token) bool {
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "sheet" {
			if target := targets[relID(start)]; target != "" {
				sheets = append(sheets, sheetRef{name: attribute(start, "name"), part: target})
			}
		}
		return false
	})
	if err != nil {
		return "", err
	}
	if len(sheets) == 0 {
		return "", ErrUnsupported
	}
	table, err := sharedStrings(parts, maxTextBytes)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, sheet := range sheets {
		if out.Len() > maxTextBytes {
			break
		}
		out.WriteString("## Sheet: " + sheet.name + "\n")
		if err := sheetRows(parts, sheet.part, table, &out, maxTextBytes); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

// sheetRows writes a worksheet as tab-separated rows. Empty rows are left out;
// empty cells keep their column so that the others stay under their headings.
func sheetRows(parts map[string]*zip.File, name string, table []string, out *strings.Builder, maxTextBytes int) error {
	var row []string
	var kind, value string
	var column int
	var inValue, inInline, inCell bool
	return walk(parts, name, func(token xml.Token) bool {
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = row[:0]
			case "c":
				inCell = true
				kind, value = attribute(t, "t"), ""
				if column = columnIndex(attribute(t, "r")); column < 0 {
					column = len(row)
				}
			case "v":
				inValue = inCell
			case "is":
				inInline = inCell
			case "t":
				inValue = inValue || inInline
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inValue = false
			case "is":
				inInline = false
			case "c":
				inCell = false
				if kind == "s" {
					if index, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && index >= 0 && index < len(table) {
						value = table[index]
					}
				} else if kind == "b" {
					value = map[string]string{"1": "TRUE", "0": "FALSE"}[strings.TrimSpace(value)]
				}
				for len(row) <= column && len(row) < 16384 {
					row = append(row, "")
				}
				if column < len(row) {
					row[column] = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(value)
				}
			case "row":
				if line := strings.TrimRight(strings.Join(row, "\t"), "\t"); line != "" {
					out.WriteString(line)
					out.WriteByte('\n')
				}
			case "t":
				inValue = false
			}
		case xml.CharData:
			if inValue && inCell {
				value += string(t)
			}
		}
		return out.Len() > maxTextBytes
	})
}
