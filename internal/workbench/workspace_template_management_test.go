package workbench

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
)

func TestWorkspaceTemplateMenuOpensFromPointerAndKeyboard(t *testing.T) {
	state := templateManagementState(t, 2)

	for _, test := range []struct {
		name string
		key  gfx.Key
	}{
		{name: "Enter", key: gfx.KeyEnter},
		{name: "Space", key: gfx.KeySpace},
	} {
		t.Run(test.name, func(t *testing.T) {
			state.workspaceTemplateMenu = workspaceTemplateMenu{}
			result := state.Key(gfx.KeyEvent{Key: test.key, Target: hitWorkspaceTemplateMore.At(1)})
			if !result.Handled || !state.workspaceTemplateMenu.open ||
				state.workspaceTemplateMenu.templateID != state.templates[1].ID {
				t.Fatalf("%s = %+v, menu = %+v", test.name, result, state.workspaceTemplateMenu)
			}
			if state.workspaceTemplateFocusPending != hitWorkspaceTemplateMenuEdit {
				t.Fatalf("menu focus = %v", state.workspaceTemplateFocusPending)
			}
		})
	}

	state.workspaceTemplateMenu = workspaceTemplateMenu{}
	state.Click(hitWorkspaceTemplateMore.At(0))
	if !state.workspaceTemplateMenu.open || state.workspaceTemplateMenu.templateID != state.templates[0].ID {
		t.Fatalf("pointer menu = %+v", state.workspaceTemplateMenu)
	}
}

func TestWorkspaceTemplateActionDoesNotCompleteARowDoubleClick(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	state.templates = []WorkspaceTemplate{
		mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 0), 1),
	}
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		ClickCount: 1, Target: hitWorkspaceTemplateRowList.At(0)})
	state.Click(hitWorkspaceTemplateRowList.At(0))

	result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		ClickCount: 2, Target: hitWorkspaceTemplateMore.At(0)})
	if result.Handled || len(world.configs) != 0 || state.activeSection != sectionWorkspaces {
		t.Fatalf("actions press launched a workspace: result=%+v dials=%d section=%d", result, len(world.configs), state.activeSection)
	}
}

func TestWorkspaceTemplateMenuSelectsEditAndDelete(t *testing.T) {
	state := templateManagementState(t, 1)

	state.Click(hitWorkspaceTemplateMore.At(0))
	state.Click(hitWorkspaceTemplateMenuEdit)
	if state.modal != modalWorkspaceDraft || state.workspaceDraft.mode != workspaceDraftEdit ||
		state.workspaceDraft.editID != state.templates[0].ID {
		t.Fatalf("edit menu: modal=%d draft=%+v", state.modal, state.workspaceDraft)
	}

	state.closeModal()
	state.workspaceTemplateMenu = workspaceTemplateMenu{}
	state.Click(hitWorkspaceTemplateMore.At(0))
	state.Click(hitWorkspaceTemplateMenuDelete)
	if state.modal != modalWorkspaceTemplateDelete ||
		state.workspaceTemplateDelete.templateID != state.templates[0].ID {
		t.Fatalf("delete menu: modal=%d delete=%+v", state.modal, state.workspaceTemplateDelete)
	}
}

func TestWorkspaceTemplateMenuStaysAnchoredToAnUnselectedRow(t *testing.T) {
	state := templateManagementState(t, 2)
	state.Click(hitWorkspaceTemplateMore.At(1))
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	trigger := panelHit(t, canvas, hitWorkspaceTemplateMore.At(1)).Bounds
	want := gfx.Point{X: trigger.X + trigger.Width, Y: trigger.Y + trigger.Height}
	if state.workspaceTemplateMenu.at != want {
		t.Fatalf("menu anchor = %+v, trigger = %+v", state.workspaceTemplateMenu.at, trigger)
	}
}

