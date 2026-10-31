package capture

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

const testNonce = "abc123"

func marker(code int) string { return fmt.Sprintf("GOPAI_%s%d", testNonce, code) }

type run struct {
	display string
	res     Result
}

func feedChunks(stream []byte, sizes func(i int) int, max int) run {
	t := NewTap(testNonce, max)
	var disp []byte
	for i := 0; len(stream) > 0; i++ {
		n := min(max1(sizes(i)), len(stream))
		d, _ := t.Feed(stream[:n])
		disp = append(disp, d...)
		stream = stream[n:]
	}
	disp = append(disp, t.Abort()...)
	return run{string(disp), t.Result()}
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func TestTapBasic(t *testing.T) {
	tests := []struct {
		name, in, display, output string
		code                      int
		done                      bool
	}{
		{"lf", "hi\n\n" + marker(0) + "\nPROMPT$ ", "hi\nPROMPT$ ", "hi", 0, true},
		{"crlf", "hi\r\n\r\n" + marker(7) + "\r\nPROMPT$ ", "hi\r\nPROMPT$ ", "hi", 7, true},
		{"no trailing newline in output", "hi\n" + marker(1) + "\n", "hi", "hi", 1, true},
		{"code 255", "x\n" + marker(255) + "\n", "x", "x", 255, true},
		{"code 1000", "\n" + marker(1000) + "\n", "", "", 1000, true},
		{"empty output", "\n" + marker(0) + "\n", "", "", 0, true},
		{"fake wrong nonce", "a\nGOPAI_zzz0\nb", "a\nGOPAI_zzz0\nb", "a\nGOPAI_zzz0\nb", -1, false},
		{"marker without leading newline", "aGOPAI_" + testNonce + "0\n", "aGOPAI_" + testNonce + "0\n", "aGOPAI_" + testNonce + "0", -1, false},
		{"no digits", "\nGOPAI_" + testNonce + "x\n", "\nGOPAI_" + testNonce + "x\n", "\nGOPAI_" + testNonce + "x", -1, false},
		{"echo pieces do not complete", "printf '\\n%s%s%d\\n' GOPAI_ " + testNonce + " $?\r\n", "printf '\\n%s%s%d\\n' GOPAI_ " + testNonce + " $?\r\n", "printf '\\n%s%s%d\\n' GOPAI_ " + testNonce + " $?", -1, false},
		{"incomplete marker", "a\n" + marker(3), "a\n" + marker(3), "a\n" + marker(3), -1, false},
		{"ansi", "\x1b[31mred\x1b[0m \x1b]0;title\x07ok\x1b]8;;http://x\x1b\\!\n" + "\n" + marker(0) + "\n", "\x1b[31mred\x1b[0m \x1b]0;title\x07ok\x1b]8;;http://x\x1b\\!\n", "red ok!", 0, true},
		{"cr overwrite", "10%\r50%\r100%\ndone\n\n" + marker(0) + "\n", "10%\r50%\r100%\ndone\n", "100%\ndone", 0, true},
		{"backspace", "abc\b\bXY\n\n" + marker(0) + "\n", "abc\b\bXY\n", "aXY", 0, true},
		{"erase with backspace", "ab\b \b\n\n" + marker(0) + "\n", "ab\b \b\n", "a", 0, true},
		{"EL", "hello\r\x1b[Kbye\n\n" + marker(0) + "\n", "hello\r\x1b[Kbye\n", "bye", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tp := NewTap(testNonce, 1000)
			d, done := tp.Feed([]byte(tc.in))
			disp := string(d) + string(tp.Abort())
			if tc.done {
				disp = string(d)
			}
			if disp != tc.display || done != tc.done {
				t.Errorf("display=%q done=%v; want %q %v", disp, done, tc.display, tc.done)
			}
			r := tp.Result()
			if r.Output != tc.output || r.ExitCode != tc.code || r.Done != tc.done {
				t.Errorf("result=%+v; want output %q code %d done %v", r, tc.output, tc.code, tc.done)
			}
		})
	}
}

