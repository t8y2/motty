package workbench

import (
	"slices"
	"strings"

	"github.com/vivaui/viva/ui"
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
	columns   uint8
	items     []workspaceArrangementItem
	revision  uint64
	scroll    float32
	bounds    ui.Rect
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
	columns := state.managedWorkspaceColumns[workspace]
	revision, _ := state.deck.Revision(workspace)
	state.workspaceLayoutDraft = workspaceArrangement{workspace: workspace, columns: columns, items: items, revision: revision}
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
	if err := state.deck.SetGrid(draft.workspace, order, int(draft.columns)); err != nil {
		draft.err = state.messages.WorkspaceLayoutStale
		return
	}
	state.managedWorkspaceColumns[draft.workspace] = draft.columns
	if _, temporary := state.temporaryWorkspaceColumns[draft.workspace]; temporary {
		state.temporaryWorkspaceColumns[draft.workspace] = draft.columns
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
		if !moveWorkspaceItem(order, from, to) {
			return
		}
		state.templates[index].HostIDs = order
		state.save()
		return
	}
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

func orderedWorkspaceLaunch(plan WorkspaceLaunchPlan, ids []string) WorkspaceLaunchPlan {
	hosts := make([]Host, 0, len(plan.Hosts))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if index := slices.IndexFunc(plan.Hosts, func(host Host) bool { return host.CatalogID == id }); index >= 0 {
			hosts = append(hosts, plan.Hosts[index])
		}
	}
	plan.Hosts = hosts
	return plan
}
