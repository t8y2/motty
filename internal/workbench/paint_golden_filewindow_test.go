//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

// The document window's scenes: the editor with its footer controls, the save
// split button and its menu, the confirmations, and the comparison window. They
// are painted through the document's own page, as a window of its own paints it.
func init() {
	fileDocumentScene := func(name string, setup func(d *fileDocument), paint func(d *fileDocument, c *gfx.Canvas, size gfx.Size)) paintScene {
		return paintScene{name: name, sizes: []gfx.Size{paintDesktop, paintSmall}, open: func(t *testing.T) (*State, func(*gfx.Canvas, gfx.Size)) {
			d := documentFixture(t)
			if setup != nil {
				setup(d)
			}
			if paint == nil {
				paint = func(d *fileDocument, c *gfx.Canvas, size gfx.Size) { d.paint(c, size) }
			}
			return d.owner, func(c *gfx.Canvas, size gfx.Size) { paint(d, c, size) }
		}}
	}
	comparison := func(d *fileDocument) {
		d.local = false
		d.comparison.active = true
		d.comparison.baseline = d.original
		d.comparison.result = components.DiffCodeLines(d.original, d.editor.String())
	}
	extraPaintScenes = append(extraPaintScenes,
		fileDocumentScene("filewindow-editor", func(d *fileDocument) {
			d.local = false
			d.format.Encoding = "UTF-8"
		}, nil),
		fileDocumentScene("filewindow-save-menu", func(d *fileDocument) {
			d.local = false
			d.format.Encoding = "UTF-8"
			d.ui.saveMenuOpen = true
		}, nil),
		fileDocumentScene("filewindow-close-confirm", func(d *fileDocument) {
			d.editor.Insert("unsaved\n")
			d.confirm = "close"
		}, nil),
		fileDocumentScene("filewindow-reload-confirm", func(d *fileDocument) {
			d.editor.Insert("unsaved\n")
			d.confirm = "reload"
		}, nil),
		fileDocumentScene("filewindow-save-confirm", func(d *fileDocument) {
			d.local = false
			d.editor.Insert("unsaved\n")
			d.confirm = "save"
		}, nil),
		fileDocumentScene("filewindow-comparison", func(d *fileDocument) {
			comparison(d)
			d.editor.Insert("local edit\n")
		}, func(d *fileDocument, c *gfx.Canvas, size gfx.Size) { d.paintComparison(c, size) }),
		fileDocumentScene("filewindow-comparison-confirm", func(d *fileDocument) {
			comparison(d)
			d.editor.Insert("local edit\n")
			d.comparison.confirm = true
		}, func(d *fileDocument, c *gfx.Canvas, size gfx.Size) { d.paintComparison(c, size) }),
	)
}
