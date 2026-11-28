package workbench

import (
	"context"
	"fmt"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/remotetools"
	"motty/internal/sshconn"
)

var (
	// hitMonitorProcRow is one process of the list, numbered by the order the
	// card painted it in: a secondary press names the row it landed on.
	hitMonitorProcRows          = tree.Feature("connection-monitor-process-rows")
	hitMonitorProcMenuDismiss   = tree.ID(monitorHits, "process-menu-dismiss")
	hitMonitorProcMenuDetails   = tree.ID(monitorHits, "process-menu-details")
	hitMonitorProcMenuCopyPID   = tree.ID(monitorHits, "process-menu-copy-pid")
	hitMonitorProcMenuTerminate = tree.ID(monitorHits, "process-menu-terminate")
	hitMonitorProcMenuKill      = tree.ID(monitorHits, "process-menu-kill")
)

// monitorActionNotice is how long the footer keeps an action's outcome: long
// enough to read, short enough that the next reading takes the line back.
const monitorActionNotice = 12 * time.Second

// openMonitorProcessMenu records the row a secondary press landed on. The pid
// is copied out now, so a reading that re-orders the list between the press
// and the choice cannot point the menu at another process.
func (state *State) openMonitorProcessMenu(target gfx.ID, at gfx.Point) bool {
	if target.IsZero() || !state.monitorShown(state.deck.ActiveWorkspace()) {
		return false
	}
	session := state.contextSession()
	if session == nil || session.stats == nil {
		return false
	}
	p := &session.stats.panel
	for index, process := range p.procShown {
		if target != hitMonitorProcRows.At(index) {
			continue
		}
		p.procMenu = true
		p.procMenuPID = process.pid
		p.procMenuAt = at
		return true
	}
	return false
}

// declareMonitorProcessRowMenu declares the process actions the old tools tab
// offered, over the row that was pressed. A signal first resolves the PID to
// a verified instance and carries it through confirmation.
func (state *State) declareMonitorProcessRowMenu(c ui.C, p *monitorView) {
	if !p.procMenu {
		return
	}
	session := state.contextSession()
	if session == nil || session.stats == nil || &session.stats.panel != p {
		p.procMenu = false
		return
	}
	m := state.messages.Tools
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	// A host that cannot prove which instance runs under a pid offers its list
	// read-only, as the tools tab did: better a grey row than a signal that is
	// refused after the question was asked.
	system := monitorSystem(session)
	blocked := !state.monitorOffered(session) || system == sshconn.SystemDarwin || system == sshconn.SystemWindows
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitMonitorProcMenuDetails, Label: m.RemoteProcessDetails, Icon: icon(iconInfo)})
	rows.add(components.ContextMenuItem{ID: hitMonitorProcMenuCopyPID, Label: m.RemoteProcessCopyPID, Icon: icon(iconCopy)})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitMonitorProcMenuTerminate, Label: m.RemoteTerminate, Icon: icon(iconStop), Disabled: blocked})
	rows.add(components.ContextMenuItem{ID: hitMonitorProcMenuKill, Label: m.RemoteForceKill, Icon: icon(iconTrash), Variant: components.ContextMenuItemDestructive, Disabled: blocked})
	state.declareRowsMenu(c, &p.procMenu, p.procMenuAt, hitMonitorProcMenuDismiss, &rows, state.chooseMonitorProcessMenu)
}

func (state *State) chooseMonitorProcessMenu(id gfx.ID) {
	session := state.contextSession()
	if session == nil || session.stats == nil {
		return
	}
	p := &session.stats.panel
	pid := p.procMenuPID
	if pid <= 0 {
		return
	}
	switch id {
	case hitMonitorProcMenuDetails:
		state.startMonitorProcessDetails(session, pid)
	case hitMonitorProcMenuCopyPID:
		state.copyMonitorProcessPID(session, pid)
	case hitMonitorProcMenuTerminate:
		state.startMonitorProcessConfirmation(session, pid, remotetools.Terminate)
	case hitMonitorProcMenuKill:
		state.startMonitorProcessConfirmation(session, pid, remotetools.Kill)
	}
}

