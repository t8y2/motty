//go:build !dev

package workbench

import "github.com/vivaui/viva/gfx"

// devOnlyShellHits is empty: a release build declares no development controls.
func devOnlyShellHits() []gfx.ID { return nil }
