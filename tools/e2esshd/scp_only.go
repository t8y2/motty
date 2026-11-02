package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"golang.org/x/crypto/ssh"
)

// E2E_NO_SFTP=1 turns the fixture into the kind of server that has no SFTP
// subsystem and only accepts a short list of commands: the scp source and sink,
// and the fixed listing, directory and file commands Motty's SCP backend sends.
// Nothing goes through a shell. A command is matched against its exact shape,
// its quoted words are decoded, every path is resolved inside the fixture root,
// and the real binary is started with those words as its argv.

var (
	quotedWord   = `'(?:[^']|'\\'')*'`
	renameShape  = regexp.MustCompile(`^if \[ -e (` + quotedWord + `) \] \|\| \[ -L (` + quotedWord + `) \]; then echo "rename: destination exists" >&2; exit 1; fi; mv -- (` + quotedWord + `) (` + quotedWord + `)$`)
	plainWord    = regexp.MustCompile(`^[A-Za-z0-9_=+%.,:/@-]+$`)
	listAltSplit = " || "
)

var openSFTPSessions atomic.Int32

// sftpSessionAllowed counts an SFTP subsystem in, unless E2E_MAX_SFTP names a
// number of them and that many are open.
func sftpSessionAllowed() bool {
	limit, _ := strconv.Atoi(os.Getenv("E2E_MAX_SFTP"))
	if limit > 0 && int(openSFTPSessions.Load()) >= limit {
		return false
	}
	openSFTPSessions.Add(1)
	return true
}

func sftpSessionDone() { openSFTPSessions.Add(-1) }

// scpOnlyEnabled reports whether the server refuses SFTP.
func scpOnlyEnabled() bool { return os.Getenv("E2E_NO_SFTP") == "1" }

// words splits a command into decoded words. Only plain words and single-quoted
// words are accepted, so no shell syntax can hide in a command.
func words(command string) ([]string, bool) {
	var out []string
	for index := 0; index < len(command); {
		switch c := command[index]; {
		case c == ' ':
			index++
		case c == '\'' || plainStart(c):
			var word strings.Builder
			for index < len(command) && command[index] != ' ' {
				if command[index] == '\'' {
					end := strings.IndexByte(command[index+1:], '\'')
					if end < 0 {
						return nil, false
					}
					word.WriteString(command[index+1 : index+1+end])
					index += end + 2
					continue
				}
				if command[index] == '\\' && strings.HasPrefix(command[index:], `\'`) {
					word.WriteByte('\'')
					index += 2
					continue
				}
				start := index
				for index < len(command) && command[index] != ' ' && command[index] != '\'' && command[index] != '\\' {
					index++
				}
				if !plainWord.MatchString(command[start:index]) {
					return nil, false
				}
				word.WriteString(command[start:index])
			}
			out = append(out, word.String())
		default:
			return nil, false
		}
	}
	return out, len(out) > 0
}

func plainStart(c byte) bool { return c != '\'' && c != ' ' && c != '\\' }

func unquote(quoted string) string {
	return strings.ReplaceAll(quoted[1:len(quoted)-1], `'\''`, "'")
}

// within resolves a path the client sent to a path inside root.
func within(root, name string) (string, bool) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", false
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(root, name)
	}
	name = filepath.Clean(name)
	if name != root && !strings.HasPrefix(name, root+string(filepath.Separator)) {
		return "", false
	}
	return name, true
}

// scpCommand returns the argv alternatives to try, in order, for command, or
// false when the command is not one of the allowed shapes.
func scpCommand(root, command string) ([][]string, bool) {
	if match := renameShape.FindStringSubmatch(command); match != nil {
		destination, from, to := unquote(match[1]), unquote(match[3]), unquote(match[4])
		if destination != to || unquote(match[2]) != to {
			return nil, false
		}
		source, ok := within(root, from)
		target, ok2 := within(root, to)
		if !ok || !ok2 {
			return nil, false
		}
		if _, err := os.Lstat(target); err == nil {
			return [][]string{{"sh", "-c", `echo "rename: destination exists" >&2; exit 1`}}, true
		}
		return [][]string{{"mv", "--", source, target}}, true
	}
	var alternatives [][]string
	for _, alternative := range strings.Split(command, listAltSplit) {
		alternative = strings.TrimSuffix(alternative, " 2>/dev/null")
		alternative = strings.TrimPrefix(alternative, "LC_ALL=C ")
		fields, ok := words(alternative)
		if !ok {
			return nil, false
		}
		argv, ok := checkArgv(root, fields)
		if !ok {
			return nil, false
		}
		alternatives = append(alternatives, argv)
	}
	return alternatives, len(alternatives) > 0 && len(alternatives) <= 2
}

