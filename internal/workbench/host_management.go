package workbench

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"

	"motty/internal/keyidentity"
	"motty/internal/sshconn"
	"motty/internal/vault"
	layoutcore "motty/internal/workspace"
)

type hostDialogMode uint8

const (
	hostDialogAdd hostDialogMode = iota
	hostDialogEdit
	hostDialogCopy
)

func (state *State) clickHostManagement(id gfx.ID) bool {
	if state.importMenu {
		// A choice closes the menu and then runs as if it had been clicked
		// directly; any other press only dismisses it.
		state.importMenu = false
		switch id {
		case hitImportSSHConfig, hitImportHostCatalog, hitImportSessions, hitExportHosts:
		default:
			return true
		}
	}
	if state.hostFilterMenu {
		switch {
		case id == hitHostFilterMenuDismiss || id == hitHostFilterMore:
			state.hostFilterMenu = false
		case id.Scope() == hitHostFilterMenuList:
			state.setHostFilter(true, state.hostGroups[uint32(item(id))].Name)
		default:
			state.hostFilterMenu = false
		}
		return true
	}
	if state.hostGroupMenu {
		switch {
		case id == hitHostGroupDismiss || id == hitMoveHostGroup:
			state.hostGroupMenu = false
		case id == hitHostGroupNone:
			state.hostGroupMenu = false
			state.applyHostGroup("")
		case id == hitHostGroupNew:
			state.hostGroupMenu = false
			state.hostGroupEditor = text.Editor{}
			state.groupActions.op = hostGroupMoveHosts
			state.dialogError = ""
			state.modal = modalMoveHostGroup
		case id.Scope() == hitHostGroupItemList:
			group := state.hostGroups[uint32(item(id))].Name
			state.hostGroupMenu = false
			state.applyHostGroup(group)
		default:
			state.hostGroupMenu = false
		}
		return true
	}
	if state.activeSection != sectionHosts || state.modal != modalNone {
		return false
	}
	if state.clickHostScopeSelection(id) {
		return true
	}
	if hostID, ok := state.hostIDForSelectionHit(id); ok {
		state.selectedHostID = hostID
		state.toggleHostSelection(hostID)
		return true
	}
	if id.Scope() == hitHostGroupRowList {
		state.toggleHostGroup(item(id))
		return true
	}
	if id.Scope() == hitHostGroupOpenList {
		state.toggleHostGroup(item(id))
		return true
	}
	if state.clickHostChipMenu(id) {
		return true
	}
	if id.Scope() == hitHostFilterList {
		if index := int(item(id)); index < len(state.hostGroups) {
			state.setHostFilter(true, state.hostGroups[index].Name)
		}
		return true
	}
	switch id {
	case hitHostFilterAll:
		state.setHostFilter(false, "")
	case hitHostFilterMore:
		state.hostFilterMenu = true
	case hitHostGroupsExpandAll:
		state.setHostGroupsExpanded(true)
	case hitHostGroupsCollapseAll:
		state.setHostGroupsExpanded(false)
	case hitEditHost:
		state.openEditHost()
	case hitConnectSelectedHosts:
		state.openSelectedHostsWorkspace()
	case hitCopyHost:
		state.openCopyHost()
	case hitDeleteHost:
		state.openDeleteHosts()
	case hitMoveHostGroup:
		state.captureHostActionIDs()
		state.groupMenu.menu = false
		state.hostGroupMenu = len(state.hostActionIDs) > 0
	case hitImportMenu:
		state.importMenu = true
	case hitExportHosts:
		state.exportHostCatalog()
	case hitImportHostCatalog:
		state.importHostCatalog()
	default:
		return false
	}
	return true
}

func (state *State) openSelectedHostsWorkspace() {
	if !state.selectedHostsWorkspaceEligible() {
		return
	}
	state.openWorkspaceDraft(workspaceDraftTemporary)
	for _, id := range state.hostSelections {
		if host, ok := state.hostByID(id); ok {
			state.workspaceDraft.selected[host.CatalogID] = true
		}
	}
	state.workspaceDraft.layout = DefaultWorkspaceLayout(state.workspaceDraftSelectionCount())
}

