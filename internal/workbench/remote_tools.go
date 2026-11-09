package workbench

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/remotetools"
	"motty/internal/sshconn"
)

// Remote tools: the server's processes and Docker containers in one dialog.
// Opening it, switching tab and refreshing run fixed commands over the captured
// connection. Polling is opt-in. Destructive actions always ask first.

// remoteToolsHits is the place of the controls this file paints.
var remoteToolsHits = tree.Feature("remote-tools")

var (
	hitRemoteTabProcesses  = tree.ID(remoteToolsHits, "remote-tab-processes")
	hitRemoteTabContainers = tree.ID(remoteToolsHits, "remote-tab-containers")
	hitRemoteTabImages     = tree.ID(remoteToolsHits, "remote-tab-images")
	hitRemoteTabVolumes    = tree.ID(remoteToolsHits, "remote-tab-volumes")
	hitRemoteTabNetworks   = tree.ID(remoteToolsHits, "remote-tab-networks")
	hitRemoteTabCompose    = tree.ID(remoteToolsHits, "remote-tab-compose")
	hitRemoteNiceLower     = tree.ID(remoteToolsHits, "remote-nice-lower")
	hitRemoteNiceNormal    = tree.ID(remoteToolsHits, "remote-nice-normal")
	hitRemoteNiceHigher    = tree.ID(remoteToolsHits, "remote-nice-higher")
	hitRemoteDown          = tree.ID(remoteToolsHits, "remote-down")
	hitRemoteSortCPU       = tree.ID(remoteToolsHits, "remote-sort-cpu")
	hitRemoteSortMemory    = tree.ID(remoteToolsHits, "remote-sort-memory")
	hitRemoteTerminate     = tree.ID(remoteToolsHits, "remote-terminate")
	hitRemoteKill          = tree.ID(remoteToolsHits, "remote-kill")
	hitRemoteStart         = tree.ID(remoteToolsHits, "remote-start")
	hitRemoteStop          = tree.ID(remoteToolsHits, "remote-stop")
	hitRemoteRestart       = tree.ID(remoteToolsHits, "remote-restart")
	hitRemoteLogs          = tree.ID(remoteToolsHits, "remote-logs")
	hitRemoteShell         = tree.ID(remoteToolsHits, "remote-shell")
	hitRemoteRemove        = tree.ID(remoteToolsHits, "remote-remove")
	hitRemotePull          = tree.ID(remoteToolsHits, "remote-pull")
	hitRemoteCreate        = tree.ID(remoteToolsHits, "remote-create")
	hitRemoteRefresh       = tree.ID(remoteToolsHits, "remote-refresh")
	hitRemoteSudo          = tree.ID(remoteToolsHits, "remote-sudo")
	hitRemoteAutoRefresh   = tree.ID(remoteToolsHits, "remote-auto-refresh")
	hitRemoteAutoToggle    = tree.ID(remoteToolsHits, "remote-auto-toggle")
	hitRemoteAutoDismiss   = tree.ID(remoteToolsHits, "remote-auto-dismiss")
	hitRemoteClose         = tree.ID(remoteToolsHits, "remote-close")
	hitRemoteDismiss       = tree.ID(remoteToolsHits, "remote-dismiss")
	hitRemotePrevious      = tree.ID(remoteToolsHits, "remote-previous")
	hitRemoteNext          = tree.ID(remoteToolsHits, "remote-next")
	hitRemoteProcessTable  = tree.ID(remoteToolsHits, "remote-process-table")
	hitRemoteProcessScroll = tree.ID(remoteToolsHits, "remote-process-scroll")
)

// hitRemoteRowList starts the row ids: one per entry, up to the list limits.
var hitRemoteRowList = tree.Sub(remoteToolsHits, tree.Name("remote-row"))
var hitRemoteIntervals = tree.Sub(remoteToolsHits, tree.Name("remote-refresh-interval"))
var hitRemoteSortColumns = tree.Sub(remoteToolsHits, tree.Name("remote-sort-column"))
var remoteAutoScope = tree.ID(remoteToolsHits, "remote-refresh-menu")
var (
	hitTerminalMenuTools = tree.ID(remoteToolsHits, "terminal-menu-tools")
	// hitRemoteToolsOpen is the command palette's entry for the dialog.
	hitRemoteToolsOpen  = tree.ID(remoteToolsHits, "remote-tools-open")
	hitProcessToolsOpen = tree.ID(remoteToolsHits, "process-tools-open")
)

const (
	remoteRowsPerPage   = 12
	remoteMaxRows       = 200
	remoteTimeout       = 20 * time.Second
	remoteToolbarHeight = float32(34)
)

// clickRemoteToolsOpen opens the dialog for the active session.
func (state *State) clickRemoteToolsOpen(id gfx.ID) bool {
	if id != hitRemoteToolsOpen && id != hitProcessToolsOpen {
		return false
	}
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && state.modal == modalNone {
		if id == hitProcessToolsOpen {
			state.openProcessTools(session)
		} else {
			state.openRemoteTools(session)
		}
	}
	return true
}

type remoteTab uint8

const (
	remoteProcesses remoteTab = iota
	remoteContainers
	remoteImages
	remoteVolumes
	remoteNetworks
	remoteCompose
)

// remoteTabHits is the control of each tab, indexed by remoteTab.
var remoteTabHits = [...]gfx.ID{
	remoteProcesses: hitRemoteTabProcesses, remoteContainers: hitRemoteTabContainers,
	remoteImages: hitRemoteTabImages, remoteVolumes: hitRemoteTabVolumes, remoteNetworks: hitRemoteTabNetworks, remoteCompose: hitRemoteTabCompose,
}

