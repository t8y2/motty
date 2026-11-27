//go:build windows

package localshell

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"motty/internal/term"
)

type chunkedReader struct {
	chunks [][]byte
	eofOn  bool // return io.EOF alongside the final non-empty chunk
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	for len(r.chunks) > 0 && len(r.chunks[0]) == 0 {
		r.chunks = r.chunks[1:]
	}
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if len(r.chunks[0]) == 0 {
		r.chunks = r.chunks[1:]
	}
	if len(r.chunks) == 0 && r.eofOn {
		return n, io.EOF
	}
	return n, nil
}

func TestNeedsLegacyPSReadLineColorFix(t *testing.T) {
	cases := []struct {
		name        string
		passthrough bool
		isWSL       bool
		build       uint32
		want        bool
	}{
		{name: "win10_bundled_shell", passthrough: true, isWSL: false, build: 19045, want: true},
		{name: "win10_inbox_conpty", passthrough: false, isWSL: false, build: 19045, want: false},
		{name: "win10_wsl", passthrough: true, isWSL: true, build: 19045, want: false},
		{name: "server2022_fixed_build", passthrough: true, isWSL: false, build: legacyPSReadLineBuild, want: false},
		{name: "win11_fixed_build", passthrough: true, isWSL: false, build: 22631, want: false},
		{name: "unknown_build", passthrough: true, isWSL: false, build: 0, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsLegacyPSReadLineColorFix(tc.passthrough, tc.isWSL, tc.build); got != tc.want {
				t.Fatalf("needsLegacyPSReadLineColorFix(%v, %v, %d) = %v, want %v",
					tc.passthrough, tc.isWSL, tc.build, got, tc.want)
			}
		})
	}
}

func TestPSReadLineColorFilterRewritesLegacyDefaultPair(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "spaces_and_erase_in_render",
			in:   "\x1b[?25l\x1b[1;20H\x1b[93mhello\x1b[37m\x1b[40m   \x1b[37m\x1b[40m\x1b[0m\x1b[1;28H\x1b[?25h",
			want: "\x1b[?25l\x1b[1;20H\x1b[93mhello\x1b[39;49m   \x1b[39;49m\x1b[0m\x1b[1;28H\x1b[?25h",
		},
		{
			name: "escape_clear_line",
			in:   "\x1b[?25l\x1b[1;20H\x1b[37m\x1b[40m        \x1b[0m\x1b[1;20H\x1b[?25h",
			want: "\x1b[?25l\x1b[1;20H\x1b[39;49m        \x1b[0m\x1b[1;20H\x1b[?25h",
		},
		{
			name: "overlapping_37m_prefix",
			in:   "\x1b[37m\x1b[37m\x1b[40m ",
			want: "\x1b[37m\x1b[39;49m ",
		},
		{
			name: "false_partial_prefix_preserved",
			in:   "abc\x1b[37m\x1b[41mred\x1b[0m",
			want: "abc\x1b[37m\x1b[41mred\x1b[0m",
		},
		{
			name: "trailing_partial_prefix_flushed_at_eof",
			in:   "tail\x1b[37m\x1b[4",
			want: "tail\x1b[37m\x1b[4",
		},
		{
			name: "explicit_non_psreadline_black_bg_untouched",
			in:   "\x1b[40mA\x1b[37;40mB\x1b[31m\x1b[40mC\x1b[48;5;0mD\x1b[0m",
			want: "\x1b[40mA\x1b[37;40mB\x1b[31m\x1b[40mC\x1b[48;5;0mD\x1b[0m",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, eofOn := range []bool{false, true} {
				// Test single-chunk read and every possible 2-way split + 1-byte reads.
				got, err := io.ReadAll(newPSReadLineColorFilter(&chunkedReader{chunks: [][]byte{[]byte(tc.in)}, eofOn: eofOn}))
				if err != nil {
					t.Fatalf("full read (eofOn=%v): %v", eofOn, err)
				}
				if string(got) != tc.want {
					t.Fatalf("full read (eofOn=%v) = %q, want %q", eofOn, got, tc.want)
				}

				for split := 1; split < len(tc.in); split++ {
					src := &chunkedReader{
						chunks: [][]byte{[]byte(tc.in[:split]), []byte(tc.in[split:])},
						eofOn:  eofOn,
					}
					got, err := io.ReadAll(newPSReadLineColorFilter(src))
					if err != nil {
						t.Fatalf("split=%d (eofOn=%v): %v", split, eofOn, err)
					}
					if string(got) != tc.want {
						t.Fatalf("split=%d (eofOn=%v) = %q, want %q", split, eofOn, got, tc.want)
					}
				}

				// 1-byte chunks and 1-byte caller buffer exercise every boundary and
				// partial-copy path.
				oneByteChunks := make([][]byte, len(tc.in))
				for i := range tc.in {
					oneByteChunks[i] = []byte{tc.in[i]}
				}
				filter := newPSReadLineColorFilter(&chunkedReader{chunks: oneByteChunks, eofOn: eofOn})
				var out bytes.Buffer
				var b [1]byte
				for {
					n, err := filter.Read(b[:])
					if n > 0 {
						out.Write(b[:n])
					}
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatalf("1-byte read (eofOn=%v): %v", eofOn, err)
					}
				}
				if out.String() != tc.want {
					t.Fatalf("1-byte read (eofOn=%v) = %q, want %q", eofOn, out.String(), tc.want)
				}
			}
		})
	}
}

