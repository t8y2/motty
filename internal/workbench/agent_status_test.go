package workbench

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"motty/internal/ai/gate"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
	"motty/internal/ai/tools"
)

// monitorHandler answers the monitor's fixed commands the way a Linux host
// does: two counter readings, then silence until the client leaves; and the
// details listing.
func monitorHandler(channel ssh.Channel, command string, closed <-chan struct{}) {
	switch {
	case strings.Contains(command, "motty-stats-v2"):
		io.WriteString(channel, "gop-stats-v2 100 1000 0 0 1000 400 10.00\n")
		io.WriteString(channel, "gop-stats-v2 150 1100 0 0 1000 400 10.25\n")
		<-closed
		return
	case strings.Contains(command, "motty-monitor-details-v1"):
		io.WriteString(channel, "info\t4\t0.10\t0.20\t0.30\t100.0\t6.1.0\n")
		io.WriteString(channel, "disk\t1000\t900\t100\t90%\t0\t/data\n")
		io.WriteString(channel, "disk\t2000\t200\t1800\t10%\t0\t/\n")
		io.WriteString(channel, "disk\t500\t1\t499\t1%\t1\t/run\n")
	default:
		io.WriteString(channel, "unknown command\n")
	}
	channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
	channel.Close()
}

// withUnreachableHost adds a second authorized host whose port nobody listens
// on.
func withUnreachableHost(t *testing.T, world *agentWorld) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	world.catalog.hosts = append(world.catalog.hosts, Host{
		ID: 2, CatalogID: "host_down", Label: "down", Address: "127.0.0.1", Port: port, User: "ops",
		Protocol: protocolSSH, Auth: authPassword,
	})
	world.catalog.agentHosts["host_down"] = true
}

func runStatusTool(t *testing.T, backend *agentBackend, mode gate.Mode, name, arguments string) gate.Result {
	t.Helper()
	g := gate.Gate{Backend: backend, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low}, Mode: mode}
	return g.Run(context.Background(), name, []byte(arguments))
}

