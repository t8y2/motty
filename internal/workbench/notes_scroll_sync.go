package workbench

import (
	"math"

	"github.com/vivaui/viva/gfx"
)

// Programmatic scrolls are recorded after applying them, so only a subsequent
// user scroll (or the editor revealing its caret) can change the driving side.
type noteSplitScroll struct {
	active, preview, pending    bool
	sourceY, previewY           float32
	anchor                      int
	edge                        int
	source                      string
	sourceBounds, previewBounds gfx.Rect
	height                      float32
	caret                       int
	inImages                    bool
	imageProgress               float32
}

func (s *State) noteScrollDriver(preview bool) {
	v := &s.noteContent().view.splitScroll
	if v.active {
		v.preview, v.pending = preview, true
	}
}

func noteScrollEdge(y, maximum float32) int {
	if y <= 0 {
		return -1
	}
	if maximum > 0 && y >= maximum-.5 {
		return 1
	}
	return 0
}

func (s *State) syncNoteSplit(c *gfx.Canvas) {
	n := s.noteContent()
	v := &n.view.splitScroll
	selection, _ := n.noteSelections()
	source := n.body.String()
	sy, py := n.code.ScrollY(), n.previewScroll
	sm, pm := n.code.MaxScrollY(), max(0, n.previewHeight-n.previewBounds.Height)
	changedLayout := v.sourceBounds != n.sourceBounds || v.previewBounds != n.previewBounds || v.height != n.previewHeight
	edited := v.source != source || v.caret != selection.End
	restore := v.active && changedLayout && !edited && !v.pending
	if !v.active {
		v.preview = false
	} else if edited {
		v.preview = false
	} else if !v.pending && !restore && sy != v.sourceY {
		v.preview = false
	} else if !v.pending && !restore && py != v.previewY {
		v.preview = true
	}
	if restore {
		if v.preview {
			if y, ok := n.rendered.YAtSource(source, v.anchor); ok {
				py = y + 6
			}
			if v.inImages {
				py = n.rendered.Height() + 6 + v.imageProgress*max(0, n.previewHeight-n.rendered.Height()-12)
			}
			if v.edge == -1 {
				py = 0
			} else if v.edge == 1 {
				py = pm
			}
			n.previewScroll = min(max(0, py), pm)
		} else {
			if y, ok := n.code.YAtOffset(v.anchor); ok {
				sy = y
			}
			if v.edge == -1 {
				sy = 0
			} else if v.edge == 1 {
				sy = sm
			}
			n.code.SetScrollY(sy)
		}
	}
	if v.preview {
		py = min(max(0, n.previewScroll), pm)
		v.edge = noteScrollEdge(py, pm)
		offset, ok := n.rendered.SourceAtY(source, max(0, py-6))
		v.inImages = py >= n.rendered.Height()+6 && n.previewHeight > n.rendered.Height()+12
		if v.inImages {
			v.imageProgress = (py - n.rendered.Height() - 6) / max(1, n.previewHeight-n.rendered.Height()-12)
		}
		if py >= n.rendered.Height()+6 {
			offset, ok = len(source), true
		}
		if ok {
			v.anchor = offset
			if y, found := n.code.YAtOffset(offset); found {
				n.code.SetScrollY(y)
			}
		}
		if v.edge == -1 {
			n.code.SetScrollY(0)
		} else if v.edge == 1 {
			n.code.SetScrollY(sm)
		}
	} else {
		v.inImages = false
		sy = n.code.ScrollY()
		v.edge = noteScrollEdge(sy, sm)
		if offset, ok := n.code.OffsetAtY(sy); ok {
			v.anchor = offset
			if y, found := n.rendered.YAtSource(source, offset); found {
				n.previewScroll = min(max(0, y+6), pm)
			}
		}
		if v.edge == -1 {
			n.previewScroll = 0
		} else if v.edge == 1 {
			n.previewScroll = pm
		}
	}
	// The source was already painted this frame; a preview-driven update needs
	// one more paint, without reinterpreting that update as user input.
	if math.Abs(float64(n.code.ScrollY()-sy)) > .01 || restore && !v.preview {
		c.RequestFrameIn(0)
	}
	v.active, v.pending = true, false
	v.sourceY, v.previewY = n.code.ScrollY(), n.previewScroll
	v.source, v.caret = source, selection.End
	v.sourceBounds, v.previewBounds, v.height = n.sourceBounds, n.previewBounds, n.previewHeight
}
