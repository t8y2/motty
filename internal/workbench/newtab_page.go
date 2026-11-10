package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// newtabPageHits is the place of the controls this file paints.
var newtabPageHits = tree.Feature("newtab-page")

// The new-tab page lists where a tab can connect: this computer first, then the
// saved hosts, then the full host list. Its hit identities are a block of their own.
var (
	hitNewTabLocal    = tree.ID(newtabPageHits, "new-tab-local")
	hitNewTabShells   = tree.ID(newtabPageHits, "new-tab-shells")
	hitNewTabHostList = tree.Sub(newtabPageHits, tree.Name("new-tab-host"))
	// hitNewTabHostEdit is the pencil of a host row, by the row's place: it
	// opens the host for editing instead of connecting.
	hitNewTabHostEdit = tree.Sub(newtabPageHits, tree.Name("new-tab-host-edit"))
	// hitNewTabHostNew adds a host right from the page.
	hitNewTabHostNew = tree.ID(newtabPageHits, "new-tab-host-new")
)

const (
	newTabMaxHosts = 8
)

const (
	newTabCardWidth = float32(440)
	newTabRowHeight = float32(48)
	newTabTitle     = float32(44)
	newTabFooter    = float32(56)
)

// declareNewTabPage declares the empty pane's places a connection can go, in the
// page at the pane's room. It shows as many hosts as the pane's height holds; the
// rest are one click away under All hosts. The badges and footer buttons are
// painted after the page, into the slots it leaves.
func (state *State) declareNewTabPage(c ui.C, bounds gfx.Rect) {
	pal := state.pal
	chrome := &state.chrome
	chrome.newTabBadges = chrome.newTabBadges[:0]
	chrome.newTabShells = ui.Box{}
	chrome.newTabShown = min(newTabCardWidth, bounds.Width-48) >= 200
	if !chrome.newTabShown {
		ui.Column(c).Fill().Background(pal.workspace)
		return
	}
	m := state.messages
	width := min(newTabCardWidth, bounds.Width-48)
	room := int((bounds.Height - newTabTitle - newTabFooter - 48) / newTabRowHeight)
	rows := min(max(room, 1), newTabMaxHosts+1)
	hosts := min(rows-1, len(state.hosts))
	local := localHost(m.LocalTerminalTitle)

	ui.Column(c).Fill().PadSides(16, 0, 0, 0).Items(ui.Center).Justify(ui.Center).Background(pal.workspace).Children(func() {
		ui.Column(c).Width(width).Shrink(0).MarginSides(0, 0, 48, 0).Children(func() {
			ui.Text(c, m.NewTabConnectTo).FontSize(16).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(26).MarginSides(0, 0, newTabTitle-26, 0)
			ui.Stack(c).Children(func() {
				ui.Row(c).Fill().Radius(radius.popup).Border(1, pal.line).Background(pal.surface)
				ui.Column(c).Children(func() {
					state.newTabRow(c, hitNewTabLocal, local, m.LocalTerminal, local.Address, hosts > 0, width, localShellSettingVisible())
					for index := 0; index < hosts; index++ {
						host := state.hosts[index]
						state.newTabRow(c, hitNewTabHostList.At(index), host, host.Label, state.sessionAddress(host), index < hosts-1, width, false)
					}
				})
			})
			ui.Row(c).Shrink(0).Gap(8).MarginSides(0, 0, newTabFooter-48, 0).Children(func() {
				chrome.newTabAdd = state.declareButtonSized(c, hitNewTabHostNew, m.AddHost, 0, false, false, true, 12, false).Size(toolbarButtonWidth(c.Canvas(), m.AddHost, false), 32).Self(ui.Center)
				chrome.newTabAll = state.declareButtonSized(c, hitPaneChooseHost, m.NewTabAllHosts, 0, false, false, true, 12, false).Size(toolbarButtonWidth(c.Canvas(), m.NewTabAllHosts, false), 32).Self(ui.Center)
			})
		})
	})
	if canvas := c.Canvas(); canvas != nil {
		if canvas.Clicked(hitNewTabHostNew) {
			state.openHostDialog()
		}
		if canvas.Clicked(hitPaneChooseHost) {
			state.chooseHostSection()
		}
	}
}

// paintNewTabDetails paints what the empty pane's declaration leaves to the
// window: the system and host badges of its rows, and its footer buttons.
func (state *State) paintNewTabDetails(canvas *gfx.Canvas) {
	chrome, pal := &state.chrome, state.pal
	if !chrome.newTabShown {
		return
	}
	local := localHost(state.messages.LocalTerminalTitle)
	for index, badge := range chrome.newTabBadges {
		if index == 0 {
			paintSystemBadge(canvas, local.System, badge.Rect(), pal)
			continue
		}
		state.paintHostCatalogIcon(canvas, state.hosts[index-1], badge.Rect(), pal.workspace)
	}
}

