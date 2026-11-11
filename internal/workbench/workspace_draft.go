package workbench

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
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
	// nameField is the page's handle on the same editor the dialog reads.
	nameField ui.Field
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
				draft.commandHost = 0
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
	key      string
	label    string
	count    int
	selected int
	expanded bool
	members  []workspaceDraftRow
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
	viewport := gfx.Rect{Width: c.Bounds().Width, Height: c.Bounds().Height}
	bodyHeight := min(440, max(0, viewport.Height-236))
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), viewport, components.DialogOptions{
		ContentSize: gfx.Size{Width: width}, FitBody: true,
	}).Layout(description)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: description, Width: width, Height: layout.Content.Height + bodyHeight,
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitWorkspaceDraftDismiss, CloseID: hitWorkspaceDraftCancel, InitialFocusID: initial,
	})
	bodyWidth := width - 2*(c.Theme().Metrics.PadSurface+c.Theme().Metrics.GapItem)
	dialog.Body(func() {
		ui.Column(c).Fill().Children(func() {
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
			ui.Slot(c).Fill().Draw(func(d *ui.DrawContext) {
				state.drawWorkspaceDraftRest(d.Canvas, d.Rect)
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

// drawWorkspaceDraftRest paints the part of the draft below its fields: the
// layout preview, the host search and the host list, with the hint on the
// body's last line. bounds is the room the page gave the slot.
func (state *State) drawWorkspaceDraftRest(canvas *gfx.Canvas, bounds gfx.Rect) {
	draft, m := &state.workspaceDraft, state.messages
	wide := bounds.Width >= 620
	hostWidth := bounds.Width
	y := bounds.Y
	if wide {
		hostWidth = bounds.Width * .48
		state.paintWorkspaceDraftLayout(canvas, gfx.Rect{X: bounds.X + hostWidth + 24, Y: y, Width: max(0, bounds.Width-hostWidth-24), Height: max(0, bounds.Y+bounds.Height-y-32)}, false)
	} else {
		state.paintWorkspaceDraftLayout(canvas, gfx.Rect{X: bounds.X, Y: y, Width: bounds.Width, Height: 184}, true)
		y += 196
	}
	components.PaintInputGroup(canvas, state.Theme(), gfx.Rect{X: bounds.X, Y: y, Width: hostWidth, Height: 34}, components.InputGroupSnapshot{
		ID: hitWorkspaceDraftSearch, Label: m.WorkspaceDraftHostSearch,
		Value: draft.query.String(), Placeholder: m.WorkspaceDraftHostSearch,
		NativeEditing: canvas.Focused() == hitWorkspaceDraftSearch && draft.query.String() != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: icons.Search},
	})
	y += 46
	draft.bounds = gfx.Rect{X: bounds.X, Y: y, Width: hostWidth, Height: max(0, bounds.Y+bounds.Height-y-32)}
	state.paintWorkspaceDraftHosts(canvas)
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
	monitorLabel(canvas, elide(canvas, hint, 12, gfx.WeightRegular, bounds.Width), gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height - 22, Width: bounds.Width, Height: 22}, 12, color, gfx.WeightRegular)
}

func (state *State) paintWorkspaceDraftLayout(canvas *gfx.Canvas, bounds gfx.Rect, compact bool) {
	draft, m, pal := &state.workspaceDraft, state.messages, state.pal
	countLabel := fmt.Sprintf(m.WorkspaceDraftSelectedFmt, state.workspaceDraftSelectionCount())
	countWidth := canvas.TextWidth(countLabel, 12) + 4
	monitorLabel(canvas, m.WorkspaceDraftPreview, gfx.Rect{X: bounds.X, Y: bounds.Y, Width: max(0, bounds.Width-countWidth-12), Height: 22}, 13, pal.text, gfx.WeightSemibold)
	monitorRight(canvas, countLabel, gfx.Rect{X: bounds.X + bounds.Width - countWidth, Y: bounds.Y, Width: countWidth, Height: 22}, 12, pal.muted, gfx.WeightRegular)
	strip := workspacePresetStripHeight(bounds.Width)
	state.paintWorkspaceLayoutPresets(canvas, gfx.Rect{X: bounds.X, Y: bounds.Y + 30, Width: bounds.Width, Height: strip}, hitWorkspaceDraftPresets, 0, draft.layout)
	previewY := 30 + strip + 12
	draft.previewBounds = gfx.Rect{X: bounds.X, Y: bounds.Y + previewY, Width: bounds.Width, Height: max(0, bounds.Height-previewY)}
	items := state.workspaceItemsForRegion(workspaceCardRegion{view: workspaceCardsDraft})
	draft.previewContent = workspaceCardContentHeight(len(items), draft.layout)
	area := components.BeginScrollArea(canvas, state.Theme(), draft.previewBounds, gfx.Size{Width: max(0, bounds.Width-10), Height: draft.previewContent}).Offset(0, draft.previewScroll).ReserveScrollbars().
		Viewport(hitWorkspaceDraftPreviewScroll, m.WorkspaceDraftPreview).VerticalScrollbar(hitWorkspaceDraftPreviewScrollbar, m.WorkspaceDraftPreview, "")
	geometry := area.BeginContent()
	draft.previewScroll = geometry.OffsetY
	if len(items) == 0 {
		state.paintWorkspaceGridPreview(canvas, geometry.Viewport, nil, draft.layout)
	} else {
		content := gfx.Rect{X: geometry.Content.X, Y: geometry.Content.Y, Width: max(0, bounds.Width-10), Height: draft.previewContent}
		state.paintWorkspaceCards(canvas, content, geometry.Viewport, items, draft.layout, workspaceCardsDraft, "", 0)
		if draft.mode == workspaceDraftEdit {
			state.paintWorkspaceDraftRemoveButtons(canvas, content, geometry.Viewport, items, draft.layout)
		}
	}
	area.EndContent()
}

func (state *State) paintWorkspaceDraftRemoveButtons(canvas *gfx.Canvas, content, viewport gfx.Rect, items []workspaceArrangementItem, spec layoutcore.LayoutSpec) {
	cells := workspaceLayoutCells(len(items), spec)
	for index, item := range items {
		if index >= len(cells) {
			continue
		}
		box := pixelBounds(content, cells[index]).Inset(gfx.All(4))
		if box.Y+box.Height <= viewport.Y || box.Y >= viewport.Y+viewport.Height {
			continue
		}
		remove := gfx.Rect{
			X: box.X + box.Width - 34, Y: box.Y + box.Height - 34,
			Width: 26, Height: 26,
		}
		name := item.host
		if host, ok := state.workspaceHostByID(item.host); ok {
			name = host.Label
		}
		state.paintIconButton(canvas, hitWorkspaceDraftRemoveItem.At(index), iconClose, remove,
			fmt.Sprintf(state.messages.WorkspaceDraftRemoveFmt, name), false, iconButtonDanger, state.pal.surfaceSoft)
	}
}

func (state *State) paintWorkspaceDraftHosts(canvas *gfx.Canvas) {
	draft, m := &state.workspaceDraft, state.messages
	state.adoptWSLDistros() // the distributions join the list when wsl.exe answers
	rows := state.workspaceDraftRows()
	draft.content = 0
	for _, row := range rows {
		if row.group != nil {
			draft.content += workspaceDraftGroupRowHeight
			continue
		}
		draft.content += workspaceDraftHostRowHeight
		if state.workspaceDraftCommandConfigurable(row.host) {
			draft.content += 66
		}
	}
	state.revealWorkspaceCommandFields(rows)
	area := components.BeginScrollArea(canvas, state.Theme(), draft.bounds, gfx.Size{Width: max(0, draft.bounds.Width-10), Height: draft.content}).
		Offset(0, draft.scroll).ReserveScrollbars().Viewport(hitWorkspaceDraftScroll, m.WorkspaceDraftHosts).
		VerticalScrollbar(hitWorkspaceDraftScrollbar, m.WorkspaceDraftHosts, "")
	geometry := area.BeginContent()
	draft.scroll = geometry.OffsetY
	count := state.workspaceDraftSelectionCount()
	y := geometry.Content.Y
	groupOrdinal := 0
	for _, row := range rows {
		height := workspaceDraftHostRowHeight
		configurable := false
		if row.group != nil {
			height = workspaceDraftGroupRowHeight
		} else if state.workspaceDraftCommandConfigurable(row.host) {
			configurable = true
			height += 66
		}
		box := gfx.Rect{X: geometry.Content.X, Y: y, Width: max(0, draft.bounds.Width-10), Height: height - 4}
		if row.group == nil {
			// A host sits under its group's header, one step in, so the list
			// reads like the sidebar's tree.
			box.X += workspaceDraftHostIndent
			box.Width = max(0, box.Width-workspaceDraftHostIndent)
		}
		y += height
		if box.Y+box.Height < geometry.Viewport.Y || box.Y > geometry.Viewport.Y+geometry.Viewport.Height {
			if row.group != nil {
				groupOrdinal++
			}
			continue
		}
		if row.group != nil {
			state.paintWorkspaceDraftGroup(canvas, *row.group, box, hitWorkspaceDraftGroupList.At(groupOrdinal), hitWorkspaceDraftGroupCheckList.At(groupOrdinal))
			groupOrdinal++
			continue
		}
		header := box
		header.Height = 56
		selected := draft.selected[row.host.CatalogID]
		state.paintWorkspaceDraftHost(canvas, row.host, header, hitWorkspaceDraftHostList.At(row.hostIndex), selected, !selected && count >= maxWorkspaceTemplateHosts)
		if selected && workspaceCommandHost(row.host) {
			components.BeginButton(canvas, state.Theme(), hitWorkspaceCommandEdit.At(int(row.host.ID)), gfx.Rect{X: header.X + header.Width - 128, Y: header.Y + 16, Width: 90, Height: 26}).XSmall().Outline(m.WorkspaceCommandField)
		}
		if configurable {
			state.paintWorkspaceCommandFields(canvas, row.host, gfx.Rect{X: box.X + 8, Y: box.Y + 60, Width: max(0, box.Width-16), Height: 58})
		}
	}
	if len(rows) == 0 {
		components.Empty(canvas, state.Theme(), geometry.Viewport, components.EmptyData{Title: m.WorkspaceDraftNoHosts})
	}
	area.EndContent()
}

// revealWorkspaceCommandFields scrolls the expanded command fields of the
// draft's command host into view: a row near the list's bottom grows by 66px
// when chosen, and fields that open below the fold are where nobody types.
func (state *State) revealWorkspaceCommandFields(rows []workspaceDraftRow) {
	draft := &state.workspaceDraft
	if draft.commandHost == 0 || draft.bounds.Height <= 0 {
		return
	}
	y := float32(0)
	for _, row := range rows {
		height := workspaceDraftHostRowHeight
		if row.group != nil {
			height = workspaceDraftGroupRowHeight
		} else {
			if row.host.ID != uint32(draft.commandHost) {
				y += height
				continue
			}
			y += height
			fieldBottom := y + 58
			if fieldBottom > draft.scroll+draft.bounds.Height {
				draft.scroll = min(fieldBottom-draft.bounds.Height, max(0, workspaceDraftListHeight(rows)+66-draft.bounds.Height))
			}
			return
		}
		y += height
	}
}

// workspaceDraftCommandConfigurable is whether the host's row is expanded
// with its command fields: the selected command host that is the draft's
// current commandHost.
func (state *State) workspaceDraftCommandConfigurable(host Host) bool {
	return state.workspaceDraft.selected[host.CatalogID] && workspaceCommandHost(host) && state.workspaceDraft.commandHost == host.ID
}

// paintWorkspaceDraftGroup paints one collapsible header of the host list: a
// chevron, a select-the-group checkbox, the group's name, and how many
// terminals it holds.
func (state *State) paintWorkspaceDraftGroup(canvas *gfx.Canvas, group workspaceDraftGroup, bounds gfx.Rect, id, checkID gfx.ID) {
	pal, theme := state.pal, state.Theme()
	background := pal.surface
	if canvas.Hovered() == id {
		background = pal.surfaceSoft
	}
	canvas.Rounded(bounds, radius.control, background)
	canvas.HitWithOptions(id, bounds, group.label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: group.expanded})
	// Labels draw from the top of their rect, so every element of the header
	// centers on the row's own middle line, like the sidebar's headers do.
	center := bounds.Y + bounds.Height/2
	chevron := iconChevronRight
	if group.expanded {
		chevron = iconChevronDown
	}
	paintIcon(canvas, uint8(chevron), gfx.Rect{X: bounds.X + 10, Y: center - 8, Width: 16, Height: 16}, pal.muted)
	slot := gfx.Rect{X: bounds.X + 30, Y: center - 14, Width: 36, Height: 28}
	side := components.CheckboxExtent(theme)
	checkbox := components.BeginCheckbox(canvas, theme, checkID, gfx.Rect{
		X: slot.X + (slot.Width-side)/2, Y: center - side/2, Width: side, Height: side,
	}, group.count > 0 && group.selected == group.count).Label(group.label).HitBounds(slot)
	if group.selected > 0 && group.selected < group.count {
		checkbox = checkbox.Indeterminate()
	}
	checkbox.Paint()
	x := bounds.X + 70
	right := bounds.X + bounds.Width - 8
	count := fmt.Sprintf("(%d)", group.count)
	countWidth := canvas.TextWidth(count, 12)
	// The count follows the name, like the sidebar's group headers, and a long
	// name gives way to it. The name is measured at the weight it is drawn at,
	// the one elide budgets against.
	labelWidth := min(canvas.TextWidthWeight(group.label, 13, gfx.WeightSemibold), max(0, right-x-countWidth-6))
	monitorLabel(canvas, elide(canvas, group.label, 13, gfx.WeightSemibold, labelWidth), gfx.Rect{X: x, Y: center - 11, Width: labelWidth, Height: 22}, 13, pal.text, gfx.WeightSemibold)
	monitorLabel(canvas, count, gfx.Rect{X: x + labelWidth + 6, Y: center - 11, Width: countWidth + 4, Height: 22}, 12, pal.faint, gfx.WeightRegular)
}

func (state *State) paintWorkspaceDraftHost(canvas *gfx.Canvas, host Host, bounds gfx.Rect, id gfx.ID, selected, disabled bool) {
	pal, theme := state.pal, state.Theme()
	target := float32(0)
	if selected {
		target = 1
	}
	amount := canvas.Animate(id, 0, target, gfx.Motion{Duration: 120 * time.Millisecond, Easing: gfx.EaseOut})
	background := pal.surface.Mix(pal.accentSoft, amount)
	if !disabled && canvas.Hovered() == id {
		background = background.Mix(pal.surfaceSoft, .6)
	}
	canvas.Rounded(bounds, radius.control, background)
	if canvas.FocusVisible() == id {
		canvas.RoundedBorder(bounds, radius.control, 1.5, pal.accent, background)
	}
	state.paintHostCatalogIcon(canvas, host, gfx.Rect{X: bounds.X + 10, Y: bounds.Y + 16, Width: 24, Height: 24}, background)
	color := pal.text
	if disabled {
		color = pal.faint
	}
	width := max(0, bounds.Width-86)
	if selected && workspaceCommandHost(host) {
		width = max(0, width-100)
	}
	monitorLabel(canvas, elide(canvas, host.Label, 13, gfx.WeightSemibold, width), gfx.Rect{X: bounds.X + 46, Y: bounds.Y + 7, Width: width, Height: 22}, 13, color, gfx.WeightSemibold)
	detail := state.sessionAddress(host)
	if host.ID == localHostID {
		detail = state.messages.WorkspaceLocalTerminalHint
		if shell, ok := workspaceLocalShellRef(host.CatalogID); ok {
			detail = shell
		}
	}
	monitorLabel(canvas, elide(canvas, detail, 12, gfx.WeightRegular, width), gfx.Rect{X: bounds.X + 46, Y: bounds.Y + 29, Width: width, Height: 20}, 12, pal.muted.Mix(pal.text, .08*amount), gfx.WeightRegular)
	side := components.CheckboxExtent(theme)
	checkbox := components.BeginCheckbox(canvas, theme, id, gfx.Rect{X: bounds.X + bounds.Width - 28, Y: bounds.Y + (bounds.Height-side)/2}, selected).
		Label(host.Label).HitBounds(bounds)
	if disabled {
		checkbox = checkbox.Disabled()
	}
	checkbox.Paint()
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
	if event.Target == hitWorkspaceDraftScroll {
		area := components.BeginScrollArea(nil, state.Theme(), state.workspaceDraft.bounds, gfx.Size{Width: state.workspaceDraft.bounds.Width, Height: state.workspaceDraft.content}).Offset(0, state.workspaceDraft.scroll)
		if _, y, ok := area.ScrollForKey(0, state.workspaceDraft.scroll, event.Key, 32); ok {
			state.workspaceDraft.scroll = y
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	if event.Target == hitWorkspaceDraftPreviewScroll {
		draft := &state.workspaceDraft
		area := components.BeginScrollArea(nil, state.Theme(), draft.previewBounds, gfx.Size{Width: draft.previewBounds.Width, Height: draft.previewContent}).Offset(0, draft.previewScroll)
		if _, y, ok := area.ScrollForKey(0, draft.previewScroll, event.Key, 32); ok {
			draft.previewScroll = y
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	return gfx.EventResult{}
}

func (state *State) workspaceDraftScroll(event gfx.ScrollEvent) gfx.EventResult {
	if state.modal != modalWorkspaceDraft {
		return gfx.EventResult{}
	}
	if pointInRect(event.X, event.Y, state.workspaceDraft.previewBounds) {
		draft := &state.workspaceDraft
		area := components.BeginScrollArea(nil, state.Theme(), draft.previewBounds, gfx.Size{Width: draft.previewBounds.Width, Height: draft.previewContent}).Offset(0, draft.previewScroll)
		before := draft.previewScroll
		draft.previewScroll = min(max(0, before+event.StepY(32)), area.Geometry().MaxOffsetY)
		return gfx.EventResult{Handled: true, Repaint: before != draft.previewScroll}
	}
	if !pointInRect(event.X, event.Y, state.workspaceDraft.bounds) {
		return gfx.EventResult{Handled: true}
	}
	area := components.BeginScrollArea(nil, state.Theme(), state.workspaceDraft.bounds,
		gfx.Size{Width: state.workspaceDraft.bounds.Width, Height: state.workspaceDraft.content}).
		Offset(0, state.workspaceDraft.scroll)
	geometry := area.Geometry()
	before := state.workspaceDraft.scroll
	state.workspaceDraft.scroll = min(max(0, before+event.StepY(32)), geometry.MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: before != state.workspaceDraft.scroll}
}