func (state *State) selectedHostsWorkspaceEligible() bool {
	if len(state.hostSelections) == 0 || len(state.hostSelections) > layoutcore.MaxPanes {
		return false
	}
	for _, id := range state.hostSelections {
		host, ok := state.hostByID(id)
		if !ok || !workspaceHostEligible(host) {
			return false
		}
	}
	return true
}

func (state *State) hostIDForSelectionHit(id gfx.ID) (uint32, bool) {
	if id.Scope() != hitHostCheckList {
		return 0, false
	}
	hostID := uint32(item(id))
	_, ok := state.hostByID(hostID)
	return hostID, ok
}

func (state *State) toggleHostSelection(id uint32) {
	for index, selected := range state.hostSelections {
		if selected != id {
			continue
		}
		copy(state.hostSelections[index:], state.hostSelections[index+1:])
		state.hostSelections = state.hostSelections[:len(state.hostSelections)-1]
		state.refreshHostSelection()
		return
	}
	state.hostSelections = append(state.hostSelections, id)
	state.refreshHostSelection()
}

func (state *State) hostSelected(id uint32) bool {
	for _, selected := range state.hostSelections {
		if selected == id {
			return true
		}
	}
	return false
}

func (state *State) refreshHostSelection() {
	state.hostSelectionText = fmt.Sprintf(state.messages.HostSelectionFmt, len(state.hostSelections))
	state.refreshHostScopeSelection()
}

func (state *State) captureHostActionIDs() {
	state.hostActionIDs = state.hostActionIDs[:0]
	for _, id := range state.hostSelections {
		if _, ok := state.hostByID(id); ok {
			state.hostActionIDs = append(state.hostActionIDs, id)
		}
	}
	if len(state.hostActionIDs) == 0 {
		if _, ok := state.hostByID(state.selectedHostID); ok {
			state.hostActionIDs = append(state.hostActionIDs, state.selectedHostID)
		}
	}
}

func (state *State) openEditHost() {
	host, ok := state.singleHostAction()
	if !ok {
		return
	}
	state.openEditHostID(host.ID)
}

func (state *State) openEditHostID(id uint32) {
	host, ok := state.hostByID(id)
	if !ok {
		return
	}
	state.selectedHostID = host.ID
	state.cancelHostTest()
	state.editor.load(host)
	state.editor.agentAccess = host.isSSH() && state.agentHosts[host.CatalogID]
	state.prepareOTPState(host)
	state.editor.advancedOpen = state.editor.advancedOpen || state.editor.otpSaved || state.editor.agentAccess
	state.hostDialogMode = hostDialogEdit
	state.hostDialogID = host.ID
	state.dialogError = ""
	state.modal = modalNewHost
	state.prefillHostSecrets()
}

func (state *State) openCopyHost() {
	host, ok := state.singleHostAction()
	if !ok {
		return
	}
	state.openCopyHostID(host.ID)
}

func (state *State) openCopyHostID(id uint32) {
	host, ok := state.hostByID(id)
	if !ok {
		return
	}
	state.selectedHostID = host.ID
	state.cancelHostTest()
	state.editor.load(host)
	state.editor.agentAccess = false // a copy is a new host, closed to agents until asked
	state.editor.label = text.Editor{}
	state.editor.label.Insert(fmt.Sprintf(state.messages.HostCopyLabelFmt, host.Label))
	state.hostDialogMode = hostDialogCopy
	state.hostDialogID = host.ID
	state.dialogError = ""
	state.modal = modalNewHost
	state.prefillHostSecrets()
}

func (state *State) singleHostAction() (Host, bool) {
	if len(state.hostSelections) > 1 {
		return Host{}, false
	}
	if len(state.hostSelections) == 1 {
		return state.hostByID(state.hostSelections[0])
	}
	return state.selectedHost()
}

