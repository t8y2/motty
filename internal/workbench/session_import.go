package workbench

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/portforward"
	"motty/internal/sessionimport"
	"motty/internal/sshconfig"
	"motty/internal/sshconn"
	"motty/internal/vault"
	"motty/internal/workbench/icons"
)

// Importing the saved sessions of PuTTY, Xshell, MobaXterm, WindTerm, Electerm,
// Netcatty and Tabby. The files are read by internal/sessionimport; this file
// picks them, turns their sessions into hosts and shows what came of it in the
// same result dialog as an OpenSSH config.
//
// A session says more than where it goes: how it authenticated, the key it used,
// the host it went through, the tunnels it opened. Those are carried over too,
// except the secrets: a password or a key passphrase a client encrypted is
// written to Motty's own vault, and only once the user unlocked it.

// sessionImportHits is the place of the controls this file paints.
var sessionImportHits = tree.Feature("session-import")

var (
	hitImportSessions        = tree.ID(sessionImportHits, "import-sessions")
	hitImportPuTTY           = tree.ID(sessionImportHits, "import-pu-tty")
	hitImportXshell          = tree.ID(sessionImportHits, "import-xshell")
	hitImportMobaXterm       = tree.ID(sessionImportHits, "import-moba-xterm")
	hitMobaChooseUTF8        = tree.ID(sessionImportHits, "moba-choose-utf8")
	hitMobaChooseGB18030     = tree.ID(sessionImportHits, "moba-choose-gb18030")
	hitMobaEncodingCancel    = tree.ID(sessionImportHits, "moba-encoding-cancel")
	hitMobaEncodingClose     = tree.ID(sessionImportHits, "moba-encoding-close")
	hitImportSessionsCancel  = tree.ID(sessionImportHits, "import-sessions-cancel")
	hitImportSessionsClose   = tree.ID(sessionImportHits, "import-sessions-close")
	hitImportSessionsDismiss = tree.ID(sessionImportHits, "import-sessions-dismiss")
	hitImportWindTerm        = tree.ID(sessionImportHits, "import-wind-term")
	hitImportElecterm        = tree.ID(sessionImportHits, "import-electerm")
	hitImportNetcatty        = tree.ID(sessionImportHits, "import-netcatty")
	hitImportTabby           = tree.ID(sessionImportHits, "import-tabby")
	hitImportGuide           = tree.ID(sessionImportHits, "import-guide")
	hitImportPreviewCancel   = tree.ID(sessionImportHits, "import-preview-cancel")
	hitImportPreviewClose    = tree.ID(sessionImportHits, "import-preview-close")
	hitImportConnectionsOnly = tree.ID(sessionImportHits, "import-connections-only")
	hitImportSavePasswords   = tree.ID(sessionImportHits, "import-save-passwords")
)

type sessionImportPassword struct {
	host  Host
	value string
}

type sessionImportPreview struct {
	plan      hostImportPlan
	summary   string
	passwords []sessionImportPassword
	conflicts int
	eligible  int
}

type mobaEncodingPrompt struct {
	paths []string
	utf8  string
	gb    string
}

func importClientName(client sessionimport.Client) string {
	switch client {
	case sessionimport.PuTTY:
		return "PuTTY"
	case sessionimport.Xshell:
		return "Xshell"
	case sessionimport.WindTerm:
		return "WindTerm"
	case sessionimport.Electerm:
		return "Electerm"
	case sessionimport.Netcatty:
		return "Netcatty"
	case sessionimport.Tabby:
		return "Tabby"
	}
	return "MobaXterm"
}

func (state *State) clickImportSessions(id ui.ID) {
	switch id {
	case hitImportPuTTY:
		state.importSessions(sessionimport.PuTTY)
	case hitImportXshell:
		state.importSessions(sessionimport.Xshell)
	case hitImportMobaXterm:
		state.importSessions(sessionimport.MobaXterm)
	case hitImportWindTerm:
		state.importSessions(sessionimport.WindTerm)
	case hitImportElecterm:
		state.importSessions(sessionimport.Electerm)
	case hitImportNetcatty:
		state.importSessions(sessionimport.Netcatty)
	case hitImportTabby:
		// Tabby keeps its sessions and its secrets behind one passphrase, so it
		// asks for it before anything is added (see tabby_import.go).
		state.importTabby()
	case hitImportGuide:
		state.openExternal(state.messages.Tools.ImportGuideURL)
	case hitImportSessionsCancel, hitImportSessionsClose, hitImportSessionsDismiss:
		state.modal = modalNone
	}
}

// importSessions asks for the client's files and adds the hosts they hold.
func (state *State) importSessions(client sessionimport.Client) {
	state.importSessionsWithOptions(client, sessionimport.ParseOptions{})
}