// kind is the Docker object kind of an images, volumes or networks tab.
func (tab remoteTab) kind() (remotetools.Kind, bool) {
	switch tab {
	case remoteImages:
		return remotetools.Images, true
	case remoteVolumes:
		return remotetools.Volumes, true
	case remoteNetworks:
		return remotetools.Networks, true
	}
	return 0, false
}

// remoteRow is one entry of either table, as the cells the table paints.
type remoteRow struct {
	cells [6]string
	// key identifies the entry across refreshes: the process number or the
	// container's ID.
	key         string
	orderNumber float64
	orderText   string
	orderValid  bool
}

type remoteToolsView struct {
	session  uint32
	title    string
	tab      remoteTab
	sort     remotetools.SortKey
	ordering [6]components.TableSortState

	procs                                 []remotetools.Process
	containers                            []remotetools.Container
	compose                               []remotetools.ComposeProject
	resources                             []remotetools.Resource
	rows                                  []remoteRow
	selected                              string
	page                                  int
	pageText                              string
	processScroll                         float32
	processScrollBar                      components.ScrollAreaGeometry
	processScrollDragging                 bool
	processScrollGrab                     float32
	processScrollValue                    string
	processScrollFirst                    int
	processScrollCount                    int
	processTableBounds                    gfx.Rect
	processColumns                        [6]components.TableColumn
	searches                              [6]remoteSearch
	processMenu                           remoteProcessMenu
	processDetails                        remoteProcessDetails
	processDetailsSerial                  uint64
	containerDetails                      remoteContainerDetails
	containerDetailsSerial                uint64
	sourceTotal                           int
	processesTruncated, commandsTruncated bool
	searchSummary                         string
	collectedAt                           time.Time
	stale                                 bool
	snapshotStatus                        string

	loading  bool
	noDocker bool
	auto     remoteAutoRefresh
	// serial identifies the request whose answer may be applied; an answer to an
	// older one is dropped.
	serial uint32
	// status is the line under the table: progress, outcome or error.
	status string
	// note is what the last action did; it leads the status once the list has
	// been read again.
	note    string
	failed  bool
	columns [6]string
}

func (state *State) remoteToolsOffered(session *Session) bool {
	return session.hasServer() && session.conn != nil && session.phase == sshconn.PhaseConnected && session.term != nil && session.term.live
}

func (state *State) openRemoteTools(session *Session) {
	state.openRemoteToolsTab(session, state.remote.tab, false)
}

func (state *State) openRemoteToolsTab(session *Session, tab remoteTab, switchTab bool) {
	if !state.remoteToolsOffered(session) {
		return
	}
	if state.options.OpenContentWindow != nil {
		state.openRemoteToolsWindowTab(session, tab, switchTab)
		return
	}
	// Detail generations survive replacing the presentation state. Environment
	// requests from a closed tools surface must never be accepted by a later
	// surface that happens to use the same session and process.
	detailsSerial := state.remote.processDetailsSerial
	containerDetailsSerial := state.remote.containerDetailsSerial
	state.cancelRemoteProcessCommand()
	state.cancelRemoteProcessEnvironment()
	state.cancelRemoteContainerEnvironment()
	state.remote = remoteToolsView{
		session: session.ID, title: session.Title, tab: tab, sort: state.remote.sort,
		serial: state.remote.serial + 1, processDetailsSerial: detailsSerial,
		containerDetailsSerial: containerDetailsSerial,
	}
	state.modal = modalRemoteTools
	state.refreshRemoteTools()
}

func (state *State) remoteToolsVisible() bool {
	return state.modal == modalRemoteTools
}

func (state *State) remoteSession() (*Session, bool) {
	if window := state.remoteWindow; window != nil {
		owner, session, ok := window.source()
		if !ok || !owner.remoteToolsOffered(session) {
			return nil, false
		}
		return &window.session, true
	}
	session, ok := state.sessionByIDPointer(state.remote.session)
	return session, ok && session.conn != nil
}

