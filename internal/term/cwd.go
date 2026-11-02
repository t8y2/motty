package term

import (
	"net/url"
	"strings"
)

// The shell's working directory, as OSC 7 reports it: "file://host/path", sent by
// a prompt that has been set up to. The directory is only ever data for the
// owner to look at; nothing here touches a file system.

// maxCwd bounds the path kept.
const maxCwd = 2048

// Cwd is a directory the shell reported.
type Cwd struct {
	// Host is the machine name the shell gave, possibly empty.
	Host string
	// Path is the directory, absolute and cleaned of control characters.
	Path string
}

// SetCwdChange installs the receiver of directory reports. The callback runs
// during Write, only when the directory differs from the last one reported, and
// must not block.
func (t *Terminal) SetCwdChange(fn func(Cwd)) { t.cwdFn = fn }

// SetCwdReport receives every valid report, including repeats. Owners that
// validate a report against shell prompt markers must observe the trusted
// repeat after an identical report from a running program was ignored. Like
// SetCwdChange, this runs during Write and must not block.
func (t *Terminal) SetCwdReport(fn func(Cwd)) { t.cwdReportFn = fn }

// SetPromptReady receives OSC 133 B, the end of a shell prompt. Like CwdChange,
// it runs during Write on the owner's thread and must not block.
func (t *Terminal) SetPromptReady(fn func()) { t.promptReadyFn = fn }

// SetPromptStart receives OSC 133 A from the shell's prompt hook, rather than
// from readline repainting the already existing prompt.
func (t *Terminal) SetPromptStart(fn func()) { t.promptStartFn = fn }

// SetPromptToken confines readiness to this shell's integration, not a nested
// SSH login or a program that also implements the standard OSC 133 markers.
func (t *Terminal) SetPromptToken(token string) {
	if token != t.promptToken {
		t.cwd = Cwd{}
	}
	t.promptToken = token
}

// SetCommandStart receives the owning shell's OSC 133 C before execution,
// including commands that readline takes from previously submitted typeahead.
func (t *Terminal) SetCommandStart(fn func()) { t.commandStartFn = fn }

// osc133 reads one of the shell integration's prompt markers: A starts a
// prompt, B ends it where the command is typed, C starts the command's output.
// Only the owning shell's markers count, the ones that carry the prompt token;
// a nested login or a program that implements the standard on its own is not
// the shell Motty set up and cannot open or close a command block. The
// integration does not send D, the end of a command: a block ends where the next
// prompt starts.
func (t *Terminal) osc133(text []byte) {
	if len(text) == 0 {
		return
	}
	suffix := ""
	if t.promptToken != "" {
		suffix = ";motty=" + t.promptToken
	}
	if string(text[1:]) != suffix {
		return
	}
	switch text[0] {
	case 'A':
		t.markPromptStart()
		if t.promptStartFn != nil {
			t.promptStartFn()
		}
	case 'B':
		if t.promptReadyFn != nil {
			t.promptReadyFn()
		}
	case 'C':
		t.markOutputStart()
		if t.commandStartFn != nil {
			t.commandStartFn()
		}
	}
}

// WorkingDirectory is the last directory the shell reported, or false.
func (t *Terminal) WorkingDirectory() (Cwd, bool) { return t.cwd, t.cwd.Path != "" }

// osc7 reads a working-directory report.
func (t *Terminal) osc7(text []byte) {
	cwd, ok := parseCwd(string(text))
	if !ok {
		return
	}
	t.reportCwd(cwd)
}

// OSC 9;9 is the native-path directory report used by Windows Terminal and
// ConEmu. Keep it as data; validating an actual local directory is the owner's
// responsibility. It is not a desktop notification.
func (t *Terminal) oscWindowsDirectory(value string) {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	drive := len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
	unc := strings.HasPrefix(value, `\\`) && len(value) > 2
	if (!drive && !unc) || len(value) > maxCwd || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return
	}
	t.reportCwd(Cwd{Path: value})
}

func (t *Terminal) reportCwd(cwd Cwd) {
	changed := cwd != t.cwd
	t.cwd = cwd
	if t.cwdReportFn != nil {
		t.cwdReportFn(cwd)
	}
	if changed && t.cwdFn != nil {
		t.cwdFn(cwd)
	}
}

// parseCwd reads "file://host/percent-encoded/path". Anything else, a relative
// path or a path with control characters, is refused.
func parseCwd(value string) (Cwd, bool) {
	if len(value) > maxCwd*3 || !strings.HasPrefix(value, "file://") {
		return Cwd{}, false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Path == "" || parsed.Path[0] != '/' || len(parsed.Path) > maxCwd {
		return Cwd{}, false
	}
	for _, r := range parsed.Path {
		if r < 0x20 || r == 0x7f {
			return Cwd{}, false
		}
	}
	return Cwd{Host: parsed.Hostname(), Path: parsed.Path}, true
}
