package workbench

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/vivaui/viva/app"
	"motty/internal/sshconn"
)

type connectionEventKind uint8

const (
	connectionEventConnectStarted connectionEventKind = iota
	connectionEventHostKeyPrompt
	connectionEventAuthenticating
	connectionEventConnected
	connectionEventClosed
	connectionEventFailed
	connectionEventReconnectWaiting
	connectionEventReconnectStarted
	connectionEventReconnectStopped
	connectionEventReconnectCanceled
)

type connectionEvent struct {
	at      time.Duration
	kind    connectionEventKind
	attempt int
	detail  string
}

func (state *State) recordConnectionEvent(session *Session, kind connectionEventKind, detail string) {
	session.connectionStory = append(session.connectionStory, connectionEvent{
		at: state.now(), kind: kind, attempt: session.reconnectAttempt, detail: cleanConnectionLogDetail(detail),
	})
	// The exported log keeps its free-text detail; the journal stores only the
	// typed step, so a server error string can never reach the database.
	state.journalConnectionEvent(session, kind)
}

func (state *State) recordConnectionStatus(session *Session, previous sshconn.Phase, status sshconn.Status) {
	if status.Phase == previous && status.Phase != sshconn.PhaseClosed && status.Phase != sshconn.PhaseFailed {
		return
	}
	switch status.Phase {
	case sshconn.PhaseHostKeyPrompt:
		state.recordConnectionEvent(session, connectionEventHostKeyPrompt, "")
	case sshconn.PhaseAuthenticating, sshconn.PhaseKeyPassphrasePrompt, sshconn.PhasePasswordPrompt:
		state.recordConnectionEvent(session, connectionEventAuthenticating, "")
	case sshconn.PhaseConnected:
		detail := status.Method
		if status.ServerVersion != "" {
			if detail != "" {
				detail += " · "
			}
			detail += status.ServerVersion
		}
		if status.Negotiated.KeyExchange != "" {
			if detail != "" {
				detail += " · "
			}
			detail += fmt.Sprintf(state.messages.Tools.AlgoNegotiatedFmt, status.Negotiated)
		}
		state.recordConnectionEvent(session, connectionEventConnected, detail)
	case sshconn.PhaseClosed:
		state.recordConnectionEvent(session, connectionEventClosed, safeConnectionLogError(status.Err))
	case sshconn.PhaseFailed:
		state.recordConnectionEvent(session, connectionEventFailed, safeConnectionLogError(status.Err))
	}
}

func safeConnectionLogError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, sshconn.ErrKeepAliveTimeout):
		return "keepalive timed out"
	case errors.Is(err, sshconn.ErrAuthFailed):
		return "authentication failed"
	case errors.Is(err, sshconn.ErrHostKeyChanged):
		return "host key changed"
	case errors.Is(err, sshconn.ErrHostKeyRejected):
		return "host key rejected"
	case errors.Is(err, sshconn.ErrCanceled):
		return "canceled"
	default:
		return err.Error()
	}
}

func cleanConnectionLogDetail(value string) string {
	return strings.Map(func(value rune) rune {
		if value == '\r' || value == '\n' || value == 0 {
			return ' '
		}
		return value
	}, value)
}

func appendConnectionLog(dst []byte, session *Session) []byte {
	dst = append(dst, "Motty connection log\n\nSession: "...)
	dst = append(dst, session.Title...)
	dst = append(dst, "\nTarget: "...)
	dst = append(dst, session.Address...)
	dst = append(dst, "\n\nConnection story:\n"...)
	origin := time.Duration(0)
	if len(session.connectionStory) > 0 {
		origin = session.connectionStory[0].at
	}
	for _, event := range session.connectionStory {
		dst = fmt.Appendf(dst, "[%s] %s\n", clockText(int64(max(event.at-origin, 0)/time.Second)), connectionEventText(event))
	}
	dst = append(dst, "\nTerminal retained text:\n"...)
	if session.term != nil {
		dst = session.term.screen.AppendRetainedText(dst)
	}
	dst = append(dst, '\n')
	return dst
}