// newTabRow declares one place a tab can connect to: its system, its name and,
// at the far end, where it is. The system's badge is drawn into the slot kept for it.
// A host row also has a pencil that opens the host for editing, and takes a
// secondary press as its menu; a row with shells has an arrow after the address,
// which opens the menu of the shells this computer can start. A secondary press
// on the row does too.
func (state *State) newTabRow(c ui.C, id gfx.ID, host Host, title, address string, separator bool, width float32, shells bool) {
	pal, m := state.pal, state.messages
	canvas := c.Canvas()
	if shells {
		state.openLocalShellPickerOn(canvas, id, true)
	}
	if id.Scope() == hitNewTabHostList {
		state.openHostMenuOn(canvas, id, host)
	}
	var wash gfx.Color
	if canvas != nil && (canvas.Hovered() == id || canvas.FocusVisible() == id) {
		wash = pal.surfaceSoft
	}
	row := ui.Stack(c).Height(newTabRowHeight).HitWith(id, title, gfx.HitOptions{Role: gfx.RoleButton, Hint: address})
	if row.Clicked() {
		state.chooseNewTabRow(id)
	}
	row.Children(func() {
		ui.Row(c).Fill().Margin(4).Radius(radius.control).Background(wash)
		ui.Row(c).Fill().PadX(16).Items(ui.Center).Children(func() {
			state.chrome.newTabBadges = append(state.chrome.newTabBadges, ui.Slot(c).Size(28, 28).MarginSides(0, 12, 0, 0))
			ui.Text(c, title).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(22).Ellipsis()
			ui.Spacer(c)
			ui.Text(c, address).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20).Ellipsis().Shrink(0).MaxWidth(width*0.4).MarginSides(0, 0, 0, 12)
			if id.Scope() == hitNewTabHostList {
				edit := hitNewTabHostEdit.At(item(id))
				var editWash gfx.Color
				if canvas != nil && (canvas.Hovered() == edit || canvas.FocusVisible() == edit) {
					editWash = pal.line
				}
				button := ui.Stack(c).Size(28, 28).Shrink(0).MarginSides(0, -8, 0, 0).HitWith(edit, m.EditHost, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.EditHost})
				if button.Clicked() {
					if index := item(id); index < len(state.hosts) {
						state.openEditHostID(state.hosts[index].ID)
					}
				}
				button.Children(func() {
					ui.Row(c).Fill().Radius(radius.control).Background(editWash)
					ui.Row(c).Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
						ui.Icon(c, lucideIcons[iconPencil]).Size(14, 14).Color(pal.muted).Shrink(0)
					})
				})
			}
			if shells {
				var arrowWash gfx.Color
				if canvas != nil && (canvas.Hovered() == hitNewTabShells || canvas.FocusVisible() == hitNewTabShells) {
					arrowWash = pal.line
				}
				arrow := ui.Stack(c).Size(28, 28).Shrink(0).MarginSides(0, -8, 0, 0).HitWith(hitNewTabShells, m.LocalShellChoose, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.LocalShellChoose})
				state.chrome.newTabShells = arrow
				arrow.Children(func() {
					ui.Row(c).Fill().Radius(radius.control).Background(arrowWash)
					ui.Row(c).Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
						ui.Icon(c, lucideIcons[iconChevronDown]).Size(14, 14).Color(pal.muted).Shrink(0)
					})
				})
			}
		})
		if separator {
			ui.Column(c).Fill().Justify(ui.End).Children(func() {
				ui.Divider(c).Background(withAlpha(pal.line, 0.6)).MarginX(16)
			})
		}
	})
}

// chooseNewTabRow connects the choice in the empty pane the page is shown in.
func (state *State) chooseNewTabRow(id gfx.ID) {
	if id == hitNewTabLocal {
		state.pendingWorkspace = state.deck.ActiveWorkspace()
		state.pendingPane = state.deck.ActivePane(state.pendingWorkspace)
		state.openLocalTerminal()
		return
	}
	if id.Scope() != hitNewTabHostList {
		return
	}
	index := item(id)
	if index >= len(state.hosts) {
		return
	}
	state.pendingWorkspace = state.deck.ActiveWorkspace()
	state.pendingPane = state.deck.ActivePane(state.pendingWorkspace)
	state.newHostSession(state.hosts[index])
}

// chooseHostSection shows the hosts page, remembering the pane a host chosen
// there connects in: what the page's All hosts button does.
func (state *State) chooseHostSection() {
	state.pendingWorkspace = state.deck.ActiveWorkspace()
	state.pendingPane = state.deck.ActivePane(state.pendingWorkspace)
	state.activeSection = sectionHosts
	state.save()
}

// clickNewTabPage opens the shell menu of the page's local row. The arrow's
// rectangle is kept from the paint that drew it, so the menu hangs under it.
func (state *State) clickNewTabPage(id gfx.ID) bool {
	if id != hitNewTabShells {
		return false
	}
	arrow := state.chrome.newTabShells.Rect()
	state.openLocalShellPicker(gfx.Point{X: arrow.X + arrow.Width - 8, Y: arrow.Y + arrow.Height + 4}, true)
	return true
}
