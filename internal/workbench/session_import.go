package workbench

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sessionimport"
	"motty/internal/sshconfig"
	"motty/internal/vault"
	"motty/internal/workbench/icons"
)

// Importing the saved sessions of PuTTY, Xshell, MobaXterm, WindTerm, Electerm,
// Netcatty, SecureCRT, FinalShell, Xterminal, XPipe and Tabby. The files are read by
// internal/sessionimport; this file picks them, turns their sessions into hosts
// and shows what came of it in the same result dialog as an OpenSSH config.
// SecureCRT keeps its sessions as a tree of .ini files, and FinalShell,
// Xterminal and XPipe keep their data in a folder, so they ask for a folder
// instead of files.

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
	hitImportSecureCRT       = tree.ID(sessionImportHits, "import-secure-crt")
	hitImportFinalShell      = tree.ID(sessionImportHits, "import-final-shell")
	hitImportXterminal       = tree.ID(sessionImportHits, "import-xterminal")
	hitImportXPipe           = tree.ID(sessionImportHits, "import-xpipe")
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
	base       *configBase
	plan       hostImportPlan
	summary    string
	passwords  []sessionImportPassword
	conflicts  int
	eligible   int
	tabby      []sessionimport.Entry
	tabbyVault bool
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
	case sessionimport.SecureCRT:
		return "SecureCRT"
	case sessionimport.FinalShell:
		return "FinalShell"
	case sessionimport.Xterminal:
		return "Xterminal"
	case sessionimport.XPipe:
		return "XPipe"
	case sessionimport.Tabby:
		return "Tabby"
	}
	return "MobaXterm"
}

func (state *State) clickImportSessions(id gfx.ID) {
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
	case hitImportSecureCRT:
		state.importSessions(sessionimport.SecureCRT)
	case hitImportFinalShell:
		state.importSessions(sessionimport.FinalShell)
	case hitImportXterminal:
		state.importSessions(sessionimport.Xterminal)
	case hitImportXPipe:
		state.importSessions(sessionimport.XPipe)
	case hitImportTabby:
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
	if importClientUsesFolder(client) {
		state.importSessionFolder(client, options)
		return
	}
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

// importClientUsesFolder reports whether a client keeps its sessions in a
// folder whose inner layout becomes groups, so it is picked as a folder.
func importClientUsesFolder(client sessionimport.Client) bool {
	return client == sessionimport.SecureCRT || client == sessionimport.FinalShell ||
		client == sessionimport.Xterminal || client == sessionimport.XPipe
}

// importSessionFolder asks for the folder that holds a client's sessions. Each
// folder inside becomes a group, which picking individual files would lose.
func (state *State) importSessionFolder(client sessionimport.Client, options sessionimport.ParseOptions) {
	m := state.messages.Tools
	name := importClientName(client)
	title := fmt.Sprintf(m.ImportFolderPickerFmt, name)
	if client == sessionimport.FinalShell || client == sessionimport.Xterminal || client == sessionimport.XPipe {
		title = fmt.Sprintf(m.ImportConfigFolderPickerFmt, name)
	}
	folder, err := state.options.OpenDirectory(app.OpenDirectoryOptions{Title: title})
	if errors.Is(err, app.ErrCanceled) || err == nil && folder == "" {
		state.status = m.ImportCanceled
		return
	}
	if err != nil {
		state.showActionFailure(title, err, state.messages.ActionFeedback.FilePickerFailed)
		return
	}
	state.readSessionImport(client, []string{folder}, options)
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
		if utf8Err == nil && gbErr == nil && slices.Equal(utf8.Entries, gb.Entries) {
			state.readSessionImport(client, paths, sessionimport.ParseOptions{MobaEncoding: sessionimport.MobaUTF8})
			return
		}
		prompt := mobaEncodingPrompt{paths: append([]string(nil), paths...)}
		for index := 0; index < len(utf8.Entries) && index < len(gb.Entries); index++ {
			if utf8.Entries[index] != gb.Entries[index] {
				prompt.utf8, prompt.gb = mobaEncodingSample(utf8.Entries[index]), mobaEncodingSample(gb.Entries[index])
				break
			}
		}
		state.mobaEncoding = prompt
		state.modal = modalMobaEncoding
		return
	}
	if err != nil {
		fallback := fmt.Sprintf(state.messages.ActionFeedback.ImportSessionsFailedFmt, importClientName(client))
		if importClientUsesFolder(client) {
			fallback = fmt.Sprintf(state.messages.ActionFeedback.ImportFolderFailedFmt, importClientName(client))
		}
		state.showFileReadFailure(fmt.Sprintf(m.ImportPickerFmt, importClientName(client)), err, fallback)
		return
	}
	state.prepareSessionImport(client, result, false)
}