func (state *State) submitHostDialog() {
	if !state.commitHostGroupQuery() {
		return
	}
	if !state.commitHostTagQuery() {
		return
	}
	ignoreID := uint32(0)
	if state.hostDialogMode == hostDialogEdit {
		ignoreID = state.hostDialogID
	}
	if message := state.editor.validate(state.hosts, ignoreID, state.messages); message != "" {
		state.dialogError = message
		return
	}
	var previous, source *Host
	if state.hostDialogMode == hostDialogEdit {
		if index := state.hostIndex(state.hostDialogID); index >= 0 {
			old := state.hosts[index]
			previous, source = &old, &old
		}
	} else if state.hostDialogMode == hostDialogCopy {
		if index := state.hostIndex(state.hostDialogID); index >= 0 {
			original := state.hosts[index]
			source = &original
		}
	}
	if state.hostDialogMode == hostDialogEdit && previous == nil {
		state.modal = modalNone
		return
	}
	if message := state.otpPrecheck(); message != "" {
		state.dialogError = message
		state.editor.advancedOpen = true
		return
	}
	if message := state.hostProxyPrecheck(); message != "" {
		state.dialogError = message
		return
	}
	candidateID := state.nextHostID
	if previous != nil {
		candidateID = previous.ID
	}
	candidate := state.editor.host(candidateID)
	if previous != nil {
		candidate.CatalogID, candidate.System = previous.CatalogID, previous.System
	} else {
		used := make(map[string]struct{}, len(state.hosts))
		for _, host := range state.hosts {
			if host.CatalogID != "" {
				used[host.CatalogID] = struct{}{}
			}
		}
		// The form fills in the ID that the address would give; a host that is
		// being created takes one of its own instead.
		candidate.CatalogID = freshCatalogID(used)
	}
	passwordChanges, failure := state.hostDialogPasswordChanges(candidate, source)
	if failure != "" {
		state.dialogError = failure
		return
	}
	proxyNote, proxyChanges, failure := state.hostProxyChanges(candidate, previous, source)
	if failure != "" {
		state.dialogError = failure
		return
	}
	passphraseChanges, failure := state.hostPassphraseChanges()
	if failure != "" {
		state.dialogError = failure
		return
	}
	changes := append(passwordChanges, proxyChanges...)
	changes = append(changes, passphraseChanges...)
	if len(changes) > 0 {
		vaultFile := state.secrets.vault
		if vaultFile == nil {
			state.dialogError = state.messages.Tools.ProxyErrorNoVault
			return
		}
		if err := vaultFile.Apply(changes); err != nil {
			if errors.Is(err, vault.ErrNeedsConversion) && len(proxyChanges) > 0 {
				state.dialogError = state.messages.Tools.ProxyPasswordLocked
			} else {
				state.dialogError = state.hostPasswordSaveError(err)
			}
			return
		}
	}
	state.editor.password, state.editor.passphrase = text.Editor{}, text.Editor{}
	state.editor.proxyPassword = ""
	// A copy or a new host is closed to agents unless the switch was turned on in
	// this very dialog.
	state.setAgentAccess(candidate, state.editor.agentAccess)
	if state.hostDialogMode == hostDialogEdit {
		state.updateHostFromEditor(candidate, *previous, proxyNote)
		return
	}
	host := candidate
	state.nextHostID++
	state.hosts = append(state.hosts, host)
	state.selectedHostID = host.ID
	state.cancelHostTest()
	state.dialogError = ""
	if state.hostDialogMode == hostDialogCopy {
		state.status = state.messages.HostCopied + host.Label
	} else {
		state.status = state.messages.StatusHostAdded + host.Label
	}
	if state.editor.value(fieldOTP) != "" {
		state.commitOTP(host) // after the host's own line, so the secret's is the one read
	}
	state.appendStatusNote(proxyNote)
	state.hostDataChanged()
	state.save()
	state.restoreAfterHostDialog()
}

func (state *State) updateHostFromEditor(updated Host, previous Host, notes ...string) {
	index := state.hostIndex(state.hostDialogID)
	if index < 0 {
		state.restoreAfterHostDialog()
		return
	}
	state.replaceHost(index, previous, updated)
	state.cancelHostTest()
	state.dialogError = ""
	state.status = state.messages.HostUpdated + updated.Label
	state.commitOTP(updated)
	for _, note := range notes {
		state.appendStatusNote(note)
	}
	state.hostDataChanged()
	state.save()
	state.restoreAfterHostDialog()
}