func (state *State) importSessionsWithOptions(client sessionimport.Client, options sessionimport.ParseOptions) {
	m := state.messages.Tools
	state.modal = modalNone
	paths, err := state.options.OpenFiles(app.OpenFileOptions{
		Title: fmt.Sprintf(m.ImportPickerFmt, importClientName(client)), AllowMultiple: true,
	})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		state.status = m.ImportCanceled
		return
	}
	if err != nil {
		state.showActionFailure(fmt.Sprintf(m.ImportPickerFmt, importClientName(client)), err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	state.readSessionImport(client, paths, options)
}

func (state *State) readSessionImport(client sessionimport.Client, paths []string, options sessionimport.ParseOptions) {
	m := state.messages.Tools
	result, err := sessionimport.ParseWithOptions(client, paths, options)
	if errors.Is(err, sessionimport.ErrAmbiguousMobaEncoding) {
		if len(paths) > 1 {
			state.showActionWarning(m.ImportChooserTitle, m.ImportEncodingOneFile)
			return
		}
		utf8, utf8Err := sessionimport.ParseWithOptions(client, paths, sessionimport.ParseOptions{MobaEncoding: sessionimport.MobaUTF8})
		gb, gbErr := sessionimport.ParseWithOptions(client, paths, sessionimport.ParseOptions{MobaEncoding: sessionimport.MobaGB18030})
		// Bytes that read as both encodings are only a question for the person
		// when the imported sessions differ; a bookmark named "Café" is not.
		if utf8Err == nil && gbErr == nil && sameSessionEntries(utf8.Entries, gb.Entries) {
			state.readSessionImport(client, paths, sessionimport.ParseOptions{MobaEncoding: sessionimport.MobaUTF8})
			return
		}
		prompt := mobaEncodingPrompt{paths: append([]string(nil), paths...)}
		for index := 0; index < len(utf8.Entries) && index < len(gb.Entries); index++ {
			if !utf8.Entries[index].Same(gb.Entries[index]) {
				prompt.utf8, prompt.gb = mobaEncodingSample(utf8.Entries[index]), mobaEncodingSample(gb.Entries[index])
				break
			}
		}
		state.mobaEncoding = prompt
		state.modal = modalMobaEncoding
		return
	}
	if err != nil {
		state.showFileReadFailure(fmt.Sprintf(m.ImportPickerFmt, importClientName(client)), err, fmt.Sprintf(state.messages.ActionFeedback.ImportSessionsFailedFmt, importClientName(client)))
		return
	}
	var warnings []string
	if result.Keys > 0 {
		warnings = append(warnings, fmt.Sprintf(m.ImportWarningKeysFmt, result.Keys))
	}
	if result.Skipped > 0 {
		warnings = append(warnings, fmt.Sprintf(m.ImportWarningSkippedFmt, result.Skipped))
	}
	if sessionImportHasLoopback(result) {
		warnings = append(warnings, m.ImportWarningLoopback)
	}
	warnings = append(warnings, result.Warnings...)
	// A file that holds passwords or keys in the clear is a file to delete, not
	// one whose encryption scheme kept them back, so it is said in its own words.
	if result.PlainSecrets > 0 {
		warnings = append(warnings, fmt.Sprintf(m.ImportWarningCleartextFmt, result.PlainSecrets))
	} else {
		warnings = append(warnings, m.ImportWarningPasswords)
	}
	// A destination that is already saved is the user's call, so the import
	// asks before it writes anything (see session_import_conflicts.go). What is
	// left once that is answered goes through the password preview and the staged
	// commit below.
	state.beginSessionImport(sessionImportRequest{
		client: client, results: []sessionimport.Result{result}, warnings: warnings,
	})
}

// sameSessionEntries reports whether two readings of one file found the same
// sessions in the same order. Entry carries key material, so the two cannot be
// compared as values.
func sameSessionEntries(a, b []sessionimport.Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if !a[index].Same(b[index]) {
			return false
		}
	}
	return true
}

func mobaEncodingSample(entry sessionimport.Entry) string {
	label := entry.Name
	if entry.Group == "" {
		return label + " · " + entry.User + "@" + entry.Host
	}
	return entry.Group + " / " + label + " · " + entry.User + "@" + entry.Host
}

