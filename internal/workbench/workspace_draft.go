package workbench

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/localshell"
	"motty/internal/workbench/icons"
	layoutcore "motty/internal/workspace"
)

type workspaceDraftMode uint8

const (
	workspaceDraftTemporary workspaceDraftMode = iota
	workspaceDraftFixed
	workspaceDraftEdit
)

type workspaceDraft struct {
	commandHost    uint32
	commandEditors map[string]*text.Editor
	mode           workspaceDraftMode
	editID         string
	name, query    text.Editor
	selected       map[string]bool
	order          []string
	layout         layoutcore.LayoutSpec
	layoutTouched  bool
	error          string
	bounds         gfx.Rect
	content        float32
	scroll         float32
	previewScroll  float32
	previewBounds  gfx.Rect
	previewContent float32
	// nameField and queryField are the page's handles on the same editors the
	// dialog reads.
	nameField  ui.Field
	queryField ui.Field
	// commandFields are the page fields on the command editors, made once.
	commandFields map[string]*ui.Field
	// rows is the host list's page list, and rowCache the rows it declared.
	rows     ui.ListState
	rowCache []workspaceDraftRow
	// scrollSynced and previewScrollSynced are the offsets the page scroll
	// areas were last given, so a programmatic move reaches them next paint.
	scrollSynced, previewScrollSynced float32
	// revealedCommandHost is the command host whose expanded field the list has
	// brought into view, so a later scroll by the user is not undone.
	revealedCommandHost uint32
	// collapsed holds the host list's folded groups; a missing entry means
	// the group is expanded. Searching renders every group regardless.
	collapsed map[string]bool
}

func (draft *workspaceDraft) reset() {
	*draft = workspaceDraft{}
}

func (state *State) openWorkspaceDraft(mode workspaceDraftMode) {
	state.workspaceDraft = workspaceDraft{
		mode:     mode,
		selected: make(map[string]bool, maxWorkspaceTemplateHosts),
		layout:   layoutcore.GridSpec(1),
	}
	state.workspaceDraft.nameField.Bind(&state.workspaceDraft.name)
	state.workspaceDraft.queryField.Bind(&state.workspaceDraft.query)
	state.modal = modalWorkspaceDraft
}

func (state *State) openWorkspaceDraftEdit(templateID string) {
	index := slices.IndexFunc(state.templates, func(template WorkspaceTemplate) bool {
		return template.ID == templateID
	})
	if index < 0 {
		return
	}
	template := state.templates[index]
	draft := workspaceDraft{
		mode:          workspaceDraftEdit,
		editID:        template.ID,
		selected:      make(map[string]bool, len(template.HostIDs)),
		order:         slices.Clone(template.HostIDs),
		layout:        template.Layout,
		layoutTouched: true,
	}
	draft.name.Insert(template.Name)
	draft.name.SetSelection(text.Range{End: draft.name.RuneCount()})
	for _, id := range template.HostIDs {
		draft.selected[id] = true
		if command := template.Commands[id].Command; command != "" {
			draft.commandEditor(id).Insert(command)
		}
	}
	state.workspaceDraft = draft
	state.workspaceDraft.nameField.Bind(&state.workspaceDraft.name)
	state.workspaceDraft.queryField.Bind(&state.workspaceDraft.query)
	state.selectedWorkspaceTemplateID = template.ID
	state.workspaceLibraryExpanded = true
	state.modal = modalWorkspaceDraft
}

func (state *State) clickWorkspaceDraft(id gfx.ID) {
	draft := &state.workspaceDraft
	switch {
	case id.Scope() == hitWorkspaceCommandEdit:
		if index, ok := id.In(hitWorkspaceCommandEdit); ok {
			if draft.commandHost == uint32(index) {
				draft.commandHost, draft.revealedCommandHost = 0, 0
			} else {
				draft.commandHost = uint32(index)
			}
		}
	case id == hitWorkspaceDraftDismiss || id == hitWorkspaceDraftCancel:
		if draft.mode == workspaceDraftEdit {
			state.workspaceTemplateFocusPending = state.workspaceTemplateLauncherFocus(draft.editID)
		}
		state.closeModal()
	case id.Scope() == hitWorkspaceDraftPresets:
		if index, ok := id.In(hitWorkspaceDraftPresets); ok && index < len(workspacePresets) {
			state.setWorkspaceDraftLayout(workspacePresets[index])
		}
	case id == hitWorkspaceDraftSubmit:
		state.submitWorkspaceDraft()
	case id.Scope() == hitWorkspaceDraftHostList:
		index, ok := id.In(hitWorkspaceDraftHostList)
		host := state.workspaceDraftVisibleHost(index, ok)
		if !ok || host == nil {
			return
		}
		selected := draft.selected[host.CatalogID]
		if !selected && state.workspaceDraftSelectionCount() >= maxWorkspaceTemplateHosts {
			return
		}
		draft.selected[host.CatalogID] = !selected
		if selected {
			delete(draft.commandEditors, host.CatalogID)
		}
		if draft.mode == workspaceDraftEdit {
			if selected {
				draft.order = slices.DeleteFunc(draft.order, func(id string) bool {
					return id == host.CatalogID
				})
			} else {
				draft.order = append(draft.order, host.CatalogID)
			}
		}
		state.workspaceDraftCountChanged()
		draft.error = ""
	case id.Scope() == hitWorkspaceDraftGroupList:
		if index, ok := id.In(hitWorkspaceDraftGroupList); ok {
			state.toggleWorkspaceDraftGroup(index)
		}
	case id.Scope() == hitWorkspaceDraftGroupCheckList:
		if index, ok := id.In(hitWorkspaceDraftGroupCheckList); ok {
			state.toggleWorkspaceDraftGroupCheck(index)
		}
	case id.Scope() == hitWorkspaceDraftRemoveItem:
		index, ok := id.In(hitWorkspaceDraftRemoveItem)
		if ok {
			state.workspaceDraftRemoveItem(index)
		}
	default:
		return
	}
}

