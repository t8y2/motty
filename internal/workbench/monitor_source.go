package workbench

import (
	"motty/internal/remotestats"
	"motty/internal/sshconn"
)

// monitorRunner is what the samplers need to run their fixed commands: an
// authenticated server connection, or this computer for a local terminal.
type monitorRunner interface {
	remotestats.Runner
	remotestats.DetailRunner
}

// monitorSupported reports whether the resource monitor has a command for a
// system. A host whose system is not known yet gets the Linux one, which says
// plainly what it found when it is something else.
func monitorSupported(system sshconn.RemoteSystem) bool {
	return system == sshconn.SystemUnknown || system.IsLinux() || system == sshconn.SystemWindows || system == sshconn.SystemDarwin
}

// monitorOffered reports whether a session can be monitored: a live shell on a
// connected server, or on this computer. Unlike the remote tools it needs no
// server, since a local terminal is sampled by running the same commands here.
func (state *State) monitorOffered(session *Session) bool {
	if session == nil || session.conn == nil || session.phase != sshconn.PhaseConnected || session.term == nil || !session.term.live {
		return false
	}
	return session.isLocal() || session.hasServer()
}

// monitorRunnerFor is where a session's monitor commands run.
func monitorRunnerFor(session *Session) monitorRunner {
	if session.isLocal() {
		return remotestats.Local{}
	}
	return commandRunnerFor(session)
}

func startMonitorWorker(runner remotestats.Runner, notify func(), system sshconn.RemoteSystem, seconds int) *remotestats.Worker {
	switch system {
	case sshconn.SystemWindows:
		return remotestats.StartWindows(runner, notify, seconds)
	case sshconn.SystemDarwin:
		return remotestats.StartDarwin(runner, notify, seconds)
	}
	return remotestats.StartWithInterval(runner, notify, seconds)
}

// startMonitorDetails starts the slow disk, interface and load readings, or
// returns nil for a system whose stream already carries them.
func startMonitorDetails(runner remotestats.DetailRunner, notify func(), system sshconn.RemoteSystem, seconds int) *remotestats.DetailsWorker {
	switch system {
	case sshconn.SystemWindows:
		return nil
	case sshconn.SystemDarwin:
		return remotestats.StartDetailsDarwin(runner, notify, seconds)
	}
	return remotestats.StartDetailsWithInterval(runner, notify, seconds)
}