func TestChunkingInvariant(t *testing.T) {
	streams := []string{
		"echoed line\r\nhi\r\nthere\r\n\r\n" + marker(5) + "\r\n$ ",
		"a\n\r\nGOPAI_" + testNonce + "\nGOPAI_x\n\n" + marker(0) + "\n" + "\n" + marker(9) + "\n",
		"\x1b[1;32mhi\x1b[m\r\n\r\n" + marker(0) + "\r\n\x1b]0;t\x1b\\",
		"x\r\n\r\nGOPAI_" + testNonce + "12\n",
		strings.Repeat("line\r\n", 50) + "\r\n" + marker(2) + "\r\n",
	}
	for si, s := range streams {
		whole := feedChunks([]byte(s), func(int) int { return len(s) }, 200)
		for _, size := range []int{1, 2, 3, 5, 7, 13} {
			got := feedChunks([]byte(s), func(int) int { return size }, 200)
			if got != whole {
				t.Errorf("stream %d size %d: %+v != %+v", si, size, got, whole)
			}
		}
	}
}

func TestMarkerSplitEverywhere(t *testing.T) {
	s := []byte("out\r\n\r\n" + marker(42) + "\r\nrest")
	for i := 0; i <= len(s); i++ {
		tp := NewTap(testNonce, 100)
		d1, _ := tp.Feed(s[:i])
		d1 = append([]byte(nil), d1...)
		d2, done := tp.Feed(s[i:])
		if string(d1)+string(d2) != "out\r\nrest" || !done {
			t.Fatalf("split %d: %q %v", i, string(d1)+string(d2), done)
		}
		if r := tp.Result(); r.ExitCode != 42 || r.Output != "out" {
			t.Fatalf("split %d: %+v", i, r)
		}
	}
}

func TestHoldIsMinimal(t *testing.T) {
	tp := NewTap(testNonce, 100)
	d, _ := tp.Feed([]byte("abc\r\nGOPAI_ab"))
	if string(d) != "abc" {
		t.Fatalf("got %q", d)
	}
	d, _ = tp.Feed([]byte("X"))
	if string(d) != "\r\nGOPAI_abX" {
		t.Fatalf("got %q", d)
	}
}

func TestPassThroughAfterDone(t *testing.T) {
	tp := NewTap(testNonce, 100)
	_, done := tp.Feed([]byte("\n" + marker(0) + "\n"))
	if !done {
		t.Fatal("not done")
	}
	d, done := tp.Feed([]byte("\n" + marker(1) + "\nprompt"))
	if !done || string(d) != "\n"+marker(1)+"\nprompt" {
		t.Fatalf("got %q %v", d, done)
	}
	if tp.Result().ExitCode != 0 || tp.Result().Output != "" {
		t.Fatalf("%+v", tp.Result())
	}
}

func TestAbort(t *testing.T) {
	tp := NewTap(testNonce, 100)
	d, _ := tp.Feed([]byte("partial\r\nGOPAI_ab"))
	held := tp.Abort()
	if string(d)+string(held) != "partial\r\nGOPAI_ab" {
		t.Fatalf("%q %q", d, held)
	}
	d, done := tp.Feed([]byte("more"))
	if done || string(d) != "more" {
		t.Fatal("pass through after abort")
	}
	if r := tp.Result(); r.Done || r.ExitCode != -1 || !strings.HasPrefix(r.Output, "partial") {
		t.Fatalf("%+v", r)
	}
}

func TestEchoDropped(t *testing.T) {
	sent := Wrap("echo hi\necho there", testNonce)
	echo := strings.ReplaceAll(string(sent), "\n", "\r\n")
	tp := NewTap(testNonce, 1000)
	tp.SetEcho(sent)
	tp.Feed([]byte(echo + "hi\r\nthere\r\n\r\n" + marker(0) + "\r\n"))
	if r := tp.Result(); r.Output != "hi\nthere" || r.ExitCode != 0 {
		t.Fatalf("%+v", r)
	}
	// No echo (stty -echo): nothing matches, nothing is dropped.
	tp = NewTap(testNonce, 1000)
	tp.SetEcho(sent)
	tp.Feed([]byte("hi\r\n\r\n" + marker(0) + "\r\n"))
	if r := tp.Result(); r.Output != "hi" {
		t.Fatalf("%+v", r)
	}
}

func TestDisplayShowsTheLogicalCommandInsteadOfTheWrapper(t *testing.T) {
	sent := Wrap("echo hi", testNonce)
	echo := strings.ReplaceAll(string(sent), "\n", "\r\n")
	tp := NewTap(testNonce, 1000)
	tp.SetEcho(sent)
	tp.SetDisplay("echo hi")
	display, done := tp.Feed([]byte(echo + "hi\r\n\r\n" + marker(0) + "\r\nPROMPT$ "))
	if !done || string(display) != "echo hi\r\nhi\r\nPROMPT$ " {
		t.Fatalf("display = %q, done %v", display, done)
	}
	if r := tp.Result(); r.Output != "hi" || r.ExitCode != 0 {
		t.Fatalf("result = %+v", r)
	}
}

