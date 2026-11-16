// Package crashlog keeps the two files that explain a program that went away
// without a word: a log of what it said, and the report of a crash.
//
// A Windows program built for the GUI subsystem has no standard error, and a Go
// program that dies of an unrecovered panic or a fatal runtime error leaves no
// report with the system either, on Windows or macOS. Without these files the
// only account of such a death is the user's "it closed". The crash report is
// written by the Go runtime itself (debug.SetCrashOutput), so it also covers the
// goroutines and faults no application code could have recovered.
package crashlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
)

const (
	logName        = "motty.log"
	crashName      = "crash.log"
	crashPrefix    = "crash-"
	crashSuffix    = ".log"
	maxLogBytes    = 1 << 20
	keptCrashes    = 5
	maxCrashReport = 64 << 10
)

// Session is the open pair of files of one run.
type Session struct {
	dir string
	// PreviousCrash is the path of the crash report the previous run left, or
	// empty when it ended cleanly.
	PreviousCrash string

	mu    sync.Mutex
	log   *os.File
	size  int64
	crash *os.File
}

// Dir is the directory the files live in for a data directory.
func Dir(dataDir string) string { return filepath.Join(dataDir, "logs") }

// Start opens the log and the crash report in dir, creating it. A crash report
// left by the previous run is kept under its own name, the newest few. Call it
// once, in the process that owns the data directory.
func Start(dir string) (*Session, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("crashlog: %w", err)
	}
	session := &Session{dir: dir}
	session.PreviousCrash = keepPreviousCrash(dir)

	logFile, err := os.OpenFile(filepath.Join(dir, logName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("crashlog: %w", err)
	}
	session.log = logFile
	if info, err := logFile.Stat(); err == nil {
		session.size = info.Size()
	}
	crashFile, err := os.OpenFile(filepath.Join(dir, crashName), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("crashlog: %w", err)
	}
	session.crash = crashFile
	if err := debug.SetCrashOutput(crashFile, debug.CrashOptions{}); err != nil {
		crashFile.Close()
		logFile.Close()
		return nil, fmt.Errorf("crashlog: %w", err)
	}
	return session, nil
}

// Write appends to the log. It never fails, so it is safe in an io.MultiWriter,
// and keeps the file to about maxLogBytes by moving the old half aside.
func (session *Session) Write(p []byte) (int, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.log == nil {
		return len(p), nil
	}
	if session.size+int64(len(p)) > maxLogBytes {
		session.rotateLocked()
	}
	if written, err := session.log.Write(p); err == nil {
		session.size += int64(written)
	}
	return len(p), nil
}

var _ io.Writer = (*Session)(nil)

func (session *Session) rotateLocked() {
	session.log.Close()
	current := filepath.Join(session.dir, logName)
	os.Remove(current + ".1")
	os.Rename(current, current+".1")
	file, err := os.OpenFile(current, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		session.log = nil
		return
	}
	session.log, session.size = file, 0
}

// Close ends the session cleanly: the crash report, which stayed empty, is
// removed so the next run does not take it for a crash. It is safe to call twice.
func (session *Session) Close() {
	if session == nil {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.crash != nil {
		debug.SetCrashOutput(nil, debug.CrashOptions{})
		session.crash.Close()
		session.crash = nil
		if info, err := os.Stat(filepath.Join(session.dir, crashName)); err == nil && info.Size() == 0 {
			os.Remove(filepath.Join(session.dir, crashName))
		}
	}
	if session.log != nil {
		session.log.Close()
		session.log = nil
	}
}

// keepPreviousCrash moves a non-empty crash report left by the previous run to a
// name of its own, prunes the old ones, and returns the new path.
func keepPreviousCrash(dir string) string {
	current := filepath.Join(dir, crashName)
	info, err := os.Stat(current)
	if err != nil || info.Size() == 0 {
		return ""
	}
	stamp := info.ModTime().UTC().Format("20060102-150405")
	kept := filepath.Join(dir, crashPrefix+stamp+crashSuffix)
	if err := os.Rename(current, kept); err != nil {
		return ""
	}
	pruneCrashes(dir)
	return kept
}

func pruneCrashes(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, crashPrefix) && strings.HasSuffix(name, crashSuffix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for len(names) > keptCrashes {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// ReadReport returns the start of the crash report at path, cut to a size a
// bug report can carry. The cut keeps the head: the panic message and the
// goroutine that died come first.
func ReadReport(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCrashReport+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxCrashReport {
		return string(data[:maxCrashReport]) + "\n… (cut)", nil
	}
	return string(data), nil
}
