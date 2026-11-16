package attach

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func makeZip(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for name, body := range files {
		w, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

const wordNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

func TestWordTextKeepsParagraphsTabsBreaksAndTables(t *testing.T) {
	document := `<w:document ` + wordNS + `><w:body>
<w:p><w:r><w:t>Title</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">Hello </w:t></w:r><w:r><w:t>world</w:t></w:r><w:r><w:br/></w:r><w:r><w:t>next</w:t></w:r></w:p>
<w:p><w:r><w:delText>removed</w:delText></w:r><w:r><w:instrText>PAGE</w:instrText></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>a</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>b</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
</w:body></w:document>`
	file, err := Decode("r.docx", makeZip(t, map[string]string{"word/document.xml": document}))
	if err != nil || file.Kind != KindText {
		t.Fatalf("file = %+v, err = %v", file, err)
	}
	want := "Title\nHello world\nnext\n\na\tb\n"
	if file.Text != want {
		t.Fatalf("text = %q, want %q", file.Text, want)
	}
}

func TestPowerPointTextFollowsThePresentationOrder(t *testing.T) {
	slide := func(text string) string {
		return `<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="p"><p:txBody><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sld>`
	}
	file, err := Decode("deck.pptx", makeZip(t, map[string]string{
		"ppt/presentation.xml":            `<p:presentation xmlns:p="p" xmlns:r="r"><p:sldIdLst><p:sldId id="256" r:id="rId2"/><p:sldId id="257" r:id="rId1"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<Relationships><Relationship Id="rId1" Target="slides/slide1.xml"/><Relationship Id="rId2" Target="slides/slide2.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":           slide("was first in the file"),
		"ppt/slides/slide2.xml":           slide("is first in the deck"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := "--- Slide 1 ---\nis first in the deck\n--- Slide 2 ---\nwas first in the file\n"
	if file.Text != want {
		t.Fatalf("text = %q, want %q", file.Text, want)
	}
}

func TestExcelTextIsTabSeparatedWithColumnsKeptInPlace(t *testing.T) {
	sheet := `<worksheet><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1" t="s"><v>1</v></c></row>
<row r="2"><c r="A2"><v>42</v></c><c r="B2" t="b"><v>1</v></c><c r="C2" t="inlineStr"><is><t>inline</t></is></c></row>
<row r="3"></row>
</sheetData></worksheet>`
	file, err := Decode("book.xlsx", makeZip(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>name</t></si><si><r><t>qty</t></r><rPh><t>ignored</t></rPh></si></sst>`,
		"xl/worksheets/sheet1.xml":   sheet,
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := "## Sheet: Data\nname\t\tqty\n42\tTRUE\tinline\n"
	if file.Text != want {
		t.Fatalf("text = %q, want %q", file.Text, want)
	}
}

func TestAnArchiveThatIsNotOfficeIsNotText(t *testing.T) {
	if _, err := Decode("a.zip", makeZip(t, map[string]string{"readme.txt": "hello"})); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
}

func TestAWordFileWithNoTextIsEmpty(t *testing.T) {
	_, err := Decode("blank.docx", makeZip(t, map[string]string{"word/document.xml": `<w:document ` + wordNS + `><w:body><w:p/></w:body></w:document>`}))
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v", err)
	}
}

func TestADocumentWithTooMuchTextIsRefusedNotCut(t *testing.T) {
	body := strings.Repeat(`<w:p><w:r><w:t>`+strings.Repeat("x", 1000)+`</w:t></w:r></w:p>`, 4500)
	_, err := Decode("big.docx", makeZip(t, map[string]string{"word/document.xml": `<w:document ` + wordNS + `><w:body>` + body + `</w:body></w:document>`}))
	if !errors.Is(err, ErrTextTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestADamagedDocumentIsUnsupportedNotAPanic(t *testing.T) {
	_, err := Decode("bad.docx", makeZip(t, map[string]string{"word/document.xml": `<w:document><w:p>`}))
	if err == nil {
		t.Fatal("a truncated document was accepted")
	}
}

func TestColumnIndex(t *testing.T) {
	for ref, want := range map[string]int{"A1": 0, "C7": 2, "Z1": 25, "AA1": 26, "AB12": 27, "1": -1} {
		if got := columnIndex(ref); got != want {
			t.Errorf("columnIndex(%q) = %d, want %d", ref, got, want)
		}
	}
}

func FuzzOfficeText(f *testing.F) {
	f.Add([]byte(zipMagic))
	f.Add(makeZip(f, map[string]string{"word/document.xml": `<w:document><w:p><w:t>x</w:t></w:p></w:document>`}))
	f.Fuzz(func(t *testing.T, data []byte) {
		Decode("f", data)
	})
}
