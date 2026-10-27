package filecontent

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestEncodingRoundTripPreservesBOMAndMixedLineEndings(t *testing.T) {
	for _, name := range Encodings {
		value := "中文測試\nsecond\nthird\n"
		if name == "Shift_JIS" {
			value = "日本語\nsecond\nthird\n"
		}
		if name == "Windows-1252" || name == "ISO-8859-1" {
			value = "café\nsecond\nthird\n"
		}
		for _, withBOM := range []bool{false, true} {
			if withBOM && len(bom(name)) == 0 {
				continue
			}
			format := Format{Encoding: name, BOM: withBOM, LineEnding: "\r\n", Breaks: []string{"\r\n", "\n", "\r"}}
			encoded, err := Encode(value, format)
			if err != nil {
				t.Fatalf("%s BOM=%v: %v", name, withBOM, err)
			}
			decoded, err := Decode(encoded, name)
			if err != nil {
				t.Fatalf("%s decode: %v", name, err)
			}
			if decoded.Value != value || decoded.Format.BOM != withBOM || decoded.Format.Encoding != name {
				t.Fatalf("%s: %+v", name, decoded)
			}
			again, err := Encode(decoded.Value, decoded.Format)
			if err != nil || !bytes.Equal(again, encoded) {
				t.Fatalf("%s round trip changed bytes", name)
			}
			changed := strings.Replace(decoded.Value, "second", "edited", 1)
			saved, err := Encode(changed, decoded.Format)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := Decode(saved, name)
			if err != nil || reopened.Value != changed {
				t.Fatalf("%s edited bytes do not round-trip", name)
			}
			if strings.Join(reopened.Format.Breaks, "|") != "\r\n|\n|\r" {
				t.Fatalf("%s changed mixed endings", name)
			}
		}
	}
}

func TestAutoEncodingRequiresExplicitLegacyChoice(t *testing.T) {
	data, err := Encode("中文\n", Format{Encoding: "GB18030", LineEnding: "\r\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Decode(data, ""); !errors.Is(err, ErrChooseEncoding) {
		t.Fatalf("legacy guessed silently: %v", err)
	}
	if doc, err := Decode(data, "GB18030"); err != nil || doc.Value != "中文\n" {
		t.Fatalf("explicit encoding: %+v %v", doc, err)
	}
	for _, name := range []string{"UTF-8", "UTF-16LE", "UTF-16BE", "UTF-32LE", "UTF-32BE"} {
		data, err := Encode("中文😀\n", Format{Encoding: name, BOM: true, LineEnding: "\n"})
		if err != nil {
			t.Fatal(err)
		}
		doc, err := Decode(data, "")
		if err != nil || doc.Format.Encoding != name || doc.Value != "中文😀\n" {
			t.Fatalf("BOM %s: %+v %v", name, doc, err)
		}
	}
}

func TestEncodingRejectsInvalidAndUnrepresentableInput(t *testing.T) {
	for _, test := range []struct {
		data []byte
		name string
	}{{[]byte{0xff}, "UTF-8"}, {[]byte{0x00, 0xd8}, "UTF-16LE"}, {[]byte{0x81}, "GBK"}, {[]byte("a\x00b"), "UTF-8"}} {
		if _, err := Decode(test.data, test.name); err == nil {
			t.Fatalf("accepted invalid bytes %x as %s", test.data, test.name)
		}
	}
	if _, err := Encode("emoji 😀", Format{Encoding: "GBK"}); !errors.Is(err, ErrEncoding) {
		t.Fatalf("unrepresentable text accepted: %v", err)
	}
	if _, err := Decode(bytes.Repeat([]byte{'a'}, MaxTextBytes+1), ""); !errors.Is(err, ErrTooLarge) {
		t.Fatal("no text size cap")
	}
	if _, err := Encode(strings.Repeat("a", MaxTextBytes+1), Format{Encoding: "UTF-8"}); !errors.Is(err, ErrTooLarge) {
		t.Fatal("no save size cap")
	}
}

func TestWindowsPasteKeepsOriginalLineEnding(t *testing.T) {
	data, err := Encode("a\r\nb\r\n", Format{Encoding: "UTF-8", LineEnding: "\n"})
	if err != nil || string(data) != "a\nb\n" {
		t.Fatalf("paste normalization: %q %v", data, err)
	}
}
