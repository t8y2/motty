//go:build !windows

package localshell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"motty/internal/termsize"
)

func start(t *testing.T, config Config) (*Shell, chan struct{}) {
	t.Helper()
	wakes := make(chan struct{}, 1)
	config.Notify = func() {
		select {
		case wakes <- struct{}{}:
		default:
		}
	}
	if config.Shell == "" {
		config.Shell = "/bin/sh"
	}
	if config.Cols == 0 {
		config.Cols, config.Rows = 80, 24
	}
	shell, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shell.Close)
	return shell, wakes
}

func until(t *testing.T, shell *Shell, wakes chan struct{}, want string) string {
	t.Helper()
	var output []byte
	deadline := time.After(5 * time.Second)
	for {
		output = shell.Drain(output)
		if strings.Contains(string(output), want) {
			return string(output)
		}
		select {
		case <-wakes:
		case <-deadline:
			t.Fatalf("waiting for %q; output so far %q", want, output)
		}
	}
}

func TestShellEchoesInputAndRunsCommands(t *testing.T) {
	shell, wakes := start(t, Config{})
	if !shell.Write([]byte("echo hello-$((6*7))\n")) {
		t.Fatal("Write refused")
	}
	until(t, shell, wakes, "hello-42")
}

func TestShellStartsInTheRequestedDirectoryWithTheTerminalType(t *testing.T) {
	dir := t.TempDir()
	shell, wakes := start(t, Config{Dir: dir})
	shell.Write([]byte("pwd; echo term=$TERM\n"))
	output := until(t, shell, wakes, "term=xterm-256color")
	if !strings.Contains(output, dir) && !strings.Contains(output, strings.TrimPrefix(dir, "/private")) {
		t.Fatalf("shell started somewhere else: %q (want %s)", output, dir)
	}
}

// An application opened from Finder inherits no locale, and the C locale it
// would otherwise see makes macOS ls print "?" for every character of a
// non-ASCII file name.
func TestShellSeesChineseFileNamesWhenNoLocaleIsInherited(t *testing.T) {
	for _, name := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	const name = "产品分类映射.xlsx"
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	shell, wakes := start(t, Config{Dir: dir})
	shell.Write([]byte("ls\n"))
	until(t, shell, wakes, name)
}

func TestResizeReachesTheProgram(t *testing.T) {
	shell, wakes := start(t, Config{Cols: 80, Rows: 24})
	shell.Resize(termsize.Size{Cols: 132, Rows: 43})
	shell.Write([]byte("stty size\n"))
	until(t, shell, wakes, "43 132")
}