func TestDisplayReplacesAContinuationPromptEcho(t *testing.T) {
	sent := Wrap("echo hi\necho there", testNonce)
	echo := strings.ReplaceAll(string(sent), "\n", "\r\n")
	echo = strings.Replace(echo, "}; printf", "> }; printf", 1)
	tp := NewTap(testNonce, 1000)
	tp.SetEcho(sent)
	tp.SetDisplay("echo hi\necho there")
	display, done := tp.Feed([]byte(echo + "hi\r\nthere\r\n\r\n" + marker(0) + "\r\n"))
	if !done || string(display) != "echo hi\r\necho there\r\nhi\r\nthere\r\n" {
		t.Fatalf("display = %q, done %v", display, done)
	}
}

func TestDisplayFlushesHeldEchoOnAbort(t *testing.T) {
	sent := Wrap("echo hi", testNonce)
	echo := strings.ReplaceAll(string(sent), "\n", "\r\n")
	partial := echo[:len(echo)/2]
	tp := NewTap(testNonce, 1000)
	tp.SetEcho(sent)
	tp.SetDisplay("echo hi")
	if display, _ := tp.Feed([]byte(partial)); len(display) != 0 {
		t.Fatalf("displayed %q before the echo was complete", display)
	}
	if got := string(tp.Abort()); got != partial {
		t.Fatalf("abort display = %q, want the held echo %q", got, partial)
	}
}

func TestDisplayFallsBackToTheRawEchoWhenTheShellDiffers(t *testing.T) {
	sent := Wrap("echo hi", testNonce)
	echo := strings.ReplaceAll(string(sent), "\n", "\r\n")
	raw := strings.Replace(echo, "echo hi", "echo bye", 1)
	tp := NewTap(testNonce, 1000)
	tp.SetEcho(sent)
	tp.SetDisplay("echo hi")
	display, done := tp.Feed([]byte(raw + "bye\r\n\r\n" + marker(0) + "\r\n"))
	if !done || string(display) != raw+"bye\r\n" {
		t.Fatalf("display = %q, done %v; want raw echo", display, done)
	}
}

func TestDisplayIsChunkIndependent(t *testing.T) {
	sent := Wrap("echo hi\necho there", testNonce)
	stream := strings.ReplaceAll(string(sent), "\n", "\r\n") + "hi\r\nthere\r\n\r\n" + marker(0) + "\r\nPROMPT$ "
	whole := feedDisplay([]byte(stream), len(stream))
	for _, size := range []int{1, 2, 3, 5, 7, 13} {
		if got := feedDisplay([]byte(stream), size); got != whole {
			t.Errorf("size %d: display %q, want %q", size, got, whole)
		}
	}
}

func feedDisplay(stream []byte, size int) string {
	tp := NewTap(testNonce, 1000)
	sent := Wrap("echo hi\necho there", testNonce)
	tp.SetEcho(sent)
	tp.SetDisplay("echo hi\necho there")
	var display []byte
	for len(stream) > 0 {
		n := min(size, len(stream))
		d, _ := tp.Feed(stream[:n])
		display = append(display, d...)
		stream = stream[n:]
	}
	return string(display)
}

func TestTruncation(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&sb, "line %04d\n", i)
	}
	stream := sb.String() + "\n" + marker(0) + "\n"
	for _, size := range []int{len(stream), 1, 17} {
		r := feedChunks([]byte(stream), func(int) int { return size }, 200)
		if !r.res.Truncated || !r.res.Done {
			t.Fatalf("size %d: %+v", size, r.res)
		}
		if !strings.HasPrefix(r.res.Output, "line 0000\n") || !strings.HasSuffix(r.res.Output, "line 0999") ||
			!strings.Contains(r.res.Output, "bytes omitted ...]") {
			t.Fatalf("size %d: %q", size, r.res.Output)
		}
		if len(r.res.Output) > 200+40 {
			t.Fatalf("too long: %d", len(r.res.Output))
		}
		if !strings.Contains(r.res.Output, "[... 9800 bytes omitted ...]") {
			t.Fatalf("omitted count: %q", r.res.Output)
		}
	}
	short := feedChunks([]byte("small\n\n"+marker(0)+"\n"), func(int) int { return 3 }, 200)
	if short.res.Truncated || short.res.Output != "small" {
		t.Fatalf("%+v", short.res)
	}
}

