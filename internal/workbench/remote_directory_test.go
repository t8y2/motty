package workbench

import (
	"testing"
	"time"

	"motty/internal/sshconn"
)

func TestRemoteDirectoryFollowsOnlyTrustedPromptReports(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	shell.say("\x1b]7;file://box/srv/program\x07")
	state.Wake()
	if session.startDirectory != "" {
		t.Fatalf("a report before shell integration changed the start directory to %q", session.startDirectory)
	}

	reportDirectory(state, session, shell, "/srv/app")
	if session.startDirectory != "/srv/app" {
		t.Fatalf("trusted prompt left start directory %q, want /srv/app", session.startDirectory)
	}

	shell.say(folderSyncMarker(session, "C") + "\x1b]7;file://box/srv/program\x07")
	state.Wake()
	if session.startDirectory != "/srv/app" {
		t.Fatalf("a running program changed the start directory to %q", session.startDirectory)
	}

	reportDirectory(state, session, shell, "/srv/app/./nested")
	if session.startDirectory != "/srv/app/nested" {
		t.Fatalf("trusted prompt left start directory %q, want /srv/app/nested", session.startDirectory)
	}
}

func TestRestoredRemoteSessionStartsInLastTrustedDirectory(t *testing.T) {
	state, world, _, shell := liveSession(t)
	reportDirectory(state, &state.sessions[0], shell, "/srv/app")

	saved, _, err := loadState(state.options.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Sessions) != 1 || saved.Sessions[0].RemoteDirectory != "/srv/app" {
		t.Fatalf("saved sessions = %+v, want one with remote directory /srv/app", saved.Sessions)
	}

	restored := newTestState(t, state.options)
	if len(restored.sessions) != 1 {
		t.Fatalf("restored sessions = %d, want 1", len(restored.sessions))
	}
	session := &restored.sessions[0]
	if session.startDirectory != "/srv/app" {
		t.Fatalf("restored start directory = %q, want /srv/app", session.startDirectory)
	}
	if directory := session.splitDirectory(); directory != "/srv/app" {
		t.Fatalf("a split before reconnect starts in %q, want /srv/app", directory)
	}

	restored.reconnect(session)
	if len(world.conns) != 2 {
		t.Fatalf("reconnect dialed %d connections, want 2 total", len(world.conns))
	}
	connection := world.conns[len(world.conns)-1]
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	restored.Wake()

	connection.mu.Lock()
	configs := append([]sshconn.ShellConfig(nil), connection.configs...)
	connection.mu.Unlock()
	if len(configs) != 1 || configs[0].StartDirectory != "/srv/app" {
		t.Fatalf("shell configs = %+v, want one starting in /srv/app", configs)
	}
}

func TestAutomaticReconnectStartsInLastTrustedDirectory(t *testing.T) {
	state, world, connection, clock := reconnectingSession(t, reconnectThree)
	session := &state.sessions[0]
	reportDirectory(state, session, connection.shell(t), "/srv/app")

	disconnectEstablished(connection, state)
	*clock += time.Second
	state.Wake()
	if len(world.conns) != 2 {
		t.Fatalf("reconnect dialed %d connections, want 2 total", len(world.conns))
	}
	retry := world.conns[len(world.conns)-1]
	retry.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()

	retry.mu.Lock()
	configs := append([]sshconn.ShellConfig(nil), retry.configs...)
	retry.mu.Unlock()
	if len(configs) != 1 || configs[0].StartDirectory != "/srv/app" {
		t.Fatalf("shell configs = %+v, want one starting in /srv/app", configs)
	}
}
