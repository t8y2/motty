package workbench

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/sessionimport"
	"motty/internal/sshconfig"
)

// Importing the saved sessions of PuTTY, Xshell, MobaXterm and WindTerm. The files are read
// by internal/sessionimport; this file picks them, turns their sessions into
// hosts and shows what came of it in the same result dialog as an OpenSSH config.

// sessionImportHits is the place of the controls this file paints.
var sessionImportHits = tree.Feature("session-import")

var (
	hitImportSessions        = tree.ID(sessionImportHits, "import-sessions")
	hitImportPuTTY           = tree.ID(sessionImportHits, "import-pu-tty")
	hitImportXshell          = tree.ID(sessionImportHits, "import-xshell")
	hitImportMobaXterm       = tree.ID(sessionImportHits, "import-moba-xterm")
	hitImportSessionsCancel  = tree.ID(sessionImportHits, "import-sessions-cancel")
	hitImportSessionsClose   = tree.ID(sessionImportHits, "import-sessions-close")
	hitImportSessionsDismiss = tree.ID(sessionImportHits, "import-sessions-dismiss")
	hitImportWindTerm        = tree.ID(sessionImportHits, "import-wind-term")
)

func importClientName(client sessionimport.Client) string {
	switch client {
	case sessionimport.PuTTY:
		return "PuTTY"
	case sessionimport.Xshell:
		return "Xshell"
	case sessionimport.WindTerm:
		return "WindTerm"
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
	case hitImportSessionsCancel, hitImportSessionsClose, hitImportSessionsDismiss:
		state.modal = modalNone
	}
}

// importSessions asks for the client's files and adds the hosts they hold.
func (state *State) importSessions(client sessionimport.Client) {
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
		state.status = fmt.Sprintf(m.ImportFailedFmt, err)
		return
	}
	result, err := sessionimport.Parse(client, paths)
	if err != nil {
		state.status = fmt.Sprintf(m.ImportFailedFmt, err)
		return
	}
	plan := state.sessionImportPlan(result)
	if result.Keys > 0 {
		plan.warnings = append(plan.warnings, fmt.Sprintf(m.ImportWarningKeysFmt, result.Keys))
	}
	if result.Skipped > 0 {
		plan.warnings = append(plan.warnings, fmt.Sprintf(m.ImportWarningSkippedFmt, result.Skipped))
	}
	plan.warnings = append(plan.warnings, result.Warnings...)
	plan.warnings = append(plan.warnings, m.ImportWarningPasswords)
	summary := fmt.Sprintf(m.ImportSummaryFmt, len(plan.hosts), importClientName(client), plan.duplicateHosts)
	state.applyHostImportSummary(plan, summary)
}

// sessionImportPlan turns sessions into new hosts, leaving out one whose address,
// user and port a saved host (or an earlier session) already has.
func (state *State) sessionImportPlan(result sessionimport.Result) hostImportPlan {
	builder := newHostImportBuilder(state, sshconfig.Result{})
	for _, entry := range result.Entries {
		if entry.Protocol != sessionimport.SSH {
			state.planStreamImport(&builder.plan, entry)
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
			ID: builder.plan.nextHostID, Label: entry.Name, Group: strings.ReplaceAll(entry.Group, "/", " / "),
			Address: entry.Host, User: entry.User, Port: port,
		}
		host.finish()
		builder.plan.nextHostID++
		builder.plan.hosts = append(builder.plan.hosts, host)
		builder.connections[key] = host
	}
	return builder.plan
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
	dialog.Actions(layout,
		components.ButtonAction{ID: hitImportSessionsCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
	)
	dialog.End()
}

// planStreamImport turns a Telnet or serial session into a host, unless the
// saved hosts or an earlier session already have that destination.
func (state *State) planStreamImport(plan *hostImportPlan, entry sessionimport.Entry) {
	host := Host{
		ID: plan.nextHostID, Label: entry.Name, Group: strings.ReplaceAll(entry.Group, "/", " / "),
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
