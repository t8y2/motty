//go:build windows

package localshell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows/registry"
)

func TestWindowsShellsReceiveProcessAndConfiguredEnvironment(t *testing.T) {
	t.Setenv("MOTTY_TEST_INHERITED_ENV", "inherited-value")

	tests := []struct {
		name    string
		command string
	}{
		{
			name:    "cmd.exe",
			command: "echo inherited=%MOTTY_TEST_INHERITED_ENV% configured=%MOTTY_TEST_CONFIGURED_ENV%\r\n",
		},
		{
			name:    "powershell.exe",
			command: "Write-Output ('inherited=' + $env:MOTTY_TEST_INHERITED_ENV + ' configured=' + $env:MOTTY_TEST_CONFIGURED_ENV)\r\n",
		},
		{
			name:    "pwsh.exe",
			command: "Write-Output ('inherited=' + $env:MOTTY_TEST_INHERITED_ENV + ' configured=' + $env:MOTTY_TEST_CONFIGURED_ENV)\r\n",
		},
		{
			name:    "Git Bash",
			command: "printf 'inherited=%s configured=%s\\n' \"$MOTTY_TEST_INHERITED_ENV\" \"$MOTTY_TEST_CONFIGURED_ENV\"\r\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			program := windowsTestShell(t, test.name)
			shell, wakes := startWindows(t, Config{
				Shell: program,
				Args:  ArgsFor(program),
				Env:   []string{"MOTTY_TEST_CONFIGURED_ENV=configured-value"},
			})
			if !shell.Write([]byte(test.command)) {
				t.Fatal("Write refused")
			}
			untilWindows(t, shell, wakes, "inherited=inherited-value configured=configured-value")
		})
	}
}

func TestWindowsLocalShellRefreshesPersistedUserEnvironment(t *testing.T) {
	name := fmt.Sprintf("MOTTY_TEST_REGISTRY_ENV_%d", os.Getpid())
	t.Setenv(name, "stale-process-value")
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = key.DeleteValue(name)
		_ = key.Close()
	})
	if err := key.SetStringValue(name, "saved-value"); err != nil {
		t.Fatal(err)
	}

	shell, wakes := startWindows(t, Config{
		Shell: "cmd.exe",
	})
	if !shell.Write([]byte(fmt.Sprintf("echo refreshed=%%%s%%\r\n", name))) {
		t.Fatal("Write refused")
	}
	untilWindows(t, shell, wakes, "refreshed=saved-value")
}

func TestWindowsEnvironmentMergeIsCaseInsensitiveAndSorted(t *testing.T) {
	environment := mergeWindowsEnvironment(
		[]string{"Path=old", "Z=last", "=C:=C:\\old"},
		"PATH=new",
		"=C:=C:\\new",
		"A=first",
	)
	want := []string{"=C:=C:\\new", "A=first", "PATH=new", "Z=last"}
	block, err := environmentBlock(environment)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for len(block) > 0 && block[0] != 0 {
		end := 0
		for block[end] != 0 {
			end++
		}
		got = append(got, string(utf16.Decode(block[:end])))
		block = block[end+1:]
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("environment block = %#v, want %#v", got, want)
	}
}

func windowsTestShell(t *testing.T, name string) string {
	t.Helper()
	if name == "Git Bash" {
		for _, choice := range InstalledWindowsShells() {
			if choice.Name == "Git Bash" {
				return choice.Path
			}
		}
		t.Skip("Git Bash is not installed")
	}
	program, err := exec.LookPath(name)
	if err != nil {
		t.Skip(name + " is not installed")
	}
	if !filepath.IsAbs(program) {
		t.Fatalf("%s resolved to a relative path %q", name, program)
	}
	return strings.TrimSpace(program)
}
