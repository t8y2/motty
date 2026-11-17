//go:build dev

package workbench

import "github.com/vivaui/viva/gfx"

// devOnlyShellHits names the shell controls that only a development build
// declares: the sidebar entry of the component gallery.
func devOnlyShellHits() []gfx.ID { return []gfx.ID{hitDebug} }