func (state *State) stageSessionImport(plan hostImportPlan) error {
	oldHostCount, oldIdentityCount, oldProxyCount := len(state.hosts), len(state.identities), len(state.proxies)
	oldForwardCount := len(state.forwards)
	oldHostID, oldIdentityID, oldProxyID := state.nextHostID, state.nextIdentityID, state.nextProxyID
	oldForwardID := state.nextForwardID
	// A host written over keeps its id, so the sessions and tunnels that point at
	// it keep pointing at it. What it was is kept, to put back if the save fails.
	replaced := make([]Host, len(plan.updates))
	found := make([]int, len(plan.updates))
	for index, updated := range plan.updates {
		found[index] = -1
		if at := state.hostIndex(updated.ID); at >= 0 {
			replaced[index], found[index] = state.hosts[at], at
			state.hosts[at] = updated
		}
	}
	state.hosts = append(state.hosts, plan.hosts...)
	state.identities = append(state.identities, plan.identities...)
	state.proxies = append(state.proxies, plan.proxies...)
	state.forwards = append(state.forwards, plan.forwards...)
	state.nextHostID, state.nextIdentityID, state.nextProxyID = plan.nextHostID, plan.nextIdentityID, plan.nextProxyID
	state.nextForwardID = plan.nextForwardID
	state.refreshHostGroups()
	if len(plan.hosts)+len(plan.identities)+len(plan.proxies)+len(plan.forwards)+len(plan.updates) > 0 {
		if err := state.saveWithResultMode(false); err != nil {
			for index, at := range found {
				if at >= 0 && at < len(state.hosts) {
					state.hosts[at] = replaced[index]
				}
			}
			clear(state.hosts[oldHostCount:])
			state.hosts = state.hosts[:oldHostCount]
			state.identities = state.identities[:oldIdentityCount]
			state.proxies = state.proxies[:oldProxyCount]
			clear(state.forwards[oldForwardCount:])
			state.forwards = state.forwards[:oldForwardCount]
			state.nextHostID, state.nextIdentityID, state.nextProxyID = oldHostID, oldIdentityID, oldProxyID
			state.nextForwardID = oldForwardID
			state.refreshHostGroups()
			return err
		}
	}
	return nil
}

func (state *State) completeSessionImport(plan hostImportPlan, summary string, passwordCount int) {
	// The secrets an unlocked client vault gave up are written here, after the
	// hosts are in place and outside the vault's own transaction: they belong to
	// the hosts that just landed, and asking the vault to take them while it holds
	// its lock for the passwords above would wait forever.
	if warning := state.writeImportedSecrets(plan.secrets); warning != "" {
		plan.warnings = append(plan.warnings, warning)
	}
	if len(plan.hosts) > 0 {
		state.selectedHostID = plan.hosts[0].ID
	}
	if len(plan.identities) > 0 {
		state.selectedIdentityID = plan.identities[len(plan.identities)-1].ID
	}
	if len(plan.proxies) > 0 {
		state.selectedProxyID = plan.proxies[len(plan.proxies)-1].ID
	}
	if len(plan.forwards) > 0 {
		state.selectedForwardID = plan.forwards[len(plan.forwards)-1].ID
	}
	state.hostDataChanged()
	state.refreshIdentityCount()
	state.refreshProxies()
	state.refreshForwards()
	state.hostImportResult.set(state.messages, summary, plan.warnings)
	state.status = summary
	state.modal = modalHostImportResult
	state.sshSyncFlush()
	if len(plan.hosts)+len(plan.updates)+passwordCount > 0 {
		state.scheduleSync()
	}
}

func (state *State) clickMobaEncoding(id ui.ID) {
	switch id {
	case hitMobaEncodingCancel, hitMobaEncodingClose:
		state.mobaEncoding = mobaEncodingPrompt{}
		state.modal = modalNone
	case hitMobaChooseUTF8, hitMobaChooseGB18030:
		paths := state.mobaEncoding.paths
		state.mobaEncoding = mobaEncodingPrompt{}
		state.modal = modalNone
		encoding := sessionimport.MobaUTF8
		if id == hitMobaChooseGB18030 {
			encoding = sessionimport.MobaGB18030
		}
		state.readSessionImport(sessionimport.MobaXterm, paths, sessionimport.ParseOptions{MobaEncoding: encoding})
	}
}

func (state *State) paintMobaEncoding(canvas *ui.Canvas, viewport ui.Rect) {
	m := state.messages.Tools
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitMobaEncodingCancel, CloseID: hitMobaEncodingClose, InitialFocusID: hitMobaChooseUTF8,
		ContentSize: ui.Size{Width: 520}, FitBody: true, BodyHeight: 52,
	})
	layout := dialog.Begin(m.ImportEncodingTitle, m.ImportEncodingBody)
	content := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
	content.Column().Fill().Gap(6).Children(func() {
		content.Text(fmt.Sprintf(m.ImportEncodingSampleUTF8Fmt, state.mobaEncoding.utf8)).FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(20).Ellipsis()
		content.Text(fmt.Sprintf(m.ImportEncodingSampleGBFmt, state.mobaEncoding.gb)).FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(20).Ellipsis()
	})
	content.End()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitMobaEncodingCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitMobaChooseUTF8, Label: "UTF-8", Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitMobaChooseGB18030, Label: "GB18030"},
	)
	dialog.End()
}