func (state *State) prepareSessionImport(client sessionimport.Client, result sessionimport.Result, tabbyVault bool) {
	m := state.messages.Tools
	plan := state.sessionImportPlan(result)
	if result.Keys > 0 {
		plan.warnings = append(plan.warnings, fmt.Sprintf(m.ImportWarningKeysFmt, result.Keys))
	}
	if result.Skipped > 0 {
		plan.warnings = append(plan.warnings, fmt.Sprintf(m.ImportWarningSkippedFmt, result.Skipped))
	}
	if sessionImportHasLoopback(result) {
		plan.warnings = append(plan.warnings, m.ImportWarningLoopback)
	}
	plan.warnings = append(plan.warnings, result.Warnings...)
	if result.PlainSecrets > 0 {
		plan.warnings = append(plan.warnings, fmt.Sprintf(m.ImportWarningCleartextFmt, result.PlainSecrets))
	} else if client != sessionimport.Tabby {
		plan.warnings = append(plan.warnings, m.ImportWarningPasswords)
	}
	summary := fmt.Sprintf(m.ImportSummaryFmt, len(plan.hosts), importClientName(client), plan.duplicateHosts)
	passwords, conflicts := state.sessionImportPasswords(result, plan)
	if conflicts > 0 {
		plan.warnings = append(plan.warnings, fmt.Sprintf(m.ImportPasswordConflictFmt, conflicts))
	}
	if tabbyVault || len(passwords) > 0 || client == sessionimport.Tabby && tabbyImportHasKeys(result.Entries) {
		state.sessionImport = sessionImportPreview{
			base: state.configBase, plan: plan, summary: summary, passwords: passwords, conflicts: conflicts,
		}
		if client == sessionimport.Tabby {
			state.sessionImport.tabby = result.Entries
			state.sessionImport.tabbyVault = tabbyVault
		}
		state.dialogError = ""
		if changes, err := state.sessionImportPasswordChanges(); err != nil {
			state.dialogError = state.hostPasswordSaveError(err)
		} else {
			state.sessionImport.eligible = len(changes)
		}
		state.modal = modalSessionImportPreview
		return
	}
	if err := state.stageSessionImport(plan, true); err != nil {
		state.modal = modalNone
		state.showActionWarning(m.ImportChooserTitle, fmt.Sprintf(m.ImportHostSaveFailedFmt, err))
		return
	}
	state.completeSessionImport(plan, summary, 0)
}

func mobaEncodingSample(entry sessionimport.Entry) string {
	label := entry.Name
	if entry.Group == "" {
		return label + " · " + entry.User + "@" + entry.Host
	}
	return entry.Group + " / " + label + " · " + entry.User + "@" + entry.Host
}

