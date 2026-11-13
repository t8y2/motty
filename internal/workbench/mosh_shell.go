package workbench

import (
	"sync"
	"time"

	"motty/internal/i18n"
	"motty/internal/mosh"
)

// moshShell is the workbench's Shell over a mosh client. Mosh has no SSH
// channel behind it, so this is the only surface the terminal sees: host
// output, keystrokes, resize and the end of the session.
type moshShell struct {
	client   *mosh.Client
	notify   func()
	messages i18n.ToolsMessages

	mu        sync.Mutex
	local     []byte
	lastInput mosh.InputID
	noticed   bool
}

func newMoshShell(client *mosh.Client, notify func(), messages i18n.ToolsMessages) *moshShell {
	shell := &moshShell{client: client, notify: notify, messages: messages}
	// One timer, once, to explain an empty terminal whose datagrams have not
	// been answered yet. The session keeps retrying, as mosh does.
	time.AfterFunc(moshPendingNotice, shell.noticeIfSilent)
	return shell
}

func (shell *moshShell) noticeIfSilent() {
	select {
	case <-shell.client.Done():
		return
	default:
	}
	if !shell.client.LastRecv().IsZero() {
		return
	}
	shell.mu.Lock()
	if !shell.noticed {
		shell.noticed = true
		shell.local = append(shell.local, []byte("\r\n"+shell.messages.MoshWaiting+"\r\n")...)
	}
	shell.mu.Unlock()
	if shell.notify != nil {
		shell.notify()
	}
}

func (shell *moshShell) Drain(dst []byte) []byte {
	shell.mu.Lock()
	if len(shell.local) > 0 {
		dst = append(dst, shell.local...)
		shell.local = shell.local[:0]
	}
	shell.mu.Unlock()
	return shell.client.Drain(dst)
}

func (shell *moshShell) Write(p []byte) bool {
	id := shell.client.SendInput(p)
	if id == 0 {
		return false
	}
	shell.mu.Lock()
	shell.lastInput = id
	shell.mu.Unlock()
	return true
}

func (shell *moshShell) Resize(cols, rows int) { shell.client.Resize(cols, rows) }

func (shell *moshShell) Done() <-chan struct{} { return shell.client.Done() }

func (shell *moshShell) Err() error { return shell.client.Err() }

func (shell *moshShell) Close() { shell.client.Close() }

// The prediction overlay asks the shell which of its input the server has
// applied, so a guess survives a long round trip but is still taken back when
// nobody echoes it.
func (shell *moshShell) LastInputID() uint64 {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	return uint64(shell.lastInput)
}

func (shell *moshShell) AckedInputID() uint64 { return uint64(shell.client.AckedInput()) }

// LinkAlive says the datagram path has been heard from recently. A mosh
// session can survive a long silence by design, so this is about the
// prediction overlay's patience, not about ending the session.
func (shell *moshShell) LinkAlive() bool {
	received := shell.client.LastRecv()
	return !received.IsZero() && time.Since(received) < 10*time.Second
}

func (shell *moshShell) LastReceived() time.Time { return shell.client.LastRecv() }
