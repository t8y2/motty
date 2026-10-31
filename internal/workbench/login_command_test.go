package workbench

import (
	"testing"
	"time"

	"motty/internal/sshconn"
)

// loginCommandSession connects a host that has a login command, with a clock the
// test moves.
func loginCommandSession(t *testing.T, command string) (*State, *fakeShell, *time.Duration) {
	t.Helper()
	world, options := newWorld(t)
	hosts := append([]savedHost(nil), sampleHosts...)
	hosts[0].savedTransport.LoginCommand = command
	if err := saveState(options.StatePath, savedState{Hosts: hosts}); err != nil {
		t.Fatal(err)
	}
	now := new(time.Duration)
	options.Now = func() time.Duration { return *now }
	options.Invalidate = func() {}
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	return state, fake.shell(t), now
}

func TestLoginCommandWaitsForThePromptTheShellReports(t *testing.T) {
	state, shell, _ := loginCommandSession(t, "sudo -i")
	token := state.sessions[0].term.folderToken

	shell.say("Last login: today\r\n")
	state.Wake()
	if shell.typed() != "" {
		t.Fatalf("typed before the shell was ready: %q", shell.typed())
	}

	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "sudo -i\r" {
		t.Fatalf("typed %q once the prompt was ready", shell.typed())
	}

	state.Wake()
	if shell.typed() != "sudo -i\r" {
		t.Fatalf("typed again: %q", shell.typed())
	}
}

func TestLoginCommandFallsBackToQuietWhenNoPromptIsReported(t *testing.T) {
	state, shell, now := loginCommandSession(t, "cd /srv")
	shell.say("banner\r\n$ ")
	state.Wake()
	*now += loginCommandGrace / 2
	state.Wake()
	if shell.typed() != "" {
		t.Fatalf("typed while the shell might still announce a prompt: %q", shell.typed())
	}
	*now += loginCommandGrace
	state.Wake()
	if shell.typed() != "cd /srv\r" {
		t.Fatalf("typed %q after the shell stayed quiet", shell.typed())
	}
}

func TestLoginCommandStaysOutOfTheCommandHistory(t *testing.T) {
	state, shell, _ := loginCommandSession(t, "sudo -i")
	var remembered []string
	state.sessions[0].term.onCommand = func(command string) { remembered = append(remembered, command) }
	token := state.sessions[0].term.folderToken
	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
	if shell.typed() != "sudo -i\r" {
		t.Fatalf("typed %q", shell.typed())
	}
	if len(remembered) != 0 {
		t.Fatalf("the login command was offered to the command history: %q", remembered)
	}
}

func TestHostFormSavesTheLoginCommand(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Edge")
	typeInto(state, hitDialogAddress, "edge.example.com")
	typeInto(state, hitDialogUser, "ops")
	typeInto(state, hitDialogLoginCommand, "sudo -i")
	state.Click(hitDialogSubmit)
	if state.modal == modalNewHost {
		t.Fatalf("form refused: %q", state.dialogError)
	}
	added := state.hosts[len(state.hosts)-1]
	if added.LoginCommand != "sudo -i" {
		t.Fatalf("saved host = %+v", added)
	}
	if got := added.transport().LoginCommand; got != "sudo -i" {
		t.Fatalf("persisted %q", got)
	}
}

func TestALoginCommandThatIsNotOneLineIsRefusedAndNotRestored(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	typeInto(state, hitDialogLabel, "Edge")
	typeInto(state, hitDialogAddress, "edge.example.com")
	typeInto(state, hitDialogLoginCommand, "one\ntwo")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.ErrLoginCommand {
		t.Fatalf("a two-line command was accepted: modal=%v error=%q", state.modal, state.dialogError)
	}
	if restoredLoginCommand("one\ntwo") != "" || restoredLoginCommand("  sudo -i ") != "sudo -i" {
		t.Fatal("a saved login command is not read back as the form would have it")
	}
}