// sessionImportPasswords finds the readable passwords a client left, one per host
// they would be saved for. A host two entries disagree about is counted as a
// conflict and offered no password, since there is no way to tell which is meant.
func (state *State) sessionImportPasswords(results []sessionimport.Result, plan hostImportPlan) ([]sessionImportPassword, int) {
	hosts := make(map[hostConnection]Host, len(state.hosts)+len(plan.hosts))
	for _, host := range state.hosts {
		if host.isSSH() {
			hosts[connectionKey(host.Address, host.User, host.Port)] = host
		}
	}
	for _, host := range plan.hosts {
		if host.isSSH() {
			hosts[connectionKey(host.Address, host.User, host.Port)] = host
		}
	}
	byName := make(map[string]sessionImportPassword)
	conflicted := make(map[string]bool)
	for _, result := range results {
		for _, entry := range result.Entries {
			// A password that came out of a vault is already on its way with the
			// host, and asking about it again would be asking twice.
			if entry.Protocol != sessionimport.SSH || entry.Password == "" || entry.PasswordFromVault {
				continue
			}
			host, ok := hosts[connectionKey(entry.Host, entry.User, uint16(entry.Port))]
			if !ok {
				continue
			}
			name, eligible := hostPasswordSecretName(host)
			if !eligible || conflicted[name] {
				continue
			}
			if previous, seen := byName[name]; seen && previous.value != entry.Password {
				delete(byName, name)
				conflicted[name] = true
				continue
			}
			byName[name] = sessionImportPassword{host: host, value: entry.Password}
		}
	}
	passwords := make([]sessionImportPassword, 0, len(byName))
	for _, candidate := range byName {
		passwords = append(passwords, candidate)
	}
	return passwords, len(conflicted)
}

func (state *State) sessionImportPasswordChanges() ([]vault.Change, error) {
	if state.secrets.vault == nil {
		return nil, vault.ErrUnavailable
	}
	changes := make([]vault.Change, 0, len(state.sessionImport.passwords))
	for _, candidate := range state.sessionImport.passwords {
		name, ok := hostPasswordSecretName(candidate.host)
		if !ok {
			continue
		}
		_, found, err := state.secrets.vault.Get(name)
		if err != nil {
			return nil, err
		}
		if !found {
			changes = append(changes, vault.Change{Name: name, Value: candidate.value})
		}
	}
	return changes, nil
}

func (state *State) clickSessionImportPreview(id ui.ID) {
	switch id {
	case hitImportPreviewCancel, hitImportPreviewClose:
		state.sessionImport = sessionImportPreview{}
		state.dialogError = ""
		state.modal = modalNone
	case hitImportConnectionsOnly, hitImportSavePasswords:
		if state.saveBlocked || state.config == nil || state.pauseSave > 0 {
			state.dialogError = state.messages.Tools.ImportHostSaveUnavailable
			return
		}
		pending := state.sessionImport
		plan := pending.plan
		var changes []vault.Change
		if id == hitImportSavePasswords {
			var err error
			changes, err = state.sessionImportPasswordChanges()
			if err != nil {
				state.dialogError = state.hostPasswordSaveError(err)
				return
			}
		}
		state.sessionImport = sessionImportPreview{}
		state.dialogError = ""
		commit := func(count int) error {
			if id == hitImportSavePasswords {
				plan.warnings = append(plan.warnings, fmt.Sprintf(state.messages.Tools.ImportPasswordsSavedFmt, count))
			}
			return state.stageSessionImport(plan)
		}
		var err error
		applied := 0
		if len(changes) > 0 {
			applied, err = state.secrets.vault.ApplyMissingWithCommit(changes, commit)
		} else {
			err = commit(0)
		}
		if err != nil {
			state.sessionImport = pending
			state.modal = modalSessionImportPreview
			state.dialogError = fmt.Sprintf(state.messages.Tools.ImportHostSaveFailedFmt, err)
			if errors.Is(err, vault.ErrRollbackFailed) {
				state.dialogError += " " + state.messages.Tools.ImportHostSaveRollbackFailed
			}
			return
		}
		state.completeSessionImport(plan, pending.summary, applied)
	}
}