// A menu row only has a PID. Resolve it to a verified instance before asking
// the user to confirm, then carry that exact instance through the dialog.
func (state *State) startMonitorProcessConfirmation(session *Session, pid int, signal remotetools.Signal) {
	var process remotetools.Process
	state.monitorProcessAction(session, func(ctx context.Context, runner remotetools.Runner) error {
		snapshot, err := remotetools.ReadSafeProcessSnapshot(ctx, runner, remotetools.SortCPU)
		if err != nil {
			return err
		}
		for _, candidate := range snapshot.Processes {
			if candidate.PID == pid {
				process = candidate
				return nil
			}
		}
		return remotetools.ErrProcessChanged
	}, func(state *State, target *Session, err error) {
		if err == nil && process.StartIdentity == "" {
			err = remotetools.ErrProcessUnverifiable
		}
		if err != nil {
			state.monitorNotice(target, state.remoteErrorText(err), true)
			return
		}
		state.confirmMonitorProcessSignal(target, process, shortCommand(process.Command), signal)
	})
}

// copyMonitorProcessPID puts the pid on the clipboard and says so in the footer,
// where the panel reports everything the host said.
func (state *State) copyMonitorProcessPID(session *Session, pid int) {
	if state.options.SetClipboardText == nil {
		return
	}
	err := state.options.SetClipboardText(fmt.Sprint(pid))
	if err != nil {
		state.monitorNotice(session, fmt.Sprintf(state.messages.StatusClipboardFailedFmt, err), true)
		return
	}
	state.monitorNotice(session, state.messages.StatusClipboardCopied, false)
}

// confirmMonitorProcessSignal asks before a signal leaves, as the tools tab did:
// the row names the process and what is lost with it.
func (state *State) confirmMonitorProcessSignal(session *Session, process remotetools.Process, name string, signal remotetools.Signal) {
	m := state.messages.Tools
	title, submit := m.RemoteConfirmTerminateTitle, m.RemoteTerminate
	if signal == remotetools.Kill {
		title, submit = m.RemoteConfirmKillTitle, m.RemoteForceKill
	}
	if name == "" {
		name = fmt.Sprintf("PID %d", process.PID)
	}
	sessionID := session.ID
	state.openForm(formDialog{
		title:       title,
		description: fmt.Sprintf(m.RemoteConfirmProcessFmt, process.PID, name),
		submit:      submit,
		danger:      true,
		run: func(state *State, _ *formDialog) string {
			// The question may outlive the session it was asked for.
			if target, ok := state.sessionByIDPointer(sessionID); ok {
				state.startMonitorProcessSignal(target, process, signal)
			}
			return ""
		},
	})
}

// monitorProcessAction runs one process action against a session's host off the
// window thread. It binds the runner the panel samples through, says the panel
// is reading, and hands the result back on the window thread, where done
// reports it — or opens what it found — with the session still in hand.
func (state *State) monitorProcessAction(
	session *Session,
	work func(context.Context, remotetools.Runner) error,
	done func(*State, *Session, error),
) {
	if !state.monitorOffered(session) || session.stats == nil {
		return
	}
	runner := remotetools.ForSystem(monitorRunnerFor(session), monitorSystem(session).String())
	sessionID := session.ID
	state.monitorNotice(session, state.messages.Tools.RemoteLoading, false)
	ctx, cancel := context.WithTimeout(state.remoteToolsContext(), remoteTimeout)
	go func() {
		defer cancel()
		err := work(ctx, runner)
		state.aiPost(func(state *State) {
			target, ok := state.sessionByIDPointer(sessionID)
			if !ok || target.stats == nil {
				return
			}
			done(state, target, err)
		})
	}()
}

// The guard checks that the confirmed instance still owns its PID immediately
// before sending the signal; it must not resolve the PID anew after confirmation.
func (state *State) startMonitorProcessSignal(session *Session, process remotetools.Process, signal remotetools.Signal) {
	label := "TERM"
	if signal == remotetools.Kill {
		label = "KILL"
	}
	state.monitorProcessAction(session, func(ctx context.Context, runner remotetools.Runner) error {
		return remotetools.SendSignalToProcess(ctx, runner, process, signal)
	}, func(state *State, target *Session, err error) {
		if err != nil {
			state.monitorNotice(target, state.remoteErrorText(err), true)
			return
		}
		state.monitorNotice(target, fmt.Sprintf(state.messages.Tools.RemoteSignalSentFmt, label, process.PID), false)
	})
}