func (state *State) stageSessionImport(plan hostImportPlan, notify bool) error {
	oldHosts := state.hosts
	oldHostCount, oldIdentityCount, oldProxyCount := len(state.hosts), len(state.identities), len(state.proxies)
	oldHostID, oldIdentityID, oldProxyID := state.nextHostID, state.nextIdentityID, state.nextProxyID
	if len(plan.hostUpdates) > 0 {
		state.hosts = slices.Clone(state.hosts)
		for index := range state.hosts {
			if options, ok := plan.hostUpdates[state.hosts[index].ID]; ok {
				state.hosts[index].SSHOptions = options
			}
		}
	}
	state.hosts = append(state.hosts, plan.hosts...)
	state.identities = append(state.identities, plan.identities...)
	state.proxies = append(state.proxies, plan.proxies...)
	state.nextHostID, state.nextIdentityID, state.nextProxyID = plan.nextHostID, plan.nextIdentityID, plan.nextProxyID
	state.refreshHostGroups()
	if len(plan.hosts)+len(plan.hostUpdates)+len(plan.identities)+len(plan.proxies) > 0 {
		// The caller may hold the vault lock. A config conflict must return
		// without re-entering the vault. Other windows reload after commit.
		if err := state.saveWithResultModeRecovery(false, false, notify); err != nil {
			clear(state.hosts[oldHostCount:])
			state.hosts = oldHosts
			state.identities = state.identities[:oldIdentityCount]
			state.proxies = state.proxies[:oldProxyCount]
			state.nextHostID, state.nextIdentityID, state.nextProxyID = oldHostID, oldIdentityID, oldProxyID
			state.refreshHostGroups()
			return err
		}
	}
	return nil
}

func (state *State) completeSessionImport(plan hostImportPlan, summary string, passwordCount int) {
	if len(plan.hosts) > 0 {
		state.selectedHostID = plan.hosts[0].ID
	}
	state.hostDataChanged()
	state.refreshIdentityCount()
	state.refreshProxies()
	state.hostImportResult.set(state.messages, summary, plan.warnings)
	state.status = summary
	state.modal = modalHostImportResult
	state.sshSyncFlush()
	if len(plan.hosts)+len(plan.hostUpdates)+passwordCount > 0 {
		state.scheduleSync()
	}
}

func (state *State) clickMobaEncoding(id gfx.ID) {
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

// declareImportSessions declares the client chooser in the window page: one
// large outline button per client, the guide link at the footer's left and
// Cancel at its right.
func (state *State) declareImportSessions(c ui.C) {
	if state.modal != modalImportSessions {
		return
	}
	m := state.messages.Tools
	theme := *c.Theme()
	choices := [...]struct {
		id    gfx.ID
		label string
	}{
		{hitImportPuTTY, "PuTTY"},
		{hitImportXshell, "Xshell"},
		{hitImportMobaXterm, "MobaXterm"},
		{hitImportWindTerm, "WindTerm"},
		{hitImportElecterm, "Electerm"},
		{hitImportNetcatty, "Netcatty"},
		{hitImportSecureCRT, "SecureCRT"},
		{hitImportFinalShell, "FinalShell"},
		{hitImportXterminal, "Xterminal"},
		{hitImportXPipe, "XPipe"},
		{hitImportTabby, "Tabby"},
	}
	const columns = 2
	gap := theme.Metrics.GapItem
	rows := (len(choices) + columns - 1) / columns
	const width float32 = 480
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.ImportChooserTitle, Description: m.ImportChooserBody,
		Width: width, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitImportSessionsDismiss, CloseID: hitImportSessionsClose,
	})
	dialog.Body(func() {
		// The buttons' own Large height and the gap size the grid; the dialog
		// fits itself to the body rather than taking a height measured beforehand.
		ui.Column(c).Gap(gap).Children(func() {
			for row := 0; row < rows; row++ {
				ui.Row(c).Gap(gap).Children(func() {
					for column := 0; column < columns; column++ {
						index := row*columns + column
						if index >= len(choices) {
							ui.Spacer(c).Grow(1)
							continue
						}
						choice := choices[index]
						button := ui.Button(c, choice.label).Large().Outline().Basis(0).Grow(1).Shrink(0)
						if index == 0 {
							button = button.AutoFocus()
						}
						if button.Clicked() {
							c.After(func() { state.clickImportSessions(choice.id) })
						}
					}
				})
			}
		})
	})
	dialog.Footer(func() {
		if ui.Button(c, m.ImportGuide).Link().Icon(icons.BookOpen).Shrink(0).Clicked() {
			c.After(func() { state.clickImportSessions(hitImportGuide) })
		}
		ui.Spacer(c).Grow(1)
		if ui.Button(c, state.messages.Cancel).Secondary().Shrink(0).Clicked() {
			c.After(func() { state.clickImportSessions(hitImportSessionsCancel) })
		}
	})
	if dialog.Dismissed() {
		state.closeModal()
	}
}

