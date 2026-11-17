package workbench

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
)

var (
	hitSettingsLocalShell        = tree.ID(settingsHits, "settings-local-shell")
	hitSettingsLocalShellDismiss = tree.ID(settingsHits, "settings-local-shell-dismiss")
	hitSettingsLocalShellItems   = tree.Sub(settingsHits, tree.Name("settings-local-shell-item"))
	hitSettingsShellEyes         = tree.Sub(settingsHits, tree.Name("settings-shell-eye"))
)

type localShellSelect struct {
	choices  []localshell.Choice // the shells, then the WSL distributions
	shells   []localshell.Choice
	distros  []localshell.Choice
	loaded   bool
	open     bool
	bounds   gfx.Rect
	discover func() []localshell.Choice // nil uses the Windows discovery
	// discoverWSL lists the WSL distributions; nil asks wsl.exe. It starts a
	// process, so it runs on its own goroutine and answers through listing.
	discoverWSL func(context.Context) []localshell.Choice
	listing     chan []localshell.Choice // the answer being waited for, else nil
}

// wslListTimeout is how long wsl.exe gets to name the installed distributions.
const wslListTimeout = 10 * time.Second

// localShellsSupported is whether this computer lists its shells for the user
// to choose from. All platforms do: Windows has pwsh/cmd, Unix has zsh/bash/sh.
var localShellsSupported = true

func localShellSettingVisible() bool { return localShellsSupported }

func (state *State) refreshLocalShells() {
	menu := &state.localShellMenu
	discover := menu.discover
	if discover == nil {
		discover = localshell.InstalledWindowsShells
	}
	menu.shells, menu.loaded = discover(), true
	menu.rebuild()
	state.listWSLDistros()
}

func (menu *localShellSelect) rebuild() {
	menu.choices = append(slices.Clone(menu.shells), menu.distros...)
}

// defaultShellChoice is the shell the shell setting shows as the default: the
// detected one marked Default, or the first found, so what the setting displays
// is what a terminal without a choice of its own runs.
func (state *State) defaultShellChoice() localshell.Choice {
	for _, choice := range state.localShellMenu.choices {
		if choice.Default {
			return choice
		}
	}
	if len(state.localShellMenu.choices) > 0 {
		return state.localShellMenu.choices[0]
	}
	return localshell.Choice{}
}

// resolveLocalShell names the program a local terminal runs: the tab's own
// shell, the settings' default, then this machine's detected default. Without
// the last step a shell that was never picked would fall to a PATH lookup,
// which can land on another program than the setting shows.
func (state *State) resolveLocalShell(host Host) string {
	if host.localShell != "" {
		return host.localShell
	}
	if state.localShell != "" {
		return state.localShell
	}
	if !state.localShellMenu.loaded {
		state.refreshLocalShells()
	}
	return state.defaultShellChoice().Path
}

// listWSLDistros asks for the installed distributions without waiting: the
// shells are found by looking at files, but naming distributions takes wsl.exe,
// which may take a moment, or start WSL. Their entries join the list when the
// answer is adopted by the next paint.
func (state *State) listWSLDistros() {
	menu := &state.localShellMenu
	if menu.listing != nil {
		return
	}
	discover := menu.discoverWSL
	if discover == nil {
		discover = localshell.InstalledWSL
	}
	listing, invalidate := make(chan []localshell.Choice, 1), state.options.Invalidate
	menu.listing = listing
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), wslListTimeout)
		defer cancel()
		listing <- discover(ctx)
		if invalidate != nil {
			invalidate()
		}
	}()
}

// adoptWSLDistros takes the distributions that were asked for, if they have
// been named.
func (state *State) adoptWSLDistros() {
	menu := &state.localShellMenu
	if menu.listing == nil {
		return
	}
	select {
	case distros := <-menu.listing:
		menu.listing = nil
		if !slices.Equal(distros, menu.distros) {
			menu.distros = distros
			menu.rebuild()
			state.shellsListed()
		}
	default:
	}
}