// replaceHost puts updated in the place of previous, the host at index, and
// brings what kept a copy of it in step: the tabs open on it, its forwards and
// the lists that show it.
func (state *State) replaceHost(index int, previous, updated Host) {
	state.hosts[index] = updated
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.HostID != updated.ID {
			continue
		}
		session.host = updated
		if previous.Label != updated.Label {
			state.renameSession(session, updated.Label)
		}
		if session.conn == nil {
			session.Address = state.sessionAddress(updated)
		}
	}
	// A forward belongs to a destination, not to one host: while another host
	// still connects where this one did, the forward stays with it.
	forwardsFollow := !state.endpointShared(previous, previous.ID)
	for index := range state.forwards {
		forward := &state.forwards[index]
		if !forwardsFollow || !forward.matchesHost(previous) {
			continue
		}
		forward.HostLabel = updated.Label
		forward.HostAddress = updated.Address
		forward.HostUser = updated.User
		forward.HostPort = updated.Port
	}
	state.rebuildTabOptions()
	state.refreshForwards()
	state.refreshProxies()
}

// hostPasswordSecretName is where the password of a host is kept, and false for a
// host that signs in without one. It is kept under the host, not its address:
// editing the address or the login moves nothing, deleting the host deletes it,
// and a copy of the host has a password of its own.
func hostPasswordSecretName(host Host) (string, bool) {
	switch host.Protocol {
	case protocolSSH:
		if host.Auth == authPrivateKey || host.Auth == authAgent {
			return "", false
		}
	case protocolVNC, protocolRDP:
	default:
		return "", false
	}
	return hostSecretName(host.CatalogID, hostPasswordPartFor(host)), true
}

// hostPasswordPartFor identifies the authentication account independently of
// the catalog identity assigned to an edited or copied host.
func hostPasswordPartFor(host Host) string {
	if host.Protocol == protocolVNC && !host.tunneled() && host.User != "" {
		return vncSystemPasswordPart(host.User)
	}
	return hostPasswordPart
}

// hostDialogPasswordChanges plans what the form's password field does to the
// vault: the text in it is the host's password. A field that held the saved
// password and was emptied removes it; a field that never showed it (the vault
// could not be read) leaves it alone. The secret never becomes part of Host; the
// caller applies these changes atomically with the proxy login.
func (state *State) hostDialogPasswordChanges(host Host, source *Host) (changes []vault.Change, failure string) {
	name, hasPassword := hostPasswordSecretName(host)
	typed := state.editor.password.String()
	editing := state.hostDialogMode == hostDialogEdit && source != nil
	if state.hostTakesCredential() {
		// The password is the saved one's. What the host kept of its own is left
		// as it is, for the day the host goes back to it.
		return nil, ""
	}
	if source != nil && source.Protocol == protocolVNC && host.Protocol == protocolVNC && state.editor.passwordSaved &&
		hostPasswordPartFor(*source) != hostPasswordPartFor(host) {
		previous, _ := hostPasswordSecretName(*source)
		if typed == "" {
			return nil, "" // changing the account must not delete another account's secret
		}
		if state.secrets.vault != nil {
			value, found, err := state.secrets.vault.Get(previous)
			if err != nil {
				return nil, state.hostPasswordSaveError(err)
			}
			if found && typed == value {
				return nil, state.messages.Desktop.VNCAccountPasswordChanged
			}
		}
	}
	switch {
	case hasPassword && typed != "":
		if state.secrets.vault == nil {
			return nil, state.hostPasswordSaveError(vault.ErrUnavailable)
		}
		changes = append(changes, vault.Change{Name: name, Value: typed})
	case editing && state.editor.passwordSaved && state.secrets.vault != nil:
		// Emptied by the user, or no longer a host that has a password.
		if previous, had := hostPasswordSecretName(*source); had {
			changes = append(changes, vault.Change{Name: previous, Delete: true})
		}
	}
	if editing && source.Protocol != host.Protocol && state.secrets.vault != nil {
		if previous, had := hostPasswordSecretName(*source); had && previous != name {
			changes = append(changes, vault.Change{Name: previous, Delete: true})
		}
		if source.Protocol == protocolVNC && state.secrets.vault.Len() > 0 {
			names, err := state.secrets.vault.Names()
			if err != nil {
				return nil, state.hostPasswordSaveError(err)
			}
			prefix := hostSecretName(source.CatalogID, "vnc-system-password:")
			for _, saved := range names {
				if strings.HasPrefix(saved, prefix) {
					changes = append(changes, vault.Change{Name: saved, Delete: true})
				}
			}
		}
	}
	return changes, ""
}

