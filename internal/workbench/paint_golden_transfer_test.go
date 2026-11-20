package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/zmodem"
)

func init() {
	extraPaintScenes = append(extraPaintScenes,
		paintScene{
			name:  "transfer-center",
			sizes: []gfx.Size{paintDesktop},
			open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
				state := transferDesignPreview(t)
				return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
			},
		},
		paintScene{
			name:  "transfer-progress",
			sizes: []gfx.Size{paintDesktop},
			open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
				state, session, _ := transferState(t)
				session.transfer.phase = transferRunning
				session.transfer.progress = zmodem.Progress{Name: "release-2026.tar.gz", Done: 24 << 20, Size: 100 << 20}
				return state, func(c *gfx.Canvas, size gfx.Size) { state.Paint(c, size) }
			},
		},
	)
}
