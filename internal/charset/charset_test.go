package charset

import (
	"bytes"
	"testing"

	"golang.org/x/text/encoding"
)

// codecFor is the test's own encoder lookup, independent of the package's
// internal codec, so a test does not pass just because both use the same map.
func codecFor(t *testing.T, name string) encoding.Encoding {
	t.Helper()
	enc := codec(name)
	if enc == nil {
		t.Fatalf("no codec for %q", name)
	}
	return enc
}

func encoded(t *testing.T, name, text string) []byte {
	t.Helper()
	if name == UTF8 {
		return []byte(text)
	}
	data, err := codecFor(t, name).NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatalf("encode %q %q: %v", name, text, err)
	}
	return data
}

func TestEncodingsAreValidAndSearchable(t *testing.T) {
	if !Valid("") || !Valid(UTF8) {
		t.Fatal("UTF-8 and the empty default must be valid")
	}
	for _, name := range Encodings {
		if !Valid(name) {
			t.Errorf("listed encoding %q is not valid", name)
		}
		if Search(name) == "" {
			t.Errorf("listed encoding %q has no search text", name)
		}
		if _, ok := Normalize(name); !ok {
			t.Errorf("listed encoding %q does not normalize", name)
		}
	}
	for _, bad := range []string{"utf-9", "UTF8", "gb18030", "nonsense"} {
		if Valid(bad) {
			t.Errorf("unknown encoding %q reported valid", bad)
		}
		if _, ok := Normalize(bad); ok {
			t.Errorf("unknown encoding %q normalized", bad)
		}
	}
	if got, ok := Normalize(""); !ok || got != UTF8 {
		t.Fatalf("empty encoding = %q, %v; want UTF-8, true", got, ok)
	}
	// Terminal-unsafe encodings must not be offered.
	for _, unsafe := range []string{"UTF-16LE", "UTF-32BE"} {
		if Valid(unsafe) {
			t.Errorf("terminal-unsafe encoding %q must not be offered", unsafe)
		}
	}
}

func TestDecodeRoundTripForEveryEncoding(t *testing.T) {
	// ASCII (and therefore terminal control bytes) is representable in every
	// offered encoding. Use it to check the round trip without asking a charset
	// to encode characters it cannot express.
	const text = "Hello, world! 123 ~!@#$%\r\n\t"
	for _, name := range Encodings {
		t.Run(name, func(t *testing.T) {
			data := encoded(t, name, text)
			d := NewDecoder(name)
			got := d.Append(nil, data)
			got = d.Flush(got)
			if string(got) != text {
				t.Fatalf("%s decoded to %q; want %q", name, got, text)
			}
		})
	}
}

func TestDecodeChineseEncodings(t *testing.T) {
	for _, name := range []string{"GB18030", "GBK", "GB2312"} {
		t.Run(name, func(t *testing.T) {
			data := encoded(t, name, "中文乱码 abc")
			d := NewDecoder(name)
			got := d.Append(nil, data)
			got = d.Flush(got)
			if string(got) != "中文乱码 abc" {
				t.Fatalf("%s decoded to %q", name, got)
			}
		})
	}
}

func TestDecodeAcrossWriteBoundaries(t *testing.T) {
	// An SSH read can split a multibyte character at any byte. Decoding each
	// slice must give exactly the same UTF-8 as decoding the whole stream.
	// Each charset gets text it can actually represent, plus ASCII controls.
	cases := []struct{ name, text string }{
		{"GB18030", "总计: 中文测试 ABC\r\n"},
		{"GBK", "总计: 中文测试 ABC\r\n"},
		{"Big5", "總計: 中文測試 ABC\r\n"},
		{"Shift_JIS", "合計: 日本語 ABC\r\n"},
		{"EUC-JP", "合計: 日本語 ABC\r\n"},
		{"EUC-KR", "합계: 한국어 ABC\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, text := tc.name, tc.text
			data := encoded(t, name, text)
			whole := NewDecoder(name)
			want := whole.Append(nil, data)
			want = whole.Flush(want)

			for size := 1; size <= 5; size++ {
				d := NewDecoder(name)
				var got []byte
				for at := 0; at < len(data); at += size {
					end := min(at+size, len(data))
					got = d.Append(got, data[at:end])
				}
				got = d.Flush(got)
				if !bytes.Equal(got, want) {
					t.Fatalf("%s split by %d: got %q, want %q", name, size, got, want)
				}
			}
		})
	}
}

func TestDecodeKeepsTerminalControlBytes(t *testing.T) {
	// The remote mixes text with ANSI escapes. All supported encodings leave
	// ASCII untouched, so ESC sequences must survive decoding byte-for-byte.
	data := append(encoded(t, "GB18030", "中文"), []byte("\x1b[31m红色\x1b[0m\r\n")...)
	d := NewDecoder("GB18030")
	got := d.Append(nil, data)
	got = d.Flush(got)
	if !bytes.Contains(got, []byte("\x1b[31m")) || !bytes.Contains(got, []byte("\x1b[0m")) {
		t.Fatalf("ANSI escapes changed: %q", got)
	}
	if !bytes.Contains(got, []byte("中文")) {
		t.Fatalf("text lost: %q", got)
	}
}

