package workbench

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"motty/internal/localshell"
	"motty/internal/sshconn"
)

func TestOnlyAWindowsWSLShellGetsTheDistributionStartScript(t *testing.T) {
	report := sshconn.ShellConfig{Cols: 100, Rows: 30, IntegrateShell: true, IntegrationToken: "wsl-token"}
	wsl := (&localConn{shell: localshell.WSLShell("Ubuntu")}).shellConfig(report, "windows")
	if wsl.Shell != "wsl:Ubuntu" || !strings.Contains(wsl.WSLScript, ";motty=wsl-token") || wsl.Cols != 100 {
		t.Fatalf("WSL shell config = %+v", wsl)
	}
	for name, test := range map[string]struct {
		conn *localConn
		goos string
	}{
		"PowerShell":             {&localConn{shell: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}, "windows"},
		"the platform default":   {&localConn{}, "windows"},
		"a WSL name off Windows": {&localConn{shell: localshell.WSLShell("Ubuntu")}, "linux"},
	} {
		if config := test.conn.shellConfig(report, test.goos); config.WSLScript != "" {
			t.Errorf("%s was given a WSL script", name)
		}
	}
	report.IntegrateShell = false
	if config := (&localConn{shell: localshell.WSLShell("Ubuntu")}).shellConfig(report, "windows"); config.WSLScript != "" || config.DirectoryToken != "" {
		t.Fatalf("integration was off, yet %+v", config)
	}
}

func TestPathsTypedIntoAWSLShellAreThePathsItSees(t *testing.T) {
	paths := []string{`C:\Users\me\a b.txt`, `\\wsl.localhost\Ubuntu\home\me\it's`, `\\wsl.localhost\Debian\home\me\x`}
	got := quotedPaths(paths, "Ubuntu")
	// A path the distribution cannot name is typed as it is, still quoted.
	want := `'/mnt/c/Users/me/a b.txt' '/home/me/it'\''s' '\\wsl.localhost\Debian\home\me\x'`
	if got != want {
		t.Fatalf("typed %s, want %s", got, want)
	}
}

func TestASessionKnowsWhichDistributionItsShellRunsIn(t *testing.T) {
	for conn, want := range map[Connection]string{
		&localConn{shell: localshell.WSLShell("Debian")}: "Debian",
		&localConn{shell: "/bin/zsh"}:                    "",
		&localConn{}:                                     "",
		nil:                                              "",
	} {
		if got := (&Session{conn: conn}).wslDistro(); got != want {
			t.Errorf("distribution = %q, want %q", got, want)
		}
	}
}

func TestDistributionsJoinTheShellListAfterTheShellsAndWhenWSLAnswers(t *testing.T) {
	state := newTestState(t, testOptions(t))
	shells := []localshell.Choice{{Name: "CMD", Path: `C:\Windows\System32\cmd.exe`, Default: true}}
	release := make(chan struct{})
	var asked atomic.Int32
	menu := &state.localShellMenu
	menu.discover = func() []localshell.Choice { return shells }
	menu.discoverWSL = func(ctx context.Context) []localshell.Choice {
		asked.Add(1)
		<-release
		return localshell.WSLChoices([]string{"Ubuntu", "Debian"})
	}
	state.refreshLocalShells()
	state.refreshLocalShells() // asking again while the first is out does not start another
	state.adoptWSLDistros()
	if len(menu.choices) != 1 || menu.choices[0].Name != "CMD" {
		t.Fatalf("shells waited for WSL: %+v", menu.choices)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for len(menu.choices) == 1 && time.Now().Before(deadline) {
		state.adoptWSLDistros()
		time.Sleep(time.Millisecond)
	}
	var names []string
	for _, choice := range menu.choices {
		names = append(names, choice.Name+"="+choice.Path)
	}
	if want := []string{`CMD=C:\Windows\System32\cmd.exe`, "WSL: Ubuntu=wsl:Ubuntu", "WSL: Debian=wsl:Debian"}; !slices.Equal(names, want) {
		t.Fatalf("choices = %q, want %q", names, want)
	}
	if asked.Load() != 1 {
		t.Fatalf("wsl.exe was asked %d times", asked.Load())
	}
	// Reopening the list shows the distributions at once and asks again, in case
	// one was installed or removed.
	state.refreshLocalShells()
	if len(menu.choices) != 3 {
		t.Fatalf("the distributions vanished while WSL was asked again: %+v", menu.choices)
	}
	for menu.listing != nil && time.Now().Before(deadline) {
		state.adoptWSLDistros()
		time.Sleep(time.Millisecond)
	}
	if menu.listing != nil || asked.Load() != 2 {
		t.Fatalf("the second question was not answered: %d asked", asked.Load())
	}
}
