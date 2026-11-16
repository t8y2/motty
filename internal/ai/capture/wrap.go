// Package capture runs one command inside the user's visible interactive
// terminal and learns how it ended.
//
// The command is typed into the pty wrapped by [Wrap]; a [Tap] watches the pty
// output, removes the completion marker from what the user sees, and records
// the command's plain-text output and exit status.
//
// The wrapper is chosen by the shell's family (see [WrapFor]): POSIX-style
// shells (sh, bash, zsh, dash, ksh) and PowerShell are supported. Shells that
// have neither "{ ...; }" groups with "$?" nor PowerShell's syntax (fish,
// nushell, cmd.exe) would never print the marker, so [WrapFor] refuses them
// instead of letting the caller wait for a timeout.
//
// A device console (see [shellkind.Console]) has no shell syntax at all. It gets
// the command line exactly as the user would type it; [Tap.FinishConsole] ends
// the capture when the device's prompt returns.
package capture

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"motty/internal/shellkind"
)

const markerPrefix = "GOPAI_"

// NewNonce returns a fresh random nonce (16 hex digits) for one command.
func NewNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}

// cleanNonce keeps only [A-Za-z0-9]; both Wrap and NewTap apply it so that they
// always agree, and so that the nonce is safe to place unquoted in a shell word.
func cleanNonce(n string) string {
	var sb strings.Builder
	for i := 0; i < len(n); i++ {
		c := n[i]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			sb.WriteByte(c)
		}
	}
	if sb.Len() == 0 {
		return "0"
	}
	return sb.String()
}

// Wrap returns the exact bytes to type into the pty for one command. It ends
// with a newline and has the form
//
//	{ <command>
//	}; printf '\n%s%s%d\n' GOPAI_ <nonce> $?
//
// Design notes:
//   - The command is one brace group, so "$?" is the status of the whole
//     command (lists, pipelines, subshells), and a command that ends with a
//     comment cannot swallow the printf: the closing brace sits on its own line.
//   - Heredocs work because their terminator is followed by a newline before
//     the closing brace.
//   - The marker is printed from split pieces. The echoed command line only
//     contains "GOPAI_ <nonce> $?", never the contiguous "GOPAI_<nonce>", so a
//     terminal echo cannot be mistaken for the real marker.
//   - Only newline and tab survive as control characters: CR and CRLF become a
//     newline, every other C0 control and DEL is dropped, so the text can never
//     act as line editing or as a signal.
//   - One trailing backslash (a pending line continuation, meaningless at the
//     end of input) is dropped; otherwise it would glue the closing brace onto
//     the command line.
//   - An unterminated quote, an unfinished heredoc or an unfinished construct
//     cannot be repaired: the shell keeps waiting for input and the marker is
//     never printed, the Tap never completes, and the caller times out, calls
//     [Tap.Abort] and interrupts the shell (Ctrl-C) itself.
//   - An empty command becomes ":" because an empty group is a syntax error.
func Wrap(command, nonce string) []byte {
	return wrapPOSIX(sanitizeCommand(command, shellkind.POSIX), nonce)
}

// ErrUnsupportedShell is returned by [WrapFor] for a shell whose syntax has no
// wrapper, so that the command was never typed.
type ErrUnsupportedShell struct{ Kind shellkind.Kind }

func (err ErrUnsupportedShell) Error() string {
	return fmt.Sprintf("terminal_exec cannot run commands in %s; open a session whose shell is a POSIX shell or PowerShell", err.Kind)
}

// WrapFor is [Wrap] for the shell family the terminal is running. An Unknown
// family gets the POSIX wrapper, which is what every shell got before the
// family was known.
//
// PowerShell gets
//
//	$global:LASTEXITCODE = 0; $global:__motty_ok = $false; try { . { <command>
//	$global:__motty_ok = $? } } catch { $_ }; & { param($ok, $code) ... } $global:__motty_ok $LASTEXITCODE; Remove-Variable __motty_ok -Scope Global
//
// The command is dot-sourced, so its variables stay in the user's session as
// they do in a POSIX brace group, and the closing brace is on its own line for
// the same reason as there. "$?" after a script block is always true, so the
// status is stored by the block's last statement, where it still describes the
// command. A terminating error (throw) would end the whole line, marker
// included, so it is caught and shown the way PowerShell shows it, and the
// status stays false. The printed status is 0 on success, the native program's
// exit code when one failed, and 1 for a failed cmdlet or expression. A
// negative code (a Windows NTSTATUS) is printed as the unsigned number it is.
// As in the POSIX wrapper the marker is built from split string literals, so
// the echoed line never contains it. Lines are separated by carriage returns,
// which is what Enter sends: PSReadLine does not take a bare line feed as a
// line break on Unix. A command that does not parse is never run and so never
// completes, which the caller's timeout reports, as for the POSIX wrapper.
func WrapFor(kind shellkind.Kind, command, nonce string) ([]byte, error) {
	switch kind {
	case shellkind.Unknown, shellkind.POSIX:
		return wrapPOSIX(sanitizeCommand(command, shellkind.POSIX), nonce), nil
	case shellkind.PowerShell:
		return wrapPowerShell(sanitizeCommand(command, shellkind.PowerShell), nonce), nil
	case shellkind.Console:
		return wrapConsole(sanitizeCommand(command, shellkind.Console)), nil
	}
	return nil, ErrUnsupportedShell{kind}
}