func (state *State) setWorkspaceDraftLayout(spec layoutcore.LayoutSpec) {
	state.workspaceDraft.layout = spec
	state.workspaceDraft.layoutTouched = true
}

func (state *State) submitWorkspaceDraft() {
	draft := &state.workspaceDraft
	hostIDs := state.workspaceDraftOrderedIDs()
	if len(hostIDs) == 0 {
		draft.error = state.messages.WorkspaceDraftHostsRequired
		return
	}
	commands, valid := state.workspaceDraftCommands()
	if !valid {
		draft.error = state.messages.WorkspaceCommandInvalid
		return
	}
	name := strings.TrimSpace(draft.name.String())
	if draft.mode == workspaceDraftFixed && name == "" {
		draft.error = state.messages.WorkspaceDraftNameRequired
		return
	}
	if draft.mode == workspaceDraftEdit {
		state.submitWorkspaceDraftEdit(name, hostIDs, commands)
		return
	}

	plan, err := PlanWorkspaceLaunch(state.workspaceHosts(), hostIDs, draft.layout)
	if err != nil {
		draft.error = state.messages.WorkspaceDraftHostsRequired
		return
	}
	plan.Commands = commands
	plan = orderedWorkspaceLaunch(plan, hostIDs)
	if draft.mode == workspaceDraftTemporary {
		title := state.messages.WorkspaceDraftTemporaryName
		if len(plan.Hosts) == 1 {
			title = plan.Hosts[0].Label
		}
		workspaceID, _, err := state.launchWorkspaceWithIdentity(plan, title, workspaceLaunchIdentity{
			managedLayout:   plan.Layout,
			temporary:       true,
			temporaryLayout: plan.Layout,
		})
		if err != nil {
			draft.error = state.messages.WorkspaceTemplateApplyFailed
			return
		}
		state.fitWorkspaceTerminals(workspaceID)
		state.closeModal()
		return
	}

	template, err := NewWorkspaceTemplate(name, state.workspaceDraftHostCatalogIDs(plan.Hosts), plan.Layout)
	if err != nil {
		draft.error = state.messages.WorkspaceDraftNameRequired
		return
	}
	template.Commands = commands
	previousSelected := state.selectedWorkspaceTemplateID
	previousExpanded := state.workspaceLibraryExpanded
	state.templates = append(state.templates, template)
	state.selectedWorkspaceTemplateID = template.ID
	state.workspaceLibraryExpanded = true
	if !state.saveResult() {
		state.templates = state.templates[:len(state.templates)-1]
		state.selectedWorkspaceTemplateID = previousSelected
		state.workspaceLibraryExpanded = previousExpanded
		draft.error = state.messages.WorkspaceSaveFailed
		return
	}
	state.closeModal()
	state.showActionInfo(
		state.messages.WorkspaceLibraryTitle,
		fmt.Sprintf(state.messages.WorkspaceDraftSavedFmt, template.Name),
	)
}

func (state *State) submitWorkspaceDraftEdit(name string, hostIDs []string, commands map[string]WorkspaceCommand) {
	draft := &state.workspaceDraft
	index := slices.IndexFunc(state.templates, func(template WorkspaceTemplate) bool {
		return template.ID == draft.editID
	})
	if index < 0 {
		draft.error = state.messages.WorkspaceTemplateEditMissing
		return
	}
	template := WorkspaceTemplate{
		ID: draft.editID, Name: name, HostIDs: slices.Clone(hostIDs), Layout: draft.layout,
		Commands: commands,
	}
	if err := template.validate(); err != nil {
		draft.error = state.workspaceSaveTemplateError(err)
		return
	}

	previousTemplates := cloneWorkspaceTemplates(state.templates)
	previousSelected := state.selectedWorkspaceTemplateID
	previousExpanded := state.workspaceLibraryExpanded
	state.templates = slices.Clone(state.templates)
	state.templates[index] = template
	state.selectedWorkspaceTemplateID = template.ID
	state.workspaceLibraryExpanded = true
	if !state.saveResult() {
		state.templates = previousTemplates
		state.selectedWorkspaceTemplateID = previousSelected
		state.workspaceLibraryExpanded = previousExpanded
		draft.error = state.messages.WorkspaceSaveFailed
		return
	}
	state.workspaceTemplateFocusPending = state.workspaceTemplateLauncherFocus(template.ID)
	state.closeModal()
	state.showActionInfo(
		state.messages.WorkspaceLibraryTitle,
		fmt.Sprintf(state.messages.WorkspaceTemplateEditedFmt, template.Name),
	)
}

func (state *State) workspaceDraftSelectedCatalogIDs() []string {
	ids := make([]string, 0, len(state.workspaceDraft.selected))
	for _, host := range state.workspaceHosts() {
		if state.workspaceDraft.selected[host.CatalogID] {
			ids = append(ids, host.CatalogID)
		}
	}
	if len(state.workspaceDraft.order) > 0 {
		ordered := make([]string, 0, len(ids))
		for _, id := range state.workspaceDraft.order {
			if slices.Contains(ids, id) {
				ordered = append(ordered, id)
			}
		}
		for _, id := range ids {
			if !slices.Contains(ordered, id) {
				ordered = append(ordered, id)
			}
		}
		return ordered
	}
	return ids
}

func (state *State) workspaceDraftOrderedIDs() []string {
	if state.workspaceDraft.mode == workspaceDraftEdit {
		return slices.Clone(state.workspaceDraft.order)
	}
	return state.workspaceDraftSelectedCatalogIDs()
}