func (state *State) paintSessionImportPreview(canvas *ui.Canvas, viewport ui.Rect) {
	m := state.messages.Tools
	bodyHeight := float32(0)
	if state.dialogError != "" {
		bodyHeight = 24
	}
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitImportPreviewCancel, CloseID: hitImportPreviewClose, InitialFocusID: hitImportConnectionsOnly,
		ContentSize: ui.Size{Width: 550}, FitBody: true, BodyHeight: bodyHeight,
	})
	layout := dialog.Begin(m.ImportPasswordPreviewTitle,
		fmt.Sprintf(m.ImportPasswordPreviewBodyFmt, len(state.sessionImport.plan.hosts), len(state.sessionImport.passwords), state.sessionImport.eligible))
	if state.dialogError != "" {
		content := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
		content.Column().Fill().Children(func() {
			content.Text(state.dialogError).FontSize(metrics.FontSizeS).Color(state.pal.danger).LineHeight(24)
		})
		content.End()
	}
	dialog.Actions(layout,
		components.ButtonAction{ID: hitImportPreviewCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitImportConnectionsOnly, Label: m.ImportConnectionsOnly, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitImportSavePasswords, Label: m.ImportSavePasswords},
	)
	dialog.End()
}

// sessionImportPlan turns sessions into new hosts, leaving out one whose address,
// user and port a saved host (or an earlier session) already has, and carrying
// over what the client saved beside the destination: the way it authenticated,
// the key it used, the jump route it went through, the tunnels it opened, and the
// secrets the user unlocked. overwrite names the saved hosts the user chose to
// write over; nil asks the question instead of answering it (see
// session_import_conflicts.go).
func (state *State) sessionImportPlan(overwrite map[uint32]bool, results ...sessionimport.Result) hostImportPlan {
	builder := newHostImportBuilder(state, sshconfig.Result{})
	builder.overwrite = overwrite
	for _, result := range results {
		state.planSessionResult(builder, result)
	}
	return builder.plan
}

// planSessionResult adds one file's sessions to the plan, then the routes and
// tunnels that name them.
func (state *State) planSessionResult(builder *hostImportBuilder, result sessionimport.Result) {
	// landed is the host each session became, by its index in the file: a route
	// and a tunnel name their session that way, and a session that was left out
	// has none, so what names it is dropped rather than pointed at a neighbour.
	landed := make([]Host, len(result.Entries))
	found := make([]bool, len(result.Entries))
	for index, entry := range result.Entries {
		host, identity, ok := state.planSessionHost(builder, entry)
		if !ok {
			continue
		}
		landed[index], found[index] = host, true
		// A password the user unlocked by typing its vault's passphrase is
		// written with the host. One a file kept in the clear is only a
		// candidate: continueSessionImport puts it to the user first.
		if entry.Password != "" && entry.PasswordFromVault {
			builder.addSecret(hostSecretName(host.CatalogID, hostPasswordPart), entry.Password)
		}
		if entry.KeyPassphrase != "" && identity != "" {
			builder.addSecret(passphraseSecretName(identity), entry.KeyPassphrase)
		}
	}
	planSessionRoutes(builder, result, landed, found)
	planSessionForwards(builder, result, landed, found)
}

// planSessionHost adds one session to the plan, and answers the host it became
// and the key file its authentication uses, if the client saved one.
func (state *State) planSessionHost(builder *hostImportBuilder, entry sessionimport.Entry) (Host, string, bool) {
	if entry.Protocol != sessionimport.SSH {
		host, ok := state.planStreamImport(&builder.plan, entry)
		return host, "", ok
	}
	port := uint16(entry.Port)
	if port == 0 {
		port = defaultPort
	}
	key := connectionKey(entry.Host, entry.User, port)
	if existing, exists := builder.connections[key]; exists {
		return state.planSessionConflict(builder, entry, existing, port)
	}
	identity, hasKey := state.planSessionIdentity(builder, entry)
	host := Host{
		ID: builder.plan.nextHostID, Label: entry.Name, Group: strings.ReplaceAll(entry.Group, "/", " / "),
		Address: entry.Host, User: entry.User, Port: port, Color: entry.Color,
		Auth: importedAuth(entry.Auth), ForwardX11: entry.X11, AgentForward: entry.AgentForward,
		Proxy: importedSessionProxy(builder, entry),
	}
	// Naming the key is what makes the session offer that key and no other; a key
	// whose fingerprint cannot be read is still added, and the mode then tries
	// the imported keys as it does for a host that names none.
	if hasKey && identity.Fingerprint != "" {
		host.Identity = identity.Fingerprint
	}
	host.finish()
	builder.plan.nextHostID++
	builder.plan.hosts = append(builder.plan.hosts, host)
	builder.connections[key] = host
	if hasKey {
		return host, identity.Path, true
	}
	return host, "", true
}