// hostPassphraseChanges plans the passphrase typed for the key the host is
// pinned to. It is kept under the key, as the Keychain page keeps it, so every
// host that uses the key and the page itself see the same one. A passphrase that
// does not open the key is refused here, as there: it would only fail at the
// next connection.
func (state *State) hostPassphraseChanges() (changes []vault.Change, failure string) {
	typed := state.editor.passphrase.String()
	if typed == "" || !state.hostAsksKeyPassphrase() {
		return nil, ""
	}
	identity, ok := state.identityForFingerprint(state.editor.identity)
	if !ok {
		return nil, ""
	}
	if state.secrets.vault == nil {
		return nil, state.messages.VaultUnavailable
	}
	secret := []byte(typed)
	_, err := keyidentity.Load(identity.Path, secret)
	clear(secret)
	if err != nil {
		return nil, state.messages.KeyPassphraseWrong
	}
	return []vault.Change{{Name: passphraseSecretName(identity.Path), Value: typed}}, ""
}

func (state *State) appendStatusNote(note string) {
	if note != "" {
		state.status += " · " + note
	}
}

func (state *State) hostPasswordSaveError(err error) string {
	return fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
}

func (state *State) hostIndex(id uint32) int {
	for index := range state.hosts {
		if state.hosts[index].ID == id {
			return index
		}
	}
	return -1
}

func (state *State) openDeleteHosts() {
	state.captureHostActionIDs()
	state.openDeleteCapturedHosts()
}

func (state *State) openDeleteCapturedHosts() {
	if len(state.hostActionIDs) == 0 {
		return
	}
	if message := state.hostReferenceError(state.hostActionIDs); message != "" {
		state.showActionWarning(state.messages.DeleteHostTitle, message)
		return
	}
	state.dialogError = ""
	state.hostDialogText = fmt.Sprintf(state.messages.DeleteHostDescriptionFmt, len(state.hostActionIDs))
	state.modal = modalDeleteHost
}

func (state *State) hostReferenceError(ids []uint32) string {
	for _, id := range ids {
		host, ok := state.hostByID(id)
		if !ok {
			continue
		}
		for _, route := range state.proxies {
			if route.TargetID == id || route.JumpID == id {
				return fmt.Sprintf(state.messages.HostDeleteBlockedProxyFmt, host.Label)
			}
		}
		for _, forward := range state.forwards {
			if forward.matchesHost(host) && !state.endpointShared(host, ids...) {
				return fmt.Sprintf(state.messages.HostDeleteBlockedForwardFmt, host.Label)
			}
		}
		for _, session := range state.sessions {
			if session.HostID == id {
				return fmt.Sprintf(state.messages.HostDeleteBlockedSessionFmt, host.Label)
			}
		}
	}
	return ""
}

func (state *State) clickDeleteHostDialog(id gfx.ID) {
	switch id {
	case hitHostDeleteDismiss, hitHostDeleteClose, hitHostDeleteCancel:
		state.modal = modalNone
		state.dialogError = ""
	case hitHostDeleteConfirm:
		state.confirmDeleteHosts()
	}
}

func (state *State) confirmDeleteHosts() {
	if !state.flushNoteDraft() {
		return
	}
	if message := state.hostReferenceError(state.hostActionIDs); message != "" {
		state.dialogError = message
		return
	}
	// The host's secrets go with it: its password, its one-time code secret and
	// the password of its proxy.
	vaultFile := state.secrets.vault
	var changes []vault.Change
	if vaultFile != nil && vaultFile.Len() > 0 {
		var names []string
		if !vaultFile.NeedsConversion() {
			var err error
			names, err = vaultFile.Names()
			if err != nil {
				state.dialogError = state.hostPasswordSaveError(err)
				return
			}
		}
		for _, host := range state.hosts {
			if !containsHostID(state.hostActionIDs, host.ID) {
				continue
			}
			changes = append(changes,
				vault.Change{Name: hostSecretName(host.CatalogID, hostPasswordPart), Delete: true},
				vault.Change{Name: hostSecretName(host.CatalogID, hostOTPPart), Delete: true})
			prefix := hostSecretName(host.CatalogID, "vnc-system-password:")
			for _, name := range names {
				if strings.HasPrefix(name, prefix) {
					changes = append(changes, vault.Change{Name: name, Delete: true})
				}
			}
			if host.Proxy != nil && host.Proxy.kind != sshconn.ProxyNone && host.Proxy.user != "" {
				changes = append(changes, vault.Change{Name: hostProxyPasswordSecret(host.CatalogID, host.Proxy), Delete: true})
			}
		}
	}
	if len(changes) > 0 {
		if vaultFile.NeedsConversion() {
			state.dialogError = state.messages.VaultNeedsConversion
			return
		}
		if err := vaultFile.Apply(changes); err != nil {
			state.dialogError = state.hostPasswordSaveError(err)
			return
		}
	}
	for _, host := range state.hosts {
		if containsHostID(state.hostActionIDs, host.ID) {
			state.unlinkNoteHost(host.CatalogID)
		}
	}
	count := len(state.hostActionIDs)
	kept := state.hosts[:0]
	for _, host := range state.hosts {
		if !containsHostID(state.hostActionIDs, host.ID) {
			kept = append(kept, host)
		}
	}
	clear(state.hosts[len(kept):])
	state.hosts = kept
	state.hostSelections = state.hostSelections[:0]
	state.hostActionIDs = state.hostActionIDs[:0]
	state.modal = modalNone
	state.dialogError = ""
	state.hostDataChanged()
	state.status = fmt.Sprintf(state.messages.HostDeletedFmt, count)
	state.save()
}