func (state *State) moveWorkspaceDraftItem(from, to int) bool {
	draft := &state.workspaceDraft
	if draft.mode == workspaceDraftEdit {
		return placeWorkspaceItem(draft.order, from, to, draft.layout)
	}
	ids := state.workspaceDraftSelectedCatalogIDs()
	if !placeWorkspaceItem(ids, from, to, draft.layout) {
		return false
	}
	draft.order = ids
	return true
}

// dockWorkspaceDraftCard records a layout drawn on the draft's cards.
func (state *State) dockWorkspaceDraftCard(spec layoutcore.LayoutSpec, order []int) {
	draft := &state.workspaceDraft
	ids := draft.order
	if draft.mode != workspaceDraftEdit {
		ids = state.workspaceDraftSelectedCatalogIDs()
	}
	draft.order = permuteWorkspaceItems(ids, order)
	state.setWorkspaceDraftLayout(spec)
}

func (state *State) workspaceDraftRemoveItem(index int) {
	draft := &state.workspaceDraft
	if index < 0 || index >= len(draft.order) {
		return
	}
	id := draft.order[index]
	draft.order = slices.Delete(draft.order, index, index+1)
	delete(draft.selected, id)
	delete(draft.commandEditors, id)
	if draft.layout.Kind == layoutcore.LayoutCustom {
		state.workspaceDraftCountChanged()
	}
	draft.error = ""
}

// workspaceDraftCountChanged keeps the layout fitting the chosen hosts: an
// untouched choice follows the count, and a drawn layout, which names its
// panes one by one, cannot outlive a change of count.
func (state *State) workspaceDraftCountChanged() {
	draft := &state.workspaceDraft
	if !draft.layoutTouched || draft.layout.Kind == layoutcore.LayoutCustom {
		draft.layout = DefaultWorkspaceLayout(state.workspaceDraftSelectionCount())
		draft.layoutTouched = false
	}
}

func cloneWorkspaceTemplates(templates []WorkspaceTemplate) []WorkspaceTemplate {
	cloned := slices.Clone(templates)
	for index := range cloned {
		cloned[index].HostIDs = slices.Clone(cloned[index].HostIDs)
		cloned[index].Commands = maps.Clone(cloned[index].Commands)
	}
	return cloned
}

func (state *State) workspaceTemplateLauncherFocus(templateID string) gfx.ID {
	if index := slices.IndexFunc(state.templates, func(template WorkspaceTemplate) bool {
		return template.ID == templateID
	}); index >= 0 {
		return hitWorkspaceTemplateMore.At(index)
	}
	return hitWorkspaceNew
}

func (state *State) workspaceDraftHostCatalogIDs(hosts []Host) []string {
	ids := make([]string, len(hosts))
	for index := range hosts {
		ids[index] = workspaceHostReference(hosts[index])
	}
	return ids
}

func (state *State) workspaceDraftSelectionCount() int {
	count := 0
	for _, selected := range state.workspaceDraft.selected {
		if selected {
			count++
		}
	}
	return count
}

func (state *State) workspaceDraftHosts() []Host {
	query := strings.ToLower(strings.TrimSpace(state.workspaceDraft.query.String()))
	hosts := make([]Host, 0, len(state.hosts)+1)
	for _, host := range state.workspaceHosts() {
		if !workspaceHostEligible(host) {
			continue
		}
		search := host.Label + " " + host.Address + " " + host.User + " " + host.Group + " " + host.Tags
		if host.ID == localHostID {
			search += " " + state.messages.LocalTerminal
			if shell, ok := workspaceLocalShellRef(host.CatalogID); ok {
				search += " " + shell
				if _, wsl := localshell.WSLDistro(shell); wsl {
					search += " wsl"
				}
			}
		}
		if query != "" && !strings.Contains(strings.ToLower(search), query) {
			continue
		}
		hosts = append(hosts, host)
	}
	return hosts
}

func (state *State) workspaceDraftVisibleHost(index int, ok bool) *Host {
	hosts := state.workspaceDraftHosts()
	if !ok || index < 0 || index >= len(hosts) {
		return nil
	}
	return &hosts[index]
}

// The host list's rows: a group header takes 42px, a host 60px, the heights
// paintWorkspaceDraftHosts lays out and every scroll path repeats.
const (
	workspaceDraftHostRowHeight  = float32(60)
	workspaceDraftGroupRowHeight = float32(42)
	// workspaceDraftHostIndent is how far a host row sits in from its group's
	// header, the one tree step the list has.
	workspaceDraftHostIndent = float32(16)
)

// The local rows and the hosts without a group fold under keys no group can
// carry: a name is normalized to non-empty, and the zero byte ends a key just
// as it ends the snippet picker's ungrouped one.
const (
	workspaceDraftLocalGroupKey = "\x00local"
	workspaceDraftHostsGroupKey = "\x00hosts"
)

// workspaceDraftGroup is one collapsible section of the host list. Expanded is
// what the rows were built with, so searching — which renders every group —
// still shows the chevron as open. Members are the group's terminals as the
// list currently shows them, folds included, so the header's checkbox can
// select or clear the whole group at once.
type workspaceDraftGroup struct {
	key        string
	groupIndex int
	label      string
	count      int
	selected   int
	expanded   bool
	members    []workspaceDraftRow
}

// workspaceDraftRow is one painted row of the host list: a group header, or a
// host with the index it keeps in the flat host list. Clicks are keyed by that
// index, so folding changes only what is painted, never what a click selects.
type workspaceDraftRow struct {
	group     *workspaceDraftGroup
	host      Host
	hostIndex int
}

