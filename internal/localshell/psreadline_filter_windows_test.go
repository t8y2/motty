//go:build windows

package localshell

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
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

func withWindowsBuild(t *testing.T, build uint32) {
	t.Helper()
	previous := windowsBuildNumberFn
	windowsBuildNumberFn = func() uint32 { return build }
	t.Cleanup(func() { windowsBuildNumberFn = previous })
}

func TestNeedsLegacyPSReadLineColorFix(t *testing.T) {
	cases := []struct {
		name        string
		passthrough bool
		program     string
		build       uint32
		want        bool
	}{
		{name: "win10_bundled_powershell_full_path", passthrough: true, program: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, build: 19045, want: true},
		{name: "win10_bundled_powershell_mixed_case", passthrough: true, program: `PowerShell.EXE`, build: 19045, want: true},
		{name: "win10_bundled_cmd", passthrough: true, program: `C:\Windows\system32\cmd.exe`, build: 19045, want: false},
		{name: "win10_bundled_pwsh7", passthrough: true, program: `C:\Program Files\PowerShell\7\pwsh.exe`, build: 19045, want: false},
		{name: "win10_bundled_git_bash", passthrough: true, program: `C:\Program Files\Git\bin\bash.exe`, build: 19045, want: false},
		{name: "win10_bundled_wsl", passthrough: true, program: `C:\Windows\System32\wsl.exe`, build: 19045, want: false},
		{name: "win10_inbox_conpty_powershell", passthrough: false, program: `powershell.exe`, build: 19045, want: false},
		{name: "server2022_fixed_build", passthrough: true, program: `powershell.exe`, build: legacyPSReadLineBuild, want: false},
		{name: "win11_fixed_build", passthrough: true, program: `powershell.exe`, build: 22631, want: false},
		{name: "unknown_build", passthrough: true, program: `powershell.exe`, build: 0, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsLegacyPSReadLineColorFix(tc.passthrough, tc.program, tc.build); got != tc.want {
				t.Fatalf("needsLegacyPSReadLineColorFix(%v, %q, %d) = %v, want %v",
					tc.passthrough, tc.program, tc.build, got, tc.want)
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
				got, err := io.ReadAll(newPSReadLineColorFilter(&chunkedReader{chunks: [][]byte{[]byte(tc.in)}, eofOn: eofOn}, nil))
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
					got, err := io.ReadAll(newPSReadLineColorFilter(src, nil))
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
				filter := newPSReadLineColorFilter(&chunkedReader{chunks: oneByteChunks, eofOn: eofOn}, nil)
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

func TestPSReadLineColorFilterSkipsRewriteWhenPredicateIsFalse(t *testing.T) {
	input := "child\x1b[37m\x1b[40m   \x1b[0m"
	filter := newPSReadLineColorFilter(&chunkedReader{chunks: [][]byte{[]byte(input)}}, func() bool { return false })
	got, err := io.ReadAll(filter)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != input {
		t.Fatalf("got %q, want %q unchanged when predicate is false", got, input)
	}
}

func TestPSReadLineColorFilterFlushesTrailingPrefixWhenIdle(t *testing.T) {
	cases := []struct {
		name  string
		write string
	}{
		{name: "bare_esc", write: "\x1b"},
		{name: "text_then_esc", write: "prompt> \x1b"},
		{name: "bare_sgr_37", write: "\x1b[37m"},
		{name: "text_then_sgr_37", write: "prompt> \x1b[37m"},
		{name: "text_then_partial_40", write: "prompt> \x1b[37m\x1b[4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pr, pw := io.Pipe()
			filter := newPSReadLineColorFilterWithTimeout(pr, nil, 15*time.Millisecond)
			t.Cleanup(func() {
				filter.Close()
				_ = pw.Close()
				_ = pr.Close()
			})

			go func() {
				_, _ = pw.Write([]byte(tc.write))
			}()

			done := make(chan string, 1)
			go func() {
				var collected bytes.Buffer
				buf := make([]byte, 64)
				for collected.Len() < len(tc.write) {
					n, err := filter.Read(buf)
					if n > 0 {
						collected.Write(buf[:n])
					}
					if err != nil {
						break
					}
				}
				done <- collected.String()
			}()

			select {
			case got := <-done:
				if got != tc.write {
					t.Fatalf("flushed %q, want %q", got, tc.write)
				}
			case <-time.After(time.Second):
				t.Fatalf("trailing prefix %q was held without flushing", tc.write)
			}
		})
	}

	// Verify a split sequence arriving immediately still rewrites before the
	// flush timeout expires.
	t.Run("split_within_timeout_still_rewrites", func(t *testing.T) {
		pr, pw := io.Pipe()
		filter := newPSReadLineColorFilterWithTimeout(pr, nil, 200*time.Millisecond)
		t.Cleanup(func() {
			filter.Close()
			_ = pw.Close()
			_ = pr.Close()
		})

		go func() {
			_, _ = pw.Write([]byte("\x1b[37m"))
			_, _ = pw.Write([]byte("\x1b[40m "))
		}()

		const want = "\x1b[39;49m "
		done := make(chan string, 1)
		go func() {
			var collected bytes.Buffer
			buf := make([]byte, 64)
			for collected.Len() < len(want) {
				n, err := filter.Read(buf)
				if n > 0 {
					collected.Write(buf[:n])
				}
				if err != nil {
					break
				}
			}
			done <- collected.String()
		}()

		select {
		case got := <-done:
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for split sequence rewrite")
		}
	})
}

// TestBundledConPTYColorProbeChild is run as a child process inside ConPTY to
// emit an explicit "\x1b[37m\x1b[40m" sequence while staying alive as the
// foreground process until stdin receives a byte.
func TestBundledConPTYColorProbeChild(t *testing.T) {
	marker := os.Getenv("MOTTY_COLOR_PROBE_MARKER")
	if marker == "" {
		t.Skip("color probe child only")
	}
	fmt.Printf("%s\x1b[37m\x1b[40m   \x1b[0m\r\n", marker)
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
	os.Exit(0)
}

// TestBundledConPTYLegacyPSReadLineFilterPath exercises the real bundled ConPTY
// path regardless of the host Windows build (including Windows 11 CI runners).
func TestBundledConPTYLegacyPSReadLineFilterPath(t *testing.T) {
	powershellPath, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("powershell.exe not installed")
	}
	cmdPath, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Skip("cmd.exe not installed")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	findMarkerCells := func(screen *term.Terminal, marker string) ([]term.Cell, bool) {
		_, rows := screen.Size()
		for y := 0; y < rows; y++ {
			row := screen.Row(y)
			text := rowText(row)
			if col := strings.Index(text, marker); col >= 0 && col+len(marker)+3 <= len(row.Cells) {
				return row.Cells[col+len(marker) : col+len(marker)+3], true
			}
		}
		return nil, false
	}

	runPowerShellWrite := func(t *testing.T, build uint32, wantBG term.Color) {
		t.Helper()
		conptyTree(t)
		withWindowsBuild(t, build)
		shell, wakes := startWindows(t, Config{
			Shell:          powershellPath,
			Cols:           120,
			Rows:           30,
			DirectoryToken: "psrl-path-test",
		})
		screen := term.New(120, 30, 100)
		screen.SetPromptToken("psrl-path-test")
		screen.SetReply(func(reply []byte) { shell.Write(append([]byte(nil), reply...)) })
		promptReady := false
		screen.SetPromptReady(func() { promptReady = true })

		wait := func(timeout time.Duration, cond func() bool, desc string) {
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
					t.Fatalf("timed out waiting for %s; screen=%q", desc, screen.Text())
				}
			}
		}
		wait(15*time.Second, func() bool { return promptReady }, "PowerShell prompt")

		const marker = "PSRLBGCHECK:"
		// Concatenate the marker dynamically so the echoed command line does not
		// match marker before the command output runs.
		cmd := "[Console]::Write(\"PSRLBG\" + \"CHECK:$([char]27)[37m$([char]27)[40m   $([char]27)[0m`r`n\")\r"
		if !shell.Write([]byte(cmd)) {
			t.Fatal("shell refused command")
		}
		var cells []term.Cell
		wait(10*time.Second, func() bool {
			var ok bool
			cells, ok = findMarkerCells(screen, marker)
			return ok
		}, "PowerShell marker output")

		for i, cell := range cells {
			if cell.BG != wantBG {
				t.Fatalf("build=%d cell[%d] (%q) BG = %v, want %v", build, i, cell.R, cell.BG, wantBG)
			}
		}
	}

	t.Run("powershell_filter_disabled_preserves_ansi_black", func(t *testing.T) {
		runPowerShellWrite(t, 22631, term.Indexed(0))
	})

	t.Run("powershell_filter_enabled_normalizes_to_default", func(t *testing.T) {
		runPowerShellWrite(t, 19045, 0)
	})

	t.Run("cmd_not_filtered_on_win10_build", func(t *testing.T) {
		conptyTree(t)
		withWindowsBuild(t, 19045)
		const marker = "CMDBGCHECK:"
		shell, wakes := startWindows(t, Config{
			Shell: cmdPath,
			Cols:  120,
			Rows:  30,
			Env:   []string{"MOTTY_COLOR_PROBE_MARKER=" + marker},
		})
		screen := term.New(120, 30, 100)
		screen.SetReply(func(reply []byte) { shell.Write(append([]byte(nil), reply...)) })

		command := windows.ComposeCommandLine([]string{executable, "-test.run=^TestBundledConPTYColorProbeChild$"})
		if !shell.Write([]byte(command + "\r")) {
			t.Fatal("cmd refused probe command")
		}
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		for {
			if chunk := shell.Drain(nil); len(chunk) > 0 {
				screen.Write(chunk)
			}
			if cells, ok := findMarkerCells(screen, marker); ok {
				shell.Write([]byte("\r"))
				for i, cell := range cells {
					if cell.BG != term.Indexed(0) {
						t.Fatalf("cmd cell[%d] (%q) BG = %v, want ANSI black %v", i, cell.R, cell.BG, term.Indexed(0))
					}
				}
				return
			}
			select {
			case <-wakes:
			case <-deadline.C:
				t.Fatalf("timed out waiting for cmd probe; screen=%q", screen.Text())
			}
		}
	})

	t.Run("foreground_child_inside_powershell_not_filtered", func(t *testing.T) {
		conptyTree(t)
		withWindowsBuild(t, 19045)
		const marker = "CHILDBGCHECK:"
		shell, wakes := startWindows(t, Config{
			Shell:          powershellPath,
			Cols:           120,
			Rows:           30,
			DirectoryToken: "psrl-child-test",
			Env:            []string{"MOTTY_COLOR_PROBE_MARKER=" + marker},
		})
		screen := term.New(120, 30, 100)
		screen.SetPromptToken("psrl-child-test")
		screen.SetReply(func(reply []byte) { shell.Write(append([]byte(nil), reply...)) })
		promptReady := false
		screen.SetPromptReady(func() { promptReady = true })

		deadline := time.NewTimer(20 * time.Second)
		defer deadline.Stop()
		for {
			if chunk := shell.Drain(nil); len(chunk) > 0 {
				screen.Write(chunk)
			}
			if promptReady {
				break
			}
			select {
			case <-wakes:
			case <-deadline.C:
				t.Fatalf("timed out waiting for PowerShell prompt; screen=%q", screen.Text())
			}
		}

		cmd := fmt.Sprintf("& '%s' '-test.run=^TestBundledConPTYColorProbeChild$'\r", strings.ReplaceAll(executable, "'", "''"))
		if !shell.Write([]byte(cmd)) {
			t.Fatal("PowerShell refused child probe command")
		}
		for {
			if chunk := shell.Drain(nil); len(chunk) > 0 {
				screen.Write(chunk)
			}
			if cells, ok := findMarkerCells(screen, marker); ok {
				shell.Write([]byte("\r"))
				for i, cell := range cells {
					if cell.BG != term.Indexed(0) {
						t.Fatalf("child cell[%d] (%q) BG = %v, want ANSI black %v", i, cell.R, cell.BG, term.Indexed(0))
					}
				}
				return
			}
			select {
			case <-wakes:
			case <-deadline.C:
				t.Fatalf("timed out waiting for child probe; screen=%q", screen.Text())
			}
		}
	})
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
