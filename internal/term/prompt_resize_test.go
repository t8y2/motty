package term

import (
	"strings"
	"testing"
)

// A shell's line editor redraws its prompt after a resize from the rows it last
// drew it on: it clears the current row, moves up as many rows as it had drawn,
// and writes the prompt again. Reflowing those rows underneath it moves the text
// the redraw is based on, and the prompt is left on screen a second time.
func TestResizeLeavesTheShellPromptForTheShellToRedraw(t *testing.T) {
	prompt := "Tdaofei@user-System-Product-Name:/data/Tdaofei/project/DefMamba-main$ (base) "
	visible := strings.TrimRight(prompt, " ") // the retained text drops trailing blanks
	promptMarker := "\x1b]133;A;motty=t\x07"
	for _, test := range []struct {
		name   string
		cols   int
		resize int
		redraw string
	}{
		// Bash 5.2 at 118 columns, prompt printed on one row, then narrowed:
		// it has not seen the prompt wrap and redraws it from the current row.
		{"redrawn as one row", 118, 65,
			"\r\x1b[K\r" + prompt[:66] + "\r" + prompt[65:]},
		// The prompt was already wrapped at 65, so the redraw moves up one row
		// before writing the prompt at 55 columns.
		{"redrawn after wrapping", 65, 55,
			"\r\x1b[K\r\x1b[A\x1b[K\r" + prompt[:58] + "\r" + prompt[55:]},
	} {
		t.Run(test.name, func(t *testing.T) {
			terminal := New(test.cols, 12, 100)
			terminal.SetPromptToken("t")
			feed(terminal, promptMarker+prompt)
			terminal.Resize(test.resize, 12)
			feed(terminal, test.redraw)
			text := strings.TrimRight(string(terminal.AppendRetainedText(nil)), " ")
			if text != visible {
				t.Fatalf("the redraw left %q instead of the one prompt %q", text, visible)
			}
		})
	}
}

// A prompt leaves its layout to the shell, but the output a command writes after
// it is the terminal's to reflow: OSC 133 C ends the prompt and the output a
// command writes is wrapped again at the new width.
func TestResizeReflowsOutputAfterThePromptEnds(t *testing.T) {
	terminal := New(40, 8, 100)
	terminal.SetPromptToken("t")
	feed(terminal, "\x1b]133;A;motty=t\x07$ \x1b]133;C;motty=t\x07")
	line := "output that is longer than the terminal is wide"
	feed(terminal, line)
	terminal.Resize(20, 8)
	if text := string(terminal.AppendRetainedText(nil)); !strings.Contains(text, line) {
		t.Fatalf("the output was not reflowed at the new width:\n%s", text)
	}
}

// A resize at a prompt leaves the prompt and the input the shell has not
// redrawn yet on screen, cut only by the new width: readers such as the command
// recorder and completion see the line until the shell replaces it.
func TestResizeKeepsThePromptOnScreenUntilTheShellRedraws(t *testing.T) {
	prompt := "Tdaofei@user-System-Product-Name:/data/Tdaofei/project/DefMamba-main$ (base) "
	terminal := New(118, 12, 100)
	terminal.SetPromptToken("t")
	feed(terminal, "\x1b]133;A;motty=t\x07"+prompt)
	terminal.Resize(65, 12)
	text := string(terminal.AppendRetainedText(nil))
	if len(text) < 32 || !strings.HasPrefix(prompt, text) {
		t.Fatalf("the prompt is %q right after the resize, want its visible %q", text, prompt[:32])
	}
}

// Narrowing and widening again before the shell redraws leaves the prompt and
// the cursor where the shell believes they are: the rows are kept aside and put
// back, so nothing under the new width is lost.
func TestResizeBackToItsWidthRestoresThePrompt(t *testing.T) {
	prompt := "Tdaofei@user-System-Product-Name:/data/Tdaofei/project/DefMamba-main$ (base) "
	terminal := New(118, 12, 100)
	terminal.SetPromptToken("t")
	feed(terminal, "\x1b]133;A;motty=t\x07"+prompt)
	terminal.Resize(60, 12)
	terminal.Resize(118, 12)
	text := string(terminal.AppendRetainedText(nil))
	if text != strings.TrimRight(prompt, " ") {
		t.Fatalf("the prompt came back as %q", text)
	}
	if terminal.cx != len(prompt) {
		t.Fatalf("the cursor is at column %d, want it after the prompt at %d", terminal.cx, len(prompt))
	}
}

// Typing at a prompt before the shell answers a resize is not the shell's
// redraw: the echoed input must not drop the prompt the shell has not replaced
// yet. The redraw that follows replaces the line exactly once.
func TestResizeKeepsThePromptWhenInputArrivesFirst(t *testing.T) {
	prompt := "user@host:~$ "
	terminal := New(60, 8, 100)
	terminal.SetPromptToken("t")
	feed(terminal, "\x1b]133;A;motty=t\x07"+prompt)
	terminal.Resize(40, 8)
	feed(terminal, "ls")
	if text := string(terminal.AppendRetainedText(nil)); !strings.Contains(text, prompt+"ls") {
		t.Fatalf("the echoed input dropped the prompt: %q", text)
	}
	feed(terminal, "\r\x1b[K"+prompt+"ls")
	if text := strings.TrimRight(string(terminal.AppendRetainedText(nil)), " "); text != prompt+"ls" {
		t.Fatalf("the redraw left %q instead of the one prompt and input %q", text, prompt+"ls")
	}
}