// workspaceDraftRows lays the host list out in groups: the local terminals
// first, then the hosts without a group, then the named groups in alphabetical
// order, each keeping the list's own order within it. A folded group renders
// only its header, unless the search box has something in it, which expands
// every group like the pickers do.
func (state *State) workspaceDraftRows() []workspaceDraftRow {
	hosts := state.workspaceDraftHosts()
	searching := strings.TrimSpace(state.workspaceDraft.query.String()) != ""
	collapsed := state.workspaceDraft.collapsed

	type bucket struct {
		group workspaceDraftGroup
		hosts []workspaceDraftRow
	}
	var local bucket
	local.group = workspaceDraftGroup{key: workspaceDraftLocalGroupKey, expanded: true}
	var ungrouped bucket
	ungrouped.group = workspaceDraftGroup{key: workspaceDraftHostsGroupKey, expanded: true}
	var names []string
	buckets := make(map[string]*bucket)

	for index, host := range hosts {
		row := workspaceDraftRow{host: host, hostIndex: index}
		switch {
		case host.ID == localHostID:
			local.hosts = append(local.hosts, row)
		default:
			name := strings.TrimSpace(host.Group)
			if name == "" {
				ungrouped.hosts = append(ungrouped.hosts, row)
				continue
			}
			if _, seen := buckets[name]; !seen {
				names = append(names, name)
				buckets[name] = &bucket{group: workspaceDraftGroup{key: name, expanded: true}}
			}
			buckets[name].hosts = append(buckets[name].hosts, row)
		}
	}

	groups := []*bucket{&local, &ungrouped}
	slices.SortFunc(names, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	for _, name := range names {
		groups = append(groups, buckets[name])
	}

	rows := make([]workspaceDraftRow, 0, len(hosts)+len(groups))
	groupOrdinal := 0
	for _, bucket := range groups {
		if len(bucket.hosts) == 0 {
			continue
		}
		bucket.group.count = len(bucket.hosts)
		bucket.group.members = bucket.hosts
		for _, row := range bucket.hosts {
			if state.workspaceDraft.selected[row.host.CatalogID] {
				bucket.group.selected++
			}
		}
		bucket.group.expanded = searching || !collapsed[bucket.group.key]
		switch bucket.group.key {
		case workspaceDraftLocalGroupKey:
			bucket.group.label = state.messages.WorkspaceDraftLocalGroup
		case workspaceDraftHostsGroupKey:
			bucket.group.label = state.messages.HostGroupDefault
		default:
			bucket.group.label = bucket.group.key
		}
		bucket.group.groupIndex = groupOrdinal
		groupOrdinal++
		rows = append(rows, workspaceDraftRow{group: &bucket.group})
		if bucket.group.expanded {
			rows = append(rows, bucket.hosts...)
		}
	}
	return rows
}

// workspaceDraftListHeight is the content height the rows paint into.
func workspaceDraftListHeight(rows []workspaceDraftRow) float32 {
	height := float32(0)
	for _, row := range rows {
		if row.group != nil {
			height += workspaceDraftGroupRowHeight
		} else {
			height += workspaceDraftHostRowHeight
		}
	}
	return height
}

// toggleWorkspaceDraftGroup folds or opens the group the header click names.
func (state *State) toggleWorkspaceDraftGroup(index int) {
	seen := 0
	for _, row := range state.workspaceDraftRows() {
		if row.group == nil {
			continue
		}
		if seen == index {
			if state.workspaceDraft.collapsed == nil {
				state.workspaceDraft.collapsed = make(map[string]bool)
			}
			state.workspaceDraft.collapsed[row.group.key] = !state.workspaceDraft.collapsed[row.group.key]
			// The list may have shrunk: keep the scroll inside what is left.
			if content := workspaceDraftListHeight(state.workspaceDraftRows()); state.workspaceDraft.bounds.Height > 0 {
				state.workspaceDraft.scroll = min(state.workspaceDraft.scroll, max(0, content-state.workspaceDraft.bounds.Height))
			}
			return
		}
		seen++
	}
}

// toggleWorkspaceDraftGroupCheck is the group header's checkbox: it selects
// every terminal the group shows, or clears them when all are already
// selected. A folded group still answers, so whole groups can be picked
// without opening them first.
func (state *State) toggleWorkspaceDraftGroupCheck(index int) {
	seen := 0
	for _, row := range state.workspaceDraftRows() {
		if row.group == nil {
			continue
		}
		if seen == index {
			state.toggleWorkspaceDraftGroupMembers(row.group.members)
			return
		}
		seen++
	}
}

// toggleWorkspaceDraftGroupMembers applies one group checkbox: everything on
// when any member is missing, nothing when all are on. Selection keeps to the
// twelve-pane limit and says so when a group does not fit.
func (state *State) toggleWorkspaceDraftGroupMembers(members []workspaceDraftRow) {
	draft := &state.workspaceDraft
	full := true
	for _, row := range members {
		if !draft.selected[row.host.CatalogID] {
			full = false
			break
		}
	}
	draft.error = ""
	if full {
		for _, row := range members {
			if draft.selected[row.host.CatalogID] {
				delete(draft.selected, row.host.CatalogID)
				if draft.mode == workspaceDraftEdit {
					draft.order = slices.DeleteFunc(draft.order, func(id string) bool {
						return id == row.host.CatalogID
					})
				}
			}
		}
	} else {
		for _, row := range members {
			id := row.host.CatalogID
			if draft.selected[id] {
				continue
			}
			if state.workspaceDraftSelectionCount() >= maxWorkspaceTemplateHosts {
				draft.error = state.messages.WorkspaceDraftLimit
				break
			}
			draft.selected[id] = true
			if draft.mode == workspaceDraftEdit {
				draft.order = append(draft.order, id)
			}
		}
	}
	state.workspaceDraftCountChanged()
}

func (state *State) workspaceDraftModeLabel() string {
	return state.workspaceDraftModeName(state.workspaceDraft.mode)
}

// workspaceDraftModeName names one of the two modes the draft switch offers.
func (state *State) workspaceDraftModeName(mode workspaceDraftMode) string {
	if mode == workspaceDraftFixed {
		return state.messages.WorkspaceDraftFixed
	}
	return state.messages.WorkspaceDraftTemporary
}

// workspaceDraftModeChoices are the two modes the switch offers; the edit mode
// has no switch and shows its fields instead.
var workspaceDraftModeChoices = [...]workspaceDraftMode{workspaceDraftTemporary, workspaceDraftFixed}

// declareWorkspaceDraft declares the draft dialog in the window page. The
// mode switch and the name field are page controls; the preview, the search
// field and the host list keep their hand-painted components in the body slot
// where their exact geometry is the dialog's own.
func (state *State) declareWorkspaceDraft(c ui.C) {
	if state.modal != modalWorkspaceDraft {
		return
	}
	draft, m := &state.workspaceDraft, state.messages
	// The mode switch writes the new mode during this paint. The dialog was
	// laid out from the mode at the start of the paint, so the rest of the paint
	// reads that same mode; the next paint shows the new one.
	mode := draft.mode
	title, submit := m.WorkspaceDraftTitleTemporary, m.WorkspaceDraftLaunch
	var initial gfx.ID
	description := m.WorkspaceDraftDescription
	if mode == workspaceDraftFixed {
		title, submit, initial = m.WorkspaceDraftTitleFixed, m.WorkspaceDraftSave, hitWorkspaceDraftName
	} else if mode == workspaceDraftEdit {
		title, submit, initial = m.WorkspaceDraftTitleEdit, m.WorkspaceDraftSaveChanges, hitWorkspaceDraftName
		description = m.WorkspaceDraftEditDescription
	}
	const width float32 = 860
	bodyHeight := min(440, max(0, c.Bounds().Height-236))
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: width,
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitWorkspaceDraftDismiss, CloseID: hitWorkspaceDraftCancel, InitialFocusID: initial,
	})
	bodyWidth := width - 2*(c.Theme().Metrics.PadSurface+c.Theme().Metrics.GapItem)
	dialog.Body(func() {
		ui.Column(c).Height(bodyHeight).Children(func() {
			if mode != workspaceDraftEdit {
				ui.Row(c).Height(32).Width(min(300, bodyWidth)).MarginSides(0, 0, 12, 0).Children(func() {
					choice := ui.Choice(c, &draft.mode, workspaceDraftModeChoices[:], state.workspaceDraftModeName).Fill()
					// Any press on the switch clears the error, including a press
					// on the mode that is already chosen.
					if _, clicked := choice.ChosenIndex(); clicked {
						draft.error = ""
					}
				})
			}
			if mode == workspaceDraftFixed || mode == workspaceDraftEdit {
				theme := c.Theme()
				metrics := theme.Metrics
				gap := metrics.GapItem / 2
				ui.Column(c).Height(60).MarginSides(0, 0, 8, 0).Children(func() {
					ui.Text(c, m.WorkspaceDraftName).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).
						Color(theme.Foreground).LineHeight(metrics.FontSizeM + 4).Ellipsis()
					ui.TextFieldID(c, hitWorkspaceDraftName, &draft.nameField, m.WorkspaceDraftName, m.WorkspaceDraftNamePlaceholder).
						Height(metrics.ControlHeight).MarginSides(gap, 0, 0, 0)
				})
			}
			rest := max(0, bodyHeight-usedHeight(mode))
			ui.Column(c).Height(rest).Children(func() {
				state.declareWorkspaceDraftRest(c, bodyWidth, rest)
			})
		})
	})
	dialog.Footer(func() {
		if ui.Button(c, m.Cancel).Secondary().Shrink(0).Clicked() {
			c.After(func() { state.clickWorkspaceDraft(hitWorkspaceDraftCancel) })
		}
		if ui.Button(c, submit).Shrink(0).Clicked() {
			c.After(func() { state.clickWorkspaceDraft(hitWorkspaceDraftSubmit) })
		}
	})
	if dialog.Dismissed() {
		state.clickWorkspaceDraft(hitWorkspaceDraftDismiss)
	}
}