// The window size a program reads back carries the text area in pixels too: a
// program that lays images out by the character cell, as the kitty graphics
// protocol does, sizes them to that cell.
func TestWindowSizeCarriesTheTextAreaPixels(t *testing.T) {
	shell, _ := start(t, Config{Cols: 80, Rows: 24, PixelWidth: 800, PixelHeight: 408})
	master, ok := shell.b.io.(*os.File)
	if !ok {
		t.Fatalf("the shell's terminal is %T, not a pty", shell.b.io)
	}
	size, err := unix.IoctlGetWinsize(int(master.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		t.Fatal(err)
	}
	if size.Col != 80 || size.Row != 24 || size.Xpixel != 800 || size.Ypixel != 408 {
		t.Fatalf("window size = %dx%d cells, %dx%d pixels; want 80x24 cells, 800x408 pixels",
			size.Col, size.Row, size.Xpixel, size.Ypixel)
	}

	shell.Resize(termsize.Size{Cols: 100, Rows: 30, PixelWidth: 1000, PixelHeight: 510})
	deadline := time.Now().Add(5 * time.Second)
	for {
		size, err = unix.IoctlGetWinsize(int(master.Fd()), unix.TIOCGWINSZ)
		if err != nil {
			t.Fatal(err)
		}
		if size.Col == 100 && size.Row == 30 && size.Xpixel == 1000 && size.Ypixel == 510 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("window size = %dx%d cells, %dx%d pixels; want 100x30 cells, 1000x510 pixels",
				size.Col, size.Row, size.Xpixel, size.Ypixel)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestForegroundArgsNamesTheRunningCommand(t *testing.T) {
	shell, _ := start(t, Config{})
	if !shell.Write([]byte("exec sleep 30\n")) {
		t.Fatal("Write refused")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		args, ok := shell.ForegroundArgs()
		for _, arg := range args {
			if ok && strings.Contains(arg, "sleep") {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	args, _ := shell.ForegroundArgs()
	t.Fatalf("foreground = %q, want sleep", args)
}

func TestAnInFlightResizeCanOverlapClosingThePTY(t *testing.T) {
	shell, _ := start(t, Config{})
	started, resized := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(resized)
		close(started)
		// Call the backend directly to exercise a resize that has already
		// passed Shell's closed check when Close destroys the descriptor.
		for i := range 1000 {
			shell.b.resize(termsize.Size{Cols: 80 + i%2, Rows: 24})
		}
	}()
	<-started
	shell.Close()
	<-resized
	select {
	case <-shell.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent resize kept the closed shell alive")
	}
}

func TestExitEndsTheShellCleanlyAndRefusesFurtherInput(t *testing.T) {
	shell, _ := start(t, Config{})
	shell.Write([]byte("exit 0\n"))
	select {
	case <-shell.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the shell did not end")
	}
	if err := shell.Err(); err != nil {
		t.Fatalf("clean exit reported %v", err)
	}
	if shell.Write([]byte("x")) {
		t.Fatal("Write succeeded after the shell ended")
	}
}

func TestNonzeroExitIsReportedWithItsStatus(t *testing.T) {
	shell, _ := start(t, Config{})
	shell.Write([]byte("exit 3\n"))
	<-shell.Done()
	if shell.Err() == nil || shell.ExitStatus() != 3 {
		t.Fatalf("err=%v status=%d, want status 3", shell.Err(), shell.ExitStatus())
	}
}

func TestCloseHangsUpARunningProgramAndReleasesIt(t *testing.T) {
	shell, wakes := start(t, Config{})
	shell.Write([]byte("sleep 60\n"))
	until(t, shell, wakes, "sleep")
	shell.Close()
	select {
	case <-shell.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("closing did not end the shell")
	}
	shell.Close() // idempotent
}

func TestOutputIsBoundedAndNothingIsLost(t *testing.T) {
	shell, wakes := start(t, Config{})
	// 3 MiB of output against a 1 MiB buffer: the program is held back, not
	// dropped. The marker is assembled by the shell so the echo of the command
	// line does not contain it.
	const size = 3 << 20
	shell.Write([]byte("head -c 3145728 /dev/zero | tr '\\0' 'q'; echo don''e-$((1+1))\n"))
	total := 0
	var buffer []byte
	deadline := time.After(20 * time.Second)
	for {
		buffer = shell.Drain(buffer[:0])
		text := string(buffer)
		total += strings.Count(text, "q")
		if strings.Contains(text, "done-2") {
			break
		}
		select {
		case <-wakes:
		case <-deadline:
			t.Fatalf("stalled after %d bytes", total)
		}
	}
	if total < size {
		t.Fatalf("received %d bytes of output, want at least %d", total, size)
	}
}

// A shell holds a pty and goroutines until it is closed; the leak counter must see
// it as held while it runs and give it back on Close, once however often it is closed.
func TestAStartedShellIsCountedUntilItIsClosed(t *testing.T) {
	before := liveShells.Live()
	shell, _ := start(t, Config{})
	if got := liveShells.Live(); got != before+1 {
		t.Fatalf("live shells = %d after Start, want %d", got, before+1)
	}
	shell.Close()
	shell.Close()
	if got := liveShells.Live(); got != before {
		t.Fatalf("live shells = %d after Close, want %d", got, before)
	}
}
