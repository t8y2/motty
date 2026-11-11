package workbench

import (
	"errors"
	"fmt"
	"slices"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sessionimport"
	"motty/internal/sshconfig"
	"motty/internal/vault"
	"motty/internal/workbench/icons"
)

// Importing the saved sessions of PuTTY, Xshell, MobaXterm, WindTerm, Electerm
// and Netcatty. The files are read by internal/sessionimport; this file picks them,
// turns their sessions into hosts and shows what came of it in the same result
// dialog as an OpenSSH config.

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
		state.showFileReadFailure(fmt.Sprintf(m.ImportPickerFmt, importClientName(client)), err, fmt.Sprintf(state.messages.ActionFeedback.ImportSessionsFailedFmt, importClientName(client)))
		return
	}
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
	} else {
		plan.warnings = append(plan.warnings, m.ImportWarningPasswords)
	}
	summary := fmt.Sprintf(m.ImportSummaryFmt, len(plan.hosts), importClientName(client), plan.duplicateHosts)
	passwords, conflicts := state.sessionImportPasswords(result, plan)
	if conflicts > 0 {
		plan.warnings = append(plan.warnings, fmt.Sprintf(m.ImportPasswordConflictFmt, conflicts))
	}
	if len(passwords) > 0 {
		state.sessionImport = sessionImportPreview{plan: plan, summary: summary, passwords: passwords, conflicts: conflicts}
		state.dialogError = ""
		if changes, err := state.sessionImportPasswordChanges(); err != nil {
			state.dialogError = state.hostPasswordSaveError(err)
		} else {
			state.sessionImport.eligible = len(changes)
		}
		state.modal = modalSessionImportPreview
		return
	}
	if err := state.stageSessionImport(plan); err != nil {
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

func (state *State) stageSessionImport(plan hostImportPlan) error {
	oldHostCount, oldIdentityCount, oldProxyCount := len(state.hosts), len(state.identities), len(state.proxies)
	oldHostID, oldIdentityID, oldProxyID := state.nextHostID, state.nextIdentityID, state.nextProxyID
	state.hosts = append(state.hosts, plan.hosts...)
	state.identities = append(state.identities, plan.identities...)
	state.proxies = append(state.proxies, plan.proxies...)
	state.nextHostID, state.nextIdentityID, state.nextProxyID = plan.nextHostID, plan.nextIdentityID, plan.nextProxyID
	state.refreshHostGroups()
	if len(plan.hosts)+len(plan.identities)+len(plan.proxies) > 0 {
		if err := state.saveWithResultMode(false); err != nil {
			clear(state.hosts[oldHostCount:])
			state.hosts = state.hosts[:oldHostCount]
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
	if len(plan.hosts)+passwordCount > 0 {
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
	}
	const columns = 2
	rowHeight := components.BeginButton(nil, theme, gfx.ID{}, gfx.Rect{}).Large().BoundsHeight()
	gap := theme.Metrics.GapItem
	rows := (len(choices) + columns - 1) / columns
	bodyHeight := float32(rows)*rowHeight + float32(rows-1)*gap
	const width float32 = 480
	viewport := gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
	layout := components.BeginDialog(c.Canvas(), theme, viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true,
	}).Layout(m.ImportChooserBody)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.ImportChooserTitle, Description: m.ImportChooserBody,
		Width: width, Height: layout.Content.Height + bodyHeight, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitImportSessionsDismiss, CloseID: hitImportSessionsClose,
	})
	dialog.Body(func() {
		cellWidth := (layout.Body.Width - gap*(columns-1)) / columns
		ui.Column(c).Fill().Gap(gap).Children(func() {
			for row := 0; row < rows; row++ {
				ui.Row(c).Height(rowHeight).Gap(gap).Children(func() {
					for column := 0; column < columns; column++ {
						index := row*columns + column
						if index >= len(choices) {
							ui.Spacer(c).Grow(1)
							continue
						}
						choice := choices[index]
						button := ui.Button(c, choice.label).Large().Outline().Width(cellWidth).Shrink(0)
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
	viewport := gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true,
	}).Layout(m.ImportEncodingBody)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.ImportEncodingTitle, Description: m.ImportEncodingBody,
		Width: width, Height: layout.Content.Height + 52, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitMobaEncodingCancel, CloseID: hitMobaEncodingClose,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Gap(6).Children(func() {
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
	viewport := gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true,
	}).Layout(description)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.ImportPasswordPreviewTitle, Description: description,
		Width: width, Height: layout.Content.Height + bodyHeight, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitImportPreviewCancel, CloseID: hitImportPreviewClose,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Children(func() {
			if state.dialogError != "" {
				ui.Text(c, state.dialogError).FontSize(metrics.FontSizeS).Color(state.pal.danger).LineHeight(24)
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
		if ui.Button(c, m.ImportSavePasswords).Shrink(0).Clicked() {
			c.After(func() { state.clickSessionImportPreview(hitImportSavePasswords) })
		}
	})
	if dialog.Dismissed() {
		state.clickSessionImportPreview(hitImportPreviewCancel)
	}
}

func (state *State) sessionImportPasswords(result sessionimport.Result, plan hostImportPlan) ([]sessionImportPassword, int) {
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
	for _, entry := range result.Entries {
		if entry.Protocol != sessionimport.SSH || entry.Password == "" {
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
		key := connectionKey(entry.Host, entry.User, port)
		if _, exists := builder.connections[key]; exists {
			builder.plan.duplicateHosts++
			continue
		}
		host := Host{
			ID: builder.plan.nextHostID, Label: entry.Name, Group: entry.Group,
			Address: entry.Host, User: entry.User, Port: port,
		}
		host.CatalogID = freshCatalogID(builder.usedIDs)
		host.finish()
		builder.plan.nextHostID++
		builder.plan.hosts = append(builder.plan.hosts, host)
		builder.connections[key] = host
	}
	return builder.plan
}

func (state *State) planStreamImport(plan *hostImportPlan, usedIDs map[string]struct{}, entry sessionimport.Entry) {
	host := Host{
		ID: plan.nextHostID, Label: entry.Name, Group: entry.Group,
		Address: entry.Host, Port: uint16(entry.Port),
	}
	if entry.Protocol == sessionimport.Serial {
		if !host.applyTransport(savedTransport{Protocol: "serial", Line: entry.Line, Flow: entry.Flow}) {
			plan.duplicateHosts++ // a line that cannot be used is left out
			return
		}
	} else {
		host.Protocol = protocolTelnet
	}
	host.normalizeEndpoint()
	host.CatalogID = freshCatalogID(usedIDs)
	host.finish()
	if state.hostIndexByEndpoint(host) >= 0 {
		plan.duplicateHosts++
		return
	}
	for _, planned := range plan.hosts {
		if sameEndpoint(planned, host) {
			plan.duplicateHosts++
			return
		}
	}
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