func wrapPowerShell(cmd, nonce string) []byte {
	var sb strings.Builder
	sb.Grow(len(cmd) + 256)
	sb.WriteString("$global:LASTEXITCODE = 0; $global:__motty_ok = $false; try { . { ")
	sb.WriteString(cmd)
	sb.WriteString("\n$global:__motty_ok = $? } } catch { $_ }; & { param($ok, $code) \"`n{0}{1}{2}`n\" -f '")
	sb.WriteString(markerPrefix)
	sb.WriteString("', '")
	sb.WriteString(cleanNonce(nonce))
	sb.WriteString("', $(if ($ok) { 0 } elseif ($code -lt 0) { $code + 4294967296 } elseif ($code) { $code } else { 1 }) } $global:__motty_ok $LASTEXITCODE; Remove-Variable __motty_ok -Scope Global\n")
	return []byte(strings.ReplaceAll(sb.String(), "\n", "\r"))
}

func wrapPOSIX(cmd, nonce string) []byte {
	var sb strings.Builder
	sb.Grow(len(cmd) + 64)
	sb.WriteString("{ ")
	sb.WriteString(cmd)
	sb.WriteString("\n}; printf '\\n%s%s%d\\n' ")
	sb.WriteString(markerPrefix)
	sb.WriteByte(' ')
	sb.WriteString(cleanNonce(nonce))
	sb.WriteString(" $?\n")
	return []byte(sb.String())
}

// wrapConsole returns the command exactly as it should be typed into a device
// console: no shell wrapper, and CR line endings, which is what Enter sends on a
// network device or serial line. The nonce is not used; a console has no way to
// print a completion marker.
func wrapConsole(cmd string) []byte {
	if cmd == "" {
		return []byte{'\r'}
	}
	return []byte(strings.ReplaceAll(cmd, "\n", "\r") + "\r")
}

// DisplayCommand returns the command text Wrap puts inside its group. The
// terminal shows this instead of the wrapper, so the text is cleaned exactly as
// the shell receives it.
func DisplayCommand(command string) string {
	return sanitizeCommand(command, shellkind.POSIX)
}

// DisplayCommandFor is [DisplayCommand] for the shell family [WrapFor] used.
func DisplayCommandFor(kind shellkind.Kind, command string) string {
	return sanitizeCommand(command, kind)
}

// sanitizeCommand cleans command for the shell of kind. The empty command is
// the shell's no-op, and the line continuation dropped from the end is the
// shell's own: a backslash in POSIX shells, a backtick in PowerShell, where a
// trailing backslash is just the end of a path.
func sanitizeCommand(command string, kind shellkind.Kind) string {
	b := make([]byte, 0, len(command))
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\r':
			if i+1 < len(command) && command[i+1] == '\n' {
				continue
			}
			b = append(b, '\n')
		case c == '\n' || c == '\t' || c >= 0x20 && c != 0x7f:
			b = append(b, c)
		}
	}
	s := strings.TrimRight(string(b), "\n \t")
	if kind == shellkind.Console {
		if s == "" {
			return ""
		}
		return s
	}
	// Drop one pending line continuation (odd number of trailing escapes).
	escape := byte('\\')
	if kind == shellkind.PowerShell {
		escape = '`'
	}
	n := 0
	for n < len(s) && s[len(s)-1-n] == escape {
		n++
	}
	if n%2 == 1 {
		s = strings.TrimRight(s[:len(s)-1], " \t")
	}
	if strings.TrimSpace(s) == "" {
		if kind == shellkind.PowerShell {
			return "$null"
		}
		return ":"
	}
	return s
}

// splitEcho splits the bytes that were typed into the lines the terminal will
// echo, trimmed. Lines end with a line feed or, for PowerShell, a carriage
// return, which is what Enter sends.
func splitEcho(sent []byte) [][]byte {
	sent = bytes.ReplaceAll(sent, []byte("\r\n"), []byte("\n"))
	sent = bytes.ReplaceAll(sent, []byte("\r"), []byte("\n"))
	lines := bytes.Split(bytes.TrimRight(sent, "\n"), []byte{'\n'})
	for i := range lines {
		lines[i] = bytes.TrimSpace(lines[i])
	}
	return lines
}
