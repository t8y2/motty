package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// The hosts open to agents are read in a dialog of their own, opened by the
// View button beside the mode choice. It borrows the monitor panel creator's
// grouped cards, host icons and color tints, but only reads: the permissions
// are changed by the mode choice and each host's Advanced switch, not here.
// The dialog is a page control, so the page owns its focus, hits, scrolling
// and keyboard; the list virtualizes its rows so a catalog of thousands paints
// only what is on screen.

var (
	hitAgentHostsClose = tree.ID(aiSettingsHits, "agent-hosts-close")
	hitAgentHostsQuery = tree.ID(aiSettingsHits, "agent-hosts-query")
)

// agentHostRow is one virtual row: a group header, or up to a row's worth of
// host cards.
type agentHostRow struct {
	header string
	hosts  []Host
}

type agentHostRowsKey struct {
	hash    uint64
	count   int
	query   string
	columns int
}

type agentHostsViewUI struct {
	query      text.Editor
	queryField ui.Field
	queryBound bool
	list       ui.ListState
	listInit   bool
	rows       []agentHostRow
	rowsKey    agentHostRowsKey
	rowsReady  bool
	total      int
	shown      int
}

func (state *State) openAgentHostsView() {
	state.ai.cfg.agentHostsView = agentHostsViewUI{}
	state.modal = modalAgentHosts
}

func (state *State) agentHostsEditor(hit gfx.ID) *text.Editor {
	if state.modal == modalAgentHosts && hit == hitAgentHostsQuery {
		state.ai.cfg.agentHostsView.query.Restrict(sftpFilterRune, 256)
		return &state.ai.cfg.agentHostsView.query
	}
	return nil
}

func (state *State) clickAgentHosts(id gfx.ID) bool {
	if state.modal != modalAgentHosts {
		return false
	}
	if id == hitAgentHostsClose {
		state.closeModal()
		return true
	}
	return false
}

// agentHostsScroll gives the wheel to the page's own list in the dialog, as the
// dashboard does: the page hands it to the scroll area under the pointer.
func (state *State) agentHostsScroll(event gfx.ScrollEvent) gfx.EventResult {
	if state.modal != modalAgentHosts {
		return gfx.EventResult{}
	}
	result := state.windowPage().Wheel(event)
	result.Handled = true
	return result
}

func (state *State) agentHostsKey(event gfx.KeyEvent) gfx.EventResult {
	if state.modal != modalAgentHosts || event.Release {
		return gfx.EventResult{}
	}
	view := &state.ai.cfg.agentHostsView
	if event.Key == gfx.KeyEscape {
		if _, composing := view.query.Composition(); composing && event.Target == hitAgentHostsQuery {
			return gfx.EventResult{Handled: true}
		}
		if event.Target == hitAgentHostsQuery && view.query.RuneCount() > 0 {
			view.query = *text.NewEditor("")
			return gfx.EventResult{Handled: true, Repaint: true, Focus: hitAgentHostsQuery}
		}
		state.closeModal()
		return gfx.EventResult{Handled: true, Repaint: true, Focus: hitAISetAgentHostsView, FocusVisible: true}
	}
	return gfx.EventResult{}
}

