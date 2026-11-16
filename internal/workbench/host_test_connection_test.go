package workbench

import (
	"strings"
	"testing"

	"motty/internal/sshconn"
)

// openTestableHostForm opens the New Host form with an address, an account and a
// password typed in, as a user about to press Test Connection.
func openTestableHostForm(t *testing.T, password string) (*State, *world) {
	t.Helper()
	known, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitAddHost)
	typeInto(state, hitDialogAddress, "new.example.com")
	typeInto(state, hitDialogUser, "ops")
	if password != "" {
		typeInto(state, hitDialogPassword, password)
	}
	return state, known
}

func passwordPrompt(attempt int) func(*sshconn.Status) {
	return func(status *sshconn.Status) {
		status.Phase = sshconn.PhasePasswordPrompt
		status.Attempt = attempt
		status.Questions = []sshconn.Question{{Secret: true}}
		status.Endpoint = sshconn.Endpoint{Host: "new.example.com", Port: 22, User: "ops"}
	}
}

func TestHostTestVerifiesTheTypedLoginWithoutSavingTheHost(t *testing.T) {
	state, known := openTestableHostForm(t, "hunter2")
	hosts := len(state.hosts)

	state.Click(hitDialogTest)
	if len(known.conns) != 1 || known.configs[0].Host != "new.example.com" || known.configs[0].User != "ops" {
		t.Fatalf("test dialed %+v", known.configs)
	}
	conn := known.conns[0]
	conn.set(passwordPrompt(1))
	state.Wake()
	if len(conn.passwords) != 1 || conn.passwords[0] != "hunter2" {
		t.Fatalf("the typed password was not offered: %q", conn.passwords)
	}

	conn.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseConnected
		status.ServerVersion = "SSH-2.0-OpenSSH_9.6"
	})
	state.Wake()
	if state.hostTest.outcome != hostTestConnected || !strings.Contains(state.hostTest.message, "OpenSSH_9.6") {
		t.Fatalf("outcome = %v %q", state.hostTest.outcome, state.hostTest.message)
	}
	if conn.canceled == 0 {
		t.Fatal("the test left its connection open")
	}
	if len(state.hosts) != hosts || state.modal != modalNewHost {
		t.Fatalf("testing saved or closed the form: hosts=%d modal=%v", len(state.hosts), state.modal)
	}
	if _, texts := paintedTexts(state); !contains(texts, "Connected") {
		t.Fatalf("the result was not painted: %q", texts)
	}
}

func TestHostTestNeverTrustsAnUnknownHostKeyOrSendsItTheLogin(t *testing.T) {
	state, known := openTestableHostForm(t, "hunter2")
	state.Click(hitDialogTest)
	conn := known.conns[0]
	conn.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseHostKeyPrompt
		status.HostKey = testHostKey
	})
	state.Wake()

	if state.hostTest.outcome != hostTestReachable || state.hostTest.message != state.messages.HostTestHostKey {
		t.Fatalf("outcome = %v %q", state.hostTest.outcome, state.hostTest.message)
	}
	if len(conn.trusted) != 0 || len(conn.passwords) != 0 || len(conn.answers) != 0 {
		t.Fatalf("an unverified server was given trust or credentials: trusted=%v answers=%v", conn.trusted, conn.answers)
	}
}

func TestHostTestReportsARefusedPasswordAndNeverRetriesIt(t *testing.T) {
	state, known := openTestableHostForm(t, "hunter2")
	state.Click(hitDialogTest)
	conn := known.conns[0]
	conn.set(passwordPrompt(1))
	state.Wake()
	conn.set(passwordPrompt(2))
	state.Wake()

	if state.hostTest.outcome != hostTestFailed || state.hostTest.message != state.messages.ConnAuthFailed {
		t.Fatalf("outcome = %v %q", state.hostTest.outcome, state.hostTest.message)
	}
	if len(conn.passwords) != 1 {
		t.Fatalf("the refused password was sent again: %q", conn.passwords)
	}
}

func TestHostTestWithoutAPasswordIsUnverifiedNotFailed(t *testing.T) {
	state, known := openTestableHostForm(t, "")
	state.Click(hitDialogTest)
	known.conns[0].set(passwordPrompt(1))
	state.Wake()

	if state.hostTest.outcome != hostTestReachable || state.hostTest.message != state.messages.HostTestNoPassword {
		t.Fatalf("outcome = %v %q", state.hostTest.outcome, state.hostTest.message)
	}
}

func TestClosingTheHostFormEndsARunningTest(t *testing.T) {
	state, known := openTestableHostForm(t, "hunter2")
	state.Click(hitDialogTest)
	state.Click(hitDialogCancel)

	if known.conns[0].canceled == 0 || state.hostTest.outcome != hostTestIdle {
		t.Fatalf("a test outlived its form: canceled=%d outcome=%v", known.conns[0].canceled, state.hostTest.outcome)
	}
}

func TestHostTestResultIsHiddenOnceTheFormChanges(t *testing.T) {
	state, known := openTestableHostForm(t, "hunter2")
	state.Click(hitDialogTest)
	known.conns[0].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseFailed
		status.Err = sshconn.ErrAuthFailed
	})
	state.Wake()
	if _, texts := paintedTexts(state); !contains(texts, state.messages.ConnAuthFailed) {
		t.Fatalf("the failure was not painted: %q", texts)
	}

	typeInto(state, hitDialogAddress, "x")
	if _, texts := paintedTexts(state); contains(texts, state.messages.ConnAuthFailed) {
		t.Fatal("a result for the old address is still shown")
	}
}
