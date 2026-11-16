package crashlog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const crashChild = "CRASHLOG_TEST_CHILD"

// TestMain lets the test binary play the program that dies.
func TestMain(m *testing.M) {
	if dir := os.Getenv(crashChild); dir != "" {
		session, err := Start(dir)
		if err != nil {
			os.Exit(3)
		}
		_ = session
		done := make(chan struct{})
		go func() {
			var table map[string]int
			table["x"] = 1 // a goroutine nothing recovers
			close(done)
		}()
		<-done
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestAnUnrecoveredPanicLeavesAReportTheNextRunFinds(t *testing.T) {
	dir := t.TempDir()
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), crashChild+"="+dir)
	if err := child.Run(); err == nil {
		t.Fatal("the child was meant to die")
	}

	report, err := os.ReadFile(filepath.Join(dir, crashName))
	if err != nil || !strings.Contains(string(report), "assignment to entry in nil map") {
		t.Fatalf("crash report = %q, %v", report, err)
	}

	next, err := Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.PreviousCrash == "" {
		t.Fatal("the next run does not know the last one crashed")
	}
	kept, err := ReadReport(next.PreviousCrash)
	if err != nil || !strings.Contains(kept, "nil map") {
		t.Fatalf("kept report = %q, %v", kept, err)
	}
}

func TestACleanRunLeavesNoCrashReport(t *testing.T) {
	dir := t.TempDir()
	session, err := Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	session.Write([]byte("motty: started\n"))
	session.Close()
	session.Close()

	if _, err := os.Stat(filepath.Join(dir, crashName)); !os.IsNotExist(err) {
		t.Fatalf("crash report remains: %v", err)
	}
	next, err := Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.PreviousCrash != "" {
		t.Fatalf("a clean run was reported as a crash: %s", next.PreviousCrash)
	}
	logged, _ := os.ReadFile(filepath.Join(dir, logName))
	if !strings.Contains(string(logged), "motty: started") {
		t.Fatalf("log = %q", logged)
	}
}

func TestTheLogIsKeptNearItsLimit(t *testing.T) {
	dir := t.TempDir()
	session, err := Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	line := []byte(strings.Repeat("x", 1023) + "\n")
	for written := 0; written < 3*maxLogBytes; written += len(line) {
		session.Write(line)
	}
	info, err := os.Stat(filepath.Join(dir, logName))
	if err != nil || info.Size() > maxLogBytes {
		t.Fatalf("log size = %v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(dir, logName+".1")); err != nil {
		t.Fatalf("the old log was not kept: %v", err)
	}
}

func TestOnlyTheNewestCrashReportsAreKept(t *testing.T) {
	dir := t.TempDir()
	for index := 0; index < keptCrashes+3; index++ {
		name := crashPrefix + "2026010" + string(rune('1'+index)) + "-000000" + crashSuffix
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pruneCrashes(dir)
	entries, _ := os.ReadDir(dir)
	if len(entries) != keptCrashes {
		t.Fatalf("%d reports kept", len(entries))
	}
}
