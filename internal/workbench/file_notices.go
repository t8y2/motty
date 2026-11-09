package workbench

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var fileNoticeHits = tree.Feature("file-notices")
var hitFileNotices = tree.Sub(fileNoticeHits, tree.Name("dismiss"))

const maxFileNoticeDetailRunes = 240

type fileNotice struct {
	id            gfx.ID
	title, detail string
	kind          components.ToastType
	// action is the button beside the text, when the notice offers one.
	action      gfx.ID
	actionLabel string
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

// showAction queues a notice with an action button. It is how news that needs
// no immediate answer is announced without a dialog taking the keyboard from
// what the user is doing; whatever it announces must also have a lasting place,
// since the notice expires.
func (notices *fileNotices) showAction(title, detail string, kind components.ToastType, action gfx.ID, actionLabel string) {
	for _, item := range notices.items {
		if item.title == title && item.detail == detail {
			return
		}
	}
	notices.next++
	id := hitFileNotices.At(notices.next)
	if evicted, ok := notices.queue.Add(id, time.Now(), false); ok {
		notices.removeItem(evicted)
		notices.items = append(notices.items, fileNotice{id: id, title: title, detail: detail, kind: kind, action: action, actionLabel: actionLabel})
	}
}

// dismissAction removes the notice whose button is action.
func (notices *fileNotices) dismissAction(action gfx.ID) {
	for _, item := range notices.items {
		if item.action == action {
			notices.queue.Remove(item.id)
			notices.removeItem(item.id)
			return
		}
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
func (notices *fileNotices) removeItem(id gfx.ID) {
	for i, item := range notices.items {
		if item.id == id {
			notices.items = append(notices.items[:i], notices.items[i+1:]...)
			return
		}
	}
}
func (notices *fileNotices) click(id gfx.ID) bool {
	if id.Scope() != hitFileNotices {
		return false
	}
	notices.queue.Remove(id)
	notices.removeItem(id)
	return true
}
func (notices *fileNotices) paint(canvas *gfx.Canvas, theme gfx.Theme, bounds gfx.Rect) {
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
		layout := components.PaintToast(canvas, theme, bounds, snapshot, components.ToastOptions{Layer: tooltipLayer + 1, DismissID: item.id, ActionID: item.action, ActionLabel: item.actionLabel, Position: components.ToastTopCenter})
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
