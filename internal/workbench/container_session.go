package workbench

import (
	"errors"

	"motty/internal/remotetools"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// openContainerTab borrows the selected tab's authenticated SSH transport, never
// another matching host or a new login. The command is constructed by remotetools.
func (state *State) openContainerTab(sourceID uint32, name, command string) error {
	source, ok := state.sessionByIDPointer(sourceID)
	if !ok || !state.remoteToolsOffered(source) || !source.host.isSSH() {
		return errors.New("the SSH host is no longer connected")
	}
	if source.host.System == sshconn.SystemWindows {
		return errors.New("container shells are not supported on Windows SSH hosts")
	}
	lender, ok := source.conn.(sharer)
	if !ok {
		return errors.New("this SSH connection cannot open another terminal")
	}
	relay := newNotifyRelay(state.options.Invalidate)
	conn, err := lender.Share(relay.call)
	if err != nil {
		return err
	}
	title := source.Title
	host, address := source.host, source.Address
	pendingWorkspace, pendingPane := state.pendingWorkspace, state.pendingPane
	state.pendingWorkspace, state.pendingPane = 0, 0
	created := state.createHostSessionWithConnection(host, address, conn, command, relay)
	if created == nil {
		state.pendingWorkspace, state.pendingPane = pendingWorkspace, pendingPane
		conn.Cancel()
		return errors.New("unable to create a new terminal tab")
	}
	if remotetools.ValidName(name) {
		workspace, _, ok := state.deck.PaneForSession(layoutcore.SessionID(created.ID))
		label := title + " · " + name
		if ok && state.deck.Rename(workspace, label) == nil {
			created.Title = label
			state.rebuildTabOptions()
			state.save()
		}
	}
	return nil
}
