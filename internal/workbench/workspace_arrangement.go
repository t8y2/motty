package workbench

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

type workspaceArrangementItem struct {
	host    string
	pane    layoutcore.PaneID
	session uint32
}

// The open layout editor follows the live deck revision. Each completed edit
// updates only its split tree, preserving every session and connection.
type workspaceArrangement struct {
	workspace layoutcore.WorkspaceID
	layout    layoutcore.LayoutSpec
	items     []workspaceArrangementItem
	revision  uint64
	scroll    float32
	bounds    gfx.Rect
	err       string
}

func workspaceTemplateItems(template WorkspaceTemplate) []workspaceArrangementItem {
	items := make([]workspaceArrangementItem, len(template.HostIDs))
	for index, id := range template.HostIDs {
		items[index].host = id
	}
	return items
}

func (state *State) openWorkspaceArrangement() {
	workspace := state.deck.ActiveWorkspace()
	panes := state.deck.Panes(workspace, nil)
	items := make([]workspaceArrangementItem, len(panes))
	for index, pane := range panes {
		item := workspaceArrangementItem{pane: pane.ID, session: uint32(pane.SessionID)}
		if session, ok := state.sessionByIDPointer(item.session); ok {
			item.host = workspaceHostReference(session.host)
		}
		items[index] = item
	}
	revision, _ := state.deck.Revision(workspace)
	state.workspaceLayoutDraft = workspaceArrangement{workspace: workspace, layout: state.workspaceLayoutSpec(workspace), items: items, revision: revision}
	state.workspaceHeaderMenu = workspaceHeaderMenuLayout
}

func (state *State) applyWorkspaceArrangement() {
	draft := &state.workspaceLayoutDraft
	revision, ok := state.deck.Revision(draft.workspace)
	if !ok || state.deck.ActiveWorkspace() != draft.workspace || revision != draft.revision {
		draft.err = state.messages.WorkspaceLayoutStale
		return
	}
	order := make([]layoutcore.PaneID, len(draft.items))
	for index, item := range draft.items {
		order[index] = item.pane
	}
	if err := state.deck.SetLayout(draft.workspace, order, draft.layout); err != nil {
		draft.err = state.messages.WorkspaceLayoutStale
		return
	}
	state.managedWorkspaceLayouts[draft.workspace] = draft.layout
	if _, temporary := state.temporaryWorkspaceLayouts[draft.workspace]; temporary {
		state.temporaryWorkspaceLayouts[draft.workspace] = draft.layout
	}
	state.workspaceCardDrag = workspaceCardDrag{}
	state.workspaceViewCacheID = 0
	draft.revision, _ = state.deck.Revision(draft.workspace)
	draft.err = ""
	state.rebuildManagedWorkspace(draft.workspace)
}

func (state *State) moveWorkspaceTemplateCard(id string, from, to int) {
	for index, template := range state.templates {
		if template.ID != id {
			continue
		}
		order := slices.Clone(template.HostIDs)
		if !placeWorkspaceItem(order, from, to, template.Layout) {
			return
		}
		state.templates[index].HostIDs = order
		state.save()
		return
	}
}

// placeWorkspaceItem moves the item at from onto the place of the one at to. In
// a grid the others shift, as the cards read left to right; in every other
// layout the two trade places, because the cards' places are not a sequence.
func placeWorkspaceItem[T any](items []T, from, to int, spec layoutcore.LayoutSpec) bool {
	if spec.Kind == layoutcore.LayoutGrid || spec.Kind == layoutcore.LayoutTabs {
		return moveWorkspaceItem(items, from, to)
	}
	if from < 0 || to < 0 || from >= len(items) || to >= len(items) || from == to {
		return false
	}
	items[from], items[to] = items[to], items[from]
	return true
}

// Insertion order matches the visual reading order, including between rows.
func moveWorkspaceItem[T any](items []T, from, to int) bool {
	if from < 0 || to < 0 || from >= len(items) || to >= len(items) || from == to {
		return false
	}
	item := items[from]
	if from < to {
		copy(items[from:to], items[from+1:to+1])
	} else {
		copy(items[to+1:from+1], items[to:from])
	}
	items[to] = item
	return true
}

// orderedWorkspaceLaunch puts the planned hosts in the order of ids, which is
// also the order the plan's layout addresses its panes by. Hosts the catalog no
// longer has leave their positions out of a custom layout.
func orderedWorkspaceLaunch(plan WorkspaceLaunchPlan, ids []string) WorkspaceLaunchPlan {
	hosts := make([]Host, 0, len(plan.Hosts))
	kept := make([]int, 0, len(plan.Hosts))
	for position, id := range ids {
		id = strings.TrimSpace(id)
		if index := slices.IndexFunc(plan.Hosts, func(host Host) bool { return host.CatalogID == id }); index >= 0 {
			hosts = append(hosts, plan.Hosts[index])
			kept = append(kept, position+1)
		}
	}
	plan.Hosts = hosts
	layout, err := plan.Layout.Retain(kept, len(ids))
	if err != nil {
		layout = DefaultWorkspaceLayout(len(hosts))
	}
	plan.Layout = layout
	return plan
}

// permuteWorkspaceItems puts the items in the order a docked layout reads in:
// the n-th result is the item that was at position order[n].
func permuteWorkspaceItems[T any](items []T, order []int) []T {
	result := make([]T, len(order))
	for index, position := range order {
		result[index] = items[position-1]
	}
	return result
}

// dockWorkspaceTemplateCard records a layout drawn on a template's cards.
func (state *State) dockWorkspaceTemplateCard(id string, spec layoutcore.LayoutSpec, order []int) {
	for index := range state.templates {
		if state.templates[index].ID == id {
			state.templates[index].HostIDs = permuteWorkspaceItems(state.templates[index].HostIDs, order)
			state.templates[index].Layout = spec
			state.workspaceCardDrag = workspaceCardDrag{}
			state.save()
			return
		}
	}
}

// workspaceLayoutSpec is the arrangement the workspace's panes are in, as a
// spec over their reading order. The recorded choice is used while the live
// tree still has its shape; panes dragged around since, or added and closed,
// make it a custom layout of what is on screen, so the editor never offers a
// stale preset as selected.
func (state *State) workspaceLayoutSpec(workspace layoutcore.WorkspaceID) layoutcore.LayoutSpec {
	live, ok := state.deck.Layout(workspace)
	if !ok || live.Root() == 0 {
		return layoutcore.GridSpec(1)
	}
	order := live.Sessions(nil)
	if spec, managed := state.managedWorkspaceLayouts[workspace]; managed {
		if expected, err := layoutcore.NewLayout(order, spec); err == nil && live.SameShape(&expected) {
			return spec
		}
	}
	return live.Spec()
}
