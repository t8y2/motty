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

// paintNewTabPage fills an empty pane with the places a connection can go. It
// shows as many hosts as the pane's height holds; the rest are one click away
// under All hosts.
func (state *State) paintNewTabPage(canvas *gfx.Canvas, bounds gfx.Rect) {
	pal, m := state.pal, state.messages
	width := min(newTabCardWidth, bounds.Width-48)
	if width < 200 {
		canvas.Fill(bounds, pal.workspace)
		return
	}
	room := int((bounds.Height - newTabTitle - newTabFooter - 48) / newTabRowHeight)
	rows := min(max(room, 1), newTabMaxHosts+1)
	hosts := min(rows-1, len(state.hosts))
	addHosts := toolbarButtonWidth(canvas, m.AddHost, false)
	allHosts := toolbarButtonWidth(canvas, m.NewTabAllHosts, false)

	local := localHost(m.LocalTerminalTitle)
	chrome := &state.chrome
	chrome.newTabBadges = chrome.newTabBadges[:0]
	var all, add ui.Box
	f := chrome.newTab.Begin(canvas, state.Theme(), bounds)
	// The block sits a little above the middle, and never closer than 16 to the top.
	f.Column().Fill().PadSides(16, 0, 0, 0).Items(ui.Center).Justify(ui.Center).Background(pal.workspace).Children(func() {
		f.Column().Width(width).Shrink(0).MarginSides(0, 0, 48, 0).Children(func() {
			f.Text(m.NewTabConnectTo).FontSize(16).Weight(gfx.WeightSemibold).Color(pal.text).LineHeight(26).MarginSides(0, 0, newTabTitle-26, 0)
			f.Stack().Children(func() {
				f.Row().Fill().Radius(radius.popup).Border(1, pal.line).Background(pal.surface)
				f.Column().Children(func() {
					state.newTabRow(f, hitNewTabLocal, local, m.LocalTerminal, local.Address, hosts > 0, width, localShellSettingVisible())
					for index := 0; index < hosts; index++ {
						host := state.hosts[index]
						state.newTabRow(f, hitNewTabHostList.At(index), host, host.Label, state.sessionAddress(host), index < hosts-1, width, false)
					}
				})
			})
			f.Row().Shrink(0).Gap(8).MarginSides(0, 0, newTabFooter-48, 0).Children(func() {
				add = f.Slot().Size(addHosts, 32).Self(ui.Center)
				all = f.Slot().Size(allHosts, 32).Self(ui.Center)
			})
		})
	})
	f.End()
	for index, badge := range chrome.newTabBadges {
		if index == 0 {
			paintSystemBadge(canvas, local.System, badge.Rect(), pal)
			continue
		}
		state.paintHostCatalogIcon(canvas, state.hosts[index-1], badge.Rect(), pal.workspace)
	}
	state.paintButton(canvas, hitNewTabHostNew, add.Rect(), m.AddHost, 0, false, false, true)
	state.paintButton(canvas, hitPaneChooseHost, all.Rect(), m.NewTabAllHosts, 0, false, false, true)
}

// newTabRow declares one place a tab can connect to: its system, its name and,
// at the far end, where it is. The system's badge is drawn into the slot kept for it.
// A host row also has a pencil that opens the host for editing, and takes a
// secondary press as its menu; a row with shells has an arrow after the address,
// which opens the menu of the shells this computer can start. A secondary press
// on the row does too.
func (state *State) newTabRow(f *ui.Frame, id gfx.ID, host Host, title, address string, separator bool, width float32, shells bool) {
	pal, m := state.pal, state.messages
	if shells {
		state.openLocalShellPickerOn(f.Canvas(), id, true)
	}
	if id.Scope() == hitNewTabHostList {
		state.openHostMenuOn(f.Canvas(), id, host)
	}
	var wash gfx.Color
	if f.Canvas().Hovered() == id || f.Canvas().FocusVisible() == id {
		wash = pal.surfaceSoft
	}
	f.Stack().Height(newTabRowHeight).HitWith(id, title, gfx.HitOptions{Role: gfx.RoleButton, Hint: address}).Children(func() {
		f.Row().Fill().Margin(4).Radius(radius.control).Background(wash)
		f.Row().Fill().PadX(16).Items(ui.Center).Children(func() {
			state.chrome.newTabBadges = append(state.chrome.newTabBadges, f.Slot().Size(28, 28).MarginSides(0, 12, 0, 0))
			f.Text(title).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(22).Ellipsis()
			f.Spacer()
			f.Text(address).FontSize(metrics.FontSizeS).Color(pal.muted).LineHeight(20).Ellipsis().Shrink(0).MaxWidth(width*0.4).MarginSides(0, 0, 0, 12)
			if id.Scope() == hitNewTabHostList {
				edit := hitNewTabHostEdit.At(item(id))
				var editWash gfx.Color
				if f.Canvas().Hovered() == edit || f.Canvas().FocusVisible() == edit {
					editWash = pal.line
				}
				f.Stack().Size(28, 28).Shrink(0).MarginSides(0, -8, 0, 0).HitWith(edit, m.EditHost, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.EditHost}).Children(func() {
					f.Row().Fill().Radius(radius.control).Background(editWash)
					f.Row().Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
						f.Icon(lucideIcons[iconPencil]).Size(14, 14).Color(pal.muted).Shrink(0)
					})
				})
			}
			if shells {
				var arrowWash gfx.Color
				if f.Canvas().Hovered() == hitNewTabShells || f.Canvas().FocusVisible() == hitNewTabShells {
					arrowWash = pal.line
				}
				f.Stack().Size(28, 28).Shrink(0).MarginSides(0, -8, 0, 0).HitWith(hitNewTabShells, m.LocalShellChoose, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.LocalShellChoose}).Children(func() {
					f.Row().Fill().Radius(radius.control).Background(arrowWash)
					f.Row().Fill().Items(ui.Center).Justify(ui.Center).Children(func() {
						f.Icon(lucideIcons[iconChevronDown]).Size(14, 14).Color(pal.muted).Shrink(0)
					})
					state.chrome.newTabShells = f.Slot().Fill()
				})
			}
		})
		if separator {
			f.Column().Fill().Justify(ui.End).Children(func() {
				f.Divider().Background(withAlpha(pal.line, 0.6)).MarginX(16)
			})
		}
	})
}

// clickNewTabPage connects the choice in the empty pane the page is shown in.
func (state *State) clickNewTabPage(id gfx.ID) bool {
	var host Host
	switch {
	case id == hitNewTabShells:
		arrow := state.chrome.newTabShells.Rect()
		state.openLocalShellPicker(gfx.Point{X: arrow.X + arrow.Width - 8, Y: arrow.Y + arrow.Height + 4}, true)
		return true
	case id == hitNewTabLocal:
		state.pendingWorkspace = state.deck.ActiveWorkspace()
		state.pendingPane = state.deck.ActivePane(state.pendingWorkspace)
		state.openLocalTerminal()
		return true
	case id.Scope() == hitNewTabHostEdit:
		if index := item(id); index < len(state.hosts) {
			state.openEditHostID(state.hosts[index].ID)
		}
		return true
	case id == hitNewTabHostNew:
		state.openHostDialog()
		return true
	case id.Scope() == hitNewTabHostList:
		index := item(id)
		if index >= len(state.hosts) {
			return true
		}
		host = state.hosts[index]
	default:
		return false
	}
	state.pendingWorkspace = state.deck.ActiveWorkspace()
	state.pendingPane = state.deck.ActivePane(state.pendingWorkspace)
	state.newHostSession(host)
	return true
}
