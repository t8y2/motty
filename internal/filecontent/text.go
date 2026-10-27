// Package filecontent decodes bounded file content without silently changing its
// encoding. Legacy encodings require an explicit choice; byte round trips are
// checked before text is admitted to an editor and before it is saved.
package filecontent

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	textunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/encoding/unicode/utf32"
)

const MaxTextBytes = 1 << 20

var (
	ErrChooseEncoding = errors.New("choose the file's text encoding")
	ErrBinary         = errors.New("unsupported binary file")
	ErrEncoding       = errors.New("text cannot round-trip in the selected encoding")
	ErrTooLarge       = errors.New("text file exceeds 1 MiB")
)

var Encodings = []string{"UTF-8", "UTF-16LE", "UTF-16BE", "UTF-32LE", "UTF-32BE", "GB18030", "GBK", "Big5", "Shift_JIS", "Windows-1252", "ISO-8859-1"}

// Format is captured from the original bytes, not inferred again on save.
type Format struct {
	Encoding   string
	BOM        bool
	LineEnding string
	Breaks     []string
}

type Text struct {
	Value  string
	Format Format
}

func codec(name string) encoding.Encoding {
	switch name {
	case "UTF-16LE":
		return textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM)
	case "UTF-16BE":
		return textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM)
	case "UTF-32LE":
		return utf32.UTF32(utf32.LittleEndian, utf32.IgnoreBOM)
	case "UTF-32BE":
		return utf32.UTF32(utf32.BigEndian, utf32.IgnoreBOM)
	case "GB18030":
		return simplifiedchinese.GB18030
	case "GBK":
		return simplifiedchinese.GBK
	case "Big5":
		return traditionalchinese.Big5
	case "Shift_JIS":
		return japanese.ShiftJIS
	case "Windows-1252":
		return charmap.Windows1252
	case "ISO-8859-1":
		return charmap.ISO8859_1
	}
	return nil
}

func bom(name string) []byte {
	switch name {
	case "UTF-8":
		return []byte{0xef, 0xbb, 0xbf}
	case "UTF-16LE":
		return []byte{0xff, 0xfe}
	case "UTF-16BE":
		return []byte{0xfe, 0xff}
	case "UTF-32LE":
		return []byte{0xff, 0xfe, 0, 0}
	case "UTF-32BE":
		return []byte{0, 0, 0xfe, 0xff}
	}
	return nil
}

func Decode(data []byte, name string) (Text, error) {
	if len(data) > MaxTextBytes {
		return Text{}, ErrTooLarge
	}
	format := Format{Encoding: name, LineEnding: "\n"}
	// UTF-32 BOMs must precede UTF-16 in this check.
	for _, candidate := range []string{"UTF-32LE", "UTF-32BE", "UTF-8", "UTF-16LE", "UTF-16BE"} {
		if bytes.HasPrefix(data, bom(candidate)) {
			if name != "" && name != "Auto" && name != candidate {
				return Text{}, fmt.Errorf("%w: BOM specifies %s", ErrEncoding, candidate)
			}
			format.Encoding, format.BOM = candidate, true
			data = data[len(bom(candidate)):]
			break
		}
	}
	if format.Encoding == "" || format.Encoding == "Auto" {
		if bytes.IndexByte(data, 0) >= 0 {
			if len(data)%2 == 0 {
				return Text{}, ErrChooseEncoding
			}
			return Text{}, ErrBinary
		}
		if !utf8.Valid(data) {
			return Text{}, ErrChooseEncoding
		}
		format.Encoding = "UTF-8"
	}
	decoded := data
	if format.Encoding != "UTF-8" {
		enc := codec(format.Encoding)
		if enc == nil {
			return Text{}, ErrEncoding
		}
		var err error
		decoded, err = enc.NewDecoder().Bytes(data)
		if err != nil {
			return Text{}, fmt.Errorf("%w: %v", ErrEncoding, err)
		}
		roundTrip, err := enc.NewEncoder().Bytes(decoded)
		if err != nil || !bytes.Equal(roundTrip, data) {
			return Text{}, ErrEncoding
		}
	}
	if !utf8.Valid(decoded) {
		return Text{}, ErrEncoding
	}
	for _, r := range string(decoded) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' && r != '\f' {
			return Text{}, ErrBinary
		}
	}
	var text strings.Builder
	counts := map[string]int{}
	for i := 0; i < len(decoded); i++ {
		if decoded[i] == '\r' || decoded[i] == '\n' {
			ending := "\n"
			if decoded[i] == '\r' {
				ending = "\r"
				if i+1 < len(decoded) && decoded[i+1] == '\n' {
					ending = "\r\n"
					i++
				}
			}
			format.Breaks = append(format.Breaks, ending)
			counts[ending]++
			text.WriteByte('\n')
		} else {
			text.WriteByte(decoded[i])
		}
	}
	for _, ending := range []string{"\n", "\r\n", "\r"} {
		if counts[ending] > counts[format.LineEnding] {
			format.LineEnding = ending
		}
	}
	return Text{Value: text.String(), Format: format}, nil
}

func Encode(value string, format Format) ([]byte, error) {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	if !utf8.ValidString(value) {
		return nil, ErrEncoding
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\f' {
			return nil, ErrBinary
		}
	}
	var text strings.Builder
	line := 0
	ending := format.LineEnding
	if ending == "" {
		ending = "\n"
	}
	for _, r := range value {
		if r == '\n' {
			next := ending
			if line < len(format.Breaks) {
				next = format.Breaks[line]
			}
			text.WriteString(next)
			line++
		} else {
			text.WriteRune(r)
		}
	}
	data := []byte(text.String())
	if format.Encoding != "UTF-8" {
		enc := codec(format.Encoding)
		if enc == nil {
			return nil, ErrEncoding
		}
		encoded, err := enc.NewEncoder().Bytes(data)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEncoding, err)
		}
		decoded, err := enc.NewDecoder().Bytes(encoded)
		if err != nil || !bytes.Equal(decoded, data) {
			return nil, ErrEncoding
		}
		data = encoded
	}
	if format.BOM {
		if len(bom(format.Encoding)) == 0 {
			return nil, ErrEncoding
		}
		data = append(bom(format.Encoding), data...)
	}
	if len(data) > MaxTextBytes {
		return nil, ErrTooLarge
	}
	return data, nil
}