// planSessionConflict decides what becomes of a session whose destination a host
// already has. The same file naming one destination twice is a duplicate and
// stays silent; a host that is already saved is a question, so the first run of
// the plan records it, and the run that has the answer either writes over the
// saved host or leaves it alone.
func (state *State) planSessionConflict(builder *hostImportBuilder, entry sessionimport.Entry, existing Host, port uint16) (Host, string, bool) {
	if _, saved := builder.savedHosts[existing.ID]; !saved {
		builder.plan.duplicateHosts++
		return Host{}, "", false
	}
	if builder.overwrite == nil {
		// One question per saved host: a file that names it twice is answered
		// once, and the second entry would land on the same host anyway.
		for _, conflict := range builder.plan.conflicts {
			if conflict.existing.ID == existing.ID {
				return Host{}, "", false
			}
		}
		builder.plan.conflicts = append(builder.plan.conflicts, importConflict{
			imported: Host{
				Label: entry.Name, Group: strings.ReplaceAll(entry.Group, "/", " / "),
				Address: entry.Host, User: entry.User, Port: port,
			},
			existing: existing,
		})
		return Host{}, "", false
	}
	if !builder.overwrite[existing.ID] {
		builder.plan.duplicateHosts++
		return Host{}, "", false
	}
	identity, hasKey := state.planSessionIdentity(builder, entry)
	updated := existing
	updated.Label = entry.Name
	updated.Group = strings.ReplaceAll(entry.Group, "/", " / ")
	updated.Color = entry.Color
	updated.Auth = importedAuth(entry.Auth)
	updated.ForwardX11 = entry.X11
	updated.AgentForward = entry.AgentForward
	updated.Proxy = importedSessionProxy(builder, entry)
	// A key the import could not read leaves the saved one in place: pointing
	// the host at nothing would be worse than what it already had.
	if hasKey && identity.Fingerprint != "" {
		updated.Identity = identity.Fingerprint
	}
	updated.finish()
	builder.plan.updates = append(builder.plan.updates, updated)
	builder.connections[connectionKey(updated.Address, updated.User, updated.Port)] = updated
	if hasKey {
		return updated, identity.Path, true
	}
	return updated, "", true
}

// planSessionIdentity finds the key a session authenticates with: the one the
// client kept inside its own vault, written out beside the state file so it can
// be used like any other key, or the file it names.
func (state *State) planSessionIdentity(builder *hostImportBuilder, entry sessionimport.Entry) (identityRecord, bool) {
	path := entry.KeyPath
	if len(entry.KeyData) != 0 {
		written, err := state.writeImportedKey(entry.KeyID, entry.KeyData)
		if err != nil {
			builder.warn(fmt.Sprintf(builder.state.messages.Tools.ImportWarningKeyWriteFmt, entry.Name, err))
			return identityRecord{}, false
		}
		path = written
	}
	if path == "" {
		return identityRecord{}, false
	}
	return builder.ensureIdentity(path)
}

// importedAuth is the authentication a session asked for. A client that names
// none gets Motty's automatic mode, which is the same "ask the server".
func importedAuth(auth sessionimport.Auth) hostAuth {
	switch auth {
	case sessionimport.AuthPassword:
		return authPassword
	case sessionimport.AuthPrivateKey:
		return authPrivateKey
	case sessionimport.AuthAgent:
		return authAgent
	}
	return authAutomatic
}

// importedSessionProxy is the route a session saved for reaching it on its own:
// a proxy command, a SOCKS5 proxy or an HTTP CONNECT proxy, in that order, which
// is the order the client itself tries them in. A proxy command would run here,
// so it waits for the user to confirm it in the host's proxy dialog, the way an
// imported SSH config's does; one this program cannot run is left out and said.
func importedSessionProxy(builder *hostImportBuilder, entry sessionimport.Entry) *outboundProxy {
	m := builder.state.messages.Tools
	switch {
	case entry.ProxyCommand != "":
		if sshconn.CheckProxyCommand(entry.ProxyCommand) != nil {
			builder.warn(fmt.Sprintf(m.ImportWarningProxyCommandFmt, entry.Name))
			return nil
		}
		builder.warn(fmt.Sprintf(m.ImportProxyCommandFmt, entry.Name))
		return &outboundProxy{kind: sshconn.ProxyCommand, address: entry.ProxyCommand, unconfirmed: true}
	case entry.ProxySOCKS5 != "":
		return &outboundProxy{kind: sshconn.ProxySOCKS5, address: entry.ProxySOCKS5}
	case entry.ProxyHTTP != "":
		return &outboundProxy{kind: sshconn.ProxyHTTPConnect, address: entry.ProxyHTTP}
	}
	return nil
}