func (state *State) clickRemoteTools(id gfx.ID) {
	view := &state.remote
	m := state.messages.Tools
	if id == hitRemoteSudo {
		if session, ok := state.remoteSession(); ok {
			state.toggleSudoCommands(session, true)
		}
		return
	}
	if state.clickRemoteProcessDetails(id) {
		return
	}
	if state.clickRemoteContainerDetails(id) {
		return
	}
	if state.clickRemoteProcessMenu(id) {
		return
	}
	if remoteMutation(id) && (view.stale || view.loading || view.collectedAt.IsZero()) {
		view.status, view.failed = m.RemoteRefreshRequired, true
		return
	}
	switch {
	case id == hitRemoteSearchClear:
		state.clearRemoteSearch()
	case id == hitRemoteClose || id == hitRemoteDismiss:
		state.closeRemoteToolsSurface()
	case id == hitRemoteAutoRefresh:
		view.auto.open = !view.auto.open
	case id == hitRemoteAutoDismiss:
		view.auto.open = false
	case id == hitRemoteAutoToggle:
		view.auto.enabled = !view.auto.enabled
		view.auto.restart()
	case id.Scope() == hitRemoteIntervals:
		if index := item(id); index >= 0 && index < len(remoteRefreshIntervals) {
			view.auto.interval = remoteRefreshIntervals[index]
			view.auto.enabled = true
			view.auto.restart()
		}
	case id == hitRemoteTabProcesses || id == hitRemoteTabContainers || id == hitRemoteTabImages || id == hitRemoteTabVolumes || id == hitRemoteTabNetworks || id == hitRemoteTabCompose:
		var tab remoteTab
		for candidate, hit := range remoteTabHits {
			if hit == id {
				tab = remoteTab(candidate)
			}
		}
		if tab != view.tab {
			view.tab, view.selected, view.page = tab, "", 0
			view.rows, view.pageText = nil, ""
			view.procs, view.containers, view.compose, view.resources = nil, nil, nil, nil
			view.collectedAt, view.stale, view.snapshotStatus = time.Time{}, false, ""
			view.processesTruncated, view.commandsTruncated = false, false
			state.rebuildRemoteRows()
			if view.noDocker && tab != remoteProcesses {
				// Docker availability belongs to the connection, not its tab.
				// Keep the empty state until an explicit or timed refresh rechecks it.
				view.serial++
				view.loading, view.failed, view.status = false, true, m.RemoteErrNoDocker
			} else {
				state.refreshRemoteTools()
			}
		}
	case state.clickRemoteTableSort(id):
		return
	case id == hitRemoteRefresh:
		if !view.loading {
			// An explicit refresh rechecks Docker as well, so an answer from
			// before the installation does not outlive it.
			view.noDocker = false
			state.refreshRemoteTools()
		}
	case id == hitRemotePrevious:
		if view.page > 0 {
			view.page--
			view.pageText = ""
		}
	case id == hitRemoteNext:
		if (view.page+1)*state.remotePageSize() < len(view.rows) {
			view.page++
			view.pageText = ""
		}
	case id.Scope() == hitRemoteRowList:
		if index := item(id); index < len(view.rows) {
			view.selected = view.rows[index].key
			if view.tab == remoteProcesses {
				if process, found := state.remoteProcessByKey(view.selected); found {
					state.openRemoteProcessDetails(process)
				}
			}
		}
	case id == hitRemoteTerminate || id == hitRemoteKill:
		process, ok := state.selectedProcess()
		if !ok {
			view.status, view.failed = m.RemoteNeedSelection, true
			return
		}
		if !state.remoteProcessReady(process, view.session) {
			return
		}
		signal, title := remotetools.Terminate, m.RemoteConfirmTerminateTitle
		if id == hitRemoteKill {
			signal, title = remotetools.Kill, m.RemoteConfirmKillTitle
		}
		sourceID := view.session
		state.confirmRemote(title, fmt.Sprintf(m.RemoteConfirmProcessFmt, process.PID, shortCommand(process.Command)), m.RemoteTerminate, func(state *State) {
			if !state.remoteProcessReady(process, sourceID) {
				return
			}
			label := "TERM"
			if signal == remotetools.Kill {
				label = "KILL"
			}
			state.runRemoteAction(fmt.Sprintf(m.RemoteSignalSentFmt, label, process.PID), func(ctx context.Context, runner remotetools.Runner) error {
				return remotetools.SendSignalToProcess(ctx, runner, process, signal)
			})
		})
	case id == hitRemoteNiceLower || id == hitRemoteNiceNormal || id == hitRemoteNiceHigher:
		process, ok := state.selectedProcess()
		if !ok {
			view.status, view.failed = m.RemoteNeedSelection, true
			return
		}
		if !state.remoteProcessReady(process, view.session) {
			return
		}
		nice := 0
		switch id {
		case hitRemoteNiceLower:
			nice = 10
		case hitRemoteNiceHigher:
			nice = -5
		}
		state.runRemoteAction(fmt.Sprintf(m.RemoteReniceDoneFmt, process.PID, nice), func(ctx context.Context, runner remotetools.Runner) error {
			return remotetools.ReniceProcess(ctx, runner, process, nice)
		})
	case (id == hitRemoteStart || id == hitRemoteStop || id == hitRemoteRestart || id == hitRemoteDown) && view.tab == remoteCompose:
		project, ok := state.selectedCompose()
		if !ok {
			view.status, view.failed = m.RemoteNeedSelection, true
			return
		}
		action, label := remotetools.ComposeStart, m.RemoteStart
		switch id {
		case hitRemoteStop:
			action, label = remotetools.ComposeStop, m.RemoteStop
		case hitRemoteRestart:
			action, label = remotetools.ComposeRestart, m.RemoteRestart
		case hitRemoteDown:
			action, label = remotetools.ComposeDown, m.RemoteDown
		}
		run := func(state *State) {
			state.runRemoteAction(fmt.Sprintf(m.RemoteActionDoneFmt, label+" "+project.Name), func(ctx context.Context, runner remotetools.Runner) error {
				return remotetools.DoCompose(ctx, runner, action, project.Name)
			})
		}
		if action == remotetools.ComposeDown {
			state.confirmRemote(m.RemoteConfirmDownTitle, fmt.Sprintf(m.RemoteConfirmDownFmt, project.Name), m.RemoteDown, run)
		} else {
			run(state)
		}
	case id == hitRemoteStart || id == hitRemoteStop || id == hitRemoteRestart:
		container, ok := state.selectedContainer()
		if !ok {
			view.status, view.failed = m.RemoteNeedSelection, true
			return
		}
		action, label := remotetools.Start, m.RemoteStart
		switch id {
		case hitRemoteStop:
			action, label = remotetools.Stop, m.RemoteStop
		case hitRemoteRestart:
			action, label = remotetools.Restart, m.RemoteRestart
		}
		state.runRemoteAction(fmt.Sprintf(m.RemoteActionDoneFmt, label+" "+container.Name), func(ctx context.Context, runner remotetools.Runner) error {
			return remotetools.Do(ctx, runner, action, container.ID)
		})
	case id == hitRemotePull:
		state.askPullImage()
	case id == hitRemoteCreate:
		state.askCreateResource()
	case id == hitRemoteRemove && view.tab != remoteContainers:
		state.removeSelectedResource()
	case id == hitRemoteRemove:
		container, ok := state.selectedContainer()
		if !ok {
			view.status, view.failed = m.RemoteNeedSelection, true
			return
		}
		state.confirmRemote(m.RemoteConfirmRemoveTitle, fmt.Sprintf(m.RemoteConfirmRemoveFmt, container.Name), m.RemoteRemove, func(state *State) {
			state.runRemoteAction(fmt.Sprintf(m.RemoteActionDoneFmt, m.RemoteRemove+" "+container.Name), func(ctx context.Context, runner remotetools.Runner) error {
				return remotetools.Do(ctx, runner, remotetools.Remove, container.ID)
			})
		})
	case id == hitRemoteLogs:
		container, ok := state.selectedContainer()
		if !ok {
			view.status, view.failed = m.RemoteNeedSelection, true
			return
		}
		state.followContainerLog(container)
	case id == hitRemoteShell && view.tab == remoteContainers:
		source, ok := state.remoteSession()
		if !ok || source.host.System == sshconn.SystemWindows {
			view.status, view.failed = m.RemoteShellUnsupportedHost, true
			return
		}
		container, ok := state.selectedContainer()
		if !ok || !container.Running() {
			view.status, view.failed = m.RemoteShellRequiresRunning, true
			return
		}
		state.openContainerShell(container)
	case id == hitRemoteContainerDetails && view.tab == remoteContainers:
		container, ok := state.selectedContainer()
		if !ok {
			view.status, view.failed = m.RemoteNeedSelection, true
			return
		}
		state.openRemoteContainerDetails(container)
	}
}