// declareMobaEncoding declares the MobaXterm encoding question in the window
// page: the two sample lines and the three answers.
func (state *State) declareMobaEncoding(c ui.C) {
	if state.modal != modalMobaEncoding {
		return
	}
	m := state.messages.Tools
	const width float32 = 520
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.ImportEncodingTitle, Description: m.ImportEncodingBody,
		Width: width, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitMobaEncodingCancel, CloseID: hitMobaEncodingClose,
	})
	dialog.Body(func() {
		ui.Column(c).Height(52).Gap(6).Children(func() {
			ui.Text(c, fmt.Sprintf(m.ImportEncodingSampleUTF8Fmt, state.mobaEncoding.utf8)).
				FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(20).Ellipsis()
			ui.Text(c, fmt.Sprintf(m.ImportEncodingSampleGBFmt, state.mobaEncoding.gb)).
				FontSize(metrics.FontSizeS).Color(state.pal.muted).LineHeight(20).Ellipsis()
		})
	})
	dialog.Footer(func() {
		if ui.Button(c, state.messages.Cancel).Secondary().Shrink(0).Clicked() {
			c.After(func() { state.clickMobaEncoding(hitMobaEncodingCancel) })
		}
		if ui.Button(c, "UTF-8").Secondary().Shrink(0).AutoFocus().Clicked() {
			c.After(func() { state.clickMobaEncoding(hitMobaChooseUTF8) })
		}
		if ui.Button(c, "GB18030").Shrink(0).Clicked() {
			c.After(func() { state.clickMobaEncoding(hitMobaChooseGB18030) })
		}
	})
	if dialog.Dismissed() {
		state.clickMobaEncoding(hitMobaEncodingCancel)
	}
}

// declareSessionImportPreview declares the password preview in the window
// page: its error line and the three ways out.
func (state *State) declareSessionImportPreview(c ui.C) {
	if state.modal != modalSessionImportPreview {
		return
	}
	m := state.messages.Tools
	bodyHeight := float32(0)
	if state.dialogError != "" {
		bodyHeight = 24
	}
	const width float32 = 550
	description := fmt.Sprintf(m.ImportPasswordPreviewBodyFmt, len(state.sessionImport.plan.hosts), len(state.sessionImport.passwords), state.sessionImport.eligible)
	title, saveLabel := m.ImportPasswordPreviewTitle, m.ImportSavePasswords
	if state.sessionImport.tabby != nil {
		title, saveLabel = m.TabbyImportTitle, m.TabbyImportSave
		description = m.TabbyImportDescription
	}
	if state.sessionImport.tabbyVault {
		saveLabel = m.TabbyVaultSave
		description = fmt.Sprintf(m.TabbyVaultPreviewFmt, len(state.sessionImport.plan.hosts), state.sessionImport.eligible)
	}
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description,
		Width: width, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitImportPreviewCancel, CloseID: hitImportPreviewClose,
	})
	dialog.Body(func() {
		ui.Column(c).Height(bodyHeight).Children(func() {
			if state.dialogError != "" {
				ui.TextSelectionArea(c, &state.dialogErrorSelection).Children(func() {
					ui.Text(c, state.dialogError).FontSize(metrics.FontSizeS).Color(state.pal.danger).LineHeight(24)
				})
			}
		})
	})
	dialog.Footer(func() {
		if ui.Button(c, state.messages.Cancel).Secondary().Shrink(0).Clicked() {
			c.After(func() { state.clickSessionImportPreview(hitImportPreviewCancel) })
		}
		if ui.Button(c, m.ImportConnectionsOnly).Secondary().Shrink(0).AutoFocus().Clicked() {
			c.After(func() { state.clickSessionImportPreview(hitImportConnectionsOnly) })
		}
		if ui.Button(c, saveLabel).Shrink(0).Clicked() {
			c.After(func() { state.clickSessionImportPreview(hitImportSavePasswords) })
		}
	})
	if dialog.Dismissed() {
		state.clickSessionImportPreview(hitImportPreviewCancel)
	}
}