// planSessionRoutes adds the jump routes the sessions saved. One is dropped, and
// counted, when one of its two ends was not imported or when it would close a
// loop: a route through the wrong server is worse than none.
func planSessionRoutes(builder *hostImportBuilder, result sessionimport.Result, landed []Host, found []bool) {
	routes := make(map[uint32]uint32, len(builder.plan.proxies)+len(result.Jumps))
	for _, route := range builder.plan.proxies {
		routes[route.TargetID] = route.JumpID
	}
	dropped := 0
	for _, jump := range result.Jumps {
		if !found[jump.Target] || !found[jump.Via] {
			dropped++
			continue
		}
		target, via := landed[jump.Target].ID, landed[jump.Via].ID
		if configured, exists := routes[target]; exists {
			if configured != via {
				dropped++
			}
			continue
		}
		if target == via || proxyRouteWouldCycle(routes, target, via) {
			dropped++
			continue
		}
		routes[target] = via
		builder.plan.proxies = append(builder.plan.proxies, proxyRecord{
			ID: builder.plan.nextProxyID, TargetID: target, JumpID: via,
		})
		builder.plan.nextProxyID++
	}
	if dropped > 0 {
		builder.warn(fmt.Sprintf(builder.state.messages.Tools.ImportWarningRouteFmt, dropped))
	}
}

// planSessionForwards adds the tunnels the sessions saved. One is dropped, and
// counted, when its session was not imported, when it names no destination, or
// when the port it listens on is already taken: Motty's tunnels are global, and
// two clients' sessions often reuse the same port.
func planSessionForwards(builder *hostImportBuilder, result sessionimport.Result, landed []Host, found []bool) {
	taken := make(map[uint16]struct{}, len(builder.state.forwards)+len(result.Forwards))
	for _, forward := range builder.state.forwards {
		if forward.Mode != portforward.ModeRemote {
			taken[forward.LocalPort] = struct{}{}
		}
	}
	for _, forward := range builder.plan.forwards {
		if forward.Mode != portforward.ModeRemote {
			taken[forward.LocalPort] = struct{}{}
		}
	}
	dropped := 0
	for _, forward := range result.Forwards {
		if !found[forward.Entry] || forward.LocalPort < 1 || forward.LocalPort > 65535 {
			dropped++
			continue
		}
		host := landed[forward.Entry]
		if !host.isSSH() {
			dropped++
			continue
		}
		mode := importedForwardMode(forward.Mode)
		localPort := uint16(forward.LocalPort)
		if mode != portforward.ModeRemote {
			if _, used := taken[localPort]; used {
				dropped++
				continue
			}
		}
		record := forwardRecord{
			ID: builder.plan.nextForwardID, Mode: mode, Label: builder.forwardLabel(host, localPort),
			HostLabel: host.Label, HostAddress: host.Address, HostUser: host.User, HostPort: host.Port,
			LocalPort: localPort,
		}
		if mode != portforward.ModeSOCKS5 {
			if forward.DestinationHost == "" || forward.DestinationPort < 1 || forward.DestinationPort > 65535 {
				dropped++
				continue
			}
			record.DestinationHost = forward.DestinationHost
			record.DestinationPort = uint16(forward.DestinationPort)
		}
		if mode != portforward.ModeRemote {
			taken[localPort] = struct{}{}
		}
		builder.plan.nextForwardID++
		builder.plan.forwards = append(builder.plan.forwards, record)
	}
	if dropped > 0 {
		builder.warn(fmt.Sprintf(builder.state.messages.Tools.ImportWarningForwardFmt, dropped))
	}
}

// importedForwardMode is the kind of tunnel Motty opens for one a client saved.
func importedForwardMode(mode sessionimport.ForwardMode) portforward.Mode {
	switch mode {
	case sessionimport.ForwardRemote:
		return portforward.ModeRemote
	case sessionimport.ForwardSOCKS:
		return portforward.ModeSOCKS5
	}
	return portforward.ModeLocal
}

// forwardLabel names an imported tunnel after the host it goes through and the
// port it listens on, which is what tells two of them apart in the table.
func (builder *hostImportBuilder) forwardLabel(host Host, port uint16) string {
	base := fmt.Sprintf("%s %d", host.Label, port)
	label := base
	for suffix := 2; builder.forwardLabelTaken(label); suffix++ {
		label = fmt.Sprintf("%s (%d)", base, suffix)
	}
	return label
}

func (builder *hostImportBuilder) forwardLabelTaken(label string) bool {
	for _, forward := range builder.state.forwards {
		if strings.EqualFold(forward.Label, label) {
			return true
		}
	}
	for _, forward := range builder.plan.forwards {
		if strings.EqualFold(forward.Label, label) {
			return true
		}
	}
	return false
}