// usedHeight is the room the mode switch and the name field take above the
// draft's preview and host list.
func usedHeight(mode workspaceDraftMode) float32 {
	used := float32(0)
	if mode != workspaceDraftEdit {
		used += 44
	}
	if mode == workspaceDraftFixed || mode == workspaceDraftEdit {
		used += 68
	}
	return used
}

// declareWorkspaceDraftRest declares everything below the draft's fields: the
// layout preview, the host search and the host list, with the hint on the last
// line. width and height are the room the dialog gives it.
func (state *State) declareWorkspaceDraftRest(c ui.C, width, height float32) {
	wide := width >= 620
	hostWidth := width
	if wide {
		hostWidth = width * .48
	}
	ui.Column(c).FillWidth().Height(height).Children(func() {
		if wide {
			ui.Row(c).Grow(1).MinHeight(0).MarginBottom(10).Children(func() {
				ui.Column(c).Width(hostWidth).FillHeight().Children(func() {
					state.declareWorkspaceDraftHostPane(c, hostWidth, max(0, height-32))
				})
				ui.Slot(c).Width(24).Shrink(0)
				ui.Column(c).Grow(1).FillHeight().Children(func() {
					state.declareWorkspaceDraftPreview(c, max(0, width-hostWidth-24))
				})
			})
		} else {
			ui.Column(c).Height(184).Shrink(0).Children(func() {
				state.declareWorkspaceDraftPreview(c, width)
			})
			ui.Slot(c).Height(12).Shrink(0)
			ui.Column(c).Grow(1).MinHeight(0).MarginBottom(10).Children(func() {
				state.declareWorkspaceDraftHostPane(c, width, max(0, height-184-12-32))
			})
		}
		state.declareWorkspaceDraftHint(c, width, wide)
	})
}