func shortCommand(command string) string {
	runes := []rune(command)
	if len(runes) > 60 {
		return string(runes[:60]) + "…"
	}
	return command
}

// confirmRemote asks before something destructive; yes runs the action, and
// either answer returns to the tools.
func (state *State) confirmRemote(title, description, confirm string, yes func(*State)) {
	back := func(state *State) { state.restoreRemoteToolsSurface() }
	state.openForm(formDialog{
		title: title, description: description, submit: confirm, danger: true,
		run: func(state *State, _ *formDialog) string {
			state.restoreRemoteToolsSurface()
			yes(state)
			return ""
		},
		cancel: back,
	})
}

// restoreRemoteToolsSurface returns a form or confirmation to the tools
// surface that opened it.
func (state *State) restoreRemoteToolsSurface() {
	state.modal = modalRemoteTools
}

func (state *State) selectedProcess() (remotetools.Process, bool) {
	if state.remote.selected == "" {
		return remotetools.Process{}, false
	}
	pidText, identity, _ := strings.Cut(state.remote.selected, ":")
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		return remotetools.Process{}, false
	}
	for _, process := range state.remote.procs {
		if process.PID == pid && process.StartIdentity == identity {
			return process, true
		}
	}
	return remotetools.Process{}, false
}

// askPullImage asks for an image reference, then types the pull into the
// terminal, as the container log is: a pull is as long as the network and
// prints progress the user may want to stop.
func (state *State) askPullImage() {
	m := state.messages.Tools
	back := func(state *State) { state.restoreRemoteToolsSurface() }
	state.openForm(formDialog{
		title: m.RemotePullTitle, description: m.RemotePullDescription, submit: m.RemotePull,
		run: func(state *State, form *formDialog) string {
			if state.remote.stale || state.remote.loading || state.remote.collectedAt.IsZero() {
				return m.RemoteRefreshRequired
			}
			command, ok := remotetools.PullCommand(strings.TrimSpace(form.value(0)))
			if !ok {
				return m.RemoteBadImage
			}
			session, found := state.remoteSession()
			if !found || !state.typeRemoteCommand(session, command) {
				return state.messages.History.StatusNotConnected
			}
			state.status = command
			state.remote.serial++
			state.finishRemoteTerminalAction()
			return ""
		},
		cancel: back,
	}, formField{label: m.RemoteImageField, placeholder: m.RemoteImagePlaceholder})
}

// askCreateResource asks for a name, then creates the volume or network.
func (state *State) askCreateResource() {
	m := state.messages.Tools
	kind, ok := state.remote.tab.kind()
	if !ok || kind == remotetools.Images {
		return
	}
	title, description := m.RemoteCreateVolumeTitle, m.RemoteCreateVolumeDescription
	if kind == remotetools.Networks {
		title, description = m.RemoteCreateNetworkTitle, m.RemoteCreateNetworkDescription
	}
	back := func(state *State) { state.restoreRemoteToolsSurface() }
	state.openForm(formDialog{
		title: title, description: description, submit: m.RemoteCreate,
		run: func(state *State, form *formDialog) string {
			name := strings.TrimSpace(form.value(0))
			if !remotetools.ValidName(name) {
				return m.RemoteBadName
			}
			state.restoreRemoteToolsSurface()
			state.runRemoteAction(fmt.Sprintf(m.RemoteActionDoneFmt, m.RemoteCreate+" "+name), func(ctx context.Context, runner remotetools.Runner) error {
				return remotetools.CreateResource(ctx, runner, kind, name)
			})
			return ""
		},
		cancel: back,
	}, formField{label: m.RemoteNameField})
}

// removeSelectedResource asks, then removes the selected image, volume or
// network. Docker refuses what is in use; its reason becomes the status line.
func (state *State) removeSelectedResource() {
	view := &state.remote
	m := state.messages.Tools
	kind, ok := view.tab.kind()
	if !ok {
		return
	}
	var resource remotetools.Resource
	found := false
	for _, candidate := range view.resources {
		if candidate.Key == view.selected {
			resource, found = candidate, true
		}
	}
	if !found {
		view.status, view.failed = m.RemoteNeedSelection, true
		return
	}
	title, format := m.RemoteConfirmRemoveImageTitle, m.RemoteConfirmRemoveImageFmt
	switch kind {
	case remotetools.Volumes:
		title, format = m.RemoteConfirmRemoveVolumeTitle, m.RemoteConfirmRemoveVolumeFmt
	case remotetools.Networks:
		title, format = m.RemoteConfirmRemoveNetworkTitle, m.RemoteConfirmRemoveNetworkFmt
	}
	state.confirmRemote(title, fmt.Sprintf(format, resource.Cells[0]), m.RemoteRemove, func(state *State) {
		state.runRemoteAction(fmt.Sprintf(m.RemoteActionDoneFmt, m.RemoteRemove+" "+resource.Cells[0]), func(ctx context.Context, runner remotetools.Runner) error {
			return remotetools.RemoveResource(ctx, runner, kind, resource.Key)
		})
	})
}

