package workbench

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
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
	}
	state.workspaceDraft = draft
	state.selectedWorkspaceTemplateID = template.ID
	state.workspaceLibraryExpanded = true
	state.modal = modalWorkspaceDraft
}

func (state *State) clickWorkspaceDraft(id gfx.ID) {
	draft := &state.workspaceDraft
	switch {
	case id == hitWorkspaceDraftDismiss || id == hitWorkspaceDraftCancel:
		if draft.mode == workspaceDraftEdit {
			state.workspaceTemplateFocusPending = state.workspaceTemplateLauncherFocus(draft.editID)
		}
		state.closeModal()
	case id == hitWorkspaceDraftTemporary:
		if draft.mode == workspaceDraftEdit {
			return
		}
		draft.mode = workspaceDraftTemporary
		draft.error = ""
	case id == hitWorkspaceDraftFixed:
		if draft.mode == workspaceDraftEdit {
			return
		}
		draft.mode = workspaceDraftFixed
		draft.error = ""
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
	name := strings.TrimSpace(draft.name.String())
	if draft.mode == workspaceDraftFixed && name == "" {
		draft.error = state.messages.WorkspaceDraftNameRequired
		return
	}
	if draft.mode == workspaceDraftEdit {
		state.submitWorkspaceDraftEdit(name, hostIDs)
		return
	}

	plan, err := PlanWorkspaceLaunch(state.workspaceHosts(), hostIDs, draft.layout)
	if err != nil {
		draft.error = state.messages.WorkspaceDraftHostsRequired
		return
	}
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

func (state *State) submitWorkspaceDraftEdit(name string, hostIDs []string) {
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

func (state *State) workspaceDraftModeLabel() string {
	if state.workspaceDraft.mode == workspaceDraftFixed {
		return state.messages.WorkspaceDraftFixed
	}
	return state.messages.WorkspaceDraftTemporary
}

func (state *State) paintWorkspaceDraft(canvas *gfx.Canvas, viewport gfx.Rect) {
	state.paintWorkspaceDraftDialog(canvas, viewport)
}

func (state *State) paintWorkspaceDraftDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	draft, m, theme := &state.workspaceDraft, state.messages, state.Theme()
	title, submit := m.WorkspaceDraftTitleTemporary, m.WorkspaceDraftLaunch
	initial := hitWorkspaceDraftTemporary
	description := m.WorkspaceDraftDescription
	if draft.mode == workspaceDraftFixed {
		title, submit, initial = m.WorkspaceDraftTitleFixed, m.WorkspaceDraftSave, hitWorkspaceDraftName
	} else if draft.mode == workspaceDraftEdit {
		title, submit, initial = m.WorkspaceDraftTitleEdit, m.WorkspaceDraftSaveChanges, hitWorkspaceDraftName
		description = m.WorkspaceDraftEditDescription
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitWorkspaceDraftDismiss, CloseID: hitWorkspaceDraftCancel,
		InitialFocusID: initial, ContentSize: gfx.Size{Width: 860}, FitBody: true,
		BodyHeight: min(440, max(0, viewport.Height-236)),
	})
	layout := dialog.Begin(title, description)
	body := layout.Body
	y := body.Y
	if draft.mode != workspaceDraftEdit {
		modes := [2]components.SegmentOption{
			{ID: hitWorkspaceDraftTemporary, Label: m.WorkspaceDraftTemporary},
			{ID: hitWorkspaceDraftFixed, Label: m.WorkspaceDraftFixed},
		}
		selectedMode := hitWorkspaceDraftTemporary
		if draft.mode == workspaceDraftFixed {
			selectedMode = hitWorkspaceDraftFixed
		}
		components.BeginSegmentedControl(canvas, theme, gfx.Rect{X: body.X, Y: body.Y, Width: min(300, body.Width), Height: 32}, modes[:], selectedMode).Paint()
		y += 44
	}
	if draft.mode == workspaceDraftFixed || draft.mode == workspaceDraftEdit {
		field := components.BeginField(canvas, theme, gfx.Rect{X: body.X, Y: y, Width: body.Width, Height: 60}).For(hitWorkspaceDraftName)
		control := field.Paint(m.WorkspaceDraftName, "", "")
		components.PaintInput(canvas, theme, control, components.InputSnapshot{
			ID: hitWorkspaceDraftName, Label: m.WorkspaceDraftName,
			Value: draft.name.String(), Placeholder: m.WorkspaceDraftNamePlaceholder,
			Focused:         canvas.Focused() == hitWorkspaceDraftName,
			PlatformEditing: canvas.Focused() == hitWorkspaceDraftName && draft.name.String() != "",
		})
		y += 68
	}

	// The preview sits beside the host list when there is room; compact dialogs
	// keep one short layout strip above the list and never cover the footer.
	hostWidth := body.Width
	wide := body.Width >= 620
	if wide {
		hostWidth = body.Width * .48
		state.paintWorkspaceDraftLayout(canvas, gfx.Rect{X: body.X + hostWidth + 24, Y: y, Width: max(0, body.Width-hostWidth-24), Height: max(0, body.Y+body.Height-y-32)}, false)
	} else {
		state.paintWorkspaceDraftLayout(canvas, gfx.Rect{X: body.X, Y: y, Width: body.Width, Height: 184}, true)
		y += 196
	}
	components.PaintInputGroup(canvas, theme, gfx.Rect{X: body.X, Y: y, Width: hostWidth, Height: 34}, components.InputGroupSnapshot{
		ID: hitWorkspaceDraftSearch, Label: m.WorkspaceDraftHostSearch,
		Value: draft.query.String(), Placeholder: m.WorkspaceDraftHostSearch,
		NativeEditing: canvas.Focused() == hitWorkspaceDraftSearch && draft.query.String() != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: icons.Search},
	})
	y += 46
	draft.bounds = gfx.Rect{X: body.X, Y: y, Width: hostWidth, Height: max(0, body.Y+body.Height-y-32)}
	state.paintWorkspaceDraftHosts(canvas)
	hint, color := m.WorkspaceDraftLimit, state.pal.muted
	if draft.mode == workspaceDraftTemporary {
		hint = m.WorkspaceDraftTemporaryHint
	} else if draft.mode == workspaceDraftEdit {
		hint = m.WorkspaceDraftFixedHint
	} else {
		hint = m.WorkspaceDraftFixedHint
	}
	if !wide {
		hint = fmt.Sprintf(m.WorkspaceDraftSelectedFmt, state.workspaceDraftSelectionCount()) + " · " + hint
	}
	if draft.error != "" {
		hint, color = draft.error, state.pal.danger
	}
	monitorLabel(canvas, elide(canvas, hint, 12, gfx.WeightRegular, body.Width), gfx.Rect{X: body.X, Y: body.Y + body.Height - 22, Width: body.Width, Height: 22}, 12, color, gfx.WeightRegular)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitWorkspaceDraftCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitWorkspaceDraftSubmit, Label: submit},
	)
	dialog.End()
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
	hosts := state.workspaceDraftHosts()
	const rowHeight = float32(60)
	draft.content = float32(len(hosts)) * rowHeight
	area := components.BeginScrollArea(canvas, state.Theme(), draft.bounds, gfx.Size{Width: max(0, draft.bounds.Width-10), Height: draft.content}).
		Offset(0, draft.scroll).ReserveScrollbars().Viewport(hitWorkspaceDraftScroll, m.WorkspaceDraftHosts).
		VerticalScrollbar(hitWorkspaceDraftScrollbar, m.WorkspaceDraftHosts, "")
	geometry := area.BeginContent()
	draft.scroll = geometry.OffsetY
	count := state.workspaceDraftSelectionCount()
	for index, host := range hosts {
		box := gfx.Rect{X: geometry.Content.X, Y: geometry.Content.Y + float32(index)*rowHeight, Width: max(0, draft.bounds.Width-10), Height: 56}
		if box.Y+box.Height < geometry.Viewport.Y || box.Y > geometry.Viewport.Y+geometry.Viewport.Height {
			continue
		}
		selected := draft.selected[host.CatalogID]
		state.paintWorkspaceDraftHost(canvas, host, box, hitWorkspaceDraftHostList.At(index), selected, !selected && count >= maxWorkspaceTemplateHosts)
	}
	if len(hosts) == 0 {
		components.Empty(canvas, state.Theme(), geometry.Viewport, components.EmptyData{Title: m.WorkspaceDraftNoHosts})
	}
	area.EndContent()
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
	monitorLabel(canvas, elide(canvas, host.Label, 13, gfx.WeightSemibold, width), gfx.Rect{X: bounds.X + 46, Y: bounds.Y + 7, Width: width, Height: 22}, 13, color, gfx.WeightSemibold)
	detail := state.sessionAddress(host)
	if host.ID == localHostID {
		detail = state.messages.WorkspaceLocalTerminalHint
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
	if event.Target == hitWorkspaceDraftTemporary || event.Target == hitWorkspaceDraftFixed {
		if state.workspaceDraft.mode == workspaceDraftEdit {
			return gfx.EventResult{}
		}
		options := [2]components.TabOption{
			{ID: hitWorkspaceDraftTemporary, Label: state.messages.WorkspaceDraftTemporary},
			{ID: hitWorkspaceDraftFixed, Label: state.messages.WorkspaceDraftFixed},
		}
		if next, ok := components.TabsKeyDown(event.Target, options[:], components.TabsHorizontal, components.DirectionLTR, event.Key); ok {
			return gfx.EventResult{Handled: true, Repaint: true, Focus: next, FocusVisible: true}
		}
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