// declareWorkspaceDraftPreview declares the layout preview: its heading and
// count, the preset strip, and the cards in a page scroll area.
func (state *State) declareWorkspaceDraftPreview(c ui.C, width float32) {
	draft, m, pal := &state.workspaceDraft, state.messages, state.pal
	items := state.workspaceItemsForRegion(workspaceCardRegion{view: workspaceCardsDraft})
	draft.previewContent = workspaceCardContentHeight(len(items), draft.layout)
	ui.Row(c).Height(22).FillWidth().Children(func() {
		ui.Text(c, m.WorkspaceDraftPreview).FontSize(13).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(22).Ellipsis().Grow(1).Shrink(1)
		ui.Text(c, fmt.Sprintf(m.WorkspaceDraftSelectedFmt, state.workspaceDraftSelectionCount())).
			FontSize(12).Color(pal.muted).LineHeight(22).Shrink(0)
	})
	strip := workspacePresetStripHeight(width)
	ui.Slot(c).FillWidth().Height(strip).Shrink(0).MarginTop(8).Draw(func(d *ui.DrawContext) {
		state.paintWorkspaceLayoutPresets(d.Canvas, d.Rect, hitWorkspaceDraftPresets, 0, draft.layout)
	})
	if len(items) == 0 {
		ui.Slot(c).Grow(1).MinHeight(0).MarginTop(12).Draw(func(d *ui.DrawContext) {
			state.paintWorkspaceGridPreview(d.Canvas, d.Rect, nil, draft.layout)
		})
		return
	}
	scroll := ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().MarginTop(12).AlwaysScrollbars().
		ScrollbarIDs(hitWorkspaceDraftPreviewScrollbar, gfx.ID{}).ScrollbarLabels(m.WorkspaceDraftPreview, "").
		HitWith(hitWorkspaceDraftPreviewScroll, m.WorkspaceDraftPreview, gfx.HitOptions{Role: gfx.RoleGroup})
	area := scroll.ScrollState()
	if draft.previewScroll != draft.previewScrollSynced {
		area.ScrollTo(draft.previewScroll)
		draft.previewScrollSynced = draft.previewScroll
		c.Canvas().RequestFrame()
	}
	scroll.Children(func() {
		contentWidth := max(0, width-10)
		content := ui.Stack(c).Width(contentWidth).Height(draft.previewContent)
		content.Children(func() {
			ui.Slot(c).Absolute().Inset(0).Draw(func(d *ui.DrawContext) {
				state.paintWorkspaceCards(d.Canvas, d.Rect, d.Rect, items, draft.layout, workspaceCardsDraft, "", 0)
			})
			if draft.mode == workspaceDraftEdit {
				state.declareWorkspaceDraftRemoveButtons(c, contentWidth, items, draft.layout)
			}
		})
	})
	scroll.DrawOver(func(d *ui.DrawContext) {
		draft.previewBounds = d.Rect
		draft.previewScroll = area.Offset
		draft.previewScrollSynced = area.Offset
	})
}

// declareWorkspaceDraftHostPane declares the host search field and the host
// list below it.
func (state *State) declareWorkspaceDraftHostPane(c ui.C, width, paneHeight float32) {
	draft, m := &state.workspaceDraft, state.messages
	ui.TextFieldID(c, hitWorkspaceDraftSearch, &draft.queryField, m.WorkspaceDraftHostSearch, m.WorkspaceDraftHostSearch).
		Icon(icons.Search).Height(34).MarginSides(0, 0, 12, 0)
	state.adoptWSLDistros() // the distributions join the list when wsl.exe answers
	rows := state.workspaceDraftRows()
	draft.rowCache = rows
	if viewport := paneHeight - 46; viewport > 0 &&
		draft.commandHost != 0 && draft.commandHost != draft.revealedCommandHost {
		state.revealWorkspaceCommandField(rows, viewport)
		draft.revealedCommandHost = draft.commandHost
	}
	if len(rows) == 0 {
		ui.Text(c, m.WorkspaceDraftNoHosts).FontSize(metrics.FontSizeM).Color(state.pal.muted).
			LineHeight(24).TextCenter().MarginSides(24, 0, 0, 0)
		return
	}
	// The hand-painted list kept a 10px gutter for its scrollbar; the page bar
	// floats over the edge, so the room is kept as padding to keep the rows at
	// their old width.
	list := ui.List(c, &draft.rows, len(rows), state.declareWorkspaceDraftRow).
		Grow(1).MinHeight(0).FillWidth().PadRight(10).Scrollbars().Label(m.WorkspaceDraftHosts)
	area := list.ScrollState()
	if draft.scroll != draft.scrollSynced {
		area.ScrollTo(draft.scroll)
		draft.scrollSynced = draft.scroll
		c.Canvas().RequestFrame()
	}
	list.DrawOver(func(d *ui.DrawContext) {
		draft.bounds = d.Rect
		draft.content = area.Content
		draft.scroll = area.Offset
		draft.scrollSynced = area.Offset
	})
}

// declareWorkspaceDraftHint declares the hint or error on the draft's last line.
func (state *State) declareWorkspaceDraftHint(c ui.C, width float32, wide bool) {
	draft, m := &state.workspaceDraft, state.messages
	hint, color := m.WorkspaceDraftLimit, state.pal.muted
	if draft.mode == workspaceDraftTemporary {
		hint = m.WorkspaceDraftTemporaryHint
	} else {
		hint = m.WorkspaceDraftFixedHint
	}
	if !wide {
		hint = fmt.Sprintf(m.WorkspaceDraftSelectedFmt, state.workspaceDraftSelectionCount()) + " · " + hint
	}
	if draft.error != "" {
		hint, color = draft.error, state.pal.danger
	}
	ui.Text(c, hint).FontSize(12).Color(color).LineHeight(22).Ellipsis().Height(22).Shrink(0)
}