// writeImportedSecrets saves what an import unlocked in the vault, in one write
// so a vault that cannot take one secret takes none. It answers the line that
// tells the user when that failed, and "" when it did not.
func (state *State) writeImportedSecrets(changes []vault.Change) string {
	if len(changes) == 0 {
		return ""
	}
	if state.secrets.vault == nil {
		if state.secrets.denied {
			return state.messages.Tools.ImportWarningSecretsDenied
		}
		return state.messages.Tools.ImportWarningSecretsMissing
	}
	if err := state.secrets.vault.Apply(changes); err != nil {
		if errors.Is(err, vault.ErrLocked) {
			// The master password is the user's own; say where to type it rather
			// than the prompt's own wording, which points at a session.
			return state.messages.Tools.ImportWarningSecretsLocked
		}
		// A vault that could be opened still needs the system's store to keep
		// its key the first time a secret is written, and that is where the user
		// may have answered the keychain prompt with Deny.
		if errors.Is(err, vault.ErrKeystoreDenied) {
			return state.messages.Tools.ImportWarningSecretsDenied
		}
		return fmt.Sprintf(state.messages.Tools.ImportWarningSecretsFmt, state.vaultErrorText(err))
	}
	return ""
}

// planStreamImport turns a Telnet or serial session into a host, unless the
// saved hosts or an earlier session already have that destination.
func (state *State) planStreamImport(plan *hostImportPlan, entry sessionimport.Entry) (Host, bool) {
	host := Host{
		ID: plan.nextHostID, Label: entry.Name, Group: strings.ReplaceAll(entry.Group, "/", " / "),
		Address: entry.Host, Port: uint16(entry.Port), Color: entry.Color,
	}
	if entry.Protocol == sessionimport.Serial {
		if !host.applyTransport(savedTransport{Protocol: "serial", Line: entry.Line, Flow: entry.Flow}) {
			plan.duplicateHosts++ // a line that cannot be used is left out
			return Host{}, false
		}
	} else {
		host.Protocol = protocolTelnet
	}
	host.normalizeEndpoint()
	host.finish()
	if state.hostIndexByEndpoint(host) >= 0 {
		plan.duplicateHosts++
		return Host{}, false
	}
	for _, planned := range plan.hosts {
		if sameEndpoint(planned, host) {
			plan.duplicateHosts++
			return Host{}, false
		}
	}
	plan.nextHostID++
	plan.hosts = append(plan.hosts, host)
	return host, true
}

func (state *State) paintImportSessionsDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m := state.messages.Tools
	theme := state.Theme()
	choices := [...]struct {
		id    ui.ID
		label string
	}{
		{hitImportPuTTY, "PuTTY"},
		{hitImportXshell, "Xshell"},
		{hitImportMobaXterm, "MobaXterm"},
		{hitImportWindTerm, "WindTerm"},
		{hitImportElecterm, "Electerm"},
		{hitImportNetcatty, "Netcatty"},
		{hitImportTabby, "Tabby"},
	}
	const columns = 2
	rowHeight := components.BeginButton(nil, theme, ui.ID{}, ui.Rect{}).Large().BoundsHeight()
	gap := theme.Metrics.GapItem
	rows := float32((len(choices) + columns - 1) / columns)

	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitImportSessionsDismiss, CloseID: hitImportSessionsClose, InitialFocusID: hitImportPuTTY,
		ContentSize: ui.Size{Width: 480}, FitBody: true,
		BodyHeight: rows*rowHeight + (rows-1)*gap,
	})
	layout := dialog.Begin(m.ImportChooserTitle, m.ImportChooserBody)
	cellWidth := (layout.Body.Width - gap*(columns-1)) / columns
	for index, choice := range choices {
		cell := ui.Rect{
			X:     layout.Body.X + float32(index%columns)*(cellWidth+gap),
			Y:     layout.Body.Y + float32(index/columns)*(rowHeight+gap),
			Width: cellWidth,
		}
		components.BeginButton(canvas, theme, choice.id, cell).Large().Outline(choice.label)
	}
	// Where each client keeps what this dialog reads is told in the docs, which
	// the link opens; it sits at the footer's left, across from Cancel.
	components.BeginButton(canvas, theme, hitImportGuide, ui.Rect{X: layout.Footer.X, Y: layout.Footer.Y}).
		WithIcon(icons.BookOpen).SizeToFit(m.ImportGuide).Link(m.ImportGuide)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitImportSessionsCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
	)
	dialog.End()
}

// sessionImportHasLoopback reports a session aimed at this computer, which is
// usually a one-shot bastion tunnel rather than a host worth keeping.
func sessionImportHasLoopback(result sessionimport.Result) bool {
	for _, entry := range result.Entries {
		if entry.Protocol == sessionimport.SSH && sessionimport.IsLoopbackHost(entry.Host) {
			return true
		}
	}
	return false
}
