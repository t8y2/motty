package workbench

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/autofill"
	"motty/internal/sshconn"
	"motty/internal/zmodem"
)

func TestLockedConnectedShellConsumesBoundedOutput(t *testing.T) {
	for _, local := range []bool{false, true} {
		name := "SSH"
		if local {
			name = "local"
		}
		t.Run(name, func(t *testing.T) {
			state, _, _, inner := liveSession(t)
			session := &state.sessions[0]
			if local {
				session.HostID = localHostID
			}
			shell := newFloodShell(inner)
			session.term.shell = shell
			t.Cleanup(shell.Close)
			// One slice per wake makes the consumer slower than the producer.
			session.term.pumpBudget = 0
			state.appProtection.locked.Store(true)
			chunk := floodChunk(512 << 10)
			fed := make(chan struct{})
			go func() {
				defer close(fed)
				for range 8 {
					if !shell.say(chunk) {
						return
					}
				}
				shell.say([]byte("\r\nLATEST LOCKED OUTPUT\r\n"))
			}()
			deadline := time.Now().Add(15 * time.Second)
			peak, completed := 0, false
			for time.Now().Before(deadline) {
				state.Wake()
				peak = max(peak, len(session.term.rx))
				if len(session.term.rx) > maxPumpBacklog+2*len(chunk) {
					t.Fatalf("locked output backlog grew to %d bytes", len(session.term.rx))
				}
				select {
				case <-fed:
					shell.mu.Lock()
					pending := len(shell.data)
					shell.mu.Unlock()
					completed = pending == 0 && len(session.term.rx) == 0
				default:
				}
				if completed {
					break
				}
				runtime.Gosched()
			}
			if !completed {
				t.Fatal("a locked shell's bounded reader remained blocked")
			}
			if peak == 0 {
				t.Fatal("the flood never exercised the bounded output backlog")
			}
			if session.term.shell != shell || session.term.ended {
				t.Fatal("locking ended the existing shell")
			}
			state.appProtection.locked.Store(false)
			state.Wake()
			if !strings.Contains(screenText(state, 0), "LATEST LOCKED OUTPUT") {
				t.Fatal("unlock did not reveal the latest output consumed while locked")
			}
		})
	}
}

func TestLockedOutputSuppressesEffectsButAnswersTerminalQueries(t *testing.T) {
	w := liveAutofillSession(t)
	state, shell := w.state, w.shell
	w.addRule(t, "token", `^Token:`, "", autofill.SendText, "autofill-fixture-secret")
	state.Click(hitAutofillToggleList.At(0))
	session := &state.sessions[0]
	session.term.autofill.Allow(state.autofillRules.rules[0].ID)
	// A prompt was already awaiting its quiet-period scan when locking began.
	session.term.autofill.Output(w.clock)
	w.clock += 2 * autofill.Pause
	writes, notifications := 0, 0
	bells := make(chan struct{}, 1)
	state.options.SetClipboardText = func(string) error { writes++; return nil }
	state.options.Notify = func(app.Notification) error { notifications++; return nil }
	state.options.Bell = func() error { bells <- struct{}{}; return nil }
	state.terminalClipboard = terminalClipboardAllow
	state.appProtection.locked.Store(true)
	before := shell.typed()
	shell.say("\a\x1b]52;c;bG9ja2VkIGNsaXBib2FyZA==\a\x1b]9;locked notification\a\x1b[5n\r\nToken: ")
	state.Wake()
	w.clock += 2 * autofill.Pause
	state.Wake()
	if writes != 0 || notifications != 0 || session.didBell || state.clipboardRequest.pending || state.modal != modalNone {
		t.Fatalf("locked effects: clipboard=%d notifications=%d bell=%v pending=%v modal=%v", writes, notifications, session.didBell, state.clipboardRequest.pending, state.modal)
	}
	select {
	case <-bells:
		t.Fatal("locked terminal rang the system bell")
	default:
	}
	if got := strings.TrimPrefix(shell.typed(), before); got != "\x1b[0n" {
		t.Fatalf("locked shell received %q; want only the terminal status reply", got)
	}
	if !strings.Contains(screenText(state, 0), "Token:") {
		t.Fatal("the hidden parser did not retain the password prompt")
	}
	// Blocked OSC effects are discarded rather than replayed on unlock.
	state.appProtection.locked.Store(false)
	state.autofillRules.armed = false
	state.Wake()
	if writes != 0 || notifications != 0 || session.didBell {
		t.Fatal("unlock replayed a blocked terminal effect")
	}
}

func TestLockedOutputFinishesIntegrationWithoutSendingQueuedTyping(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	const token = "locked-integration-token"
	session.term.injection = terminalInjection{
		phase: injectionSuppressing, marker: sshconn.IntegrationReadyMarker(token),
		queued: [][]byte{[]byte("must not be typed\r")},
	}
	state.appProtection.locked.Store(true)
	before := shell.typed()
	shell.say("hidden setup echo" + string(sshconn.IntegrationReadyMarker(token)) + "ready prompt$ ")
	state.Wake()
	if shell.typed() != before || len(session.term.injection.queued) != 0 || session.term.injection.phase != injectionDone {
		t.Fatal("integration released queued typing while locked or failed to consume its marker")
	}
	if text := screenText(state, 0); !strings.Contains(text, "ready prompt$") || strings.Contains(text, "hidden setup echo") {
		t.Fatalf("integration parser displayed %q", text)
	}
}

func TestLockedOutputKeepsAcceptedTransferMoving(t *testing.T) {
	state, session, shell := transferState(t)
	collector := &collect{}
	payload := bytes.Repeat([]byte("accepted transfer data\n"), 10000)
	sender := zmodem.NewSender([]zmodem.SendFile{
		{Name: "locked.txt", Size: int64(len(payload)), Source: bytes.NewReader(payload)},
	}, zmodem.SendOptions{})
	shell.onWrite = func(p []byte) {
		sender.Feed(p)
		shell.say(string(sender.TakeOutput(nil)))
	}
	// The user accepted this receiver before the screen was locked.
	session.transfer.held = []byte(zrqinit)
	state.startTransfer(session, zmodem.NewReceiver(collector))
	state.appProtection.locked.Store(true)
	waitUntil(t, state, "the accepted transfer to end while locked", func() bool { return session.transfer.phase == transferNone })
	file := collector.files["locked.txt"]
	if file == nil || !bytes.Equal(file.Bytes(), payload) {
		t.Fatal("locking stopped or corrupted an already accepted transfer")
	}
	if session.term.shell != shell {
		t.Fatal("the transfer closed its existing shell")
	}
	// A new protocol announcement cannot open a picker behind the lock screen.
	shell.say(zrqinit)
	state.Wake()
	if session.transfer.phase != transferNone || state.modal == modalFileTransfer {
		t.Fatal("a new transfer was offered while locked")
	}
}