func containsHostID(ids []uint32, id uint32) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func (state *State) clickMoveHostGroupDialog(id gfx.ID) {
	if state.groupActions.op == hostGroupRelocate {
		switch {
		case id == hitMoveGroupField:
			state.groupActions.parentOpen = !state.groupActions.parentOpen
			return
		case id == hitGroupParentDismiss:
			state.groupActions.parentOpen = false
			return
		case id == hitGroupParentRoot:
			state.hostGroupEditor = text.Editor{}
			state.groupActions.parentOpen = false
			return
		case id.Scope() == hitGroupParentItems:
			index := item(id)
			if index >= 0 && index < len(state.hostGroups) {
				state.hostGroupEditor = text.Editor{}
				state.hostGroupEditor.Insert(state.hostGroups[index].Name)
			}
			state.groupActions.parentOpen = false
			return
		}
	}
	switch id {
	case hitGroupDeleteParent, hitGroupDeleteUngroup:
		if state.groupActions.op == hostGroupDelete {
			state.groupActions.ungroup = id == hitGroupDeleteUngroup
		}
	case hitMoveGroupDismiss, hitMoveGroupClose, hitMoveGroupCancel:
		state.modal = modalNone
		state.dialogError = ""
	case hitMoveGroupSubmit:
		group := strings.TrimSpace(state.hostGroupEditor.String())
		switch state.groupActions.op {
		case hostGroupRelocate:
			if !state.moveHostGroup(state.groupActions.path, group) {
				return
			}
		case hostGroupDelete:
			if !state.deleteHostGroup(state.groupActions.path, state.groupActions.ungroup) {
				return
			}
		case hostGroupRename:
			if !state.renameHostGroup(state.groupActions.path, group) {
				return
			}
		case hostGroupCreateChild:
			if !state.createHostChildGroup(state.groupActions.path, group) {
				return
			}
		default:
			if group == "" {
				state.dialogError = state.messages.ErrGroupRequired
				return
			}
			if !validHostGroup(group) {
				state.dialogError = state.messages.ErrHostGroupInvalid
				return
			}
			state.applyHostGroup(group)
		}
		state.modal = modalNone
		state.dialogError = ""
	}
}

func (state *State) applyHostGroup(group string) {
	count := 0
	for index := range state.hosts {
		if !containsHostID(state.hostActionIDs, state.hosts[index].ID) {
			continue
		}
		state.hosts[index].Group = group
		count++
	}
	if count == 0 {
		return
	}
	state.hostDataChanged()
	label := group
	if label == "" {
		label = state.messages.HostGroupDefault
	}
	state.status = fmt.Sprintf(state.messages.HostMovedFmt, count, label)
	state.save()
}

func (state *State) hostDataChanged() {
	state.reconcileDashboard()
	state.sshSyncMark()
	state.visible = nil
	state.workspaceViewCacheID = 0
	state.refreshHostGroups()
	state.pruneHostSelections()
	selected, ok := state.hostByID(state.selectedHostID)
	if !ok {
		state.selectedHostID = 0
	} else if !state.inHostFilter(selected) {
		// The host just made or moved is not in the group on show: show them all
		// rather than have it vanish.
		state.hostFilterOn = false
		state.rebuildHostTreeRows()
	}
	state.syncVisible()
}

