package workbench

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sessionimport"
)

func requireActionNotice(t *testing.T, state *State, detail string, kind components.ToastType) {
	t.Helper()
	if len(state.fileNotices.items) != 1 {
		t.Fatalf("expected one visible notice, got %v", state.fileNotices.items)
	}
	n := state.fileNotices.items[0]
	if n.title == "" || n.detail == "" || n.detail != detail || n.kind != kind || state.status != detail {
		t.Fatalf("notice=%+v status=%q want=%q", n, state.status, detail)
	}
}

func TestCreationButtonsUseOnlySSHHosts(t *testing.T) {
	for _, protocols := range [][]hostProtocol{nil, {protocolRDP, protocolVNC}, {protocolSSH, protocolTelnet}, {protocolSSH, protocolSSH}} {
		t.Run(fmt.Sprint(protocols), func(t *testing.T) {
			_, o := newWorld(t)
			s := newTestState(t, o)
			s.hosts = nil
			sshHosts := 0
			for i, protocol := range protocols {
				s.hosts = append(s.hosts, Host{ID: uint32(i + 1), Protocol: protocol})
				if protocol == protocolSSH {
					sshHosts++
				}
			}
			for _, page := range []struct {
				section uint8
				id      gfx.ID
				minimum int
			}{{sectionForwards, hitForwardAdd, 1}, {sectionProxies, hitProxyAdd, 2}} {
				s.activeSection = page.section
				canvas, _ := paintedTexts(s)
				buttons := 0
				for _, hit := range canvas.Hits() {
					if hit.ID == page.id {
						buttons++
						if hit.Disabled != (sshHosts < page.minimum) {
							t.Fatalf("wrong creation availability: %+v", hit)
						}
					}
				}
				if buttons != 2 {
					t.Fatalf("expected toolbar and empty-state buttons, got %d", buttons)
				}
			}
		})
	}
}

func TestNoChangeActionsUseInformationalNotices(t *testing.T) {
	t.Run("empty output", func(t *testing.T) {
		_, o := newWorld(t)
		s := newTestState(t, o)
		s.saveTerminalOutput(&Session{term: newTerminal(80, 24)})
		requireActionNotice(t, s, s.messages.Tools.OutputEmpty, components.ToastInfo)
	})
	t.Run("duplicate keys", func(t *testing.T) {
		_, o := newWorld(t)
		key := writeWorkbenchIdentity(t, "id_test", "")
		o.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{key}, nil }
		s := newTestState(t, o)
		s.importKeys()
		s.importKeys()
		requireActionNotice(t, s, s.messages.KeyNothingImported, components.ToastInfo)
		if len(s.identities) != 1 {
			t.Fatal("duplicate import changed the key list")
		}
	})
}

