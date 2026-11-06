package selfupdate

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Command is the argument that makes the program act as the helper instead of
// the application: `motty apply-update --kind=… --pid=… …`.
const Command = "apply-update"

// failureFile names, inside the updates directory, what went wrong the last time
// the helper tried; the next start tells the user and removes it.
const failureFile = "failed.txt"

// exitTimeout is how long the helper gives the program to quit.
const exitTimeout = 60 * time.Second

// Job is what the helper is asked to do once the program has quit.
type Job struct {
	Kind Kind
	// PID is the program to wait for.
	PID int
	// Staged is the checked new version: a .app, an installer, an unpacked archive.
	Staged    string
	Version   string
	Signature string
	// Root is what is replaced, as in Install.
	Root string
	// Exe is the program to start again.
	Exe string
	// Dir is the updates directory: where a failure is written, and what is
	// cleaned up afterwards.
	Dir string

	// starter replaces start in tests, which must not open the program.
	starter func() error
}

// Arguments is Job as the helper's command line.
func (job Job) Arguments() []string {
	args := []string{
		Command, "--kind=" + job.Kind.String(), "--pid=" + strconv.Itoa(job.PID), "--staged=" + job.Staged,
		"--root=" + job.Root, "--exe=" + job.Exe, "--dir=" + job.Dir,
	}
	if job.Kind == KindLinuxDeb {
		args = append(args, "--version="+job.Version, "--signature="+job.Signature)
	}
	return args
}

// ParseJob reads the helper's command line, without its leading Command.
func ParseJob(args []string) (Job, error) {
	flags := flag.NewFlagSet(Command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	kind := flags.String("kind", "", "")
	pid := flags.Int("pid", 0, "")
	var job Job
	flags.StringVar(&job.Staged, "staged", "", "")
	flags.StringVar(&job.Version, "version", "", "")
	flags.StringVar(&job.Signature, "signature", "", "")
	flags.StringVar(&job.Root, "root", "", "")
	flags.StringVar(&job.Exe, "exe", "", "")
	flags.StringVar(&job.Dir, "dir", "", "")
	if err := flags.Parse(args); err != nil {
		return Job{}, err
	}
	job.Kind, job.PID = parseKind(*kind), *pid
	if job.Kind == KindNone || job.PID <= 0 || job.Staged == "" || job.Root == "" || job.Exe == "" || job.Dir == "" {
		return Job{}, errors.New("selfupdate: the update job is incomplete")
	}
	if job.Kind == KindLinuxDeb && (job.Version == "" || job.Signature == "") {
		return Job{}, errors.New("selfupdate: the DEB update is unsigned")
	}
	return job, nil
}

// Run is the helper: it waits for the program to quit, replaces it with the
// staged version and starts it again. When the replacement fails it puts the old
// version back as far as it can, starts that, and records why, so the user finds
// a working program and a reason rather than nothing.
func (job Job) Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), exitTimeout)
	defer cancel()
	if err := waitExit(ctx, job.PID); err != nil {
		// The program is still running: touching its files could break it, and
		// nothing has changed, so there is nothing to restart either.
		return job.fail(fmt.Errorf("Motty did not quit: %w", err), false)
	}
	var err error
	switch job.Kind {
	case KindMacApp:
		err = job.applyMac()
	case KindWindowsSetup:
		err = job.applyWindows()
	case KindLinuxPrefix:
		err = job.applyLinux()
	case KindLinuxDeb:
		err = job.applyDeb()
	default:
		err = errors.New("unknown installation kind")
	}
	if err != nil {
		return job.fail(err, true)
	}
	if err := job.start(); err != nil {
		return job.fail(fmt.Errorf("starting the new version: %w", err), false)
	}
	// What is left of the download is no use now. The helper's own copy stays
	// when it cannot be removed while running (Windows); the next update clears it.
	if job.Kind == KindLinuxDeb {
		os.Remove(job.Staged)
	} else {
		os.RemoveAll(job.Staged)
	}
	return nil
}

// fail records err and, when restart is set, starts the program that is there.
func (job Job) fail(err error, restart bool) error {
	if writeErr := os.WriteFile(filepath.Join(job.Dir, failureFile), []byte(err.Error()), 0o600); writeErr != nil {
		err = errors.Join(err, fmt.Errorf("recording update failure: %w", writeErr))
	}
	if restart {
		if startErr := job.start(); startErr != nil {
			return errors.Join(err, startErr)
		}
	}
	return err
}

// start launches the installed program, detached from the helper.
func (job Job) start() error {
	if job.starter != nil {
		return job.starter()
	}
	var cmd *exec.Cmd
	switch job.Kind {
	case KindMacApp:
		cmd = exec.Command("/usr/bin/open", job.Root)
	case KindWindowsSetup:
		cmd = exec.Command(filepath.Join(job.Root, filepath.Base(job.Exe)))
	case KindLinuxDeb:
		cmd = exec.Command(job.Exe)
	default:
		cmd = exec.Command(filepath.Join(job.Root, "bin", filepath.Base(job.Exe)))
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// TakeFailure returns why the last update failed, if it did, and forgets it.
func TakeFailure(dir string) string {
	path := filepath.Join(dir, failureFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	os.Remove(path)
	return string(data)
}