func (state *State) pruneHostSelections() {
	kept := state.hostSelections[:0]
	for _, id := range state.hostSelections {
		if host, ok := state.hostByID(id); ok && state.inHostFilter(host) {
			kept = append(kept, id)
		}
	}
	state.hostSelections = kept
	state.refreshHostSelection()
}

func (state *State) hostManagementKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	handled := gfx.EventResult{Handled: true, Repaint: true}
	if state.hostOrderMenu && event.Key == gfx.KeyEscape {
		state.hostOrderMenu = false
		return handled, true
	}
	if state.hostSort.enabled && state.activeSection == sectionHosts && state.modal == modalNone {
		move, result := state.hostSort.grid.Key(event)
		if result.Handled {
			if move.Valid() && state.applyHostSortMove(move) {
				state.hostRevealID = uint32(item(move.ID))
				state.save()
			}
			return result, true
		}
		if event.Key == gfx.KeyEscape && event.Modifiers == 0 && !event.Release && !event.Repeat {
			state.hostSort.grid.Cancel()
			state.hostSort.enabled = false
			return handled, true
		}
	}
	if state.groupActions.drag.pressed && event.Key == gfx.KeyEscape {
		state.groupActions.drag = hostGroupDrag{}
		return handled, true
	}
	if state.groupActions.open && event.Key == gfx.KeyEscape {
		state.groupActions.open = false
		return handled, true
	}
	if state.modal == modalDeleteHost {
		if event.Key == gfx.KeyEnter && event.Target == hitHostDeleteConfirm && !event.Repeat {
			state.confirmDeleteHosts()
			return handled, true
		}
		return gfx.EventResult{}, false
	}
	if state.modal == modalMoveHostGroup {
		if state.groupActions.parentOpen && event.Key == gfx.KeyEscape {
			state.groupActions.parentOpen = false
			return handled, true
		}
		if state.groupActions.op == hostGroupDelete && (event.Target == hitGroupDeleteParent || event.Target == hitGroupDeleteUngroup) {
			choices := state.groupDeleteChoices()
			if next, ok := components.RadioGroupKeyDown(event.Target, choices[:], components.DirectionLTR, event.Key); ok {
				state.groupActions.ungroup = next == hitGroupDeleteUngroup
				return gfx.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true}, true
			}
		}
		editable := state.groupActions.op == hostGroupMoveHosts || state.groupActions.op == hostGroupRename || state.groupActions.op == hostGroupCreateChild
		if event.Key == gfx.KeyEnter && event.Modifiers == 0 && !event.Release && (editable && event.Target == hitMoveGroupField || event.Target == hitMoveGroupSubmit) {
			if _, composing := state.hostGroupEditor.Composition(); event.Repeat || editable && composing {
				return handled, true
			}
			state.clickMoveHostGroupDialog(hitMoveGroupSubmit)
			return handled, true
		}
		return gfx.EventResult{}, false
	}
	if state.importMenu && event.Key == gfx.KeyEscape {
		state.importMenu = false
		return handled, true
	}
	if result, ok := state.hostChipMenuKey(event); ok {
		return result, true
	}
	if state.hostFilterMenu && event.Key == gfx.KeyEscape {
		state.hostFilterMenu = false
		return handled, true
	}
	if state.hostGroupMenu && event.Key == gfx.KeyEscape {
		state.hostGroupMenu = false
		return handled, true
	}
	if state.activeSection != sectionHosts || state.modal != modalNone || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeySpace && !event.Release && state.clickHostScopeSelection(event.Target) {
		return handled, true
	}
	hostID, checkbox := state.hostIDForSelectionHit(event.Target)
	if !checkbox {
		if host, ok := state.hostForRow(event.Target); ok {
			hostID = host.ID
		}
	}
	if hostID != 0 && event.Key == gfx.KeySpace {
		state.selectedHostID = hostID
		state.toggleHostSelection(hostID)
		return handled, true
	}
	if event.Key == gfx.KeyDelete && (hostID != 0 || len(state.hostSelections) > 0) {
		if hostID != 0 {
			state.selectedHostID = hostID
		}
		state.openDeleteHosts()
		return handled, true
	}
	return gfx.EventResult{}, false
}