func (state *State) localShellSelected() int {
	for index, choice := range state.localShellMenu.choices {
		if state.localShell == "" && choice.Default || strings.EqualFold(choice.Path, state.localShell) {
			return index
		}
	}
	return -1
}

// cleanShellPaths keeps the saved hidden shells in order, without empties or
// duplicates. Nothing is dropped against what this computer detects: a shell
// that is away may come back.
func cleanShellPaths(paths []string) []string {
	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" && !slices.ContainsFunc(kept, func(kept string) bool { return strings.EqualFold(kept, path) }) {
			kept = append(kept, path)
		}
	}
	return kept
}

// shellHidden is whether the pickers leave this shell out. The default shell
// always stays: it is the Local Terminal entry itself, and hiding it would
// leave a computer with one shell nothing to open.
func (state *State) shellHidden(choice localshell.Choice) bool {
	if choice.Path == "" {
		return false
	}
	if index := state.localShellSelected(); index >= 0 && strings.EqualFold(state.localShellMenu.choices[index].Path, choice.Path) {
		return false
	}
	return slices.ContainsFunc(state.hiddenShells, func(path string) bool { return strings.EqualFold(path, choice.Path) })
}

// visibleShellCount is how many shells the pickers would offer.
func (state *State) visibleShellCount() int {
	count := 0
	for _, choice := range state.localShellMenu.choices {
		if !state.shellHidden(choice) {
			count++
		}
	}
	return count
}

// hideShell records a shell among the hidden ones and saves. Showing it again
// takes the entry back out. The default shell refuses: it is the Local
// Terminal entry itself.
func (state *State) hideShell(shell string, hidden bool) {
	if shell == "" {
		return
	}
	if index := state.localShellSelected(); index >= 0 && strings.EqualFold(state.localShellMenu.choices[index].Path, shell) {
		return
	}
	kept := cleanShellPaths(state.hiddenShells)
	if hidden {
		if !slices.ContainsFunc(kept, func(kept string) bool { return strings.EqualFold(kept, shell) }) {
			kept = append(kept, shell)
		}
	} else {
		kept = slices.DeleteFunc(kept, func(kept string) bool { return strings.EqualFold(kept, shell) })
	}
	state.hiddenShells = kept
	state.save()
}

func (state *State) paintLocalShellSetting(page *settingsPage) {
	if !localShellSettingVisible() {
		return
	}
	page.add(settingsControl{kind: controlLocalShellSelect, slot: page.rowFrame(state.messages.SettingsLocalShell, state.messages.SettingsLocalShellDescription)})
}