func (state *State) selectedCompose() (remotetools.ComposeProject, bool) {
	for _, project := range state.remote.compose {
		if state.remote.selected == project.Name {
			return project, true
		}
	}
	return remotetools.ComposeProject{}, false
}

func (state *State) selectedContainer() (remotetools.Container, bool) {
	for _, container := range state.remote.containers {
		if state.remote.selected == container.ID {
			return container, true
		}
	}
	return remotetools.Container{}, false
}

// refreshRemoteTools reads the current tab again, off the window thread.
func (state *State) refreshRemoteTools() {
	view := &state.remote
	session, ok := state.remoteSession()
	if !ok {
		return
	}
	view.serial++
	serial, tab, key, sourceID := view.serial, view.tab, view.sort, session.ID
	view.loading, view.failed, view.status = true, false, state.messages.Tools.RemoteLoading
	runner := remotetools.ForSystem(commandRunnerFor(session), session.host.System.String())
	go func() {
		ctx, cancel := context.WithTimeout(state.remoteToolsContext(), remoteTimeout)
		defer cancel()
		var (
			procs      []remotetools.Process
			snapshot   remotetools.ProcessSnapshot
			containers []remotetools.Container
			compose    []remotetools.ComposeProject
			resources  []remotetools.Resource
			err        error
		)
		if kind, isResource := tab.kind(); isResource {
			resources, err = remotetools.Resources(ctx, runner, kind)
		} else if tab == remoteCompose {
			compose, err = remotetools.Compose(ctx, runner)
		} else if tab == remoteProcesses {
			snapshot, err = remotetools.ReadSafeProcessSnapshot(ctx, runner, key)
			procs = snapshot.Processes
		} else {
			containers, err = remotetools.Containers(ctx, runner)
		}
		collectedAt := time.Now()
		state.postRemoteToolsUpdate(func(state *State) {
			view := &state.remote
			if view.serial != serial || view.session != sourceID || view.tab != tab || !state.remoteToolsVisible() {
				return
			}
			view.loading = false
			if tab != remoteProcesses {
				view.noDocker = errors.Is(err, remotetools.ErrNoDocker)
			}
			if err != nil {
				view.failed, view.status = true, state.remoteErrorText(err)
				view.stale = !view.collectedAt.IsZero()
				state.updateRemoteSnapshotStatus()
				state.rebuildRemoteRows()
				return
			}
			view.collectedAt, view.stale = collectedAt, false
			view.procs, view.containers, view.compose, view.resources = procs, containers, compose, resources
			view.processesTruncated, view.commandsTruncated = snapshot.Truncated, snapshot.CommandsTruncated
			state.updateRemoteSnapshotStatus()
			state.rebuildRemoteRows()
			m := state.messages.Tools
			empty := ""
			switch tab {
			case remoteContainers:
				empty = m.RemoteEmptyContainers
			case remoteCompose:
				empty = m.RemoteEmptyCompose
			case remoteImages:
				empty = m.RemoteEmptyImages
			case remoteVolumes:
				empty = m.RemoteEmptyVolumes
			case remoteNetworks:
				empty = m.RemoteEmptyNetworks
			}
			switch {
			case empty != "" && view.sourceTotal == 0:
				view.status = empty
			default:
				view.status = fmt.Sprintf(m.RemoteLoadedFmt, len(view.rows))
			}
			if view.note != "" {
				view.status, view.note = view.note+" "+view.status, ""
			}
		})
	}()
}

// runRemoteAction runs a change on the server and then reads the list again.
func (state *State) runRemoteAction(done string, action func(context.Context, remotetools.Runner) error) {
	view := &state.remote
	if view.stale || view.loading || view.collectedAt.IsZero() {
		view.status, view.failed = state.messages.Tools.RemoteRefreshRequired, true
		return
	}
	session, ok := state.remoteSession()
	if !ok {
		return
	}
	view.serial++
	serial, tab, sourceID := view.serial, view.tab, session.ID
	view.loading, view.failed, view.status = true, false, state.messages.Tools.RemoteLoading
	runner := remotetools.ForSystem(commandRunnerFor(session), session.host.System.String())
	go func() {
		ctx, cancel := context.WithTimeout(state.remoteToolsContext(), remoteTimeout)
		defer cancel()
		err := action(ctx, runner)
		state.postRemoteToolsUpdate(func(state *State) {
			view := &state.remote
			if view.serial != serial || view.session != sourceID || view.tab != tab || !state.remoteToolsVisible() {
				return
			}
			if err != nil {
				view.loading, view.failed, view.status = false, true, state.remoteErrorText(err)
				if tab == remoteProcesses {
					view.stale, view.selected = true, ""
					state.updateRemoteSnapshotStatus()
					state.rebuildRemoteRows()
				}
				return
			}
			state.refreshRemoteTools()
			view.note = done
		})
	}()
}

func (state *State) closeRemoteToolsSurface() {
	state.remote.auto.enabled, state.remote.auto.open = false, false
	state.remote.processMenu = remoteProcessMenu{}
	state.closeRemoteProcessDetails()
	state.closeRemoteContainerDetails()
	state.remote.serial++
	state.closeModal()
}