func TestMissingActionPrerequisitesHaveVisibleFeedback(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*State)
		run     func(*State)
		want    func(*State) string
	}{
		{"forward/stale action", func(s *State) { s.hosts = nil; s.activeSection = sectionForwards }, func(s *State) { s.Click(hitForwardAdd) }, func(s *State) string { return s.messages.ErrForwardHostRequired }},
		{"forward/desktop only", func(s *State) {
			for i := range s.hosts {
				s.hosts[i].Protocol = protocolVNC
			}
		}, (*State).openForwardDialog, func(s *State) string { return s.messages.ErrForwardHostRequired }},
		{"proxy/one SSH host", func(s *State) { s.hosts = s.hosts[:1] }, (*State).openProxyDialog, func(s *State) string { return s.messages.ErrProxyHostsRequired }},
		{"snippet/no terminal", nil, func(s *State) { s.sendSnippet(0) }, func(s *State) string { return s.messages.Tools.StatusSnippetNoTerminal }},
		{"snippet/from page", nil, func(s *State) {
			s.snippets.list = []snippet{{Name: "list", Command: "ls"}}
			s.sendSnippetFromPage(0, gfx.Point{})
		}, func(s *State) string { return s.messages.Tools.StatusSnippetNoTerminal }},
		{"snippet/disconnected", func(s *State) {
			s.sessions = []Session{{ID: 1, term: newTerminal(80, 24)}}
			s.activeSessionID = 1
			s.snippets.list = []snippet{{Name: "list", Command: "ls"}}
		}, func(s *State) { s.sendSnippet(0) }, func(s *State) string { return s.messages.Tools.StatusSnippetNotConnected }},
		{"history/no terminal", nil, (*State).openHistoryActive, func(s *State) string { return s.messages.History.StatusNoTerminal }},
		{"backup/open session", func(s *State) { s.sessions = []Session{{ID: 1}} }, (*State).openRestoreBackup, func(s *State) string { return s.messages.Tools.BackupErrSessions }},
		{"broadcast/no workspace", nil, (*State).toggleBroadcast, func(s *State) string { return s.messages.Tools.StatusBroadcastNeedsSplit }},
		{"connection/invalid address", func(s *State) { s.search.Insert("bad host name") }, (*State).connectTyped, func(s *State) string { return s.messages.ActionFeedback.InvalidConnection }},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, options := newWorld(t)
			wakes := 0
			options.Invalidate = func() { wakes++ }
			s := newTestState(t, options)
			if test.prepare != nil {
				test.prepare(s)
			}
			before := wakes
			test.run(s)
			requireActionNotice(t, s, test.want(s), components.ToastWarning)
			if s.modal != modalNone || wakes <= before {
				t.Fatalf("blocked action changed modal or failed to invalidate: %v", s.modal)
			}
		})
	}
}

func TestReferencedHostAndKeyRemovalExplainTheBlock(t *testing.T) {
	t.Run("host", func(t *testing.T) {
		_, o := newWorld(t)
		s := newTestState(t, o)
		host := s.hosts[0]
		s.forwards = []forwardRecord{{HostAddress: host.Address, HostPort: host.Port, HostUser: host.User}}
		s.hostActionIDs = []uint32{host.ID}
		before := len(s.hosts)
		s.openDeleteCapturedHosts()
		requireActionNotice(t, s, fmt.Sprintf(s.messages.HostDeleteBlockedForwardFmt, host.Label), components.ToastWarning)
		if len(s.hosts) != before || s.modal != modalNone {
			t.Fatal("blocked deletion changed hosts")
		}
	})
	t.Run("key", func(t *testing.T) {
		_, o := newWorld(t)
		key := writeWorkbenchIdentity(t, "id_test", "")
		o.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{key}, nil }
		s := newTestState(t, o)
		s.importKeys()
		s.hosts[0].Auth = authPrivateKey
		s.hosts[0].Identity = s.identities[0].Fingerprint
		s.removeSelectedIdentity()
		requireActionNotice(t, s, fmt.Sprintf(s.messages.KeyInUseFmt, s.hosts[0].Label), components.ToastWarning)
		if len(s.identities) != 1 {
			t.Fatal("removed a referenced key")
		}
	})
}

