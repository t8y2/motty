package remotestats

import "context"

// WSL runs the monitor's fixed commands inside a WSL distribution, for a
// terminal whose shell is that distribution's. The commands are the ones a
// Linux server gets, because that is the system a distribution is; only the
// process that carries them starts through wsl.exe. Exe is that executable,
// and Distro the name wsl.exe was given.
type WSL struct {
	Exe    string
	Distro string
	User   string
}

// args runs one script in the session's distribution and user account.
// --exec runs /bin/sh directly, without the distribution's login shell.
func (w WSL) args(command string) []string {
	args := []string{"-d", w.Distro}
	if w.User != "" {
		args = append(args, "--user", w.User)
	}
	return append(args, "--exec", "/bin/sh", "-c", command)
}

// StreamCommand runs command in the distribution and hands each stdout line,
// newline included, to line until it reports false, the command ends or ctx is
// canceled.
func (w WSL) StreamCommand(ctx context.Context, command string, maxLineBytes int, line func([]byte) bool) error {
	return streamCommand(ctx, w, command, maxLineBytes, line)
}

// RunCommand runs command in the distribution to completion and returns its
// stdout, at most maxOutputBytes of it; more is ErrOutputTooLarge with what
// fit.
func (w WSL) RunCommand(ctx context.Context, command string, maxOutputBytes int) ([]byte, error) {
	return runCommand(ctx, w, command, maxOutputBytes)
}