func TestWorkspaceTemplateMenuKeyboardAndFocusRestore(t *testing.T) {
	state := templateManagementState(t, 2)
	state.Click(hitWorkspaceTemplateMore.At(0))
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	result := state.Key(gfx.KeyEvent{Key: gfx.KeyDown, Target: hitWorkspaceTemplateMenuEdit})
	if !result.Handled || result.Focus != hitWorkspaceTemplateMenuDelete {
		t.Fatalf("menu Down = %+v", result)
	}
	result = state.Key(gfx.KeyEvent{
		Key: gfx.KeySpace, Target: hitWorkspaceTemplateMenuDelete, Text: " ",
	})
	if !result.Handled || state.modal != modalWorkspaceTemplateDelete {
		t.Fatalf("menu Space = %+v modal=%d", result, state.modal)
	}
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if !result.Handled || state.workspaceTemplateFocusPending != hitWorkspaceTemplateMore.At(0) {
		t.Fatalf("delete Escape = %+v pending=%v", result, state.workspaceTemplateFocusPending)
	}

	state.Click(hitWorkspaceTemplateMore.At(0))
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if !result.Handled || state.workspaceTemplateFocusPending != hitWorkspaceTemplateMore.At(0) {
		t.Fatalf("menu Escape = %+v pending=%v", result, state.workspaceTemplateFocusPending)
	}
}

func TestWorkspaceTemplateMenuClosesAndRestoresFocusWhenItsTargetDisappears(t *testing.T) {
	state := templateManagementState(t, 2)
	state.Click(hitWorkspaceTemplateMore.At(1))

	state.templates = []WorkspaceTemplate{state.templates[0]}
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})

	if state.workspaceTemplateMenu.open {
		t.Fatal("menu stayed open after its target disappeared")
	}
	if canvas.FocusRequest() != hitWorkspaceTemplateMore.At(0) {
		t.Fatalf("focus after disappearing target = %v", canvas.FocusRequest())
	}
}

func TestWorkspaceTemplateMenuDoesNotFocusTheHiddenLibraryAfterNavigation(t *testing.T) {
	state := templateManagementState(t, 1)
	state.Click(hitWorkspaceTemplateMore.At(0))
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	state.activeSection = sectionHosts
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if state.workspaceTemplateMenu.open || !canvas.FocusRequest().IsZero() {
		t.Fatalf("hidden library menu open=%t focus=%v", state.workspaceTemplateMenu.open, canvas.FocusRequest())
	}
}

func TestWorkspaceTemplateEditDoesNotRequestFocusForAClosedMenu(t *testing.T) {
	state := templateManagementState(t, 1)
	state.Click(hitWorkspaceTemplateMore.At(0))
	state.Click(hitWorkspaceTemplateMenuEdit)
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if canvas.InitialFocus() != hitWorkspaceDraftName || !canvas.FocusRequest().IsZero() {
		t.Fatalf("edit initial focus=%v requested focus=%v", canvas.InitialFocus(), canvas.FocusRequest())
	}
}

func TestWorkspaceTemplatePendingFocusIsDroppedAfterLeavingTheLibrary(t *testing.T) {
	state := templateManagementState(t, 1)
	state.openWorkspaceDraftEdit(state.templates[0].ID)
	state.Click(hitWorkspaceDraftCancel)
	state.activeSection = sectionHosts
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	if !canvas.FocusRequest().IsZero() {
		t.Fatalf("requested hidden template focus %v", canvas.FocusRequest())
	}
}

func TestWorkspaceTemplateDeleteCancelAndConfirmUseKeyboard(t *testing.T) {
	state := templateManagementState(t, 2)
	first, second := state.templates[0], state.templates[1]
	state.Click(hitWorkspaceTemplateMore.At(0))
	state.Click(hitWorkspaceTemplateMenuDelete)

	result := state.Key(gfx.KeyEvent{Key: gfx.KeySpace, Target: hitWorkspaceTemplateDeleteCancel})
	if !result.Handled || state.modal != modalNone || !reflect.DeepEqual(state.templates, []WorkspaceTemplate{first, second}) {
		t.Fatalf("cancel = %+v modal=%d templates=%+v", result, state.modal, state.templates)
	}
	if state.workspaceTemplateFocusPending != hitWorkspaceTemplateMore.At(0) {
		t.Fatalf("cancel focus = %v", state.workspaceTemplateFocusPending)
	}

	state.Click(hitWorkspaceTemplateMore.At(0))
	state.Click(hitWorkspaceTemplateMenuDelete)
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitWorkspaceTemplateDeleteSubmit})
	if !result.Handled || state.modal != modalNone || !reflect.DeepEqual(state.templates, []WorkspaceTemplate{second}) {
		t.Fatalf("confirm = %+v modal=%d templates=%+v", result, state.modal, state.templates)
	}
}

