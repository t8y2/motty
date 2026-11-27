package workbench

import (
	"net"
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

// contextMenuRow is the height of an item row in every right-click menu: a dense
// 24px, so a menu of a dozen rows stays short.
const contextMenuRow = float32(24)

// connectionAddressAndPort is the endpoint text the copy actions place on the
// clipboard. Portless connections, including serial hosts, have nothing useful
// to copy under that label.
func connectionAddressAndPort(host Host) string {
	if host.Address == "" || host.Port == 0 {
		return ""
	}
	return net.JoinHostPort(host.Address, strconv.Itoa(int(host.Port)))
}

// connectionHostAddress is the endpoint text without its port: what a user
// pastes where only the host matters. A local shell has no network address and
// a serial device is not one, so neither copies anything.
func connectionHostAddress(host Host) string {
	if host.ID == localHostID || host.Protocol == protocolSerial {
		return ""
	}
	return host.Address
}

// copyConnectionText places what a connection copy row offers on the clipboard.
// A failure goes to the status line, where the terminal's own Copy reports it,
// instead of being dropped along with the menu that asked for it.
func (state *State) copyConnectionText(value string) {
	if value == "" || state.options.SetClipboardText == nil {
		return
	}
	if err := state.options.SetClipboardText(value); err != nil {
		state.status = err.Error()
	}
}

// contextMenuTheme is the theme the right-click menus are measured and painted
// with: a point smaller text than the window's, which Viva takes from the theme.
func (state *State) contextMenuTheme() gfx.Theme {
	theme := state.Theme()
	theme.Metrics.FontSizeM--
	return theme
}

// Viva's separator row and the padding it keeps around the rows, in px.
const (
	contextMenuSeparator = float32(9)
	contextMenuPadding   = float32(8)
)

// menuEntry is one row of a context menu, or the separator before the next row.
type menuEntry struct {
	item      components.ContextMenuItem
	separator bool
	// chord is the shortcut the row shows at its end, when it has one.
	chord keymap.Chord
	// content, when set, makes the entry a row painted by hand, height tall;
	// pageContent makes it a row whose controls the page declares.
	content     func(*gfx.Canvas, gfx.Rect, gfx.Theme)
	pageContent func(ui.C, gfx.Rect)
	// submenu, when set, makes the entry open a menu of its own. Choosing a row
	// of that menu is the body's concern; this row has no action of its own.
	submenu func(*ui.MenuItems)
	// trailing, when set, paints the area TrailingWidth reserved at the row's end.
	trailing func(*gfx.Canvas, gfx.Rect, gfx.Theme)
	height   float32
	// stay keeps the menu open when the row is chosen, for settings changed
	// several at a time.
	stay bool
}

// menuRows collects the rows of a context menu before it is painted, so its
// size follows its content: the widest row sets the width, and a language whose
// labels are longer gets a wider menu, not cut-off words. It lives on the stack.
type menuRows struct {
	entries [32]menuEntry
	count   int
}

func (rows *menuRows) add(item components.ContextMenuItem) {
	if rows.count < len(rows.entries) {
		rows.entries[rows.count] = menuEntry{item: item}
		rows.count++
	}
}

// addStay adds a row whose choice leaves the menu open.
func (rows *menuRows) addStay(item components.ContextMenuItem) {
	rows.add(item)
	if rows.count > 0 && rows.entries[rows.count-1].item.ID == item.ID {
		rows.entries[rows.count-1].stay = true
	}
}

// addSubmenu adds a row that opens a submenu. body runs while that submenu is
// open and declares its rows.
func (rows *menuRows) addSubmenu(item components.ContextMenuItem, body func(*ui.MenuItems)) {
	if rows.count < len(rows.entries) {
		item.Kind = components.ContextMenuSubmenu
		rows.entries[rows.count] = menuEntry{item: item, submenu: body}
		rows.count++
	}
}

// trailing reserves width at the end of the last row added, and paints it.
func (rows *menuRows) trailing(width float32, paint func(*gfx.Canvas, gfx.Rect, gfx.Theme)) {
	if rows.count > 0 && paint != nil {
		rows.entries[rows.count-1].item.TrailingWidth = width
		rows.entries[rows.count-1].trailing = paint
	}
}

// separate puts a separator after the rows so far; one at the top or doubled up
// is dropped.
// chord gives the last row added the shortcut it shows at its end.
func (rows *menuRows) chord(chord keymap.Chord) {
	if rows.count > 0 {
		rows.entries[rows.count-1].chord = chord
	}
}

// addContent adds a row height tall that paint fills.
func (rows *menuRows) addContent(height float32, paint func(*gfx.Canvas, gfx.Rect, gfx.Theme)) {
	if rows.count < len(rows.entries) {
		rows.entries[rows.count] = menuEntry{content: paint, height: height}
		rows.count++
	}
}

// addPageContent adds a row height tall whose controls the page declares in it.
func (rows *menuRows) addPageContent(height float32, declare func(ui.C, gfx.Rect)) {
	if rows.count < len(rows.entries) {
		rows.entries[rows.count] = menuEntry{pageContent: declare, height: height}
		rows.count++
	}
}

func (rows *menuRows) separate() {
	if rows.count == 0 || rows.entries[rows.count-1].separator || rows.count == len(rows.entries) {
		return
	}
	rows.entries[rows.count] = menuEntry{separator: true}
	rows.count++
}

// size is the width and height the menu needs.
func (rows *menuRows) size(canvas *gfx.Canvas, theme gfx.Theme) (width, height float32) {
	height = contextMenuPadding
	for _, entry := range rows.entries[:rows.count] {
		if entry.separator {
			height += contextMenuSeparator
			continue
		}
		if entry.content != nil || entry.pageContent != nil {
			height += entry.height
			continue
		}
		height += components.ContextMenuItemHeight(theme, entry.item, contextMenuRow)
		width = max(width, components.ContextMenuItemWidth(canvas, theme, entry.item))
	}
	return width, height
}

// paint adds every row to menu. A row that reserved a trailing slot is handed to
// slot with the rectangle, to paint what it holds.
func (rows *menuRows) paint(menu *components.ContextMenu, slot func(item components.ContextMenuItem, bounds gfx.Rect)) {
	for _, entry := range rows.entries[:rows.count] {
		switch {
		case entry.separator:
			menu.AddSeparator()
		case entry.item.TrailingWidth > 0:
			_, bounds := menu.AddItemSlot(entry.item)
			menu.BeginContent()
			slot(entry.item, bounds)
			menu.EndContent()
		default:
			menu.AddItem(entry.item)
		}
	}
}

// declare declares the rows as the items of a page menu. A row that is chosen runs
// choose with its identity once the paint that delivered the choice is over: the
// application's handlers are keyed by the identities the rows were given.
func (rows *menuRows) declare(c ui.C, menu *ui.MenuItems, choose func(gfx.ID)) {
	menu.RowHeight(contextMenuRow)
	for _, entry := range rows.entries[:rows.count] {
		if entry.separator {
			menu.Separator()
			continue
		}
		if entry.content != nil {
			menu.Content(entry.height, entry.content)
			continue
		}
		if entry.pageContent != nil {
			menu.ContentPage(entry.height, entry.pageContent)
			continue
		}
		if entry.submenu != nil {
			item := menu.Sub(entry.item.Label, entry.submenu).Mnemonic(entry.item.Mnemonic).Icon(entry.item.Icon).Disabled(entry.item.Disabled)
			if entry.item.Description != "" {
				item.Description(entry.item.Description)
			}
			continue
		}
		row := entry.item
		label := row.Label
		if row.Shortcut != "" {
			label += " · " + row.Shortcut
		}
		item := menu.Item(label).Mnemonic(row.Mnemonic).Icon(row.Icon).Disabled(row.Disabled)
		if entry.trailing != nil {
			item = item.Trailing(row.TrailingWidth, entry.trailing)
		}
		if row.Variant == components.ContextMenuItemDestructive {
			item = item.Destructive()
		}
		if row.Description != "" {
			item = item.Description(row.Description)
		}
		if entry.chord.Valid() {
			item = item.Keys(entry.chord.Modifiers, entry.chord.Name())
		}
		switch row.Kind {
		case components.ContextMenuCheckbox:
			item = item.Checked(row.Checked)
		case components.ContextMenuRadio:
			item = item.Chosen(row.Selected)
		}
		chosen := item.Clicked()
		if entry.stay {
			chosen = item.Toggled()
		}
		if chosen {
			id := row.ID
			c.After(func() { choose(id) })
		}
	}
}

// declareRowsMenu declares an open context menu at a point of the window, over the
// dialogs. The menu is named by dismiss: the identity its press-away used to have,
// kept as the target of the page's menu.
func (state *State) declareRowsMenu(c ui.C, open *bool, at gfx.Point, target gfx.ID, rows *menuRows, choose func(gfx.ID)) {
	ui.PopupLayer(c, dialogLayer)
	anchor := ui.Slot(c).Absolute().Left(at.X).Top(at.Y).Size(0, 0).HitWith(target, "", gfx.HitOptions{SkipFocus: true, SkipTab: true})
	ui.ContextMenuAt(c, anchor, open, at, func(menu *ui.MenuItems) { rows.declare(c, menu, choose) })
}

// declareAnchoredMenu declares an open menu over (or under) a rectangle of the window,
// such as a badge the terminal painted: the menu opens above it unless there is no
// room.
func (state *State) declareAnchoredMenu(c ui.C, open *bool, anchor gfx.Rect, rows *menuRows, choose func(gfx.ID)) {
	state.declareMenuAt(c, open, anchor, true, rows, choose)
}

// declareMenuAt is declareAnchoredMenu with the side chosen: above the rectangle, or
// below it.
func (state *State) declareMenuAt(c ui.C, open *bool, anchor gfx.Rect, above bool, rows *menuRows, choose func(gfx.ID)) {
	ui.PopupLayer(c, dialogLayer)
	ui.MenuAt(c, open, anchor, func(menu *ui.MenuItems) {
		if above {
			menu.Above()
		}
		rows.declare(c, menu, choose)
	})
}

// declareMenuOn declares an open menu hanging off a page control. Unlike
// declareMenuAt it takes the control's own Box, not a rectangle: the page reads a
// control's rect only after it has laid the page out, so a rect captured while
// the page is being declared is empty. The control's Box resolves to the
// rectangle it ends up at, and is also the menu's keyboard trigger.
func (state *State) declareMenuOn(c ui.C, open *bool, anchor ui.Box, rows *menuRows, choose func(gfx.ID)) {
	ui.PopupLayer(c, dialogLayer)
	ui.Menu(c, anchor, open, func(menu *ui.MenuItems) {
		rows.declare(c, menu, choose)
	})
}