// declareWorkspaceDraftRow declares one row of the host list: a collapsible
// group header or a host with its checkbox and, when expanded, its command
// field. The row's paint draws the words and the icon; its controls are page
// elements under the application's identities.
func (state *State) declareWorkspaceDraftRow(row ui.ListRow) {
	draft, c := &state.workspaceDraft, row.C
	if row.Index < 0 || row.Index >= len(draft.rowCache) {
		return
	}
	r := draft.rowCache[row.Index]
	m := state.messages
	if r.group != nil {
		group, index := *r.group, r.group.groupIndex
		id := hitWorkspaceDraftGroupList.At(index)
		// The hand-painted group row was 38 high inside its 42px slot; the margin
		// keeps the list's stride while the row, its hit and its contents are 38.
		row.Element.Height(workspaceDraftGroupRowHeight-4).MarginSides(0, 0, 4, 0).Items(ui.Center).
			HitWith(id, group.label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: group.expanded}).
			Draw(func(d *ui.DrawContext) { state.paintWorkspaceDraftGroup(d.Canvas, group, d.Rect, id) }).
			Children(func() {
				ui.Slot(c).Width(30).Shrink(0)
				checked := group.count > 0 && group.selected == group.count
				mixed := group.selected > 0 && group.selected < group.count
				ui.Checkbox(c, state.pageFlag(checked), "").Mixed(mixed).Shrink(0).
					Hit(hitWorkspaceDraftGroupCheckList.At(index), group.label)
				ui.Slot(c).Grow(1)
			})
		return
	}
	host, selected := r.host, draft.selected[r.host.CatalogID]
	disabled := !selected && state.workspaceDraftSelectionCount() >= maxWorkspaceTemplateHosts
	configurable := state.workspaceDraftCommandConfigurable(host)
	height := workspaceDraftHostRowHeight
	if configurable {
		height += 66
	}
	id := hitWorkspaceDraftHostList.At(r.hostIndex)
	row.Element.Height(height).MarginSides(0, 0, 0, workspaceDraftHostIndent).
		HitWith(id, host.Label, gfx.HitOptions{Role: gfx.RoleCheckbox, Checked: selected, Disabled: disabled}).
		Draw(func(d *ui.DrawContext) { state.paintWorkspaceDraftHost(d, host, id, selected, disabled) }).
		Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				head := ui.Row(c).Height(56).FillWidth().Items(ui.Center)
				head.Children(func() {
					if selected && workspaceCommandHost(host) {
						ui.Row(c).Grow(1).MinWidth(0).Height(24).MarginSides(4, 8, 28, 46).
							Items(ui.Center).Gap(8).Children(func() {
							ui.Text(c, host.Label).FontSize(13).Weight(gfx.WeightSemibold).Color(state.pal.text).
								Ellipsis().Shrink(1).MinWidth(0)
							ui.Button(c, m.WorkspaceCommandField).XSmall().Outline().Height(24).Shrink(0).
								Hit(hitWorkspaceCommandEdit.At(int(host.ID)), m.WorkspaceCommandField)
						})
					} else {
						ui.Slot(c).Grow(1)
					}
					ui.Checkbox(c, state.pageFlag(selected), "").Shrink(0).MarginSides(0, 8, 0, 0).
						Hit(id, host.Label).Disabled(disabled)
				})
				if configurable {
					ui.Text(c, m.WorkspaceCommandOrderHint).FontSize(12).Color(state.pal.text).LineHeight(22).
						Ellipsis().Height(22).Shrink(0).MarginSides(0, 8, 0, 8)
					ui.TextFieldID(c, hitWorkspaceCommandField.At(int(host.ID)), draft.commandField(host.CatalogID),
						m.WorkspaceCommandField, m.Tools.LoginCommandPlaceholder).Height(32).MarginSides(8, 8, 0, 8)
				}
			})
		})
}

// declareWorkspaceDraftRemoveButtons declares the close button of every card
// the draft edits, over the cards in the scroll content.
func (state *State) declareWorkspaceDraftRemoveButtons(c ui.C, width float32, items []workspaceArrangementItem, spec layoutcore.LayoutSpec) {
	cells := workspaceLayoutCells(len(items), spec)
	content := gfx.Rect{Width: width, Height: state.workspaceDraft.previewContent}
	for index, item := range items {
		if index >= len(cells) {
			continue
		}
		cell := pixelBounds(content, cells[index]).Inset(gfx.All(4))
		name := item.host
		if host, ok := state.workspaceHostByID(item.host); ok {
			name = host.Label
		}
		hint := fmt.Sprintf(state.messages.WorkspaceDraftRemoveFmt, name)
		state.declareIconButton(c, hitWorkspaceDraftRemoveItem.At(index), iconClose, hint, false, iconButtonDanger).
			Size(26, 26).
			Absolute().
			Left(cell.X + cell.Width - 34).
			Top(cell.Y + cell.Height - 34)
	}
}

// revealWorkspaceCommandField scrolls the list so an expanded command field
// opens on screen: the host's row grows by 66px, and a field that opens below
// the fold is where nobody types.
func (state *State) revealWorkspaceCommandField(rows []workspaceDraftRow, viewport float32) {
	draft := &state.workspaceDraft
	if viewport <= 0 {
		return
	}
	top := float32(0)
	for index := range rows {
		if rows[index].group != nil {
			top += workspaceDraftGroupRowHeight
			continue
		}
		height := workspaceDraftHostRowHeight
		if rows[index].host.ID == draft.commandHost {
			height += 66
			content := workspaceDraftListHeight(rows) + 66
			draft.scroll = min(max(0, top+height-viewport), max(0, content-viewport))
			return
		}
		top += height
	}
}

// workspaceDraftCommandConfigurable is whether the host's row is expanded
// with its command fields: the selected command host that is the draft's
// current commandHost.
func (state *State) workspaceDraftCommandConfigurable(host Host) bool {
	return state.workspaceDraft.selected[host.CatalogID] && workspaceCommandHost(host) && state.workspaceDraft.commandHost == host.ID
}

