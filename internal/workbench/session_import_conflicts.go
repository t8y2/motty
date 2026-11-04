package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sessionimport"
)

// Importing sessions that a saved host already has. The plan cannot decide this
// by itself: writing over a host the user set up is their call, and so is
// keeping it. So the import stops before it writes anything, asks about one
// conflict at a time, and offers to answer for the rest at once — which is what
// makes importing the same configuration twice useful rather than a no-op: the
// passwords that failed to be saved the first time land on the second run.

// sessionImportConflictHits is the place of the controls this file paints.
var sessionImportConflictHits = tree.Feature("session-import-conflicts")

var (
	hitImportConflictOverwrite    = tree.ID(sessionImportConflictHits, "overwrite")
	hitImportConflictSkip         = tree.ID(sessionImportConflictHits, "skip")
	hitImportConflictOverwriteAll = tree.ID(sessionImportConflictHits, "overwrite-all")
	hitImportConflictSkipAll      = tree.ID(sessionImportConflictHits, "skip-all")
	hitImportConflictDismiss      = tree.ID(sessionImportConflictHits, "dismiss")
	hitImportConflictClose        = tree.ID(sessionImportConflictHits, "close")
)

// sessionImportRequest is one import waiting to be applied: the sessions it read,
// the lines that belong to the client that produced them, and what to say when
// nothing lands.
type sessionImportRequest struct {
	client   sessionimport.Client
	results  []sessionimport.Result
	warnings []string
	nothing  string
}

// sessionImportUI is an import stopped on its conflicts: the request it will
// apply once answered, the saved hosts it is asking about, the answers so far,
// and which one is on screen.
type sessionImportUI struct {
	request   sessionImportRequest
	conflicts []importConflict
	overwrite map[uint32]bool
	index     int
	// layout lays out the body of the dialog, so painting it keeps no frame of
	// its own.
	layout flex.View
}

// beginSessionImport plans an import and applies it, unless it meets hosts that
// are already saved: those are put to the user first, and nothing is written
// until they answer.
func (state *State) beginSessionImport(request sessionImportRequest) {
	plan := state.sessionImportPlan(nil, request.results...)
	if len(plan.conflicts) == 0 {
		state.continueSessionImport(request, plan)
		return
	}
	state.imports = sessionImportUI{
		request:   request,
		conflicts: plan.conflicts,
		overwrite: make(map[uint32]bool, len(plan.conflicts)),
	}
	state.modal = modalImportConflicts
}

// continueSessionImport takes a planned import the rest of the way: it asks
// about the passwords the client left readable, then stages and completes. A
// plan with nothing to write says so in the words the client's import chose.
func (state *State) continueSessionImport(request sessionImportRequest, plan hostImportPlan) {
	m := state.messages.Tools
	plan.warnings = append(plan.warnings, request.warnings...)
	if len(plan.hosts)+len(plan.identities)+len(plan.proxies)+len(plan.forwards)+len(plan.updates) == 0 && request.nothing != "" {
		state.status = request.nothing
		state.hostImportResult.set(state.messages, request.nothing, plan.warnings)
		state.modal = modalHostImportResult
		return
	}
	client := importClientName(request.client)
	summary := fmt.Sprintf(m.ImportSummaryFmt, len(plan.hosts), client, plan.duplicateHosts)
	if len(plan.updates) > 0 {
		summary = fmt.Sprintf(m.ImportSummaryOverwriteFmt, len(plan.hosts), client, len(plan.updates), plan.duplicateHosts)
	}
	passwords, conflicts := state.sessionImportPasswords(request.results, plan)
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

func (state *State) clickImportConflict(id ui.ID) {
	imported := &state.imports
	switch id {
	case hitImportConflictOverwrite:
		imported.overwrite[imported.conflicts[imported.index].existing.ID] = true
		state.advanceImportConflict()
	case hitImportConflictSkip:
		state.advanceImportConflict()
	case hitImportConflictOverwriteAll:
		for _, conflict := range imported.conflicts[imported.index:] {
			imported.overwrite[conflict.existing.ID] = true
		}
		state.finishImportConflicts()
	case hitImportConflictSkipAll, hitImportConflictDismiss, hitImportConflictClose:
		// Dismissing keeps everything that is already saved: doing nothing is
		// what a closed dialog promises.
		state.finishImportConflicts()
	}
}

func (state *State) advanceImportConflict() {
	state.imports.index++
	if state.imports.index >= len(state.imports.conflicts) {
		state.finishImportConflicts()
	}
}

func (state *State) finishImportConflicts() {
	request, overwrite := state.imports.request, state.imports.overwrite
	state.imports = sessionImportUI{}
	state.modal = modalNone
	state.continueSessionImport(request, state.sessionImportPlan(overwrite, request.results...))
}

// importConflictTarget is how the dialog names one conflict: the session's own
// label, then where it goes.
func importConflictTarget(conflict importConflict) string {
	imported := conflict.imported
	target := imported.Address
	if imported.Port != 0 && imported.Port != defaultPort {
		target = fmt.Sprintf("%s:%d", imported.Address, imported.Port)
	}
	if imported.User != "" {
		target = imported.User + "@" + target
	}
	label := strings.TrimSpace(imported.Label)
	if label == "" || label == target {
		return target
	}
	return label + " · " + target
}

func (state *State) paintImportConflictsDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages, state.Theme()
	imported := &state.imports
	conflict := imported.conflicts[imported.index]
	description := fmt.Sprintf(m.Tools.ImportConflictBodyFmt,
		importConflictTarget(conflict), imported.index+1, len(imported.conflicts))
	var wrapped [3]string
	lines := canvas.WrapLines(wrapped[:0], description, metrics.FontSizeM, ui.WeightRegular, 520, 3)
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitImportConflictDismiss, CloseID: hitImportConflictClose,
		InitialFocusID: hitImportConflictOverwrite,
		ContentSize:    ui.Size{Width: 560}, FitBody: true, BodyHeight: float32(len(lines))*21 - 1,
	})
	layout := dialog.Begin(m.Tools.ImportConflictTitle, "")
	f := imported.layout.Begin(canvas, theme, layout.Body)
	f.Paragraph(description, len(wrapped)).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(21)
	f.End()
	remaining := len(imported.conflicts) - imported.index
	dialog.Actions(layout,
		components.ButtonAction{ID: hitImportConflictOverwriteAll, Label: fmt.Sprintf(m.Tools.ImportConflictOverwriteAllFmt, remaining), Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitImportConflictSkipAll, Label: fmt.Sprintf(m.Tools.ImportConflictSkipAllFmt, remaining), Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitImportConflictSkip, Label: m.Tools.ImportConflictSkip, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitImportConflictOverwrite, Label: m.Tools.ImportConflictOverwrite},
	)
	dialog.End()
}
