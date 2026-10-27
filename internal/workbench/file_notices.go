package workbench

import (
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

var fileNoticeHits = tree.Feature("file-notices")
var hitFileNotices = tree.Sub(fileNoticeHits, tree.Name("dismiss"))

type fileNotice struct {
	id            ui.ID
	title, detail string
	kind          components.ToastType
}
type fileNotices struct {
	queue components.ToastQueue
	items []fileNotice
	next  int
}

func (notices *fileNotices) show(title, detail string, kind components.ToastType) {
	if utf8.RuneCountInString(detail) > 240 {
		detail = string([]rune(detail)[:240]) + "…"
	}
	now := time.Now()
	for _, item := range notices.items {
		if item.title == title && item.detail == detail {
			notices.queue.Remove(item.id)
			notices.queue.Add(item.id, now, false)
			return
		}
	}
	notices.next++
	id := hitFileNotices.At(notices.next)
	if evicted, ok := notices.queue.Add(id, now, false); ok {
		notices.removeItem(evicted)
		notices.items = append(notices.items, fileNotice{id: id, title: title, detail: detail, kind: kind})
	}
}
func (notices *fileNotices) removeItem(id ui.ID) {
	for i, item := range notices.items {
		if item.id == id {
			notices.items = append(notices.items[:i], notices.items[i+1:]...)
			return
		}
	}
}
func (notices *fileNotices) click(id ui.ID) bool {
	if id.Scope() != hitFileNotices {
		return false
	}
	notices.queue.Remove(id)
	notices.removeItem(id)
	return true
}
func (notices *fileNotices) paint(canvas *ui.Canvas, theme ui.Theme, bounds ui.Rect) {
	now := time.Now()
	for {
		id, ok := notices.queue.Expire(now)
		if !ok {
			break
		}
		notices.removeItem(id)
	}
	offset := float32(0)
	for _, item := range notices.items {
		snapshot := components.ToastSnapshot{Title: item.title, Description: item.detail, Type: item.kind, OffsetY: offset}
		layout := components.PaintToast(canvas, theme, bounds, snapshot, components.ToastOptions{Layer: tooltipLayer + 1, DismissID: item.id, Position: components.ToastTopCenter})
		offset += layout.Bounds.Height + 8
		if canvas.Hovered() == item.id || canvas.Focused() == item.id {
			notices.queue.Pause(item.id, now)
		} else {
			notices.queue.Resume(item.id, now)
		}
	}
	if deadline, ok := notices.queue.NextDeadline(); ok {
		canvas.RequestFrameIn(max(deadline.Sub(now), time.Millisecond))
	}
}