// monitorNotice is the footer's line about what an action did, with the moment
// it was said so a later reading can take the line back.
func (state *State) monitorNotice(session *Session, text string, failed bool) {
	if session == nil || session.stats == nil {
		return
	}
	p := &session.stats.panel
	p.actionStatus, p.actionFailed, p.actionAt = text, failed, time.Now()
}

// startMonitorProcessDetails reads what the details dialog shows for a pid: the
// monitor's own rows carry only what the panel prints, so the row is looked up
// in a safe snapshot, which also proves the instance the dialog's actions would
// signal.
func (state *State) startMonitorProcessDetails(session *Session, pid int) {
	var process remotetools.Process
	found := false
	state.monitorProcessAction(session, func(ctx context.Context, runner remotetools.Runner) error {
		snapshot, err := remotetools.ReadSafeProcessSnapshot(ctx, runner, remotetools.SortCPU)
		if err != nil {
			return err
		}
		for _, candidate := range snapshot.Processes {
			if candidate.PID == pid {
				process, found = candidate, true
				break
			}
		}
		return nil
	}, func(state *State, target *Session, err error) {
		switch {
		case err != nil:
			state.monitorNotice(target, state.remoteErrorText(err), true)
		case !found:
			state.monitorNotice(target, state.messages.Tools.RemoteProcessChanged, true)
		default:
			state.openMonitorProcessDetails(target, process)
		}
	})
}

// openMonitorProcessDetails shows the tools tab's own details dialog for a row
// of the monitor. The dialog reads the tools view it was built against, so that
// view is pointed at the monitor's session and reading before it opens; the
// loads it starts then run over the same connection the panel samples.
func (state *State) openMonitorProcessDetails(session *Session, process remotetools.Process) {
	state.remote.session = session.ID
	state.remote.collectedAt = time.Now()
	state.remote.stale, state.remote.loading = false, false
	state.openRemoteProcessDetails(process)
	state.remote.processDetails.fromMonitor = true
	if session.stats != nil {
		state.monitorNotice(session, "", false)
	}
}

// declareMonitorProcessDetails declares that dialog while the monitor is the
// surface that opened it, and hands it the monitor's own actions: the footer
// buttons must signal what the panel shows, not what a tools table selected.
func (state *State) declareMonitorProcessDetails(c ui.C) {
	details := &state.remote.processDetails
	if !details.open || !details.fromMonitor {
		return
	}
	session := state.contextSession()
	if session == nil || session.ID != state.remote.session || !state.monitorShown(state.deck.ActiveWorkspace()) {
		state.closeRemoteProcessDetails()
		return
	}
	process, sessionID := details.process, session.ID
	state.declareRemoteProcessDetailsDialog(c, func(id gfx.ID) {
		state.monitorProcessDetailAction(sessionID, process, id)
	})
}

// monitorProcessDetailAction is what the details dialog's footer buttons do when
// the monitor opened it: the same signals the row menu offers, plus the two
// priority changes the dialog has always carried.
func (state *State) monitorProcessDetailAction(sessionID uint32, process remotetools.Process, id gfx.ID) {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || !state.monitorOffered(session) {
		return
	}
	name := shortCommand(process.Command)
	switch id {
	case hitRemoteTerminate:
		state.confirmMonitorProcessSignal(session, process, name, remotetools.Terminate)
	case hitRemoteKill:
		state.confirmMonitorProcessSignal(session, process, name, remotetools.Kill)
	case hitRemoteNiceLower:
		state.startMonitorProcessRenice(session, process, 10)
	case hitRemoteNiceNormal:
		state.startMonitorProcessRenice(session, process, 0)
	case hitRemoteNiceHigher:
		state.startMonitorProcessRenice(session, process, -5)
	}
}

// startMonitorProcessRenice changes the priority of the instance the snapshot
// proved, and reports it where the panel reports.
func (state *State) startMonitorProcessRenice(session *Session, process remotetools.Process, nice int) {
	state.monitorProcessAction(session, func(ctx context.Context, runner remotetools.Runner) error {
		return remotetools.ReniceProcess(ctx, runner, process, nice)
	}, func(state *State, target *Session, err error) {
		if err != nil {
			state.monitorNotice(target, state.remoteErrorText(err), true)
			return
		}
		state.monitorNotice(target, fmt.Sprintf(state.messages.Tools.RemoteReniceDoneFmt, process.PID, nice), false)
	})
}