func TestWorkspaceTemplateDeleteSelectsTheNextPreviousAndEmptyRows(t *testing.T) {
	t.Run("next", func(t *testing.T) {
		state := templateManagementState(t, 3)
		want := state.templates[1].ID
		state.submitDeleteTemplate(t, 0)
		if state.selectedWorkspaceTemplateID != want || len(state.templates) != 2 {
			t.Fatalf("next selection = %q templates=%+v", state.selectedWorkspaceTemplateID, state.templates)
		}
	})

	t.Run("previous", func(t *testing.T) {
		state := templateManagementState(t, 2)
		want := state.templates[0].ID
		state.submitDeleteTemplate(t, 1)
		if state.selectedWorkspaceTemplateID != want || len(state.templates) != 1 {
			t.Fatalf("previous selection = %q templates=%+v", state.selectedWorkspaceTemplateID, state.templates)
		}
	})

	t.Run("empty", func(t *testing.T) {
		state := templateManagementState(t, 1)
		state.submitDeleteTemplate(t, 0)
		if len(state.templates) != 0 || state.selectedWorkspaceTemplateID != "" ||
			state.workspaceLibraryExpanded || state.workspaceTemplateFocusPending != hitWorkspaceNew {
			t.Fatalf("empty state: templates=%+v selected=%q expanded=%t focus=%v",
				state.templates, state.selectedWorkspaceTemplateID, state.workspaceLibraryExpanded,
				state.workspaceTemplateFocusPending)
		}
	})
}

func TestWorkspaceTemplateDeleteFailureRestoresEverything(t *testing.T) {
	state := templateManagementState(t, 2)
	before := cloneWorkspaceTemplates(state.templates)
	beforeSelected := state.selectedWorkspaceTemplateID
	beforeExpanded := state.workspaceLibraryExpanded
	previousConfig := state.config
	t.Cleanup(func() { state.config = previousConfig })
	state.config = nil

	state.Click(hitWorkspaceTemplateMore.At(0))
	state.Click(hitWorkspaceTemplateMenuDelete)
	state.submitWorkspaceTemplateDelete()

	if state.modal != modalWorkspaceTemplateDelete ||
		state.workspaceTemplateDelete.error != state.messages.WorkspaceTemplateDeleteFailed {
		t.Fatalf("failed delete: modal=%d error=%q", state.modal, state.workspaceTemplateDelete.error)
	}
	if !reflect.DeepEqual(state.templates, before) ||
		state.selectedWorkspaceTemplateID != beforeSelected ||
		state.workspaceLibraryExpanded != beforeExpanded {
		t.Fatalf("failed delete changed state: templates=%+v selected=%q expanded=%t",
			state.templates, state.selectedWorkspaceTemplateID, state.workspaceLibraryExpanded)
	}
}

func TestWorkspaceTemplateDeleteMissingTargetDoesNotChangeTemplates(t *testing.T) {
	state := templateManagementState(t, 2)
	other := state.templates[1]
	state.Click(hitWorkspaceTemplateMore.At(0))
	state.Click(hitWorkspaceTemplateMenuDelete)
	state.templates = []WorkspaceTemplate{other}

	state.submitWorkspaceTemplateDelete()

	if state.modal != modalNone || !reflect.DeepEqual(state.templates, []WorkspaceTemplate{other}) {
		t.Fatalf("missing delete: modal=%d templates=%+v", state.modal, state.templates)
	}
}

func TestWorkspaceTemplateEditPreviewRemovesMissingAndResolvedHosts(t *testing.T) {
	state := templateManagementState(t, 1)
	template := state.templates[0]
	state.openWorkspaceDraftEdit(template.ID)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})

	for _, index := range []int{1, 0} {
		state.startWorkspaceCardDrag(gfx.PointerEvent{
			Kind: gfx.PointerDown, Target: hitWorkspaceDraftRemoveItem.At(index), X: 10, Y: 10,
		})
		state.Click(hitWorkspaceDraftRemoveItem.At(index))
		if state.workspaceCardDrag.pressed || state.workspaceCardDrag.active {
			t.Fatalf("remove %d started a card drag: %+v", index, state.workspaceCardDrag)
		}
	}
	if !slices.Equal(state.workspaceDraft.order, []string{state.hosts[1].CatalogID}) {
		t.Fatalf("order after removing resolved and missing hosts = %v", state.workspaceDraft.order)
	}
}