func TestHostResourcesAnswersForReachableHostsWhileAnotherIsDown(t *testing.T) {
	world := newAgentWorld(t)
	world.server.commandHandler = monitorHandler
	withUnreachableHost(t, world)
	backend := world.backend()

	// Read-only mode: nobody opened command access, and none is needed.
	result := runStatusTool(t, backend, gate.ModeObserve, tools.NameHostResources, `{"sessions":["host_down","host_web"]}`)
	if result.IsError {
		t.Fatalf("one reachable host must make an answer: %+v", result)
	}
	var reply struct{ Hosts []gate.HostResources }
	if err := json.Unmarshal(result.Structured, &reply); err != nil || len(reply.Hosts) != 2 {
		t.Fatalf("structured = %s, %v", result.Structured, err)
	}
	down, web := reply.Hosts[0], reply.Hosts[1]
	if down.Host != "host_down" || down.Error == "" || down.CPUPercent != nil || len(down.Disks) != 0 {
		t.Errorf("an unreachable host must carry only its error: %+v", down)
	}
	if web.Host != "host_web" || web.Error != "" || web.CollectedAt == "" {
		t.Fatalf("the reachable host = %+v", web)
	}
	if _, err := time.Parse(time.RFC3339, web.CollectedAt); err != nil {
		t.Errorf("collected_at %q: %v", web.CollectedAt, err)
	}
	if web.CPUPercent == nil || *web.CPUPercent != 50 {
		t.Errorf("cpu = %v, want 50 from the two counter lines", web.CPUPercent)
	}
	if web.Memory == nil || web.Memory.TotalBytes != 1000<<10 || web.Memory.UsedBytes != 600<<10 || web.Memory.Percent != 60 {
		t.Errorf("memory = %+v", web.Memory)
	}
	if len(web.Disks) != 2 || web.Disks[0].Mount != "/" || web.Disks[1].Mount != "/data" || web.Disks[1].Percent != 90 {
		t.Errorf("disks = %+v: mounts only, no virtual filesystem", web.Disks)
	}
	if len(web.Warnings) != 0 {
		t.Errorf("warnings = %v", web.Warnings)
	}
	if !strings.Contains(result.Text, "disk /data: 90%") {
		t.Errorf("text = %q", result.Text)
	}
	// The sampling commands end with the call: nothing keeps streaming on the
	// connection that goes back to the pool.
	deadline := time.Now().Add(3 * time.Second)
	for {
		backend.mu.Lock()
		entry := backend.entries["host_web"]
		backend.mu.Unlock()
		entry.mu.Lock()
		idle := entry.active == 0
		entry.mu.Unlock()
		if idle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the connection is still in use after the call")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHostResourcesDefaultsToTheOnlyHostAndRefusesTheUnauthorized(t *testing.T) {
	world := newAgentWorld(t)
	world.server.commandHandler = monitorHandler
	backend := world.backend()
	if result := runStatusTool(t, backend, gate.ModeObserve, tools.NameHostResources, `{}`); result.IsError || !strings.Contains(result.Text, "host_web") {
		t.Fatalf("the only host is the default: %+v", result)
	}
	result := runStatusTool(t, backend, gate.ModeObserve, tools.NameHostResources, `{"sessions":["host_other"]}`)
	if !result.IsError || !strings.Contains(result.Text, "no such host") {
		t.Fatalf("a host that is not open to agents was sampled: %+v", result)
	}
	withUnreachableHost(t, world)
	backend = world.backend()
	if result := runStatusTool(t, backend, gate.ModeObserve, tools.NameHostResources, `{}`); !result.IsError || !strings.Contains(result.Text, "name a host") {
		t.Fatalf("several hosts need a name: %+v", result)
	}
}

func TestHostActivityListsOnlyAuthorizedHostsWithSecretsMasked(t *testing.T) {
	world := newAgentWorld(t)
	backend := world.backend()
	log, err := store.Open(world.options.AIDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	backend.auditLog = log
	at := time.Now().Add(-time.Hour)
	for index, entry := range []store.AuditEntry{
		{HostID: "host_web", HostLabel: "web", Tool: "terminal_exec", Summary: "deploy --password hunter2hunter2", Outcome: store.AuditOK},
		{HostID: "host_closed", HostLabel: "closed", Tool: "terminal_exec", Summary: "uptime", Outcome: store.AuditOK},
		{HostID: "host_web,host_closed", HostLabel: "web, closed", Tool: "terminal_exec_many", Summary: "df -h", Outcome: store.AuditFailed},
		{HostLabel: "notes", Tool: "note_query", Summary: "list", Outcome: store.AuditOK},
		{HostID: "host_web", HostLabel: "web", Tool: "transfer_queued", Summary: "t1 upload /a <-> /b", Outcome: store.AuditOK},
	} {
		entry.At = at.Add(time.Duration(index) * time.Minute)
		if err := log.RecordAudit(entry); err != nil {
			t.Fatal(err)
		}
	}

	result := runStatusTool(t, backend, gate.ModeObserve, tools.NameHostActivity, `{}`)
	if result.IsError {
		t.Fatalf("host_activity: %+v", result)
	}
	var reply struct{ Entries []gate.ActivityEntry }
	if err := json.Unmarshal(result.Structured, &reply); err != nil {
		t.Fatal(err)
	}
	var used []string
	for _, entry := range reply.Entries {
		used = append(used, entry.Tool)
		if strings.Contains(entry.Summary, "hunter2") || strings.Contains(entry.Host, "closed") {
			t.Errorf("an entry leaks a secret or a host the user did not open: %+v", entry)
		}
	}
	if got := strings.Join(used, ","); got != "transfer_queued,terminal_exec_many,terminal_exec" {
		t.Errorf("tools newest first = %s: notes and closed hosts stay out", got)
	}
	if reply.Entries[1].Host != "web" || reply.Entries[1].Outcome != store.AuditFailed {
		t.Errorf("a call on two hosts = %+v", reply.Entries[1])
	}
	if !strings.Contains(reply.Entries[2].Summary, "--password ***") {
		t.Errorf("the credential was not masked: %q", reply.Entries[2].Summary)
	}

	one := runStatusTool(t, backend, gate.ModeObserve, tools.NameHostActivity, `{"session":"host_web","limit":1}`)
	var page struct{ Entries []gate.ActivityEntry }
	if err := json.Unmarshal(one.Structured, &page); err != nil || len(page.Entries) != 1 || page.Entries[0].Tool != "transfer_queued" {
		t.Fatalf("limit 1 = %s, %v", one.Structured, err)
	}
	if bad := runStatusTool(t, backend, gate.ModeObserve, tools.NameHostActivity, `{"session":"host_closed"}`); !bad.IsError {
		t.Fatalf("a closed host's history was served: %+v", bad)
	}
}