// Input that wraps below the prompt is still echoed text, not the redraw: it
// lands after the prompt's first row and must not clear the prompt.
func TestResizeKeepsThePromptWhenEchoedInputWraps(t *testing.T) {
	prompt := "$ "
	terminal := New(20, 6, 100)
	terminal.SetPromptToken("t")
	feed(terminal, "\x1b]133;A;motty=t\x07"+prompt)
	terminal.Resize(10, 6)
	input := strings.Repeat("x", 24)
	feed(terminal, input)
	text := string(terminal.AppendRetainedText(nil))
	flat := strings.ReplaceAll(text, "\n", "")
	if !strings.HasPrefix(flat, prompt+input) {
		t.Fatalf("the wrapped echo changed the prompt line: %q", text)
	}
}

// Over SSH, shell integration is injected after the login shell has already
// printed its first prompt, with the activation line's echo hidden. OSC 133 A
// therefore arrives from PROMPT_COMMAND/precmd while the cursor still stands at
// the end of the old prompt, and PS1's one-shot \r\e[2K prefix moves back to
// column zero only afterwards (#1642). Typing after a resize must still keep
// that prompt instead of taking the old cursor column for the prompt's start.
func TestResizeKeepsTheFirstInjectedPromptWhenInputArrivesFirst(t *testing.T) {
	prompt := "abc@ubuntu2404:~$ "
	terminal := New(80, 8, 100)
	terminal.SetPromptToken("t")
	// Pre-injection login output and initial unintegrated prompt.
	feed(terminal, "Last login: Thu Nov 26 15:04:26 2026 from 192.168.131.72\r\n"+prompt)
	// First integrated prompt after the hidden activation line's ready marker.
	feed(terminal, "\x1b]133;D;0;motty=t\x07\x1b]133;A;motty=t\x07\x1b]7;file://ubuntu/home/abc\x07"+
		"\x1b]7777;motty-first-prompt\x07\r\x1b[2K"+prompt+"\x1b]133;B;motty=t\x07")
	terminal.Resize(60, 8)
	feed(terminal, "docker")
	if got := terminal.RowText(1); got != prompt+"docker" {
		t.Fatalf("prompt row after typing = %q, want %q", got, prompt+"docker")
	}
	feed(terminal, "\r\x1b[K\r\x1b]7777;motty-first-prompt\x07\r\x1b[2K"+prompt+"\x1b]133;B;motty=t\x07docker")
	if got := terminal.RowText(1); got != prompt+"docker" {
		t.Fatalf("prompt row after redraw = %q, want %q", got, prompt+"docker")
	}
}

// A prompt that returns to column zero after output without a newline, or one
// narrowed past the column it began at, must still keep the prompt when echoed
// input arrives ahead of the shell's redraw.
func TestResizeKeepsPromptAfterCarriageReturnOrNarrowClamp(t *testing.T) {
	t.Run("carriage return inside PS1", func(t *testing.T) {
		terminal := New(60, 6, 100)
		terminal.SetPromptToken("t")
		feed(terminal, "partial-output")
		feed(terminal, "\x1b]133;A;motty=t\x07\r\x1b[2K$ \x1b]133;B;motty=t\x07")
		terminal.Resize(40, 6)
		feed(terminal, "docker")
		if got := terminal.RowText(0); got != "$ docker" {
			t.Fatalf("prompt row = %q, want %q", got, "$ docker")
		}
	})
	t.Run("narrowed below nonzero prompt start", func(t *testing.T) {
		terminal := New(60, 6, 100)
		terminal.SetPromptToken("t")
		feed(terminal, "01234567890123456789")
		feed(terminal, "\x1b]133;A;motty=t\x07$ \x1b]133;B;motty=t\x07")
		terminal.Resize(15, 6)
		feed(terminal, "d")
		if got := terminal.RowText(0); !strings.HasPrefix(got, "01234567890123") {
			t.Fatalf("narrowed prompt row wiped before redraw: %q", got)
		}
	})
	t.Run("indented prompt after line clear", func(t *testing.T) {
		terminal := New(60, 6, 100)
		terminal.SetPromptToken("t")
		feed(terminal, "abc@ubuntu2404:~$ ")
		feed(terminal, "\x1b]133;A;motty=t\x07\r\x1b[2K\x1b[4C$ \x1b]133;B;motty=t\x07")
		terminal.Resize(40, 6)
		feed(terminal, "docker")
		if got := terminal.RowText(0); got != "    $ docker" {
			t.Fatalf("indented prompt row = %q, want %q", got, "    $ docker")
		}
		feed(terminal, "\r\x1b[K\x1b[4C$ docker")
		if got := terminal.RowText(0); got != "    $ docker" {
			t.Fatalf("indented prompt row after redraw = %q, want %q", got, "    $ docker")
		}
	})
}