func TestWorkspaceTemplateManagementFitsMinimumWindow(t *testing.T) {
	for _, language := range []i18n.Language{i18n.Chinese, i18n.English} {
		t.Run(language.String(), func(t *testing.T) {
			state := templateManagementState(t, 1)
			state.setLanguage(language)
			state.openWorkspaceDraftEdit(state.templates[0].ID)
			canvas, texts := paintAt(state, gfx.Size{Width: MinimumWindowWidth, Height: MinimumWindowHeight})
			if !hasText(texts, state.messages.WorkspaceDraftTitleEdit) {
				t.Fatalf("missing edit title: %q", texts)
			}
			submit := panelHit(t, canvas, hitWorkspaceDraftSubmit)
			if submit.Label != state.messages.WorkspaceDraftSaveChanges || state.workspaceDraft.bounds.Height < 56 {
				t.Fatalf("edit footer=%+v host list=%+v", submit, state.workspaceDraft.bounds)
			}
			for _, id := range []gfx.ID{hitWorkspaceDraftName, hitWorkspaceDraftSearch, hitWorkspaceDraftPresets.At(0),
				hitWorkspaceDraftPresets.At(1), hitWorkspaceDraftPresets.At(2), hitWorkspaceDraftRemoveItem.At(0), hitWorkspaceDraftCancel} {
				box := panelHit(t, canvas, id).Bounds
				if box.X < 0 || box.Y < 0 || box.X+box.Width > MinimumWindowWidth || box.Y+box.Height > MinimumWindowHeight {
					t.Fatalf("edit control %v outside window: %+v", id, box)
				}
			}
			if state.workspaceDraft.bounds.Y+state.workspaceDraft.bounds.Height >= submit.Bounds.Y ||
				state.workspaceDraft.previewBounds.Y+state.workspaceDraft.previewBounds.Height >= submit.Bounds.Y {
				t.Fatalf("edit body overlaps footer: list=%+v preview=%+v footer=%+v",
					state.workspaceDraft.bounds, state.workspaceDraft.previewBounds, submit.Bounds)
			}
			config := state.config
			t.Cleanup(func() { state.config = config })
			state.config = nil
			state.Click(hitWorkspaceDraftSubmit)
			_, texts = paintAt(state, gfx.Size{Width: MinimumWindowWidth, Height: MinimumWindowHeight})
			if !hasText(texts, state.messages.WorkspaceSaveFailed) {
				t.Fatalf("edit failure is not visible: %q", texts)
			}
			state.closeModal()
			state.openWorkspaceTemplateDelete(state.templates[0].ID)
			canvas, texts = paintAt(state, gfx.Size{Width: MinimumWindowWidth, Height: MinimumWindowHeight})
			if !hasText(texts, state.messages.WorkspaceTemplateDeleteTitle) ||
				!hasText(texts, state.templates[0].Name) || canvas.InitialFocus() != hitWorkspaceTemplateDeleteCancel {
				t.Fatalf("delete dialog text=%q initial focus=%v", texts, canvas.InitialFocus())
			}
			for _, id := range []gfx.ID{hitWorkspaceTemplateDeleteCancel, hitWorkspaceTemplateDeleteSubmit} {
				box := panelHit(t, canvas, id).Bounds
				if box.X < 0 || box.Y < 0 || box.X+box.Width > MinimumWindowWidth || box.Y+box.Height > MinimumWindowHeight {
					t.Fatalf("delete control %v outside window: %+v", id, box)
				}
			}
			state.Click(hitWorkspaceTemplateDeleteSubmit)
			_, texts = paintAt(state, gfx.Size{Width: MinimumWindowWidth, Height: MinimumWindowHeight})
			if !hasText(texts, state.messages.WorkspaceTemplateDeleteFailed) {
				t.Fatalf("delete failure is not visible: %q", texts)
			}
		})
	}
}

