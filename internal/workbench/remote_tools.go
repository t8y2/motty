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
	hitRemoteAutoToggle    = tree.ID(remoteToolsHits, "remote-auto-toggle")
	hitRemoteClose         = tree.ID(remoteToolsHits, "remote-close")
	hitRemoteDismiss       = tree.ID(remoteToolsHits, "remote-dismiss")
	hitRemoteNext          = tree.ID(remoteToolsHits, "remote-next")
	hitRemoteProcessTable  = tree.ID(remoteToolsHits, "remote-process-table")
	hitRemoteProcessScroll = tree.ID(remoteToolsHits, "remote-process-scroll")
)

// hitRemoteRowList starts the row ids: one per entry, up to the list limits.
var hitRemoteRowList = tree.Sub(remoteToolsHits, tree.Name("remote-row"))
var hitRemoteIntervals = tree.Sub(remoteToolsHits, tree.Name("remote-refresh-interval"))
var hitRemoteSortColumns = tree.Sub(remoteToolsHits, tree.Name("remote-sort-column"))
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

// remoteTabChoices are the tabs in the order the switch shows them.
var remoteTabChoices = [...]remoteTab{remoteProcesses, remoteContainers, remoteImages, remoteVolumes, remoteNetworks, remoteCompose}

func (state *State) remoteTabLabel(tab remoteTab) string {
	m := state.messages.Tools
	switch tab {
	case remoteContainers:
		return m.RemoteTabContainers
	case remoteImages:
		return m.RemoteTabImages
	case remoteVolumes:
		return m.RemoteTabVolumes
	case remoteNetworks:
		return m.RemoteTabNetworks
	case remoteCompose:
		return m.RemoteTabCompose
	}
	return m.RemoteTabProcesses
}

// applyRemoteTab resets the view for the tab the page's choice already wrote and
// reads the objects that tab lists.
func (state *State) applyRemoteTab() {
	view := &state.remote
	m := state.messages.Tools
	view.selected, view.page = "", 0
	view.rows, view.pageText = nil, ""
	view.procs, view.containers, view.compose, view.resources = nil, nil, nil, nil
	view.collectedAt, view.stale, view.snapshotStatus = time.Time{}, false, ""
	view.processesTruncated, view.commandsTruncated = false, false
	state.rebuildRemoteRows()
	if view.noDocker && view.tab != remoteProcesses {
		// Docker availability belongs to the connection, not its tab. Keep the
		// empty state until an explicit or timed refresh rechecks it.
		view.serial++
		view.loading, view.failed, view.status = false, true, m.RemoteErrNoDocker
		return
	}
	state.refreshRemoteTools()
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

	procs                  []remotetools.Process
	containers             []remotetools.Container
	compose                []remotetools.ComposeProject
	resources              []remotetools.Resource
	rows                   []remoteRow
	selected               string
	page                   int
	pageText               string
	processScroll          float32
	processScrollBar       components.ScrollAreaGeometry
	processScrollDragging  bool
	processScrollGrab      float32
	processScrollValue     string
	processScrollFirst     int
	processScrollCount     int
	processTableBounds     gfx.Rect
	tableBodyBounds        gfx.Rect
	tableBodyFirst         int
	tableRowHeight         float32
	processColumns         [6]components.TableColumn
	searches               [6]remoteSearch
	processDetails         remoteProcessDetails
	processDetailsSerial   uint64
	containerDetails       remoteContainerDetails
	containerDetailsSerial uint64
	// tableHandlers, refreshHandlers and autoHandlers take the page controls'
	// input. They are made once, not in each paint.
	tableHandlers   ui.Handlers
	refreshHandlers ui.Handlers
	autoHandlers    ui.Handlers
	// The page's declaration functions are made once too: method values and
	// closures made in each paint allocate.
	tabLabel     func(remoteTab) string
	autoMenu     func(*ui.MenuItems)
	processMenu  func(*ui.MenuItems)
	autoDraw     func(*ui.DrawContext)
	processDraw  func(*ui.DrawContext)
	objectDraw   func(*ui.DrawContext)
	body, footer func()
	detailsAct   func(gfx.ID)
	// ctx is the page declaration being used while the tools dialog declares
	// itself. The stored body, footer and menu functions read it, because a
	// closure cannot capture a paint's C beyond that paint.
	ctx ui.C
	// tablePressRow is the row a press took, so a release opens the process
	// details only when it is the same row.
	tablePressRow                         int
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
	if remoteMutation(id) && (view.stale || view.loading || view.collectedAt.IsZero()) {
		view.status, view.failed = m.RemoteRefreshRequired, true
		return
	}
	switch {
	case id == hitRemoteClose || id == hitRemoteDismiss:
		state.closeRemoteToolsSurface()
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
	state.remote.tablePressRow = -1
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