func (state *State) sessionImportPasswords(result sessionimport.Result, plan hostImportPlan) ([]sessionImportPassword, int) {
	hosts := make(map[sessionImportHostKey]Host, len(state.hosts)+len(plan.hosts))
	for _, host := range state.hosts {
		if host.isSSH() {
			hosts[sessionImportHostKeyFor(host)] = host
		}
	}
	for _, host := range plan.hosts {
		if host.isSSH() {
			hosts[sessionImportHostKeyFor(host)] = host
		}
	}
	byName := make(map[string]sessionImportPassword)
	conflicted := make(map[string]bool)
	for _, entry := range result.Entries {
		if entry.Protocol != sessionimport.SSH || entry.Password == "" {
			continue
		}
		host, ok := hosts[sessionImportHostKeyFor(Host{
			Label: entry.Name, Address: entry.Host, User: entry.User, Port: uint16(entry.Port),
		})]
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
	passwords := make([]sessionImportPassword, 0, len(byName))
	for _, candidate := range byName {
		passwords = append(passwords, candidate)
	}
	return passwords, len(conflicted)
}

// sessionImportHostKey is the identity of an imported SSH host as the user sees
// it: its name and destination. Two sessions at one address with different
// names are different hosts, and each keeps its own password.
type sessionImportHostKey struct {
	label      string
	connection hostConnection
	terminal   hostTerminal
	viaSSH     bool
}

func sessionImportHostKeyFor(host Host) sessionImportHostKey {
	return sessionImportHostKey{
		label:      strings.ToLower(strings.TrimSpace(host.Label)),
		connection: connectionKey(host.Address, host.User, host.Port),
		terminal:   host.Terminal,
		viaSSH:     host.ViaSSH,
	}
}

func (state *State) sessionImportPasswordChanges() ([]vault.Change, error) {
	return state.missingSessionPasswords(state.sessionImport.passwords)
}

func (state *State) missingSessionPasswords(passwords []sessionImportPassword) ([]vault.Change, error) {
	if state.secrets.vault == nil {
		return nil, vault.ErrUnavailable
	}
	changes := make([]vault.Change, 0, len(passwords))
	for _, candidate := range passwords {
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

func (state *State) clickSessionImportPreview(id gfx.ID) {
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
		// Another window or sync may have reloaded the catalog while the
		// dialog was open. Its runtime IDs and credential owners are now stale.
		if pending.base != state.configBase {
			state.dialogError = state.messages.Tools.ImportPreviewStale
			return
		}
		plan := pending.plan
		var changes []vault.Change
		if id == hitImportSavePasswords {
			var err error
			changes, err = state.sessionImportPasswordChanges()
			if err == nil && pending.tabby != nil {
				var extra []vault.Change
				extra, err = state.planTabbyKeys(pending.tabby, &plan)
				changes = append(changes, extra...)
			}
			if err != nil {
				state.dialogError = state.hostPasswordSaveError(err)
				return
			}
		}
		state.commitSessionImport(id, pending, plan, changes)
	}
}

func (state *State) commitSessionImport(id gfx.ID, pending sessionImportPreview, plan hostImportPlan, changes []vault.Change) {
	state.sessionImport = sessionImportPreview{}
	state.dialogError = ""
	before := state.lastSaved
	commit := func(count int) error {
		if id == hitImportSavePasswords {
			saved := state.messages.Tools.ImportPasswordsSavedFmt
			if pending.tabby != nil {
				saved = state.messages.Tools.TabbyImportSavedFmt
			}
			plan.warnings = append(plan.warnings, fmt.Sprintf(saved, count))
		}
		return state.stageSessionImport(plan, false)
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
		state.dialogError = fmt.Sprintf(state.messages.Tools.ImportHostSaveFailedFmt, state.vaultErrorText(err))
		if errors.Is(err, vault.ErrRollbackFailed) {
			state.dialogError += " " + state.messages.Tools.ImportHostSaveRollbackFailed
		}
		return
	}
	if pending.tabby != nil && applied > 0 {
		state.reloadStoredIdentities()
	}
	state.configurationSaved(changedSavedFields(before, state.lastSaved), false, false)
	state.completeSessionImport(plan, pending.summary, applied)
}

func (state *State) sessionImportPlan(result sessionimport.Result) hostImportPlan {
	builder := newHostImportBuilder(state, sshconfig.Result{})
	for _, entry := range result.Entries {
		if entry.Protocol != sessionimport.SSH {
			state.planStreamImport(&builder.plan, builder.usedIDs, entry)
			continue
		}
		port := uint16(entry.Port)
		if port == 0 {
			port = defaultPort
		}
		host := Host{
			ID: builder.plan.nextHostID, Label: entry.Name, Group: entry.Group,
			Address: entry.Host, User: entry.User, Port: port,
			LoginCommand: restoredLoginCommand(entry.Command),
		}
		if settings := entry.SSHSettings; settings != nil {
			switch settings.Auth {
			case "password", "keyboardInteractive":
				host.Auth = authPassword
			case "publicKey":
				host.Auth = authPrivateKey
			case "agent":
				host.Auth = authAgent
			}
			if len(settings.IdentityFiles) > 0 {
				host.SSHOptions = &sshconfig.ConnectionOptions{IdentityFiles: slices.Clone(settings.IdentityFiles)}
			}
		}
		host.finish()
		if state.hostIndexSameName(host) >= 0 || planHasSameHost(builder.plan, host) {
			builder.plan.duplicateHosts++
			builder.plan.warnings = append(builder.plan.warnings,
				fmt.Sprintf(state.messages.Tools.ImportWarningSkippedHostFmt, host.Label, hostTarget(host)))
			continue
		}
		host.CatalogID = freshCatalogID(builder.usedIDs)
		builder.plan.nextHostID++
		builder.plan.hosts = append(builder.plan.hosts, host)
	}
	return builder.plan
}

// planHasSameHost reports whether the hosts this import has already made
// include host. Sessions from another client are told apart by their name, so
// two saved entries at one address both arrive; only the same name and address
// is a repeat.
func planHasSameHost(plan hostImportPlan, host Host) bool {
	return slices.ContainsFunc(plan.hosts, func(planned Host) bool { return sameHost(planned, host) })
}

func (state *State) planStreamImport(plan *hostImportPlan, usedIDs map[string]struct{}, entry sessionimport.Entry) {
	host := Host{
		ID: plan.nextHostID, Label: entry.Name, Group: entry.Group,
		Address: entry.Host, Port: uint16(entry.Port), User: entry.User,
	}
	switch entry.Protocol {
	case sessionimport.Serial:
		if !host.applyTransport(savedTransport{Protocol: "serial", Line: entry.Line, Flow: entry.Flow}) {
			plan.duplicateHosts++ // a line that cannot be used is left out
			return
		}
	case sessionimport.Telnet:
		host.Protocol = protocolTelnet
	case sessionimport.VNC:
		host.Protocol = protocolVNC
	case sessionimport.RDP:
		host.Protocol = protocolRDP
	default:
		host.Protocol = protocolTelnet
	}
	host.normalizeEndpoint()
	host.finish()
	if state.hostIndexSameName(host) >= 0 || planHasSameHost(*plan, host) {
		plan.duplicateHosts++
		plan.warnings = append(plan.warnings,
			fmt.Sprintf(state.messages.Tools.ImportWarningSkippedHostFmt, host.Label, hostTarget(host)))
		return
	}
	host.CatalogID = freshCatalogID(usedIDs)
	plan.nextHostID++
	plan.hosts = append(plan.hosts, host)
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