func TestWorkspaceTemplateEditDetailsDoNotOverlapRemoveAction(t *testing.T) {
	state := templateManagementState(t, 1)
	state.openWorkspaceDraftEdit(state.templates[0].ID)
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	card := panelHit(t, canvas, hitWorkspaceDraftCards.At(0)).Bounds
	remove := panelHit(t, canvas, hitWorkspaceDraftRemoveItem.At(0)).Bounds
	for _, label := range canvas.Texts() {
		if label.Bounds.X == card.X+10 && label.Bounds.Y == card.Y+66 &&
			label.Bounds.X+label.Bounds.Width > remove.X {
			t.Fatalf("card detail overlaps remove action: text=%+v remove=%+v", label.Bounds, remove)
		}
	}
}

func TestWorkspaceTemplateEditKeepsAnAllMissingTemplateUntilExplicitRemoval(t *testing.T) {
	state := templateManagementState(t, 1)
	template := mustWorkspaceTemplate(t, "Unavailable", []string{"host_missing_a", "host_missing_b"}, 2)
	state.templates = []WorkspaceTemplate{template}
	state.openWorkspaceDraftEdit(template.ID)
	canvas, texts := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	for index, id := range template.HostIDs {
		if !hasText(texts, id) || panelHit(t, canvas, hitWorkspaceDraftRemoveItem.At(index)).Role != gfx.RoleButton {
			t.Fatalf("missing host %q is not editable", id)
		}
	}
	state.workspaceDraft.name = *text.NewEditor("Renamed")
	state.Click(hitWorkspaceDraftSubmit)
	if len(state.templates) != 1 || state.templates[0].ID != template.ID ||
		state.templates[0].Name != "Renamed" || !slices.Equal(state.templates[0].HostIDs, template.HostIDs) {
		t.Fatalf("rename lost missing references: %+v", state.templates)
	}
	state.openWorkspaceDraftEdit(template.ID)
	state.Click(hitWorkspaceDraftRemoveItem.At(0))
	state.Click(hitWorkspaceDraftSubmit)
	if got := state.templates[0].HostIDs; !slices.Equal(got, []string{"host_missing_b"}) {
		t.Fatalf("explicit removal left %v", got)
	}
}

func TestWorkspaceTemplateEditUnchangedRuntimeAndPersistsStableID(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 0, 1), 2)
	other := mustWorkspaceTemplate(t, "Database", workspaceCatalogIDs(state, 2), 1)
	state.templates = []WorkspaceTemplate{template, other}
	state.selectedWorkspaceTemplateID = template.ID

	state.applySelectedWorkspaceTemplate()
	for _, connection := range world.conns {
		finishWorkspaceConnection(t, state, connection)
	}
	workspace := state.deck.ActiveWorkspace()
	state.toggleBroadcast()
	active := state.activeSessionID
	activePane := state.deck.ActivePane(workspace)
	panes := state.deck.Panes(workspace, nil)
	title := state.workspaceTabs[len(state.workspaceTabs)-1].Title
	columnCount := state.managedWorkspaceLayouts[workspace]
	broadcast := state.broadcast

	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceTemplateMore.At(0))
	state.Click(hitWorkspaceTemplateMenuEdit)
	state.workspaceDraft.name = *text.NewEditor("Production Changed")
	state.Click(workspaceDraftHostID(2))
	state.Click(hitWorkspaceDraftPresets.At(2))
	state.moveWorkspaceDraftItem(2, 0)
	state.Click(hitWorkspaceDraftSubmit)

	if len(state.templates) != 2 || state.templates[0].ID != template.ID || state.templates[0].Name != "Production Changed" ||
		!isGrid(state.templates[0].Layout, 3) || !slices.Equal(state.templates[0].HostIDs,
		[]string{state.hosts[2].CatalogID, state.hosts[0].CatalogID, state.hosts[1].CatalogID}) {
		t.Fatalf("edit did not update the stable template: %+v", state.templates)
	}
	if broadcast == 0 || state.activeSessionID != active || state.deck.ActivePane(workspace) != activePane ||
		!reflect.DeepEqual(state.deck.Panes(workspace, nil), panes) ||
		state.workspaceTabs[len(state.workspaceTabs)-1].Title != title ||
		!state.managedWorkspaceLayouts[workspace].Equal(columnCount) || state.broadcast != broadcast {
		t.Fatalf("edit changed runtime state: active=%+v panes=%v title=%q columns=%+v broadcast=%+v",
			state.activeSessionID, state.deck.Panes(workspace, nil),
			state.workspaceTabs[len(state.workspaceTabs)-1].Title,
			state.managedWorkspaceLayouts[workspace], state.broadcast)
	}
	if len(world.configs) != 2 {
		t.Fatalf("edit dialed %d connections", len(world.configs))
	}

	loaded, report, err := loadState(options.StatePath)
	if err != nil || report.outcome != loadOK || !reflect.DeepEqual(loaded.WorkspaceTemplates, state.templates) {
		t.Fatalf("persisted edit = %+v outcome=%v err=%v", loaded.WorkspaceTemplates, report.outcome, err)
	}
}

