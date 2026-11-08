package workbench

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"
	layoutcore "motty/internal/workspace"
)

func TestSelectedHostsDialTwelveLoopbackSSHConnections(t *testing.T) {
	server := startAgentServer(t)
	_, options := newWorld(t)
	line := knownhosts.Line([]string{knownhosts.Normalize(server.address())}, server.hostKey.PublicKey())
	if err := os.WriteFile(options.KnownHostsPath, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	options.Dial = nil
	state := newTestState(t, options)
	state.hosts = nil
	for index := 0; index < layoutcore.MaxPanes; index++ {
		host := testWorkspaceHost(fmt.Sprintf("server-%d", index), "127.0.0.1")
		host.ID = uint32(index + 1)
		host.Port = server.port
		host.User = "ops"
		host.Auth = authPassword
		state.hosts = append(state.hosts, host)
		if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, hostPasswordPart), server.password); err != nil {
			t.Fatal(err)
		}
		state.Click(hitHostCheckList.At(int(host.ID)))
	}
	state.Click(hitConnectSelectedHosts)
	if state.modal != modalWorkspaceDraft || state.workspaceDraftSelectionCount() != layoutcore.MaxPanes {
		t.Fatalf("batch launch modal=%d selected=%d", state.modal, state.workspaceDraftSelectionCount())
	}
	state.Click(hitWorkspaceDraftSubmit)
	workspaceID := state.deck.ActiveWorkspace()
	if state.modal != modalNone || len(state.deck.Panes(workspaceID, nil)) != layoutcore.MaxPanes {
		t.Fatalf("modal=%d panes=%d", state.modal, len(state.deck.Panes(workspaceID, nil)))
	}
	waitUntil(t, state, "twelve authenticated SSH connections", func() bool {
		connections, _ := server.counts()
		return connections == layoutcore.MaxPanes
	})
}
