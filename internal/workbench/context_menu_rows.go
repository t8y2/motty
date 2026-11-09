package workbench

import (
	"net"
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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

// separate puts a separator after the rows so far; one at the top or doubled up
// is dropped.
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