func TestWorkspaceTemplateDeletePersistsTombstoneWithoutChangingRuntime(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.activeSection = sectionWorkspaces
	template := mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 0, 1), 2)
	other := mustWorkspaceTemplate(t, "Database", workspaceCatalogIDs(state, 2), 1)
	state.templates = []WorkspaceTemplate{template, other}
	state.selectedWorkspaceTemplateID = template.ID
	state.applySelectedWorkspaceTemplate()
	for _, connection := range world.conns {
		finishWorkspaceConnection(t, state, connection)
	}
	workspace := state.deck.ActiveWorkspace()
	state.toggleBroadcast()
	broadcast := state.broadcast
	active := state.activeSessionID
	activePane := state.deck.ActivePane(workspace)
	panes := state.deck.Panes(workspace, nil)
	columns := state.managedWorkspaceLayouts[workspace]
	title := state.workspaceTabs[len(state.workspaceTabs)-1].Title

	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceTemplateMore.At(0))
	state.Click(hitWorkspaceTemplateMenuDelete)
	state.Click(hitWorkspaceTemplateDeleteSubmit)

	if !reflect.DeepEqual(state.templates, []WorkspaceTemplate{other}) {
		t.Fatalf("templates after delete = %+v", state.templates)
	}
	if broadcast == 0 || state.broadcast != broadcast || state.activeSessionID != active ||
		state.deck.ActivePane(workspace) != activePane || !reflect.DeepEqual(state.deck.Panes(workspace, nil), panes) ||
		state.workspaceTabs[len(state.workspaceTabs)-1].Title != title || !state.managedWorkspaceLayouts[workspace].Equal(columns) ||
		len(world.configs) != 2 {
		t.Fatal("delete changed the open workspace")
	}
	loaded, report, err := loadState(options.StatePath)
	if err != nil || report.outcome != loadOK || !reflect.DeepEqual(loaded.WorkspaceTemplates, []WorkspaceTemplate{other}) {
		t.Fatalf("reloaded delete = %+v outcome=%v err=%v", loaded.WorkspaceTemplates, report.outcome, err)
	}
	records, err := state.config.Records()
	if err != nil {
		t.Fatal(err)
	}
	deleted := false
	for _, record := range records {
		if record.Kind == kindWorkspaceTemplate && record.ID == template.ID {
			deleted = record.Deleted && record.Payload == nil
		}
	}
	if !deleted {
		t.Fatal("template deletion did not produce a tombstone")
	}
}

func templateManagementState(t testing.TB, templateCount int) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionWorkspaces
	hosts := []string{
		state.hosts[0].CatalogID, "host_deleted", state.hosts[1].CatalogID,
	}
	for index := 0; index < templateCount; index++ {
		state.templates = append(state.templates, mustWorkspaceTemplate(t,
			"Template "+strings.Repeat("A", index+1), hosts, uint8(index%3+1)))
	}
	state.selectedWorkspaceTemplateID = state.templates[0].ID
	state.workspaceLibraryExpanded = true
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	return state
}

func (state *State) submitDeleteTemplate(t testing.TB, index int) {
	t.Helper()
	state.Click(hitWorkspaceTemplateMore.At(index))
	state.Click(hitWorkspaceTemplateMenuDelete)
	state.Click(hitWorkspaceTemplateDeleteSubmit)
}
