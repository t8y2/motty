package term

// SetScreenCleared receives a full display erase: the ClearScreen method, CSI
// 2J on the main screen, or a reset. The cursor line is not a block boundary any
// more, so an owner that groups lines should drop its groups.
func (t *Terminal) SetScreenCleared(fn func()) { t.screenClearedFn = fn }

// SetScrollbackCleared receives a history erase, not a display erase. Groups that
// still name lines on the screen stay; ones that lived in the dropped history
// must be pruned.
func (t *Terminal) SetScrollbackCleared(fn func()) { t.scrollbackClearedFn = fn }