func connectionEventText(event connectionEvent) string {
	var text string
	switch event.kind {
	case connectionEventConnectStarted:
		text = "Connection started"
	case connectionEventHostKeyPrompt:
		text = "Waiting for host key verification"
	case connectionEventAuthenticating:
		text = "Authenticating"
	case connectionEventConnected:
		text = "Connected"
	case connectionEventClosed:
		text = "Connection closed"
	case connectionEventFailed:
		text = "Connection failed"
	case connectionEventReconnectWaiting:
		text = fmt.Sprintf("Reconnect attempt %d scheduled", event.attempt)
	case connectionEventReconnectStarted:
		text = fmt.Sprintf("Reconnect attempt %d started", event.attempt)
	case connectionEventReconnectStopped:
		text = fmt.Sprintf("Automatic reconnect stopped after %d attempts", event.attempt)
	case connectionEventReconnectCanceled:
		text = "Automatic reconnect canceled"
	}
	if event.detail != "" {
		text += ": " + event.detail
	}
	return text
}

func (state *State) exportConnectionLog(session *Session) {
	path, err := state.options.SaveFile(app.SaveFileOptions{
		Title:            state.messages.ConnExportLog,
		SuggestedName:    connectionLogName(session.Title, time.Now()),
		DefaultExtension: "txt",
		Filters:          []app.FileFilter{{Name: "Text", Pattern: "*.txt"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && path == "" {
		return
	}
	if err != nil {
		state.reportConnectionExport(session, fmt.Sprintf(state.messages.ConnExportFailedFmt, err), true)
		return
	}
	data := appendConnectionLog(nil, session)
	if err := writePrivateAtomic(path, data); err != nil {
		state.reportConnectionExport(session, fmt.Sprintf(state.messages.ConnExportFailedFmt, err), true)
		return
	}
	state.reportConnectionExport(session, fmt.Sprintf(state.messages.ConnExportedFmt, path), false)
}

// reportConnectionExport tells the user how an export ended. A live terminal is
// the remote shell's screen: a line written into it moves the cursor under the
// prompt without the shell knowing, so typing lands in the wrong place. Only
// the status line and a notice are used there. Before the shell exists, or
// after it is gone, the terminal is the connection's own log page and the line
// belongs in it.
func (state *State) reportConnectionExport(session *Session, message string, failed bool) {
	state.status = message
	if session.term == nil {
		return
	}
	if session.term.live {
		if failed {
			state.showActionError(state.messages.ConnExportLog, message)
		}
		return
	}
	style := sgrMuted
	if failed {
		style = sgrFail
	}
	session.term.say(style, message)
	session.showLog = true
}

// exportFileName suggests a name for a file saved from a session: the session's
// title, what the file holds, and the moment it was saved, so a second export
// of the same session does not offer to overwrite the first.
func exportFileName(title, kind, extension string, at time.Time) string {
	var name strings.Builder
	separator := false
	for _, value := range title {
		switch {
		case unicode.IsLetter(value), unicode.IsDigit(value), value == '-', value == '_':
			name.WriteRune(value)
			separator = false
		case name.Len() > 0 && !separator:
			name.WriteByte('-')
			separator = true
		}
	}
	value := strings.Trim(name.String(), "-")
	if value == "" {
		value = "session"
	}
	return value + "-" + kind + "-" + at.Format("20060102-150405") + "." + extension
}

func connectionLogName(title string, at time.Time) string {
	return exportFileName(title, "connection-log", "txt", at)
}

func writePrivateAtomic(path string, data []byte) (err error) {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".motty-log-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	committed := false
	defer func() {
		file.Close()
		if !committed {
			os.Remove(temporary)
		}
	}()
	if err = file.Chmod(0o600); err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = replaceConnectionLog(temporary, path); err != nil {
		return err
	}
	committed = true
	return nil
}

func replaceConnectionLog(temporary, destination string) error {
	return os.Rename(temporary, destination)
}