func (state *State) remoteErrorText(err error) string {
	m := state.messages.Tools
	var failure *remotetools.Failure
	switch {
	case errors.Is(err, remotetools.ErrProcessChanged):
		return m.RemoteProcessChanged
	case errors.Is(err, remotetools.ErrProcessUnverifiable):
		return m.RemoteProcessUnverifiable
	case errors.Is(err, remotetools.ErrProcessEnvironmentUnsupported):
		return m.RemoteProcessEnvironmentUnsupported
	case errors.Is(err, remotetools.ErrProcessEnvironmentUnreadable):
		return m.RemoteProcessEnvironmentUnreadable
	case errors.Is(err, remotetools.ErrProcessEnvironmentTooLarge):
		return m.RemoteProcessEnvironmentTooLarge
	case errors.Is(err, remotetools.ErrNoCompose):
		return m.RemoteErrNoCompose
	case errors.Is(err, remotetools.ErrComposeListUnsupported):
		return m.RemoteErrComposeListUnsupported
	case errors.Is(err, remotetools.ErrNoDocker):
		return m.RemoteErrNoDocker
	case errors.Is(err, remotetools.ErrNoShell):
		return m.RemoteErrNoShell
	case errors.Is(err, remotetools.ErrUnsupported) && errors.As(err, &failure):
		return fmt.Sprintf(m.RemoteErrUnsupportedFmt, failure.Message)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf(m.RemoteErrFailedFmt, "timeout")
	}
	return fmt.Sprintf(m.RemoteErrFailedFmt, err)
}

func (state *State) openContainerShell(container remotetools.Container) {
	view := &state.remote
	session, ok := state.remoteSession()
	if !ok {
		return
	}
	view.serial++
	serial, sourceID := view.serial, session.ID
	view.loading, view.failed, view.status = true, false, state.messages.Tools.RemoteLoading
	runner := remotetools.ForSystem(commandRunnerFor(session), session.host.System.String())
	go func() {
		ctx, cancel := context.WithTimeout(state.remoteToolsContext(), remoteTimeout)
		defer cancel()
		command, err := remotetools.ShellCommand(ctx, runner, container.ID)
		state.postRemoteToolsUpdate(func(state *State) {
			view := &state.remote
			if !state.remoteToolsVisible() || view.serial != serial || view.session != sourceID || view.tab != remoteContainers {
				return
			}
			view.loading = false
			if view.selected != container.ID || state.activeSessionID != sourceID {
				return
			}
			if err == nil {
				err = state.openRemoteContainerTab(sourceID, container.Name, command)
			}
			if err != nil {
				view.failed, view.status = true, state.remoteErrorText(err)
				return
			}
			view.serial++
			state.finishRemoteTerminalAction()
		})
	}()
}

// followContainerLog types the log command into the session's terminal, where
// the user can read, scroll and stop it as they would any command.
func (state *State) followContainerLog(container remotetools.Container) {
	session, ok := state.remoteSession()
	command, valid := remotetools.LogsCommand(container.ID)
	if !ok || !valid || !state.typeRemoteCommand(session, command) {
		return
	}
	state.status = fmt.Sprintf(state.messages.Tools.RemoteLogsFmt, container.Name)
	state.remote.serial++
	state.finishRemoteTerminalAction()
}

func (state *State) rebuildRemoteRows() {
	view := &state.remote
	view.rows = view.rows[:0]
	if view.tab == remoteProcesses {
		for _, process := range view.procs {
			view.rows = append(view.rows, remoteRow{key: remotetools.ProcessKey(process), cells: [6]string{
				strconv.Itoa(process.PID), process.User, strconv.FormatFloat(process.CPU, 'f', 1, 64),
				strconv.FormatFloat(process.Memory, 'f', 1, 64), process.Elapsed, process.Command,
			}})
		}
	} else if view.tab == remoteCompose {
		for _, project := range view.compose {
			view.rows = append(view.rows, remoteRow{key: project.Name, cells: [6]string{project.Name, project.Status, project.ConfigFiles}})
		}
	} else if _, isResource := view.tab.kind(); isResource {
		for _, resource := range view.resources {
			row := remoteRow{key: resource.Key}
			copy(row.cells[:], resource.Cells)
			view.rows = append(view.rows, row)
		}
	} else {
		for _, container := range view.containers {
			view.rows = append(view.rows, remoteRow{key: container.ID, cells: [6]string{
				container.Name, container.Image, container.State, container.Status, container.ID,
			}})
		}
	}
	state.filterRemoteRows()
	state.sortRemoteRows()
	if view.page*state.remotePageSize() >= len(view.rows) {
		view.page = 0
	}
	view.pageText = ""
}

func (state *State) paintRemoteTools(canvas *gfx.Canvas, viewport gfx.Rect) {
	theme := state.Theme()
	_, ok := state.remoteSession()
	if !ok {
		state.closeRemoteToolsSurface()
		return
	}
	tableHeight := components.TableHeaderHeight(theme) + remoteRowsPerPage*components.TableRowHeight(theme)
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitRemoteDismiss, CloseID: hitRemoteClose, InitialFocusID: hitRemoteRefresh,
		ContentSize: gfx.Size{Width: 940}, FitBody: true, BodyHeight: 52 + tableHeight + 34 + remoteSearchHeight,
	})
	layout := dialog.Begin("", "")
	state.paintRemoteToolsBody(canvas, layout.Body)
	actions, count := state.remoteToolActions()
	dialog.Actions(layout, actions[:count]...)
	state.paintRemoteRefreshMenu(canvas)
	dialog.End()
	state.paintRemoteProcessMenu(canvas, viewport)
	state.paintRemoteProcessDetails(canvas, viewport)
	state.paintRemoteContainerDetails(canvas, viewport)
}

