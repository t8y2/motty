package workbench

import (
	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

var sudoSFTPHits = tree.Feature("sftp-sudo")

var (
	hitSudoSFTPPassword = tree.ID(sudoSFTPHits, "password")
	hitSudoSFTPEnable   = tree.ID(sudoSFTPHits, "enable")
	hitSudoSFTPCancel   = tree.ID(sudoSFTPHits, "cancel")
	hitSudoSFTPDismiss  = tree.ID(sudoSFTPHits, "dismiss")
)

type sudoSFTPDialog struct {
	sessionID   uint32
	window      *sftpWindow
	password    text.Editor
	field       ui.Field
	row         ui.FormFieldState
	bound       bool
	commands    bool
	logs        bool
	dashboard   bool
	returnTools bool
}

// sudoDialogBody is the body of the dialog that asks for one secret without the
// vault's save box: the labeled field and the padding around it. The field is a
// form field like the other secret dialogs, so the control inside it keeps the
// ordinary single-line height.
func sudoDialogBody(theme *base.Theme) float32 { return dialogRowHeight(theme) + 4 }

func sudoSFTPAvailable(session *Session, view *sftpView) bool {
	return !view.localOnly && session.host.System != sshconn.SystemWindows && session.host.Transfer != transferSCP
}

func (state *State) toggleSudoSFTP(session *Session, view *sftpView) {
	if !sudoSFTPAvailable(session, view) {
		return
	}
	if view.localOnly || view.editPath != "" || view.busy || view.extracting {
		state.showActionError(state.messages.SFTPSudoEnable, state.messages.SFTPSudoBusy)
		return
	}
	if state.sftpPrivilegeDocumentBusy(view) {
		state.showActionError(state.messages.SFTPSudoEnable, state.messages.SFTPSudoBusy)
		return
	}
	state.refreshSFTPTransfers(view)
	for _, transfer := range view.transfers {
		switch transfer.State {
		case sftpclient.TransferQueued, sftpclient.TransferRunning, sftpclient.TransferPaused:
			state.showActionError(state.messages.SFTPSudoEnable, state.messages.SFTPSudoBusy)
			return
		}
	}
	if view.sudo {
		state.switchSudoSFTP(session, view, "", false)
		return
	}
	if _, ok := session.conn.(interface {
		OpenSudoSFTP(string) (sftpclient.Remote, error)
	}); !ok {
		state.showActionError(state.messages.SFTPSudoEnable, state.messages.SFTPUnavailable)
		return
	}
	state.sudoSFTP = sudoSFTPDialog{sessionID: session.ID}
	if view.detached != nil {
		state.sudoSFTP.window = view.detached
		if state.options.Raise != nil {
			state.options.Raise()
		}
	}
	state.modal = modalSudoSFTP
}

func (state *State) submitSudoSFTP() {
	sessionID := state.sudoSFTP.sessionID
	session, ok := state.sessionByIDPointer(sessionID)
	password := state.sudoSFTP.password.String()
	commands, returnTools := state.sudoSFTP.commands, state.sudoSFTP.returnTools
	dashboard, logs := state.sudoSFTP.dashboard, state.sudoSFTP.logs
	window := state.sudoSFTP.window
	state.closeModal()
	if commands {
		if dashboard {
			for _, source := range state.dashboard.sources {
				if source.session.ID == sessionID {
					state.setSudoCommands(&source.session, password, true)
					return
				}
			}
			return
		}
		if logs {
			if ok {
				state.setSudoCommands(session, password, true)
			}
			return
		}
		if state.remoteWindow != nil {
			if owner, source, found := state.remoteWindow.source(); found {
				owner.setSudoCommands(source, password, true)
				state.remoteWindow.bind(owner, source)
			}
		} else if ok {
			state.setSudoCommands(session, password, true)
		}
		if returnTools {
			state.modal = modalRemoteTools
			state.refreshRemoteTools()
		}
		return
	}
	if window != nil {
		if !window.closed {
			state.toggleSudoSFTPWithPassword(&window.session, password)
		}
		return
	}
	if ok && session.files != nil {
		state.toggleSudoSFTPWithPassword(session, password)
	}
}

func (state *State) toggleSudoSFTPWithPassword(session *Session, password string) {
	view := session.files
	if view.sudo || view.busy || view.extracting || view.editPath != "" || state.sftpPrivilegeDocumentBusy(view) {
		return
	}
	state.refreshSFTPTransfers(view)
	for _, transfer := range view.transfers {
		switch transfer.State {
		case sftpclient.TransferQueued, sftpclient.TransferRunning, sftpclient.TransferPaused:
			state.showActionError(state.messages.SFTPSudoEnable, state.messages.SFTPSudoBusy)
			return
		}
	}
	state.switchSudoSFTP(session, view, password, true)
}

func (state *State) sftpPrivilegeDocumentBusy(view *sftpView) bool {
	for _, document := range state.fileWindows.documents {
		if document.view == view && !document.local && !document.closed && (document.busy || document.comparison.active) {
			return true
		}
	}
	return false
}

func (state *State) switchSudoSFTP(session *Session, view *sftpView, password string, elevated bool) {
	// The transfer transport changes under the view; an upload or download that
	// was under way continues from its partial once the new worker is usable.
	view.worker.Interrupt()
	view.worker.Close()
	state.refreshSFTPTransfers(view)
	state.archiveTransfers(view)
	// The new worker numbers its transfers from one again; the old source
	// labels belong to ids that no longer exist. Their leases end first.
	for id := range view.remoteCopies {
		state.releaseRemoteCopySource(view, id)
	}
	view.remoteCopies = nil
	view.sudo = elevated
	connection, mode := session.conn, session.host.Transfer
	if view.dedicated != nil && view.dedicated.ready {
		// The panel opened a connection of its own when the server refused
		// another channel on the session's transport (see sftp_connection.go).
		connection = view.dedicated.conn
	}
	view.openFiles = func() (sftpclient.Remote, error) { return openRemoteFiles(connection, mode) }
	if elevated {
		opener := connection.(interface {
			OpenSudoSFTP(string) (sftpclient.Remote, error)
		})
		view.openFiles = func() (sftpclient.Remote, error) { return opener.OpenSudoSFTP(password) }
	}
	view.worker = sftpclient.NewWorker(view.openFiles, state.notifyForView(session, view))
	state.configureSFTPWorker(view)
	for _, document := range state.fileWindows.documents {
		if document.view == view && !document.local && !document.closed && document.worker != nil {
			document.worker.Close()
			document.openRemoteWorker(view.openFiles)
		}
	}
	view.listings = nil
	view.listingBytes = 0
	view.navigation = sftpNavigation{}
	view.waitingNavigation = sftpNavigation{}
	view.busy = false
	state.listSFTP(view, view.sftpFolder.path)
}

func (state *State) clickSudoSFTP(id gfx.ID) {
	switch id {
	case hitSudoSFTPEnable:
		state.submitSudoSFTP()
	case hitSudoSFTPCancel, hitSudoSFTPDismiss:
		returnTools := state.sudoSFTP.returnTools
		state.closeModal()
		if returnTools {
			state.modal = modalRemoteTools
			state.refreshRemoteTools()
		}
	}
}

// declareSudoSFTPDialog declares the open sudo password dialog as a dialog of
// the workbench page. Its field and buttons are page controls; the editor they
// bind is the one submit reads.
func (state *State) declareSudoSFTPDialog(c ui.C) {
	if state.modal != modalSudoSFTP {
		return
	}
	dialogState, m := &state.sudoSFTP, state.messages
	title, hint := m.SFTPSudoEnable, m.SFTPSudoHint
	if dialogState.commands {
		title, hint = m.SudoCommandsEnable, m.SudoCommandsHint
		if dialogState.dashboard {
			title, hint = m.DashboardSudoEnable, m.DashboardSudoHint
		}
	}
	if !dialogState.bound {
		dialogState.field.Bind(&dialogState.password)
		dialogState.bound = true
	}
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: hint, Width: 540,
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitSudoSFTPDismiss, CloseID: hitSudoSFTPCancel, InitialFocusID: hitSudoSFTPPassword,
	})
	dialog.Body(func() {
		ui.Column(c).Height(sudoDialogBody(c.Theme())).PadSides(2, 4, 0, 4).Children(func() {
			ui.FormField(c, &dialogState.row, m.SFTPSudoPassword).FillWidth().Height(dialogRowHeight(c.Theme())).Children(func() {
				ui.TextFieldID(c, hitSudoSFTPPassword, &dialogState.field, m.SFTPSudoPassword, "").
					FillWidth().Secure().AutoFocus()
			})
		})
	})
	dialog.Footer(func() {
		if ui.Button(c, m.Cancel).Secondary().Small().Shrink(0).Clicked() {
			c.After(func() { state.clickSudoSFTP(hitSudoSFTPCancel) })
		}
		if ui.Button(c, title).Small().Shrink(0).Clicked() {
			c.After(func() { state.submitSudoSFTP() })
		}
	})
	if dialog.Dismissed() {
		c.After(func() { state.clickSudoSFTP(hitSudoSFTPCancel) })
	}
}
