package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

// containerRowByKey finds the painted row of one container.
func containerRowByKey(t *testing.T, state *State, key string) remoteRow {
	t.Helper()
	for _, row := range state.remote.rows {
		if row.key == key {
			return row
		}
	}
	t.Fatalf("no row for container %q in %+v", key, state.remote.rows)
	return remoteRow{}
}

func TestContainerTableShowsDockerStatsUsage(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })

	m := state.messages.Tools
	if got := state.remote.tableHeadings[2]; got != m.RemoteColCPU {
		t.Errorf("CPU heading = %q, want %q", got, m.RemoteColCPU)
	}
	if got := state.remote.tableHeadings[3]; got != m.RemoteColContainerMemory {
		t.Errorf("memory heading = %q, want %q", got, m.RemoteColContainerMemory)
	}
	running := containerRowByKey(t, state, "0f1e2d3c4b5a")
	if running.cells[2] != "1.25" || running.cells[3] != "12.5MiB / 7.6GiB" {
		t.Fatalf("running container cells = %q", running.cells)
	}
	// docker stats reports running containers only, so a stopped one has no
	// figures rather than a stale or invented zero.
	stopped := containerRowByKey(t, state, "a1b2c3d4e5f6")
	if stopped.cells[2] != "—" || stopped.cells[3] != "—" {
		t.Fatalf("stopped container cells = %q", stopped.cells)
	}
	canvas := remoteToolsPaint(t, state)
	for _, value := range []string{"1.25", "12.5MiB / 7.6GiB", m.RemoteColContainerMemory} {
		if !remoteCanvasHasText(canvas, value) {
			t.Errorf("container table lacks %q", value)
		}
	}
}

func TestContainerTableKeepsListingWhenUsageFails(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	server.failures["docker stats"] = errors.New("exit status 1")
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })

	if state.remote.failed {
		t.Fatalf("a usage failure marked the whole listing failed: %+v", state.remote.status)
	}
	prefix := strings.TrimSuffix(state.messages.Tools.RemoteContainerUsageFailedFmt, "%s")
	if !strings.Contains(state.remote.status, prefix) || !strings.Contains(state.remote.status, "denied by the test") {
		t.Fatalf("status = %q, want the usage failure named", state.remote.status)
	}
	if row := containerRowByKey(t, state, "0f1e2d3c4b5a"); row.cells[2] != "—" {
		t.Fatalf("usage column = %q after a failed sample", row.cells[2])
	}
}

func TestContainerRowsSortByTheirUsageColumns(t *testing.T) {
	state, _, _ := openRemoteToolsDialog(t)
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	state.remote.containers = []remotetools.Container{
		{ID: "aaaaaaaaaaaa", Name: "slow"},
		{ID: "bbbbbbbbbbbb", Name: "busy"},
		{ID: "cccccccccccc", Name: "stopped"},
	}
	state.remote.containerUsage = map[string]remotetools.ContainerUsage{
		"aaaaaaaaaaaa": {ID: "aaaaaaaaaaaa", CPUPercent: 1.5, MemoryUsed: "10MiB", MemoryLimit: "1GiB"},
		"bbbbbbbbbbbb": {ID: "bbbbbbbbbbbb", CPUPercent: 25, MemoryUsed: "200MiB", MemoryLimit: "1GiB"},
	}
	state.rebuildRemoteRows()
	remoteToolsPaint(t, state)

	clickRemoteToolsHeader(t, state, remoteTableHeaderID(remoteContainers, 2))
	if state.remote.rows[0].cells[0] != "slow" {
		t.Fatalf("ascending CPU order = %+v", state.remote.rows)
	}
	// A container without a sample stays last in both directions.
	if state.remote.rows[len(state.remote.rows)-1].cells[0] != "stopped" {
		t.Fatalf("unknown usage was not last: %+v", state.remote.rows)
	}
	clickRemoteToolsHeader(t, state, remoteTableHeaderID(remoteContainers, 2))
	if state.remote.rows[0].cells[0] != "busy" {
		t.Fatalf("descending CPU order = %+v", state.remote.rows)
	}
}

func TestContainerDetailsOverviewShowsInspectAndUsage(t *testing.T) {
	state, _ := openContainerDetailsForTest(t)
	waitFor(t, state, func() bool {
		return state.remote.containerDetails.inspectLoaded && !state.remote.containerDetails.inspectLoading
	})
	details := state.remote.containerDetails
	if details.inspectFailed {
		t.Fatalf("configuration read failed: %+v", details.inspectStatus)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	m := state.messages.Tools
	values := []string{
		"/docker-entrypoint.sh", "daemon off;", "always", "/srv/www", "/usr/share/nginx/html",
		"172.17.0.2", "0.0.0.0:8080->80/tcp", "1.25%", "12.5MiB / 7.6GiB",
		"1.2kB / 648B", "0B / 0B",
		m.RemoteContainerCreated, m.RemoteContainerCommand, m.RemoteContainerMounts,
		m.RemoteContainerNetworks, m.RemoteContainerPorts, m.RemoteContainerRestartPolicy,
		m.RemoteContainerNetworkIO, m.RemoteContainerBlockIO, m.RemoteContainerPIDs,
	}
	for _, value := range values {
		if !remoteCanvasHasText(&canvas, value) {
			t.Errorf("container overview lacks %q", value)
		}
	}
}

func TestContainerDetailsReportsConfigurationFailureAndRetries(t *testing.T) {
	state, server, _ := openRemoteToolsDialog(t)
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	server.failures["docker inspect"] = errors.New("exit status 1")
	selectRemoteToolsRow(state, 0)
	clickRemoteToolsAction(t, state, hitRemoteContainerDetails)
	waitFor(t, state, func() bool {
		return state.remote.containerDetails.inspectLoaded && !state.remote.containerDetails.inspectLoading
	})
	details := state.remote.containerDetails
	if !details.inspectFailed || !strings.Contains(details.inspectStatus, "denied by the test") {
		t.Fatalf("configuration failure = %+v", details)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if !remoteCanvasHasText(&canvas, state.messages.Tools.RemoteContainerConfiguration) ||
		!remoteCanvasHasText(&canvas, "denied by the test") {
		t.Fatal("the overview did not name the failed configuration read")
	}

	// Clicking the active tab asks for that read again, as the environment tab
	// does; this time the server answers.
	delete(server.failures, "docker inspect")
	clickRemoteToolsDetailTab(t, state, hitRemoteContainerDetailsOverview)
	waitFor(t, state, func() bool {
		return state.remote.containerDetails.inspectLoaded &&
			!state.remote.containerDetails.inspectLoading && !state.remote.containerDetails.inspectFailed
	})
	if command := state.remote.containerDetails.inspect.Command; !strings.Contains(command, "/docker-entrypoint.sh") {
		t.Fatalf("retry loaded command %q", command)
	}
}