func TestLongLineBounded(t *testing.T) {
	tp := NewTap(testNonce, 100)
	tp.Feed(bytes.Repeat([]byte("x"), 1<<20))
	tp.Feed([]byte("\n\n" + marker(0) + "\n"))
	r := tp.Result()
	if !r.Truncated || len(r.Output) > 200 || !r.Done {
		t.Fatalf("%d %+v", len(r.Output), r.Truncated)
	}
}

func TestWrapShape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"echo hi", "{ echo hi\n}; printf '\\n%s%s%d\\n' GOPAI_ n1 $?\n"},
		{"echo hi # c\n\n", "{ echo hi # c\n}; printf '\\n%s%s%d\\n' GOPAI_ n1 $?\n"},
		{"echo hi \\", "{ echo hi\n}; printf '\\n%s%s%d\\n' GOPAI_ n1 $?\n"},
		{"echo hi \\\\", "{ echo hi \\\\\n}; printf '\\n%s%s%d\\n' GOPAI_ n1 $?\n"},
		{"", "{ :\n}; printf '\\n%s%s%d\\n' GOPAI_ n1 $?\n"},
		{"a\r\nb\rc\x15d\x03\x1b", "{ a\nb\ncd\n}; printf '\\n%s%s%d\\n' GOPAI_ n1 $?\n"},
		{"a", "{ a\n}; printf '\\n%s%s%d\\n' GOPAI_ n-1;x $?\n"},
	}
	for i, c := range cases {
		nonce := "n1"
		if i == len(cases)-1 {
			nonce = "n-1;x"
			c.want = "{ a\n}; printf '\\n%s%s%d\\n' GOPAI_ n1x $?\n"
		}
		if got := string(Wrap(c.in, nonce)); got != c.want {
			t.Errorf("%q: got %q want %q", c.in, got, c.want)
		}
	}
	w := Wrap("echo hi", testNonce)
	if bytes.Contains(w, []byte("GOPAI_"+testNonce)) {
		t.Fatal("wrapper contains contiguous marker")
	}
	for _, b := range w {
		if b < 0x20 && b != '\n' && b != '\t' {
			t.Fatalf("control byte %x", b)
		}
	}
}

func TestEchoOfWrapperNeverCompletes(t *testing.T) {
	w := Wrap("echo hi", testNonce)
	echo := strings.ReplaceAll(string(w), "\n", "\r\n")
	tp := NewTap(testNonce, 100)
	for i := 0; i < len(echo); i++ {
		if _, done := tp.Feed([]byte{echo[i]}); done {
			t.Fatal("completed by echo")
		}
	}
}

func FuzzTapChunking(f *testing.F) {
	f.Add([]byte("hi\r\n\r\n"+marker(0)+"\r\n"), uint8(3))
	f.Add([]byte("\x1b[31m\x1b]0;a\x07x\b\r\n\nGOPAI_abc1239\n"), uint8(1))
	f.Add([]byte("\r\nGOPAI_abc\r\nGOPAI_abc12\r"), uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, size uint8) {
		whole := feedChunks(data, func(int) int { return len(data) + 1 }, 128)
		part := feedChunks(data, func(int) int { return int(size) }, 128)
		if whole != part {
			t.Fatalf("whole %+v part %+v", whole, part)
		}
		r := rand.New(rand.NewSource(int64(size)))
		rnd := feedChunks(data, func(int) int { return r.Intn(9) }, 128)
		if rnd != whole {
			t.Fatalf("random %+v whole %+v", rnd, whole)
		}
	})
}

func BenchmarkTap1MiB(b *testing.B) {
	line := []byte("\x1b[32mthe quick brown fox jumps over the lazy dog\x1b[0m 0123456789\r\n")
	var stream []byte
	for len(stream) < 1<<20 {
		stream = append(stream, line...)
	}
	stream = append(stream, "\r\n"+marker(0)+"\r\n"...)
	b.SetBytes(int64(len(stream)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tp := NewTap(testNonce, 64<<10)
		for p := stream; len(p) > 0; {
			n := min(4096, len(p))
			tp.Feed(p[:n])
			p = p[n:]
		}
		if !tp.Result().Done {
			b.Fatal("not done")
		}
	}
}
