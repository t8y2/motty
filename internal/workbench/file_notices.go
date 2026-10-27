package workbench

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

var fileNoticeHits = tree.Feature("file-notices")
var hitFileNotices = tree.Sub(fileNoticeHits, tree.Name("dismiss"))

const maxFileNoticeDetailRunes = 240

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
	if utf8.RuneCountInString(detail) > maxFileNoticeDetailRunes {
		detail = string([]rune(detail)[:maxFileNoticeDetailRunes]) + "…"
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

func (notices *fileNotices) showPathReason(title, format, location, reason string, kind components.ToastType) {
	locationBudget := maxFileNoticeDetailRunes - utf8.RuneCountInString(fmt.Sprintf(format, "", reason))
	if locationBudget < 1 {
		locationBudget = 1
		reasonBudget := maxFileNoticeDetailRunes - utf8.RuneCountInString(fmt.Sprintf(format, "…", ""))
		if reasonBudget < 1 {
			reasonBudget = 1
		}
		reason = truncateRunes(reason, reasonBudget)
	}
	notices.show(title, fmt.Sprintf(format, elideMiddleRunes(location, locationBudget), reason), kind)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func elideMiddleRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return "…"
	}
	kept := limit - 1
	left := (kept + 1) / 2
	right := kept / 2
	return string(runes[:left]) + "…" + string(runes[len(runes)-right:])
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