func TestFlushEmitsTrailingPartialCharacter(t *testing.T) {
	data := encoded(t, "GBK", "中")
	if len(data) < 2 {
		t.Fatal("GBK Chinese should be multibyte")
	}
	d := NewDecoder("GBK")
	got := d.Append(nil, data[:len(data)-1])
	if len(got) != 0 {
		t.Fatalf("a partial character was emitted early: %q", got)
	}
	// At end of stream a truncated byte becomes a replacement rune, not a panic.
	got = d.Flush(got)
	if len(got) == 0 {
		t.Fatal("Flush emitted nothing for a truncated character")
	}
}

func TestResetStartsAFreshStream(t *testing.T) {
	data := encoded(t, "GBK", "中")
	d := NewDecoder("GBK")
	d.Append(nil, data[:1]) // half a character
	d.Reset()
	got := d.Append(nil, data)
	got = d.Flush(got)
	if string(got) != "中" {
		t.Fatalf("after Reset decoded to %q; want 中", got)
	}
}

func TestEncodeInputKeepsControlSequences(t *testing.T) {
	// Arrow keys, Enter, Ctrl-C, bracketed-paste markers and terminal replies
	// are ASCII control sequences: they must not be re-encoded.
	input := []byte("echo \x1b[200~中文\x1b[201~\r\x03\x1b[A\x1b[0n")
	got := EncodeInput(input, "GB18030")
	if !bytes.Contains(got, []byte("\x1b[200~")) || !bytes.Contains(got, []byte("\x1b[201~")) ||
		!bytes.Contains(got, []byte("\r")) || !bytes.Contains(got, []byte("\x03")) ||
		!bytes.Contains(got, []byte("\x1b[A")) || !bytes.Contains(got, []byte("\x1b[0n")) {
		t.Fatalf("control bytes changed: %q", got)
	}
	// The Chinese text must have become GB18030 bytes, not UTF-8.
	wantChinese := encoded(t, "GB18030", "中文")
	if !bytes.Contains(got, wantChinese) {
		t.Fatalf("typed Chinese was not encoded to GB18030: %q (want %q inside)", got, wantChinese)
	}
	if bytes.Contains(got, []byte("中文")) {
		t.Fatalf("UTF-8 Chinese leaked into the remote stream: %q", got)
	}
}

func TestEncodeInputUTF8IsIdentity(t *testing.T) {
	input := []byte("echo 中文\r\x1b[A")
	for _, name := range []string{"", UTF8} {
		got := EncodeInput(input, name)
		if !bytes.Equal(got, input) {
			t.Errorf("%q changed UTF-8 input: %q", name, got)
		}
	}
}

func TestEncodeInputUnrepresentableRuneIsKept(t *testing.T) {
	// A rune a single-byte charset cannot express must not vanish.
	input := []byte("中\r")
	got := EncodeInput(input, "ISO-8859-1")
	if !bytes.Equal(got, input) {
		t.Fatalf("unrepresentable rune was lost: %q", got)
	}
}

func TestInertDecoderCopiesBytes(t *testing.T) {
	for _, name := range []string{"", UTF8} {
		d := NewDecoder(name)
		if d.Active() {
			t.Errorf("%q should be an inert decoder", name)
		}
		got := d.Append(nil, []byte("raw \xff bytes\r\n"))
		if string(got) != "raw \xff bytes\r\n" {
			t.Errorf("%q changed the bytes: %q", name, got)
		}
	}
}

// FuzzDecodeSplitMatchesWhole guards the stream property the terminal depends
// on: the same bytes decode to the same UTF-8 however the transport splits
// them across reads, whole in one call or in fixed-size chunks plus a tail.
func FuzzDecodeSplitMatchesWhole(f *testing.F) {
	f.Add([]byte("hello, terminal\r\n"), 1)
	f.Add([]byte{0xC4, 0xE3, 0xBA, 0xC3, 0xD6, 0xD0, 0xCE, 0xC4}, 1)             // GBK 你好中文
	f.Add([]byte{0x82, 0xB1, 0x82, 0xF1, 0x82, 0xC9, 0x82, 0xBF, 0x82, 0xCD}, 2) // Shift_JIS こんにちは
	f.Add([]byte{0xA4, 0xB3, 0xA4, 0xF3, 0xA4, 0xCB, 0xA4, 0xC1, 0xA4, 0xCF}, 3) // EUC-JP こんにちは
	f.Add([]byte{0xFF, 0xFE, 0x81, 0x00, 0xB0, 0xA1}, 4)                         // malformed tails
	f.Add([]byte("plain ascii \x1b[2J\x1b]0;title\x07"), 5)
	f.Fuzz(func(t *testing.T, data []byte, step int) {
		if step < 1 {
			step = 1
		}
		for _, name := range Encodings {
			whole := NewDecoder(name)
			wholeOut := whole.Flush(whole.Append(nil, data))

			split := NewDecoder(name)
			var splitOut []byte
			for start := 0; start < len(data); start += step {
				splitOut = split.Append(splitOut, data[start:min(start+step, len(data))])
			}
			splitOut = split.Flush(splitOut)
			if string(splitOut) != string(wholeOut) {
				t.Fatalf("%s: decode split by %d = %q, want %q", name, step, splitOut, wholeOut)
			}
		}
	})
}