func (state *State) paintRemoteToolsBody(canvas *gfx.Canvas, bounds gfx.Rect) {
	m, pal, theme := state.messages.Tools, state.pal, state.Theme()
	view := &state.remote
	pageSize := state.remotePageSize()
	tableHeight := components.TableHeaderHeight(theme) + float32(pageSize)*components.TableRowHeight(theme)
	tabs := [...]components.SegmentOption{
		{ID: hitRemoteTabProcesses, Label: m.RemoteTabProcesses},
		{ID: hitRemoteTabContainers, Label: m.RemoteTabContainers},
		{ID: hitRemoteTabImages, Label: m.RemoteTabImages},
		{ID: hitRemoteTabVolumes, Label: m.RemoteTabVolumes},
		{ID: hitRemoteTabNetworks, Label: m.RemoteTabNetworks},
		{ID: hitRemoteTabCompose, Label: m.RemoteTabCompose},
	}
	selectedTab := remoteTabHits[view.tab]
	// Tabs and refresh controls share the toolbar; sorting belongs to the table.
	pages := (len(view.rows) + pageSize - 1) / pageSize
	status := view.status
	missingDocker := view.noDocker && view.tab != remoteProcesses && view.collectedAt.IsZero()
	if view.loading && len(view.rows) == 0 && !missingDocker {
		status = ""
	}
	if missingDocker && !view.loading {
		status = ""
	}
	color := pal.muted
	if view.failed {
		color = pal.danger
	}
	var tabsSlot, refreshSlot, sudoSlot, tableSlot, pagesSlot, searchSlot, statusSlot, snapshotSlot ui.Box
	f := state.partLayout.Begin(canvas, theme, bounds)
	stacked := bounds.Width < 800
	f.Column().Fill().Children(func() {
		f.Row().Height(remoteToolbarHeight).Shrink(0).Gap(12).Children(func() {
			// In a window too narrow for both they share the line instead of overlapping.
			tabsSlot = f.Slot().Width(600).Shrink(1)
			f.Spacer()
			if !stacked {
				refreshSlot = f.Slot().Width(174).Shrink(0)
				sudoSlot = f.Slot().Width(28).Shrink(0)
			}
		})
		if stacked {
			f.Row().Height(remoteToolbarHeight).Shrink(0).MarginSides(8, 0, 0, 0).Children(func() {
				f.Spacer()
				refreshSlot = f.Slot().Width(174).Height(remoteToolbarHeight)
				sudoSlot = f.Slot().Width(28).Height(remoteToolbarHeight)
			})
		}
		searchSlot = f.Slot().Height(remoteSearchHeight).Shrink(0).MarginSides(8, 0, 0, 0)
		if view.tab == remoteProcesses {
			tableSlot = f.Slot().Grow(1).Shrink(1).MinHeight(components.TableHeaderHeight(theme)+components.TableRowHeight(theme)).MarginSides(4, 0, 0, 0)
		} else {
			tableSlot = f.Slot().Height(tableHeight).Shrink(0).MarginSides(4, 0, 0, 0)
		}
		f.Row().Height(30).Shrink(0).Items(ui.Center).MarginSides(6, 0, 0, 0).Children(func() {
			statusSlot = f.Slot().Grow(1).Height(24)
			snapshotSlot = f.Slot().Width(360).Height(24).Shrink(1)
			if len(view.rows) > pageSize {
				pagesSlot = f.Slot().Width(250).Height(30).Shrink(0)
			}
		})
	})
	f.End()

	if status != "" && statusSlot.Rect().Width > 0 {
		bounds := statusSlot.Rect()
		canvas.Label(elide(canvas, status, metrics.FontSizeS, gfx.WeightMedium, bounds.Width), bounds,
			gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: color})
	}
	if view.snapshotStatus != "" && snapshotSlot.Rect().Width > 0 {
		bounds := snapshotSlot.Rect()
		canvas.Label(elide(canvas, view.snapshotStatus, metrics.FontSizeS, gfx.WeightMedium, bounds.Width), bounds,
			gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.muted})
		canvas.HitWithOptions(hitRemoteSnapshotStatus, bounds, view.snapshotStatus,
			gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: view.searchSummary})
	}
	components.BeginSegmentedControl(canvas, theme, tabsSlot.Rect(), tabs[:], selectedTab).Paint()
	state.paintRemoteRefresh(canvas, refreshSlot.Rect())
	if session, ok := state.remoteSession(); ok && session.host.System != sshconn.SystemWindows {
		label := state.messages.SudoCommandsEnable
		if session.sudoCommands {
			label = state.messages.SudoCommandsDisable
		}
		state.paintIconButton(canvas, hitRemoteSudo, iconLock, sudoSlot.Rect(), label, false, iconButtonQuiet, pal.surface)
	}
	state.paintRemoteSearch(canvas, searchSlot.Rect())
	if missingDocker {
		state.paintRemoteDockerEmpty(canvas, tableSlot.Rect())
		return
	}
	if view.loading && len(view.rows) == 0 {
		state.paintRemoteToolsLoading(canvas, tableSlot.Rect())
		return
	}
	if view.tab == remoteProcesses {
		state.paintRemoteProcessTable(canvas, tableSlot.Rect())
		return
	}

	var columns []components.TableColumn
	var headings []string
	if view.tab == remoteCompose {
		columns = []components.TableColumn{{Width: 220}, {Width: 200}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColStatus, m.RemoteColConfig}
		headings = view.columns[:3]
	} else if view.tab == remoteImages {
		columns = []components.TableColumn{{Width: 470}, {Width: 150}, {Width: 100, Alignment: components.TableAlignRight}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColID, m.RemoteColSize, m.RemoteColCreated}
		headings = view.columns[:4]
	} else if view.tab == remoteVolumes {
		columns = []components.TableColumn{{Width: 470}, {Width: 150}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColDriver, m.RemoteColScope}
		headings = view.columns[:3]
	} else if view.tab == remoteNetworks {
		columns = []components.TableColumn{{Width: 300}, {Width: 160}, {Width: 150}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColID, m.RemoteColDriver, m.RemoteColScope}
		headings = view.columns[:4]
	} else {
		columns = []components.TableColumn{{Width: 200}, {Width: 220}, {Width: 90}, {Width: 200}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColImage, m.RemoteColState, m.RemoteColStatus, m.RemoteColID}
		headings = view.columns[:5]
	}
	var headerCells [6]components.TableHeader
	order := view.tableSort()
	for i, heading := range headings {
		headerCells[i] = components.TableHeader{ID: remoteTableHeaderID(view.tab, i), Label: heading, Disabled: view.loading, FocusableWhenDisabled: true, Description: m.RemoteSortAction}
		if order.Direction != components.TableSortNone && order.Column == i {
			headerCells[i].Sort = order.Direction
			if order.Direction == components.TableSortAscending {
				headerCells[i].Description = m.RemoteSortAscending
			} else {
				headerCells[i].Description = m.RemoteSortDescending
			}
		}
	}
	components.FitSortableTableHeadings(canvas, theme, columns, headerCells[:len(headings)])
	if state.remoteWindow != nil {
		// Keep the last column visible as a native window is narrowed; fixed
		// desktop widths otherwise consume all space before reaching it.
		var fixed float32
		for _, column := range columns[:len(columns)-1] {
			fixed += column.Width
		}
		available := max(bounds.Width-120, 0)
		if fixed > available {
			for i := range columns[:len(columns)-1] {
				columns[i].Width *= available / fixed
			}
		}
	}
	table := components.BeginTable(canvas, theme, tableSlot.Rect(), columns)
	table.SortableHeader(headerCells[:len(headings)])
	start := view.page * pageSize
	end := min(start+pageSize, len(view.rows))
	for index := start; index < end; index++ {
		row := &view.rows[index]
		id := hitRemoteRowList.At(index)
		table.Row(id, row.cells[:len(headings)], row.key == view.selected)
		if view.tab == remoteProcesses {
			state.openRemoteProcessMenuOn(canvas, id, index)
		}
	}

	if len(view.rows) > pageSize {
		if view.pageText == "" {
			view.pageText = strconv.Itoa(view.page+1) + "/" + strconv.Itoa(pages)
		}
		pagination := components.BeginPagination(canvas, theme, pagesSlot.Rect())
		pagination.AddPrevious(hitRemotePrevious, state.messages.KnownHostsPrevious, view.page == 0)
		pagination.AddPage(gfx.ID{}, view.pageText, true)
		pagination.AddNext(hitRemoteNext, state.messages.KnownHostsNext, view.page+1 >= pages)
	}
}

