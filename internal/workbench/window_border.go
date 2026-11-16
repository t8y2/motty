package workbench

import "github.com/vivaui/viva/app"

// WindowBorderColor is the color of the system-drawn border around the main
// window: the surface its own edge is painted with, so the border Windows
// composites over the desktop does not show the desktop through it. Viva reads
// it at every paint, so a theme change reaches it.
func (state *State) WindowBorderColor() app.Color {
	return windowClearColor(state.WindowColor())
}