// agentHostsGroups buckets the open hosts by their catalog group, in name
// order, keeping only the names the query matches. total counts every open
// host; shown counts the ones the filter leaves.
func (state *State) agentHostsGroups() ([]dashboardPickerGroup, int, int) {
	view := &state.ai.cfg.agentHostsView
	query := strings.ToLower(strings.TrimSpace(view.query.String()))
	byName := make(map[string]*dashboardPickerGroup)
	total, shown := 0, 0
	for index := range state.hosts {
		host := state.hosts[index]
		if !host.isSSH() || !state.agentHosts[host.CatalogID] {
			continue
		}
		total++
		name := strings.TrimSpace(host.Group)
		group := byName[name]
		if group == nil {
			group = &dashboardPickerGroup{name: name}
			byName[name] = group
		}
		group.total++
		if query != "" && !strings.Contains(strings.ToLower(host.Label+" "+host.Address+" "+host.User+" "+host.Group+" "+host.Tags), query) {
			continue
		}
		group.hosts = append(group.hosts, host)
		shown++
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	slices.Sort(names)
	groups := make([]dashboardPickerGroup, 0, len(names))
	for _, name := range names {
		if group := byName[name]; len(group.hosts) > 0 {
			groups = append(groups, *group)
		}
	}
	return groups, total, shown
}

// ensureAgentHostRows rebuilds the virtual row model only when the open hosts,
// the query or the column count changed, so an unchanged paint allocates
// nothing. A new model returns the list to the top.
func (state *State) ensureAgentHostRows(columns int) {
	view := &state.ai.cfg.agentHostsView
	state.agentHostLabels()
	summary := &state.ai.agentSummary
	key := agentHostRowsKey{
		hash:    summary.hostsHash,
		count:   summary.hostsCount,
		query:   strings.ToLower(strings.TrimSpace(view.query.String())),
		columns: columns,
	}
	if view.rowsReady && view.rowsKey == key {
		return
	}
	groups, total, shown := state.agentHostsGroups()
	rows := make([]agentHostRow, 0, len(groups)+(shown+columns-1)/columns)
	for _, group := range groups {
		rows = append(rows, agentHostRow{
			header: fmt.Sprintf("%s  %d / %d", state.dashboardGroupLabel(group.name), len(group.hosts), group.total),
		})
		for i := 0; i < len(group.hosts); i += columns {
			rows = append(rows, agentHostRow{hosts: group.hosts[i:min(i+columns, len(group.hosts))]})
		}
	}
	view.rows, view.total, view.shown = rows, total, shown
	view.rowsKey, view.rowsReady = key, true
	view.list.ScrollTo(0, ui.ListStart)
}

func (state *State) declareAgentHostsDialog(c ui.C) {
	if state.modal != modalAgentHosts {
		return
	}
	view, m := &state.ai.cfg.agentHostsView, state.messages.AISettings
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.AgentHosts, Width: 780, Height: min(620, max(420, dialogViewport(c).Height-160)),
		Layer: dialogLayer, Scope: dialogScope, NoClose: true, CloseID: hitAgentHostsClose,
		InitialFocusID: hitAgentHostsQuery,
	})
	dialog.Body(func() {
		// The cards fill the body the dialog actually gives its content, less the
		// list's scrollbar lane, so the last column never runs under the edge.
		body := dialogContentRect(c.Canvas(), *c.Theme(), dialogViewport(c), 780, "", 0).Body
		inner := max(body.Width-catalogBarGutter, 200)
		columns := dashboardPickerColumns(inner - 12)
		cardW := (inner - float32(columns-1)*8) / float32(columns)
		state.ensureAgentHostRows(columns)
		countText := fmt.Sprintf(m.AgentHostsCountFmt, view.total)
		if strings.TrimSpace(view.query.String()) != "" {
			countText = fmt.Sprintf(m.AgentHostsSearchCountFmt, view.shown, view.total)
		}
		ui.Row(c).Height(32).Shrink(0).FillWidth().Items(ui.Center).Children(func() {
			ui.Spacer(c)
			ui.Text(c, countText).FontSize(12).Color(state.pal.muted).Ellipsis()
		})
		if !view.queryBound {
			view.queryField.Bind(&view.query)
			view.queryBound = true
		}
		ui.TextField(c, &view.queryField, m.AgentHostsSearch, m.AgentHostsSearch, ui.WithID(hitAgentHostsQuery)).
			Icon(lucideIcons[iconSearch]).Height(34).FillWidth().Shrink(0)
		if len(view.rows) == 0 {
			ui.Text(c, m.AgentHostsSearchEmpty).FontSize(12).Color(state.pal.muted).Grow(1).Basis(0).MinHeight(0)
		} else {
			list := &view.list
			if !view.listInit {
				view.listInit = true
				list.Label = func(index int) string {
					if index < 0 || index >= len(view.rows) {
						return ""
					}
					if row := view.rows[index]; row.header != "" {
						return row.header
					} else if len(row.hosts) > 0 {
						return row.hosts[0].Label
					}
					return ""
				}
			}
			ui.List(c, list, len(view.rows), func(row ui.ListRow) {
				state.declareAgentHostRow(row, cardW)
			}).Grow(1).Basis(0).MinHeight(0).Scrollbars().ScrollbarSize(catalogBarGutter).Label(m.AgentHosts)
		}
		ui.Text(c, m.AgentHostsSearchHint).FontSize(12).Color(state.pal.muted).Height(20).LineHeight(20).Shrink(0).Ellipsis()
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c, components.ButtonAction{ID: hitAgentHostsClose, Label: m.AgentHostsClose, Variant: components.ButtonSecondary})
	})
}

func (state *State) declareAgentHostRow(row ui.ListRow, cardW float32) {
	view := &state.ai.cfg.agentHostsView
	if row.Index < 0 || row.Index >= len(view.rows) {
		return
	}
	entry := view.rows[row.Index]
	if entry.header != "" {
		row.Element.Height(24).PadX(2).Gap(6).Items(ui.Center).Children(func() {
			ui.Icon(row.C, lucideIcons[iconFolderOpen]).Size(14, 14).Color(state.pal.muted).Shrink(0)
			ui.Text(row.C, entry.header).FontSize(12).Color(state.pal.muted).Weight(gfx.WeightMedium).Ellipsis().Grow(1)
		})
		return
	}
	row.Element.Height(40).Items(ui.Center).Children(func() {
		ui.Row(row.C).FillWidth().Gap(8).Children(func() {
			for _, host := range entry.hosts {
				state.declareAgentHostCard(row.C, host, cardW)
			}
		})
	})
}

// declareAgentHostCard is the creator's host card without its checkbox: the
// same tinted surface and catalog icon, reading the host's name.
func (state *State) declareAgentHostCard(c ui.C, host Host, width float32) {
	background := tintHostSurface(state.pal.surface, host.Color, .10)
	row := ui.Row(c).Height(36).Shrink(0).Width(width).PadX(8).Gap(8).Items(ui.Center).
		Radius(radius.control).Border(1, state.pal.line).Background(background)
	row.Children(func() {
		ui.Slot(c).Size(20, 20).Shrink(0).Draw(func(d *ui.DrawContext) {
			state.paintHostCatalogIcon(d.Canvas, host, d.Rect, background)
		})
		ui.Text(c, host.Label).FontSize(13).Weight(gfx.WeightMedium).Color(state.pal.text).Ellipsis().Grow(1)
	})
}