func (state *State) remoteToolActions() (actions [8]components.ButtonAction, count int) {
	m, view := state.messages.Tools, &state.remote
	source, connected := state.remoteSession()
	add := func(action components.ButtonAction) {
		actions[count] = action
		count++
	}
	add(components.ButtonAction{ID: hitRemoteClose, Label: m.RemoteClose, Variant: components.ButtonSecondary})
	processBlocked := false
	if view.tab == remoteProcesses {
		process, selected := state.selectedProcess()
		processBlocked = !selected || process.PID <= 1 || process.StartIdentity == ""
		add(components.ButtonAction{ID: hitRemoteTerminate, Label: m.RemoteTerminate, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteKill, Label: m.RemoteForceKill, Variant: components.ButtonDestructive})
		add(components.ButtonAction{ID: hitRemoteNiceLower, Label: m.RemoteReniceLower, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteNiceNormal, Label: m.RemoteReniceNormal, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteNiceHigher, Label: m.RemoteReniceHigher, Variant: components.ButtonOutline})
	} else if view.tab == remoteCompose {
		add(components.ButtonAction{ID: hitRemoteStart, Label: m.RemoteStart, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteStop, Label: m.RemoteStop, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteRestart, Label: m.RemoteRestart, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteDown, Label: m.RemoteDown, Variant: components.ButtonDestructive})
	} else if _, isResource := view.tab.kind(); isResource {
		if view.tab == remoteImages {
			add(components.ButtonAction{ID: hitRemotePull, Label: m.RemotePull, Variant: components.ButtonOutline})
		} else {
			add(components.ButtonAction{ID: hitRemoteCreate, Label: m.RemoteCreate, Variant: components.ButtonOutline})
		}
		add(components.ButtonAction{ID: hitRemoteRemove, Label: m.RemoteRemove, Variant: components.ButtonDestructive})
	} else if view.tab == remoteContainers {
		container, selected := state.selectedContainer()
		add(components.ButtonAction{ID: hitRemoteContainerDetails, Label: m.RemoteContainerDetails, Variant: components.ButtonOutline, Disabled: !selected})
		add(components.ButtonAction{ID: hitRemoteStart, Label: m.RemoteStart, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteStop, Label: m.RemoteStop, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteRestart, Label: m.RemoteRestart, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteLogs, Label: m.RemoteLogs, Variant: components.ButtonOutline})
		add(components.ButtonAction{ID: hitRemoteShell, Label: m.RemoteEnterShell, Variant: components.ButtonOutline, Disabled: !selected || !container.Running() || view.loading || !connected || source.host.System == sshconn.SystemWindows})
		add(components.ButtonAction{ID: hitRemoteRemove, Label: m.RemoteRemove, Variant: components.ButtonDestructive})
	}
	for i := 1; i < count; i++ {
		if actions[i].ID == hitRemoteContainerDetails {
			continue
		}
		actions[i].Disabled = actions[i].Disabled || view.loading || view.stale || view.collectedAt.IsZero() || !connected || view.noDocker && view.tab != remoteProcesses
		actions[i].Disabled = actions[i].Disabled || processBlocked
	}
	return actions, count
}
