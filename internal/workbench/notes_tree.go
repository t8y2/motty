package workbench

import "math"

// noteTreeRowHeight is the height of one row of the notes library tree. The
// scroll content is measured in it and the visible range is chosen in it, so
// the two cannot drift apart.
const noteTreeRowHeight = 30

// noteTreeEntry is one flattened row of a notes tree. The library page and the
// notes side panel both walk the same shape.
type noteTreeEntry struct {
	id, label      string
	folder, pinned bool
	depth          int
}

// noteTreeScratch is what one paint of a notes tree walks: the flattened rows,
// the sorted sources and the sets that decide which notes and folders show. A
// page keeps it between paints, so a repaint reuses the storage instead of
// allocating a slice and several maps for the whole library. The tree paints
// only the rows in view; the walk itself still visits every note.
type noteTreeScratch struct {
	rows    []noteTreeEntry
	notes   []savedNote
	folders []savedNoteFolder
	// matches holds the notes that pass the query; parents the folders on the
	// way from those notes to the root; seen and walked are the cycle guards of
	// the note walk and the folder walk.
	matches, parents, seen, walked map[string]bool
}

// reset empties the scratch for a new paint, keeping its storage.
func (sc *noteTreeScratch) reset() {
	sc.rows = sc.rows[:0]
	sc.notes = sc.notes[:0]
	sc.folders = sc.folders[:0]
	sc.matches = clearNoteSet(sc.matches)
	sc.parents = clearNoteSet(sc.parents)
	sc.walked = clearNoteSet(sc.walked)
	sc.seen = clearNoteSet(sc.seen)
}

// clearNoteSet returns set empty, or a new set the first time.
func clearNoteSet(set map[string]bool) map[string]bool {
	if set == nil {
		return make(map[string]bool)
	}
	clear(set)
	return set
}

// noteTreeVisibleRange returns the rows [first, last) that intersect the
// viewport of viewportHeight at offset, for count rows of noteTreeRowHeight.
// A row that is only half in view is in the range: the tree clips the part
// outside, and leaving it out would show a gap at the edge.
func noteTreeVisibleRange(offset, viewportHeight float32, count int) (first, last int) {
	if count <= 0 || viewportHeight <= 0 {
		return 0, 0
	}
	first = int(math.Floor(float64(offset / noteTreeRowHeight)))
	last = int(math.Ceil(float64((offset + viewportHeight) / noteTreeRowHeight)))
	first = min(max(first, 0), count)
	last = min(max(last, first), count)
	return first, last
}
