package selfupdate

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// The installer fixture records the actual CreateProcess command line. It
// never installs anything or opens Motty, including for a real-looking /D= path.
func TestMain(m *testing.M) {
	if os.Getenv("MOTTY_TEST_HOLD_IMAGE") == "1" {
		fmt.Fprintln(os.Stdout, "ready")
		io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	if output := os.Getenv("MOTTY_TEST_INSTALLER_COMMAND_LINE"); output != "" {
		line := windows.UTF16PtrToString(windows.GetCommandLine())
		if err := os.WriteFile(output, []byte(line), 0600); err != nil {
			os.Exit(1)
		}
		if target := os.Getenv("MOTTY_TEST_INSTALL_TARGET"); target != "" {
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				os.Exit(2)
			}
			if err := os.WriteFile(target, []byte("new image"), 0700); err != nil {
				os.Exit(3)
			}
			if os.Getenv("MOTTY_TEST_INSTALL_FAIL") == "1" {
				os.Exit(4)
			}
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// A real Windows process maps the installed image, exactly as a stdio MCP
// server does. The fixture installer requires a free destination path.
func TestWindowsUpdateWithRunningMCPImage(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("installer-failure=%v", fail), func(t *testing.T) {
			testWindowsUpdateWithRunningMCPImage(t, fail)
		})
	}
}

func testWindowsUpdateWithRunningMCPImage(t *testing.T, fail bool) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "Motty 自定义目录")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "motty.exe")
	if err := copyExecutable(self, exe); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(original)
	child := exec.Command(exe)
	child.Env = append(os.Environ(), "MOTTY_TEST_HOLD_IMAGE=1")
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { input.Close(); child.Process.Kill(); child.Wait() }()
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("child not ready: %q %v", line, err)
	}
	if err := os.Remove(exe); err == nil {
		t.Fatal("fixture image was not locked by Windows")
	}
	installer := filepath.Join(t.TempDir(), "setup.exe")
	if err := copyExecutable(self, installer); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOTTY_TEST_INSTALLER_COMMAND_LINE", filepath.Join(t.TempDir(), "args.txt"))
	t.Setenv("MOTTY_TEST_INSTALL_TARGET", exe)
	if fail {
		t.Setenv("MOTTY_TEST_INSTALL_FAIL", "1")
	}
	err = (Job{Staged: installer, Root: root, Exe: exe}).applyWindows()
	if (err != nil) != fail {
		t.Fatalf("installation error: %v, want failure=%v", err, fail)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if fail {
		if sha256.Sum256(data) != want {
			t.Fatal("failed installation did not restore the original image")
		}
	} else if string(data) != "new image" {
		t.Fatal("new image was not installed")
	}
	if _, err := io.WriteString(input, "still connected\n"); err != nil {
		t.Fatalf("MCP pipe interrupted: %v", err)
	}
	input.Close()
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	cleanRetiredExecutables(root, ".motty.exe-update-")
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("retired image not cleaned after process exit: %v", entries)
	}
}

func TestWindowsInstallerKeepsTheExactCustomDirectory(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	installer := filepath.Join(t.TempDir(), "Motty 更新 setup.exe")
	if err := copyExecutable(exe, installer); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{
		`D:\Program Files\motty`,
		`D:\开发 工具\Motty & SSH`,
		`C:\Users\me\AppData\Local\Programs\Motty`,
		`D:\`,
	} {
		t.Run(root, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "command-line.txt")
			t.Setenv("MOTTY_TEST_INSTALLER_COMMAND_LINE", output)
			if err := (Job{Staged: installer, Root: root}).runWindowsInstaller(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			_, dir, found := strings.Cut(string(data), " /S /D=")
			if !found || dir != root {
				t.Fatalf("NSIS would install elsewhere: %q, want unquoted /D=%s at the end", data, root)
			}
		})
	}
}