// paintWorkspaceDraftGroup draws one collapsible header of the host list: a
// chevron, the group's name, and how many terminals it holds.
func (state *State) paintWorkspaceDraftGroup(canvas *gfx.Canvas, group workspaceDraftGroup, bounds gfx.Rect, id gfx.ID) {
	pal := state.pal
	background := pal.surface
	if canvas.Hovered() == id {
		background = pal.surfaceSoft
	}
	canvas.Rounded(bounds, radius.control, background)
	center := bounds.Y + bounds.Height/2
	chevron := iconChevronRight
	if group.expanded {
		chevron = iconChevronDown
	}
	paintIcon(canvas, uint8(chevron), gfx.Rect{X: bounds.X + 10, Y: center - 8, Width: 16, Height: 16}, pal.muted)
	x := bounds.X + 70
	right := bounds.X + bounds.Width - 8
	count := fmt.Sprintf("(%d)", group.count)
	countWidth := canvas.TextWidth(count, 12)
	labelWidth := min(canvas.TextWidthWeight(group.label, 13, gfx.WeightSemibold), max(0, right-x-countWidth-6))
	monitorLabel(canvas, elide(canvas, group.label, 13, gfx.WeightSemibold, labelWidth), gfx.Rect{X: x, Y: center - 11, Width: labelWidth, Height: 22}, 13, pal.text, gfx.WeightSemibold)
	monitorLabel(canvas, count, gfx.Rect{X: x + labelWidth + 6, Y: center - 11, Width: countWidth + 4, Height: 22}, 12, pal.faint, gfx.WeightRegular)
}

// paintWorkspaceDraftHost draws the host row's background, icon, name and address.
func (state *State) paintWorkspaceDraftHost(d *ui.DrawContext, host Host, id gfx.ID, selected, disabled bool) {
	canvas, bounds, pal := d.Canvas, d.Rect, state.pal
	header := bounds
	header.Height = min(header.Height, 56)
	target := float32(0)
	if selected {
		target = 1
	}
	amount := canvas.Animate(id, 0, target, gfx.Motion{Duration: 120 * time.Millisecond, Easing: gfx.EaseOut})
	background := pal.surface.Mix(pal.accentSoft, amount)
	if !disabled && canvas.Hovered() == id {
		background = background.Mix(pal.surfaceSoft, .6)
	}
	canvas.Rounded(header, radius.control, background)
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(header, radius.control, 1.5, pal.accent, background)
	}
	state.paintHostCatalogIcon(canvas, host, gfx.Rect{X: header.X + 10, Y: header.Y + 16, Width: 24, Height: 24}, background)
	width := max(0, header.Width-86)
	// A selected SSH host lays out its name together with the command button.
	if !selected || !workspaceCommandHost(host) {
		color := pal.text
		if disabled {
			color = pal.faint
		}
		monitorLabel(canvas, host.Label, gfx.Rect{X: header.X + 46, Y: header.Y + 7, Width: width, Height: 22}, 13, color, gfx.WeightSemibold)
	}
	detail := state.sessionAddress(host)
	if host.ID == localHostID {
		detail = state.messages.WorkspaceLocalTerminalHint
		if shell, ok := workspaceLocalShellRef(host.CatalogID); ok {
			detail = shell
		}
	}
	monitorLabel(canvas, elide(canvas, detail, 12, gfx.WeightRegular, width), gfx.Rect{X: header.X + 46, Y: header.Y + 29, Width: width, Height: 20}, 12, pal.muted.Mix(pal.text, .08*amount), gfx.WeightRegular)
}

func (state *State) workspaceDraftKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalWorkspaceDraft || event.Release {
		return gfx.EventResult{}
	}
	if event.Key == gfx.KeyEscape {
		if state.workspaceDraft.mode == workspaceDraftEdit {
			state.workspaceTemplateFocusPending = state.workspaceTemplateLauncherFocus(state.workspaceDraft.editID)
		}
		state.closeModal()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if (event.Key == gfx.KeyEnter && event.Modifiers&^gfx.ModifierShift == 0 && !event.Repeat) &&
		(state.EditorFor(event.Target) != nil || event.Target == hitWorkspaceDraftSubmit) {
		if editor := state.EditorFor(event.Target); editor != nil {
			if _, composing := editor.Composition(); composing {
				return gfx.EventResult{}
			}
		}
		state.submitWorkspaceDraft()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	if event.Target == hitWorkspaceDraftPreviewScroll {
		draft := &state.workspaceDraft
		viewport := draft.previewBounds.Height
		maximum := max(0, draft.previewContent-viewport)
		step := float32(0)
		switch event.Key {
		case gfx.KeyDown:
			step = 32
		case gfx.KeyUp:
			step = -32
		case gfx.KeyPageDown:
			step = max(viewport, 32)
		case gfx.KeyPageUp:
			step = -max(viewport, 32)
		case gfx.KeyHome:
			step = -draft.previewScroll
		case gfx.KeyEnd:
			step = maximum - draft.previewScroll
		default:
			return gfx.EventResult{}
		}
		before := draft.previewScroll
		draft.previewScroll = min(max(0, before+step), maximum)
		return gfx.EventResult{Handled: true, Repaint: draft.previewScroll != before}
	}
	return gfx.EventResult{}
}

// workspaceDraftScroll gives the wheel over the draft's lists to the page's
// scroll areas and keeps it from reaching the window behind the dialog.
func (state *State) workspaceDraftScroll(event gfx.ScrollEvent) gfx.EventResult {
	if state.modal != modalWorkspaceDraft {
		return gfx.EventResult{}
	}
	if pointInRect(event.X, event.Y, state.workspaceDraft.previewBounds) ||
		pointInRect(event.X, event.Y, state.workspaceDraft.bounds) {
		result := state.windowPage().RouteWheel(event)
		result.Handled = true
		return result
	}
	return gfx.EventResult{Handled: true}
}
