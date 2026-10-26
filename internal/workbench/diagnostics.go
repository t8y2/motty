package workbench

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/diag"
	"motty/internal/sshconn"
	"motty/internal/update"
)

// The diagnostics report is what a user attaches to a bug report: the build, the
// settings, what is open and how each connection went, and the program's recent
// log. It is plain text the user can read before sending, and it names no host,
// account or address: those become host-1, user-1 and so on.

func phaseName(phase sshconn.Phase) string {
	switch phase {
	case sshconn.PhaseConnecting:
		return "connecting"
	case sshconn.PhaseHostKeyPrompt:
		return "host-key-prompt"
	case sshconn.PhaseAuthenticating:
		return "authenticating"
	case sshconn.PhaseKeyPassphrasePrompt:
		return "key-passphrase-prompt"
	case sshconn.PhasePasswordPrompt:
		return "password-prompt"
	case sshconn.PhaseConnected:
		return "connected"
	case sshconn.PhaseFailed:
		return "failed"
	case sshconn.PhaseClosed:
		return "closed"
	}
	return fmt.Sprintf("phase-%d", phase)
}

// orDefault names what an unset setting means.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func protocolOf(host Host) string { return strings.ToLower(protocolName(host.Protocol)) }

// diagnosticsRedactor learns every name the user's data holds.
func (state *State) diagnosticsRedactor() (*diag.Redactor, map[uint32]string) {
	var redactor diag.Redactor
	aliases := make(map[uint32]string, len(state.hosts))
	users := map[string]string{}
	hide := func(label, address, user string) {
		redactor.Hide(address, "host")
		redactor.Hide(label, "label")
		if user != "" {
			if _, ok := users[user]; !ok {
				users[user] = fmt.Sprintf("user-%d", len(users)+1)
			}
			redactor.Hide(user, users[user])
		}
	}
	for index, host := range state.hosts {
		alias := fmt.Sprintf("host-%d", index+1)
		aliases[host.ID] = alias
		redactor.Hide(host.Address, alias)
		redactor.Hide(host.Label, alias)
		hide("", "", host.User)
	}
	for index := range state.sessions {
		host := state.sessions[index].host
		hide(host.Label, host.Address, host.User)
		redactor.Hide(state.sessions[index].Title, "tab")
	}
	if state.proxy.address != "" {
		redactor.Hide(state.proxy.address, "outbound-proxy")
		hide("", "", state.proxy.user)
	}
	if name, err := os.Hostname(); err == nil {
		redactor.Hide(name, "this-computer")
	}
	for _, key := range []string{"USER", "USERNAME", "LOGNAME"} {
		redactor.Hide(os.Getenv(key), "local-user")
	}
	return &redactor, aliases
}