// Paint the select in the menu pass, outside the settings page's clip. The
// trigger stays the settings' select; the list is a menu surface of the app's,
// so every row can carry the eye that hides or shows it.
func (state *State) paintLocalShellSelect(canvas *gfx.Canvas) {
	state.adoptWSLDistros()
	menu, m := &state.localShellMenu, state.messages
	bounds := menu.bounds
	if !localShellSettingVisible() || state.modal != modalNone || bounds.Width <= 0 || bounds.Y < windowBarHeight || bounds.Y+bounds.Height > state.viewport.Height {
		menu.open = false
		return
	}
	selected := state.localShellSelected()
	value, hint := m.SettingsLocalShellEmpty, ""
	if selected >= 0 {
		value, hint = menu.choices[selected].Name, menu.choices[selected].Path
	} else if state.localShell != "" {
		value, hint = filepath.Base(state.localShell), state.localShell
		if distro, ok := localshell.WSLDistro(state.localShell); ok {
			value = localshell.WSLName(distro) // chosen, but not listed yet
		}
	}
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitSettingsLocalShell, Label: m.SettingsLocalShell, Value: value, Hint: hint,
		Disabled: len(menu.choices) == 0, Layer: popupLayer,
		Scope: hitSettingsLocalShellDismiss, DismissID: hitSettingsLocalShellDismiss, Viewport: state.windowRect(),
	})
	defer control.End()
	if !menu.open {
		return
	}

	var rows menuRows
	for index, choice := range menu.choices {
		rows.add(components.ContextMenuItem{
			ID: hitSettingsLocalShellItems.At(index), Label: choice.Name, Icon: lucideIcons[iconTerminal],
			Kind: components.ContextMenuRadio, Selected: index == selected,
			// A hidden shell cannot become the default: the row is inert and
			// only its eye answers.
			Disabled:      state.shellHidden(choice),
			TrailingWidth: 30,
		})
	}
	if len(menu.choices) == 0 {
		rows.add(components.ContextMenuItem{ID: hitSettingsLocalShellItems.At(0), Label: m.SettingsLocalShellEmpty, Icon: lucideIcons[iconTerminal], Disabled: true})
	}
	width, height := rows.size(canvas, state.contextMenuTheme())
	bounds2 := state.popupAt(canvas, state.windowRect(), gfx.Point{X: bounds.X, Y: bounds.Y + bounds.Height + 4}, width, height, 4)
	list := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds2, components.ContextMenuOptions{
		Layer: popupLayer, Scope: hitSettingsLocalShellDismiss, DismissID: hitSettingsLocalShellDismiss,
		Viewport: state.windowRect(), Scroll: state.menuScrollFor(hitSettingsLocalShellDismiss), RowHeight: contextMenuRow,
	})
	state.menuPlaced(list.Bounds())
	rows.paint(&list, func(item components.ContextMenuItem, slot gfx.Rect) {
		if index, ok := item.ID.In(hitSettingsLocalShellItems); ok {
			state.paintShellEye(canvas, hitSettingsShellEyes.At(index), slot)
			return
		}
	})
	list.End()
}

// paintShellEye paints the trailing eye of a shell row: open where the pickers
// offer the shell, closed and red where they leave it out. A press toggles it;
// the default shell has none, since it is the Local Terminal entry itself.
func (state *State) paintShellEye(canvas *gfx.Canvas, id gfx.ID, slot gfx.Rect) {
	index, ok := id.In(hitSettingsShellEyes)
	if !ok || index < 0 || index >= len(state.localShellMenu.choices) {
		return
	}
	if index == state.localShellSelected() {
		return
	}
	hidden := state.shellHidden(state.localShellMenu.choices[index])
	kind, style := iconEye, iconButtonQuiet
	hint := state.messages.ShellEyeHide
	if hidden {
		kind, style = iconEyeOff, iconButtonDanger
		hint = state.messages.ShellEyeShow
	}
	state.paintIconButton(canvas, id, kind, gfx.Rect{X: slot.X + (slot.Width-24)/2, Y: slot.Y + (slot.Height-22)/2, Width: 24, Height: 22},
		hint, false, style, state.pal.surface)
}

func (state *State) clickLocalShell(id gfx.ID) bool {
	if !localShellSettingVisible() || state.settingsCategory != settingsTerminal || state.settingsSearching() {
		return false
	}
	state.adoptWSLDistros()
	menu := &state.localShellMenu
	switch id {
	case hitSettingsLocalShell:
		if !menu.open {
			state.refreshLocalShells()
		}
		menu.open = !menu.open && len(menu.choices) > 0
		return true
	case hitSettingsLocalShellDismiss:
		menu.open = false
		return true
	}
	if index, ok := id.In(hitSettingsShellEyes); ok && index >= 0 && index < len(menu.choices) {
		choice := menu.choices[index]
		state.hideShell(choice.Path, !state.shellHidden(choice))
		return true
	}
	index, ok := id.In(hitSettingsLocalShellItems)
	if !ok || index < 0 || index >= len(menu.choices) {
		return false
	}
	program := menu.choices[index].Path
	if state.shellHidden(menu.choices[index]) {
		return true // a hidden shell stays hidden: it cannot become the default
	}
	menu.open = false
	if state.localShell != program {
		state.localShell = program
		state.save()
	}
	return true
}
