// Package charset lists the remote character encodings a connection can use and
// converts a shell's byte stream to and from UTF-8. A terminal is a stream: an
// SSH read can end in the middle of a multibyte character, so the decoder keeps
// the incomplete suffix for the next read instead of replacing it.
package charset

import (
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// UTF8 is the default and names a stream that needs no conversion. An empty
// encoding means the same thing in saved hosts, so legacy records keep working.
const UTF8 = "UTF-8"

// Encodings are the remote encodings a connection may choose. UTF-16 and UTF-32
// are deliberately absent: their bytes contain NULs and their ASCII characters
// are two or four bytes wide, which cannot carry the ESC/C1 control sequences a
// terminal streams alongside text. The single-byte and multibyte legacy sets
// here all leave ASCII (and therefore terminal control bytes) untouched.
var Encodings = []string{
	UTF8,
	"GB18030",
	"GBK",
	"GB2312",
	"Big5",
	"Shift_JIS",
	"EUC-JP",
	"EUC-KR",
	"ISO-8859-1",
	"ISO-8859-15",
	"Windows-1252",
	"KOI8-R",
}

// Search is what a picker filters on besides a name: aliases a user may type
// instead of the exact label, such as "gb" for the Chinese encodings.
func Search(name string) string {
	switch strings.ToUpper(name) {
	case "GB18030":
		return "gb chinese simplified gb18030 gbk cp936 中文 简体"
	case "GBK":
		return "gb chinese simplified gbk cp936 中文 简体"
	case "GB2312":
		return "gb chinese simplified gb2312 中文 简体"
	case "BIG5":
		return "big5 chinese traditional cp950 中文 繁體"
	case "SHIFT_JIS":
		return "shift_jis sjis japanese cp932 日本語"
	case "EUC-JP":
		return "euc-jp eucjp japanese 日本語"
	case "EUC-KR":
		return "euc-kr euckr korean cp949 한국어"
	case "ISO-8859-1":
		return "iso-8859-1 latin1 latin-1 western"
	case "ISO-8859-15":
		return "iso-8859-15 latin9 latin-9 western euro"
	case "WINDOWS-1252":
		return "windows-1252 cp1252 western"
	case "KOI8-R":
		return "koi8-r cyrillic russian русский"
	}
	return "utf-8 utf8 unicode"
}

// Valid reports whether name is a supported remote encoding. An empty name is
// valid: it is the UTF-8 default of a host saved before the setting existed.
func Valid(name string) bool {
	if name == "" || name == UTF8 {
		return true
	}
	for _, candidate := range Encodings {
		if candidate == name {
			return true
		}
	}
	return false
}

// Normalize maps an empty name to UTF-8 and reports whether the name is known.
// Use it when reading a saved value: an unknown one is a damaged record.
func Normalize(name string) (string, bool) {
	if name == "" {
		return UTF8, true
	}
	if !Valid(name) {
		return "", false
	}
	return name, true
}

// codec returns x/text's encoder for name, or nil for UTF-8 (no conversion).
func codec(name string) encoding.Encoding {
	switch name {
	case "", UTF8:
		return nil
	case "GB18030":
		return simplifiedchinese.GB18030
	case "GBK":
		return simplifiedchinese.GBK
	case "GB2312":
		// GB2312 is a strict subset of GBK; x/text has no separate decoder.
		return simplifiedchinese.GBK
	case "Big5":
		return traditionalchinese.Big5
	case "Shift_JIS":
		return japanese.ShiftJIS
	case "EUC-JP":
		return japanese.EUCJP
	case "EUC-KR":
		return korean.EUCKR
	case "ISO-8859-1":
		return charmap.ISO8859_1
	case "ISO-8859-15":
		return charmap.ISO8859_15
	case "Windows-1252":
		return charmap.Windows1252
	case "KOI8-R":
		return charmap.KOI8R
	}
	return nil
}

// Decoder turns a remote byte stream into UTF-8. It is safe to call Append with
// consecutive reads of the same stream; a trailing incomplete multibyte
// sequence is held until enough bytes arrive. Reset drops that pending suffix.
type Decoder struct {
	name        string
	transformer transform.Transformer
	pending     []byte
	active      bool
}

// NewDecoder builds a decoder for name. A UTF-8 (or empty) name returns an
// inert decoder whose Append is a pure copy.
func NewDecoder(name string) *Decoder {
	enc := codec(name)
	d := &Decoder{name: name}
	if enc != nil {
		d.transformer = enc.NewDecoder()
		d.active = true
	}
	return d
}

// Name is the encoding this decoder was built for.
func (d *Decoder) Name() string { return d.name }

// Active reports whether the decoder converts bytes at all.
func (d *Decoder) Active() bool { return d.active }

// Append converts one read of the remote stream to UTF-8 and appends it to dst.
// An incomplete multibyte character at the end of src is retained and emitted
// on a later call, once its remaining bytes arrive. When the stream ends, call
// Flush to emit a replacement for any trailing bytes that never completed.
//
// Transform is called with atEOF=false so a partial sequence is reported as
// ErrShortSrc and left unconsumed rather than becoming a replacement rune.
func (d *Decoder) Append(dst, src []byte) []byte {
	if !d.active {
		return append(dst, src...)
	}
	if len(d.pending) > 0 {
		src = append(d.pending[:len(d.pending):len(d.pending)], src...)
		d.pending = d.pending[:0]
	}
	if len(src) == 0 {
		return dst
	}
	// A legacy decoder never grows a rune: it emits at most as many UTF-8 bytes
	// as a character's worst case (GB18030 4 bytes -> 4 UTF-8 bytes is 1:1 at
	// most; single-byte sets are 1:2). Give it headroom and grow on ErrShortDst.
	buf := make([]byte, 0, len(src)*2+8)
	for len(src) > 0 {
		nDst, nSrc, err := d.transformer.Transform(buf[len(buf):cap(buf)], src, false)
		buf = buf[:len(buf)+nDst]
		src = src[nSrc:]
		switch err {
		case transform.ErrShortDst:
			// Grow and retry the rest of this read.
			grown := make([]byte, len(buf), cap(buf)*2)
			copy(grown, buf)
			buf = grown
		case transform.ErrShortSrc:
			// The tail is a partial character: keep it for the next read.
			d.pending = append(d.pending[:0], src...)
			return append(dst, buf...)
		case nil:
			return append(dst, buf...)
		default:
			// A decoder reports malformed input rather than failing the stream.
			return append(dst, buf...)
		}
	}
	return append(dst, buf...)
}

// Flush emits whatever an incomplete trailing sequence decodes to at end of
// stream. Call it once, after the last Append, when the shell has ended.
func (d *Decoder) Flush(dst []byte) []byte {
	if !d.active || len(d.pending) == 0 {
		return dst
	}
	buf := make([]byte, len(d.pending)*3+8)
	nDst, _, _ := d.transformer.Transform(buf, d.pending, true)
	d.pending = d.pending[:0]
	return append(dst, buf[:nDst]...)
}

// Reset drops the pending suffix and starts a fresh stream. Call it when a
// session reconnects and its byte stream starts over.
func (d *Decoder) Reset() {
	d.pending = d.pending[:0]
	if d.transformer != nil {
		d.transformer.Reset()
	}
}

// EncodeInput converts UTF-8 input bytes to the remote encoding. Terminal
// control bytes (C0, DEL, and the bytes of ESC-led sequences) are ASCII in
// every supported encoding and are left untouched; only UTF-8 multibyte
// characters are transformed. This keeps arrow keys, bracketed paste markers
// and terminal replies byte-identical while letting typed CJK text reach a
// legacy server in its own charset.
func EncodeInput(p []byte, name string) []byte {
	enc := codec(name)
	if enc == nil {
		return p
	}
	// Split at C0 control bytes: transform only the printable runs between them.
	// ESC starts a control sequence whose payload is ASCII, so it is a boundary.
	out := make([]byte, 0, len(p))
	start := 0
	flush := func(end int) {
		if end <= start {
			return
		}
		converted, err := enc.NewEncoder().Bytes(p[start:end])
		if err != nil {
			// A rune the remote charset cannot represent: keep the UTF-8 bytes
			// rather than dropping the user's input.
			out = append(out, p[start:end]...)
			return
		}
		out = append(out, converted...)
	}
	for i, b := range p {
		if b < 0x20 || b == 0x7f || b == 0x1b {
			flush(i)
			out = append(out, b)
			start = i + 1
		}
	}
	flush(len(p))
	return out
}
