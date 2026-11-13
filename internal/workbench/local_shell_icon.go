package workbench

import (
	"path"
	"strings"

	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
	"motty/internal/workbench/icons"
)

// A local terminal wears the mark of the shell it runs, rather than this
// computer's system badge: PowerShell, CMD and each WSL distribution are
// different tools, and a row of them should read as such at a glance.

// localShellForIcon is the shell a local host runs: the one it was opened with,
// else the setting's default. A default that discovery has not named yet falls
// back to the plain terminal mark; painting never starts discovery.
func (state *State) localShellForIcon(host Host) string {
	if host.localShell != "" {
		return host.localShell
	}
	if state.localShell != "" {
		return state.localShell
	}
	if state.localShellMenu.loaded {
		return state.defaultShellChoice().Path
	}
	return ""
}

// paintLocalShellIcon draws the mark of a local host and reports whether it
// did. Every local host gets one, so it never falls through to the system
// badge of the computer running the shell.
func (state *State) paintLocalShellIcon(canvas *gfx.Canvas, host Host, bounds gfx.Rect, background gfx.Color) bool {
	shell := state.localShellForIcon(host)
	if distro, ok := localshell.WSLDistro(shell); ok {
		if art, ok := wslDistroBadgeArt(distro, state.pal.dark); ok {
			paintBadgeArt(canvas, art, bounds)
			return true
		}
	}
	if glyph, ok := localShellGlyph(shell); ok {
		// The Material glyphs carry their own colors, so the tint only applies
		// to a shape that has none.
		paintFilledIcon(canvas, glyph, insetRect(bounds, 0.12), gfx.Color{Alpha: 1})
		return true
	}
	canvas.Rounded(bounds, bounds.Width*0.24, state.hostBadgeFill(background))
	paintIcon(canvas, iconTerminal, insetRect(bounds, 0.24), state.pal.accent)
	return true
}

// localShellGlyph is the colored mark of a Windows shell whose program name
// tells it apart. A shell without one keeps the terminal icon.
func localShellGlyph(shell string) (gfx.Icon, bool) {
	name := strings.ToLower(path.Base(strings.ReplaceAll(shell, `\`, "/")))
	switch name {
	case "pwsh", "pwsh.exe", "powershell", "powershell.exe":
		return icons.MitPowershell, true
	case "cmd", "cmd.exe":
		return icons.MitConsole, true
	}
	return gfx.Icon{}, false
}
