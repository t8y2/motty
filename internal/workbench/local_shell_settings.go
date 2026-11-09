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

func (state *State) paintLocalShellSetting(page *settingsPage) {
	if !localShellSettingVisible() {
		return
	}
	page.add(settingsControl{kind: controlLocalShellSelect, slot: page.rowFrame(state.messages.SettingsLocalShell, state.messages.SettingsLocalShellDescription)})
}

// Paint the select in the menu pass, outside the settings page's clip.
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
	below := max(state.viewport.Height-8-bounds.Y-bounds.Height-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	height := min(float32(8+len(menu.choices)*32), max(above, below))
	content := gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: bounds.Width, Height: height}
	if below < height {
		content.Y = bounds.Y - height - 4
	}
	opening := menu.open && state.popups.owner != hitSettingsLocalShellDismiss
	scroll := state.selectScroll(menu.open, hitSettingsLocalShellDismiss)
	if opening && selected >= 0 {
		scroll.Offset = max(0, min(float32(selected)*32, float32(len(menu.choices)*32)+8-height))
		canvas.SetInitialFocus(hitSettingsLocalShellItems.At(selected))
	}
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitSettingsLocalShell, Label: m.SettingsLocalShell, Value: value, Hint: hint,
		Disabled: len(menu.choices) == 0, Open: menu.open, Content: content, Layer: popupLayer,
		Scope: hitSettingsLocalShellDismiss, DismissID: hitSettingsLocalShellDismiss, Viewport: state.windowRect(), Scroll: scroll,
	})
	defer control.End()
	if menu.open {
		state.menuPlaced(control.ContentBounds())
		for index, choice := range menu.choices {
			control.AddItem(components.SelectItem{ID: hitSettingsLocalShellItems.At(index), Label: choice.Name, Selected: index == selected})
		}
	}
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
	index, ok := id.In(hitSettingsLocalShellItems)
	if !ok || index < 0 || index >= len(menu.choices) {
		return false
	}
	program := menu.choices[index].Path
	menu.open = false
	if state.localShell != program {
		state.localShell = program
		state.save()
	}
	return true
}