func checkArgv(root string, fields []string) ([]string, bool) {
	program := fields[0]
	rest := fields[1:]
	separator := -1
	for index, word := range rest {
		if word == "--" {
			separator = index
			break
		}
	}
	// Every program but pwd and chmod takes flags, then "--", then paths.
	pathsFrom := separator + 1
	switch program {
	case "pwd":
		if len(rest) != 0 {
			return nil, false
		}
		return []string{"pwd"}, true
	case "chmod":
		if separator != 1 || len(rest) != 3 || !regexp.MustCompile(`^[0-7]{3,4}$`).MatchString(rest[0]) {
			return nil, false
		}
	case "scp":
		if separator < 1 || len(rest)-pathsFrom != 1 {
			return nil, false
		}
		mode := rest[0]
		if mode != "-f" && mode != "-t" {
			return nil, false
		}
		for _, flag := range rest[1:separator] {
			if flag != "-r" && flag != "-p" {
				return nil, false
			}
		}
	case "ls":
		if separator < 1 || len(rest)-pathsFrom != 1 {
			return nil, false
		}
		for _, flag := range rest[:separator] {
			if !strings.HasPrefix(flag, "-") || strings.HasPrefix(flag, "--") && flag != "--time-style=+%s" {
				return nil, false
			}
		}
	case "mkdir":
		if separator != 1 || rest[0] != "-p" || len(rest)-pathsFrom != 1 {
			return nil, false
		}
	case "rm", "rmdir":
		if separator != 0 || len(rest)-pathsFrom != 1 {
			return nil, false
		}
	case "ln":
		if separator != 1 || rest[0] != "-s" || len(rest)-pathsFrom != 2 {
			return nil, false
		}
	default:
		return nil, false
	}
	argv := []string{program}
	argv = append(argv, rest[:max(pathsFrom, 0)]...)
	if program == "chmod" {
		pathsFrom = 2
	}
	for index := pathsFrom; index < len(rest); index++ {
		name := rest[index]
		if program == "ln" && index == pathsFrom {
			argv = append(argv, name) // the link text may point anywhere; it is only text
			continue
		}
		resolved, ok := within(root, name)
		if !ok {
			return nil, false
		}
		argv = append(argv, resolved)
	}
	if program == "chmod" {
		argv = []string{"chmod", rest[0], "--", argv[len(argv)-1]}
	}
	return argv, true
}

// runScpOnly serves one exec request of the SCP-only fixture. It reports false
// when the command is not allowed.
func runScpOnly(root, command string, channel ssh.Channel) bool {
	alternatives, ok := scpCommand(root, command)
	if !ok {
		return false
	}
	status := uint32(1)
	for _, argv := range alternatives {
		if argv[0] == "pwd" {
			io.WriteString(channel, root+"\n")
			status = 0
			break
		}
		run := exec.Command(argv[0], argv[1:]...)
		run.Dir = root
		run.Env = []string{"LC_ALL=C", "HOME=" + root, "PATH=" + os.Getenv("PATH")}
		// Stdin is fed by hand: exec would wait for a reader that only ends when
		// the client closes the channel, which it does after the command exits.
		run.Stdout, run.Stderr = channel, channel.Stderr()
		stdin, err := run.StdinPipe()
		if err != nil {
			break
		}
		go func() {
			io.Copy(stdin, channel)
			stdin.Close()
		}()
		err = run.Run()
		if err == nil {
			status = 0
			break
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			status = uint32(exit.ExitCode())
		}
	}
	channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
	return true
}
