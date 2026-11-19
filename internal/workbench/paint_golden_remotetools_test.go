package workbench

import (
	"fmt"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/remotetools"
)

// The remote-tools surfaces keep the goldens this file adds: the in-window tools
// dialog with each table, and the process and container detail dialogs. The
// separate tools window shares the same declarations but needs a host, so the
// dialog scenes cover the painting both routes show.
func init() {
	extraPaintScenes = append(extraPaintScenes,
		remoteToolsScene("remote-tools-processes", func(t *testing.T, state *State) {}),
		remoteToolsScene("remote-tools-containers", func(t *testing.T, state *State) {
			clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
			waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
		}),
		remoteToolsScene("remote-process-details-command", func(t *testing.T, state *State) {
			doubleClickRemoteToolsRow(t, state, 0)
			waitFor(t, state, func() bool {
				return state.remote.processDetails.commandLoaded && !state.remote.processDetails.commandLoading
			})
		}),
		remoteToolsScene("remote-process-details-environment", func(t *testing.T, state *State) {
			doubleClickRemoteToolsRow(t, state, 0)
			clickRemoteToolsDetailTab(t, state, hitRemoteProcessDetailsEnv)
			waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
		}),
		remoteToolsScene("remote-process-details-environment-long", func(t *testing.T, state *State) {
			doubleClickRemoteToolsRow(t, state, 0)
			clickRemoteToolsDetailTab(t, state, hitRemoteProcessDetailsEnv)
			waitFor(t, state, func() bool { return !state.remote.processDetails.loading })
			state.remote.processDetails.environment = make([]remotetools.ProcessEnvironmentVariable, 80)
			for index := range state.remote.processDetails.environment {
				state.remote.processDetails.environment[index] = remotetools.ProcessEnvironmentVariable{
					Name: fmt.Sprintf("VARIABLE_%02d", index), Value: fmt.Sprintf("value-%02d", index),
				}
			}
		}),
		remoteToolsScene("remote-container-details-overview", func(t *testing.T, state *State) {
			openRemoteContainerDetailsScene(t, state)
		}),
		remoteToolsScene("remote-container-details-environment", func(t *testing.T, state *State) {
			openRemoteContainerDetailsScene(t, state)
			clickRemoteToolsDetailTab(t, state, hitRemoteContainerDetailsEnvironment)
			waitFor(t, state, func() bool { return !state.remote.containerDetails.loading })
		}),
	)
}

func openRemoteContainerDetailsScene(t *testing.T, state *State) {
	t.Helper()
	clickSettingsChoice(t, state, state.messages.Tools.RemoteTabContainers)
	waitFor(t, state, func() bool { return !state.remote.loading && len(state.remote.rows) == 2 })
	selectRemoteToolsRow(state, 0)
	clickRemoteToolsAction(t, state, hitRemoteContainerDetails)
}

func remoteToolsScene(name string, setup func(t *testing.T, state *State)) paintScene {
	return paintScene{name: name, sizes: []gfx.Size{paintDesktop}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
		state, _, _ := openRemoteToolsDialog(t)
		setup(t, state)
		// The scene reads a fixed clock instant: the fixture is read when the
		// dialog opens, so the collected-at line would else differ per run.
		fixed := time.Date(2026, 11, 18, 12, 34, 56, 0, time.UTC)
		state.remote.collectedAt = fixed
		if state.remote.processDetails.open {
			state.remote.processDetails.collectedAt = fixed
		}
		state.updateRemoteSnapshotStatus()
		return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
	}}
}