func TestBundledConPTYPowerShellSpacesAndDeleteKeepDefaultBackground(t *testing.T) {
	conptyTree(t)
	program, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("powershell.exe not installed")
	}
	shell, wakes := startWindows(t, Config{
		Shell:          program,
		Cols:           100,
		Rows:           30,
		DirectoryToken: "psrl-bg-test",
	})
	screen := term.New(100, 30, 100)
	screen.SetPromptToken("psrl-bg-test")
	screen.SetReply(func(reply []byte) {
		shell.Write(append([]byte(nil), reply...))
	})
	promptReady := false
	screen.SetPromptReady(func() { promptReady = true })

	waitUntil := func(timeout time.Duration, cond func() bool, desc string) {
		t.Helper()
		deadline := time.NewTimer(timeout)
		defer deadline.Stop()
		for {
			if chunk := shell.Drain(nil); len(chunk) > 0 {
				screen.Write(chunk)
			}
			if cond() {
				return
			}
			select {
			case <-wakes:
			case <-deadline.C:
				t.Fatalf("timed out waiting for %s; cursor=%+v screen=%q", desc, screen.Cursor(), screen.Text())
			}
		}
	}

	waitUntil(15*time.Second, func() bool { return promptReady }, "PowerShell prompt")
	promptCursor := screen.Cursor()

	assertNoNonDefaultBG := func(stage string) {
		t.Helper()
		row := screen.Row(promptCursor.Y)
		for x, cell := range row.Cells {
			if cell.BG != 0 {
				t.Fatalf("%s: column %d (%q) has non-default background %v (want default 0); row=%q",
					stage, x, cell.R, cell.BG, rowText(row))
			}
		}
	}

	// Type command text followed by spaces.
	if !shell.Write([]byte("hello   ")) {
		t.Fatal("shell refused typed input")
	}
	waitUntil(8*time.Second, func() bool {
		c := screen.Cursor()
		return c.Y == promptCursor.Y && c.X == promptCursor.X+8 && strings.Contains(rowText(screen.Row(promptCursor.Y)), "hello")
	}, "typed text and spaces")
	assertNoNonDefaultBG("after typing spaces")

	// Delete 6 characters with Backspace (the 3 spaces and "llo").
	for range 6 {
		if !shell.Write([]byte{0x7f}) {
			t.Fatal("shell refused Backspace")
		}
	}
	waitUntil(8*time.Second, func() bool {
		c := screen.Cursor()
		return c.Y == promptCursor.Y && c.X == promptCursor.X+2
	}, "backspace deletion")
	assertNoNonDefaultBG("after backspace deletion")

	// Clear the remaining input with Escape.
	if !shell.Write([]byte{0x1b}) {
		t.Fatal("shell refused Escape")
	}
	waitUntil(8*time.Second, func() bool {
		c := screen.Cursor()
		return c.Y == promptCursor.Y && c.X == promptCursor.X
	}, "escape clear")
	assertNoNonDefaultBG("after escape clear")
}

func rowText(row term.Row) string {
	var b strings.Builder
	for _, cell := range row.Cells {
		if cell.R == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(cell.R)
		}
	}
	return strings.TrimRight(b.String(), " ")
}