func TestForegroundPickerFailuresUseTheSFTPToastQueue(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*State)
	}{{"SSH import", (*State).importSSHConfig}, {"session import", func(s *State) { s.importSessions(sessionimport.Xshell) }}, {"catalog import", (*State).importHostCatalog}, {"catalog export", (*State).exportHostCatalog}, {"key import", (*State).importKeys}, {"backup restore", (*State).openRestoreBackup}, {"diagnostics export", (*State).openExportDiagnostics}, {"output export", func(s *State) {
		session := &Session{term: newTerminal(80, 24)}
		session.term.write([]byte("output"))
		s.saveTerminalOutput(session)
	}}, {"recording export", func(s *State) {
		session := &Session{term: newTerminal(80, 24)}
		session.term.recording.start(0)
		session.term.recording.append(0, terminalRecordingOutput, []byte("output"))
		s.exportTerminalRecording(session)
	}}, {"new window", (*State).openNewWindow}} {
		t.Run(test.name, func(t *testing.T) {
			_, o := newWorld(t)
			failure := errors.New("picker unavailable")
			o.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return nil, failure }
			o.SaveFile = func(app.SaveFileOptions) (string, error) { return "", failure }
			o.NewWindow = func() error { return failure }
			s := newTestState(t, o)
			test.run(s)
			want := s.messages.ActionFeedback.FilePickerFailed
			if test.name == "new window" {
				want = s.messages.ActionFeedback.NewWindowFailed
			}
			requireActionNotice(t, s, want, components.ToastError)
			if strings.Contains(s.status, failure.Error()) {
				t.Fatalf("native error leaked into the notice: %q", s.status)
			}
		})
	}
}

func TestCanceledPickersAndValidDialogsDoNotShowWarning(t *testing.T) {
	for _, run := range []func(*State){(*State).importKeys, (*State).importHostCatalog, (*State).exportHostCatalog, (*State).importSSHConfig, func(s *State) { s.importSessions(sessionimport.Xshell) }, (*State).openRestoreBackup, (*State).openExportDiagnostics} {
		_, o := newWorld(t)
		o.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return nil, app.ErrCanceled }
		o.SaveFile = func(app.SaveFileOptions) (string, error) { return "", app.ErrCanceled }
		s := newTestState(t, o)
		run(s)
		if len(s.fileNotices.items) != 0 {
			t.Fatal("canceling a picker created a warning")
		}
	}
	_, o := newWorld(t)
	s := newTestState(t, o)
	s.openForwardDialog()
	if s.modal != modalNewForward || len(s.fileNotices.items) != 0 {
		t.Fatal("valid forward creation was blocked")
	}
	s.closeForwardDialog()
	s.openProxyDialog()
	if s.modal != modalNewProxy || len(s.fileNotices.items) != 0 {
		t.Fatal("valid route creation was blocked")
	}
}

func TestRepeatedBlockedActionDeduplicatesAndCanDismiss(t *testing.T) {
	_, o := newWorld(t)
	s := newTestState(t, o)
	s.hosts = nil
	for range 12 {
		s.openForwardDialog()
	}
	requireActionNotice(t, s, s.messages.ErrForwardHostRequired, components.ToastWarning)
	if s.fileNotices.queue.Len() != 1 {
		t.Fatal("duplicate notice accumulated")
	}
	s.Click(s.fileNotices.items[0].id)
	if len(s.fileNotices.items) != 0 || s.fileNotices.queue.Len() != 0 {
		t.Fatal("notice did not dismiss")
	}
}

func TestKeyImportKeepsInvalidFileReasonWithPartialSuccess(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(fmt.Sprintf("partial=%t", partial), func(t *testing.T) {
			_, o := newWorld(t)
			paths := []string{filepath.Join(t.TempDir(), "missing-private-key")}
			if partial {
				paths = append(paths, writeWorkbenchIdentity(t, "id_valid", ""))
			}
			o.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return paths, nil }
			s := newTestState(t, o)
			s.importKeys()
			if len(s.fileNotices.items) != 1 || !strings.Contains(s.fileNotices.items[0].detail, "missing-private-key") || s.fileNotices.items[0].kind != components.ToastError {
				t.Fatalf("invalid key reason was lost: %+v", s.fileNotices.items)
			}
			if partial {
				if len(s.identities) != 1 || s.status != fmt.Sprintf(s.messages.KeyImportedFmt, 1) {
					t.Fatalf("valid key was not imported: identities=%d status=%q", len(s.identities), s.status)
				}
			} else if len(s.identities) != 0 || s.status != s.fileNotices.items[0].detail {
				t.Fatalf("invalid key failure was overwritten: %q", s.status)
			}
		})
	}
}