// diagnosticReport builds the report for this window and its connections.
func (state *State) diagnosticReport() string {
	redactor, aliases := state.diagnosticsRedactor()
	report := diag.NewReport(redactor)
	now := state.options.WallNow()

	report.Section("Motty diagnostics")
	report.Fact("created", now.UTC().Format(time.RFC3339))
	report.Fact("version", update.Current)
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision", "vcs.time", "vcs.modified":
				report.Fact(setting.Key, setting.Value)
			}
		}
	}
	report.Fact("go", runtime.Version())
	report.Fact("platform", runtime.GOOS+"/"+runtime.GOARCH)
	report.Fact("window", fmt.Sprintf("%.0fx%.0f", state.viewport.Width, state.viewport.Height))
	report.Fact("appearance", orDefault(state.appearanceMode.saved(), "system"))
	report.Fact("state file", state.stateFileOutcome)
	report.Fact("ssh agent", os.Getenv("SSH_AUTH_SOCK") != "")

	saved := state.snapshot()
	report.Section("Settings")
	report.Fact("language", orDefault(saved.Language, "automatic"))
	report.Fact("theme", orDefault(saved.Theme, "system"))
	report.Fact("terminal", fmt.Sprintf("%s %s %dpx %q scheme=%s padding=%s scrollback=%d",
		saved.TerminalOption, saved.TerminalFontFace, saved.TerminalFontSize, saved.TerminalFontFamily, saved.TerminalScheme, saved.TerminalPadding, saved.TerminalScrollback))
	report.Fact("clipboard", saved.TerminalClipboard)
	report.Fact("connection", fmt.Sprintf("timeout=%ds keepalive=%ds reconnect=%s restore=%s share=%s",
		saved.ConnectionTimeoutSeconds, saved.ConnectionKeepAliveSeconds, saved.ConnectionReconnect, saved.RestoreConnect, saved.ShareConnections))
	report.Fact("files", fmt.Sprintf("parallel=%d keep-times=%q follow=%q", saved.SFTPParallel, saved.SFTPKeepTimes, saved.SFTPFollow))
	report.Fact("update check at start", saved.UpdateCheck != "")
	if saved.OutboundProxy != nil {
		report.Fact("outbound proxy", saved.OutboundProxy.Kind)
	}
	report.Fact("sync", saved.Sync != nil)
	report.Fact("background image", saved.Background != nil)
	report.Fact("counts", fmt.Sprintf("hosts=%d groups=%d keys=%d forwards=%d routes=%d snippets=%d highlights=%d bookmarks=%d",
		len(saved.Hosts), len(saved.HostGroups), len(saved.IdentityFiles), len(saved.Forwards), len(saved.Proxies), len(saved.Snippets), len(saved.Highlights), len(saved.SFTPBookmarks)))

	report.Section("Hosts")
	for _, host := range state.hosts {
		line := fmt.Sprintf("%s protocol=%s port=%d system=%s", aliases[host.ID], protocolOf(host), host.Port, host.System)
		if host.Algo != nil {
			line += " custom-algorithms"
		}
		report.Fact("host", line)
	}

	report.Section("Sessions")
	for index := range state.sessions {
		session := &state.sessions[index]
		alias := aliases[session.HostID]
		if session.isLocal() {
			alias = "this-computer"
		} else if alias == "" {
			alias = "unsaved"
		}
		active := ""
		if session.ID == state.activeSessionID {
			active = " active"
		}
		report.Fact(fmt.Sprintf("tab %d", index+1), fmt.Sprintf("%s%s shared=%v system=%s", alias, active, session.shared, session.host.System))
		if session.conn == nil {
			continue
		}
		status := session.conn.Status()
		report.Fact("  phase", phaseName(status.Phase))
		if status.Method != "" {
			report.Fact("  method", status.Method)
		}
		if status.ServerVersion != "" {
			report.Field("  server", status.ServerVersion)
		}
		if negotiated := status.Negotiated.String(); status.Negotiated != (sshconn.Negotiated{}) {
			report.Fact("  negotiated", negotiated)
		}
		if status.Err != nil {
			report.Field("  error", status.Err.Error())
			report.Fact("  error kind", errorKind(status.Err))
		}
		if session.term != nil {
			screen := session.term.screen
			report.Fact("  terminal", fmt.Sprintf("scrollback=%d ended=%v", screen.Scrollback(), session.term.ended))
		}
	}

	report.Section("Recent log")
	lines := state.options.Log.Lines()
	if len(lines) == 0 {
		report.Line("(nothing logged)")
	}
	for _, line := range lines {
		report.Line(line)
	}
	return report.String()
}

// errorKind names the sshconn error a failure is, which the message alone (it
// may be wrapped or localized) does not say reliably.
func errorKind(err error) string {
	for _, known := range []struct {
		err  error
		name string
	}{
		{sshconn.ErrCanceled, "canceled"}, {sshconn.ErrHostKeyRejected, "host-key-rejected"},
		{sshconn.ErrHostKeyChanged, "host-key-changed"}, {sshconn.ErrAuthFailed, "auth-failed"},
		{sshconn.ErrDisconnected, "disconnected"}, {sshconn.ErrKeepAliveTimeout, "keepalive-timeout"},
	} {
		if errors.Is(err, known.err) {
			return known.name
		}
	}
	return "other"
}

// openExportDiagnostics asks where to save the report and writes it.
func (state *State) openExportDiagnostics() {
	m := state.messages.Tools
	path, err := state.options.SaveFile(app.SaveFileOptions{
		Title:            m.DiagnosticsSaveTitle,
		SuggestedName:    "motty-diagnostics-" + state.options.WallNow().Format("20060102-1504") + ".txt",
		DefaultExtension: "txt",
		Filters:          []app.FileFilter{{Name: "Text", Pattern: "*.txt"}},
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && path == "" {
		return
	}
	if err == nil {
		err = writePrivateAtomic(path, []byte(state.diagnosticReport()))
	}
	if err != nil {
		state.status = fmt.Sprintf(m.DiagnosticsFailedFmt, err)
		return
	}
	state.status = fmt.Sprintf(m.DiagnosticsSavedFmt, path)
}
