package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

const (
	fileWindowTabScrollbarGap    = float32(2)
	fileWindowTabScrollbarHeight = float32(6)
)

type fileWindowTabUI struct {
	viewport, scrollbar flex.Box
	contentWidth        float32
	geometry            components.ScrollAreaGeometry
	hovered, dragging   bool
	grab                float32
}

func (m *fileWindowManager) hasTab(d *fileDocument) bool {
	if d == nil {
		return false
	}
	for _, key := range m.order {
		if key == d.cacheKey {
			return m.documents[key] == d
		}
	}
	return false
}

func (m *fileWindowManager) activeDocument() *fileDocument {
	if m.active == "" {
		return nil
	}
	d := m.documents[m.active]
	if d == nil || d.closed || !m.hasTab(d) {
		return nil
	}
	return d
}

func (m *fileWindowManager) tabDocument(index int) *fileDocument {
	if index < 0 || index >= len(m.order) {
		return nil
	}
	d := m.documents[m.order[index]]
	if d == nil || d.closed {
		return nil
	}
	return d
}

func (m *fileWindowManager) tabLabel(d *fileDocument) string {
	if d == nil {
		return ""
	}
	if d.dirty() {
		return "* " + d.name
	}
	return d.name
}

func (m *fileWindowManager) activate(key string) {
	d := m.documents[key]
	if d == nil || d.closed || !m.hasTab(d) {
		return
	}
	// A tab the user picked now outranks a document that is still loading.
	m.pending = ""
	m.active = key
	if d.window != nil {
		d.window.Show()
		d.window.Invalidate()
	}
}

func (m *fileWindowManager) present(d *fileDocument) error {
	if d == nil || d.closed {
		return nil
	}
	if !m.hasTab(d) {
		m.order = append(m.order, d.cacheKey)
	}
	// Only the document the user asked for last becomes visible when it
	// finishes; an earlier, slower load keeps whatever tab is already shown.
	requested := m.pending == d.cacheKey
	if requested {
		m.pending = ""
	}
	if m.active == "" || requested {
		m.active = d.cacheKey
	}
	if m.window == nil {
		m.windowEpoch++
		epoch := m.windowEpoch
		config := m.config()
		config.OnClosed = func() { m.onWindowClosed(epoch) }
		window, err := d.owner.options.OpenContentWindow(config, nil)
		if err != nil {
			if m.windowEpoch == epoch {
				m.windowEpoch = 0
			}
			return err
		}
		m.window = window
		for _, key := range m.order {
			if document := m.documents[key]; document != nil {
				document.window = window
			}
		}
	} else {
		d.window = m.window
		m.window.Show()
	}
	m.window.Invalidate()
	return nil
}

func (m *fileWindowManager) forgetDocument(d *fileDocument) {
	if d == nil {
		return
	}
	key := d.cacheKey
	index := -1
	for i, candidate := range m.order {
		if candidate == key {
			index = i
			break
		}
	}
	if index >= 0 {
		m.order = append(m.order[:index], m.order[index+1:]...)
	}
	if m.documents[key] == d {
		delete(m.documents, key)
	}
	if m.pending == key {
		m.pending = ""
	}
	if m.active == key {
		m.active = ""
		if len(m.order) > 0 {
			next := min(index, len(m.order)-1)
			if next < 0 {
				next = len(m.order) - 1
			}
			m.active = m.order[next]
		}
	}
	d.dispose()
	if len(m.order) == 0 && !m.closingAll {
		m.resetTabNavigation()
		m.discardPendingDocuments()
		if m.window != nil {
			window := m.window
			m.window = nil
			m.windowEpoch = 0
			window.Close()
			return
		}
	}
	if m.window != nil {
		m.window.Invalidate()
	}
}

func (m *fileWindowManager) closeTab(index int) {
	if m.closingAll || m.confirming() {
		return
	}
	d := m.tabDocument(index)
	if d == nil {
		return
	}
	if d.dirty() || d.busy {
		// The pending close confirmation or completion belongs to this tab,
		// so make it the visible document before asking or waiting.
		m.activate(d.cacheKey)
	}
	d.closeCanceled = nil
	d.requestClose(func() { m.forgetDocument(d) })
}

func (m *fileWindowManager) closeActiveTab() {
	for i, key := range m.order {
		if key == m.active {
			m.closeTab(i)
			return
		}
	}
}

func (m *fileWindowManager) requestClose(allow func()) {
	if len(m.order) == 0 {
		allow()
		return
	}
	if m.closingAll {
		return
	}
	m.closingAll = true
	m.closeQueue = append(m.closeQueue[:0], m.order...)
	m.closeDone = allow
	m.continueClosing()
}

func (m *fileWindowManager) continueClosing() {
	for index := 0; index < len(m.closeQueue); {
		key := m.closeQueue[index]
		d := m.documents[key]
		if d == nil || d.closed {
			m.closeQueue = append(m.closeQueue[:index], m.closeQueue[index+1:]...)
			continue
		}
		if d.dirty() || d.busy {
			m.activate(d.cacheKey)
			m.closeQueue = append(m.closeQueue[:index], m.closeQueue[index+1:]...)
			d.closeCanceled = m.cancelClosing
			d.requestClose(func() {
				m.forgetDocument(d)
				m.continueClosing()
			})
			if m.window != nil {
				m.window.Show()
				m.window.Invalidate()
			}
			return
		}
		index++
	}
	for len(m.closeQueue) > 0 {
		d := m.documents[m.closeQueue[0]]
		m.closeQueue = m.closeQueue[1:]
		if d != nil && !d.closed {
			d.closeCanceled = nil
			m.forgetDocument(d)
		}
	}
	done := m.closeDone
	m.closeDone = nil
	m.closingAll = false
	m.closeQueue = nil
	if done != nil {
		done()
	}
}

func (m *fileWindowManager) cancelClosing() {
	m.closingAll = false
	m.closeQueue = nil
	m.closeDone = nil
	if m.window != nil {
		m.window.Show()
		m.window.Invalidate()
	}
}

func (m *fileWindowManager) onWindowClosed(epoch uint64) {
	if epoch == 0 || epoch != m.windowEpoch {
		return
	}
	m.window = nil
	m.windowEpoch = 0
	m.active = ""
	m.pending = ""
	m.order = nil
	m.resetTabNavigation()
	m.closingAll = false
	m.closeQueue = nil
	m.closeDone = nil
	documents := make([]*fileDocument, 0, len(m.documents))
	for _, d := range m.documents {
		documents = append(documents, d)
	}
	m.documents = nil
	for _, d := range documents {
		d.window = nil
		d.allowClose = nil
		d.afterClosed = nil
		d.closeCanceled = nil
		d.closeAfter = false
		d.dispose()
	}
}

func (m *fileWindowManager) forceClose() {
	window := m.window
	m.window = nil
	m.windowEpoch = 0
	m.active = ""
	m.pending = ""
	m.order = nil
	m.resetTabNavigation()
	m.closingAll = false
	m.closeQueue = nil
	m.closeDone = nil
	documents := make([]*fileDocument, 0, len(m.documents))
	for _, d := range m.documents {
		documents = append(documents, d)
	}
	m.documents = nil
	for _, d := range documents {
		d.window = nil
		d.allowClose = nil
		d.afterClosed = nil
		d.closeCanceled = nil
		d.closeAfter = false
		d.dispose()
	}
	if window != nil {
		window.Close()
	}
}

func (m *fileWindowManager) confirming() bool {
	for _, d := range m.documents {
		if d != nil && d.confirm != "" {
			return true
		}
	}
	return false
}

func (m *fileWindowManager) config() app.Config {
	return app.Config{
		Title:          m.messagesTitle(),
		Icon:           AppIcon(),
		Width:          1000,
		Height:         760,
		MinWidth:       fileWindowMinWidth,
		MinHeight:      fileWindowMinHeight,
		Tree:           Tree(),
		Paint:          m.paint,
		TextInput:      m.textInput,
		OnPointer:      m.pointer,
		OnKey:          m.key,
		OnShortcut:     m.shortcut,
		OnScroll:       m.scroll,
		OnCloseRequest: m.requestClose,
	}
}

func (m *fileWindowManager) discardPendingDocuments() {
	documents := make([]*fileDocument, 0, len(m.documents))
	for _, d := range m.documents {
		if d != nil && !m.hasTab(d) {
			documents = append(documents, d)
		}
	}
	for _, d := range documents {
		if m.documents[d.cacheKey] == d {
			delete(m.documents, d.cacheKey)
		}
		if m.pending == d.cacheKey {
			m.pending = ""
		}
		d.dispose()
	}
}

func (m *fileWindowManager) messagesTitle() string {
	for _, key := range m.order {
		if d := m.documents[key]; d != nil {
			return d.owner.messages.FileWindow.Title
		}
	}
	return ""
}

func (m *fileWindowManager) paint(canvas *ui.Canvas, size ui.Size) {
	d := m.activeDocument()
	if d == nil {
		canvas.Fill(ui.Rect{Width: size.Width, Height: size.Height}, m.ownerTheme())
		return
	}
	m.painting = true
	defer func() { m.painting = false }()
	d.paint(canvas, size)
}

func (m *fileWindowManager) ownerTheme() ui.Color {
	for _, key := range m.order {
		if d := m.documents[key]; d != nil {
			return d.owner.Theme().Background
		}
	}
	return ui.LightTheme().Background
}

func (m *fileWindowManager) resetTabNavigation() {
	m.tabStrip = tabStrip{}
	m.tabs = fileWindowTabUI{}
}

func documentTabLayout(available float32, count int) (width, content float32, overflow bool) {
	if count <= 0 {
		return 0, 0, false
	}
	available = max(available, 1)
	minimum := float32(count)*barTabMin + float32(count-1)*barGap
	maximum := float32(count)*barTabMax + float32(count-1)*barGap
	switch {
	case maximum <= available:
		width = barTabMax
	case minimum <= available:
		width = (available - float32(count-1)*barGap) / float32(count)
	default:
		width, overflow = barTabMin, true
	}
	if count == 1 && width > available {
		width = available
		overflow = false
	}
	width = max(width, 1)
	content = float32(count)*width + float32(count-1)*barGap
	return width, content, overflow
}

func (m *fileWindowManager) paintTabs(f *flex.Frame, size ui.Size) {
	if len(m.order) == 0 {
		return
	}
	activeDocument := m.activeDocument()
	if activeDocument == nil {
		return
	}
	theme := activeDocument.owner.Theme()
	metrics := theme.Metrics
	available := max(size.Width-24, 1)
	tabWidth, contentWidth, overflow := documentTabLayout(available, len(m.order))
	pitch := tabWidth + barGap
	activeIndex := -1
	for index, key := range m.order {
		if key == m.active {
			activeIndex = index
			break
		}
	}
	scroll := m.tabStrip.layoutIndex(ui.Rect{Width: available}, float32(len(m.order))*pitch, len(m.order), activeIndex, pitch, tabWidth)
	m.tabs.contentWidth = contentWidth
	sectionHeight := barTabHeight
	if overflow {
		sectionHeight += fileWindowTabScrollbarGap + fileWindowTabScrollbarHeight
	}
	f.Column().FillWidth().Height(sectionHeight).Shrink(0).Children(func() {
		m.tabs.viewport = f.Row().FillWidth().Height(barTabHeight).Shrink(0).Clip()
		m.tabs.viewport.Children(func() {
			m.paintDocumentTabRows(f, activeDocument, metrics, scroll)
		})
		if overflow {
			m.tabs.scrollbar = f.Slot().FillWidth().Height(fileWindowTabScrollbarHeight).Shrink(0).
				MarginSides(fileWindowTabScrollbarGap, 0, 0, 0)
		} else {
			m.tabs.scrollbar = flex.Box{}
		}
	})
}

func (m *fileWindowManager) paintDocumentTabRows(f *flex.Frame, activeDocument *fileDocument, metrics ui.Metrics, scroll float32) {
	pal := activeDocument.owner.pal
	for index, key := range m.order {
		d := m.documents[key]
		if d == nil || d.closed {
			continue
		}
		id, closeID := hitDocumentTabList.At(index), hitDocumentTabCloseList.At(index)
		active := key == m.active
		hovered := f.Canvas().Hovered() == id || f.Canvas().Hovered() == closeID
		foreground := pal.muted
		fill, edge := pal.tab, pal.line
		switch {
		case active:
			fill, edge, foreground = pal.tabActive, pal.windowStroke, pal.text
		case hovered:
			fill, foreground = pal.tabHover, pal.text
		}
		if color := d.hostColor(); color != "" {
			amount := float32(.08)
			if active {
				amount = .18
			} else if hovered {
				amount = .12
			}
			fill = tintHostSurface(fill, color, amount)
			edge = tintHostSurface(edge, color, .35)
		}
		label := m.tabLabel(d)
		first := float32(0)
		if index == 0 {
			first = -scroll
		}
		width := m.tabStrip.tabWidth
		f.Row().Size(width, barTabHeight).Shrink(0).MarginSides(0, barGap, 0, first).
			PadSides(0, 3, 0, 4).Items(flex.Center).Radius(radius.item).Border(1, edge).Background(fill).
			HitWith(id, label, ui.HitOptions{
				Role: ui.RoleTab, Selected: active, Hint: d.source + " · " + d.location,
			}).Children(func() {
			f.Row().Size(20, 20).Shrink(0).Center().MarginSides(0, 7, 0, 0).Children(func() {
				f.Icon(lucideIcons[iconFile]).Size(14, 14).Color(foreground)
			})
			f.Text(label).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(foreground).LineHeight(20).Ellipsis()
			f.Spacer()
			if !active && !hovered {
				f.Slot().Size(20, 20)
				return
			}
			var closeFill ui.Color
			if f.Canvas().Hovered() == closeID {
				closeFill = pal.line
				if active {
					closeFill = pal.tabCloseHot
				}
			}
			f.Row().Size(20, 20).Shrink(0).Center().Radius(radius.item).Background(closeFill).
				HitWith(closeID, fmt.Sprintf(d.owner.messages.FileWindow.CloseTabFmt, d.name), ui.HitOptions{Role: ui.RoleButton, SkipFocus: true}).Children(func() {
				f.Icon(lucideIcons[iconClose]).Size(14, 14).Color(foreground)
			})
		})
	}
}

func (d *fileDocument) hostColor() string {
	if d.owner == nil || d.local {
		return ""
	}
	if d.origin != nil {
		return d.origin.session.host.Color
	}
	if d.view == nil {
		return ""
	}
	if d.view.detached != nil {
		return d.view.detached.session.host.Color
	}
	for index := range d.owner.sessions {
		if d.owner.sessions[index].files == d.view {
			return d.owner.sessions[index].host.Color
		}
	}
	return ""
}

func (m *fileWindowManager) paintTabsScrollbar(canvas *ui.Canvas) {
	m.tabStrip.area = m.tabs.viewport.Rect()
	m.tabs.geometry = components.ScrollAreaGeometry{}
	if m.tabStrip.max <= 0 {
		return
	}
	active := m.activeDocument()
	bounds := m.tabs.scrollbar.Rect()
	if active == nil || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	area := components.BeginScrollArea(canvas, active.owner.Theme(), bounds, ui.Size{
		Width: m.tabs.contentWidth, Height: bounds.Height,
	}).Offset(m.tabStrip.offset, 0).
		HorizontalScrollbar(hitDocumentTabBar, active.owner.messages.FileWindow.TabsScrollbar, "").
		HoverScrollbars(m.tabs.hovered || m.tabs.dragging)
	m.tabs.geometry = area.EndContent()
}

func (m *fileWindowManager) trackTabPointer(event ui.PointerEvent) bool {
	over := event.Kind != ui.PointerExit && (pointInRect(event.X, event.Y, m.tabStrip.area) ||
		pointInRect(event.X, event.Y, m.tabs.geometry.HorizontalTrack))
	if m.tabs.hovered == over {
		return false
	}
	m.tabs.hovered = over
	return true
}

func (m *fileWindowManager) tabScrollbarPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Target != hitDocumentTabBar && !m.tabs.dragging {
		return ui.EventResult{}, false
	}
	if event.Kind == ui.PointerDown && event.Button != ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	geometry := m.tabs.geometry
	switch event.Kind {
	case ui.PointerDown:
		thumb := geometry.HorizontalThumb
		if thumb.Width <= 0 || geometry.MaxOffsetX <= 0 {
			return ui.EventResult{}, false
		}
		m.tabs.dragging = true
		m.tabs.grab = thumb.Width / 2
		if pointInRect(event.X, event.Y, thumb) {
			m.tabs.grab = event.X - thumb.X
		}
		m.tabStrip.offset = geometry.HorizontalOffsetForThumb(event.X - m.tabs.grab)
	case ui.PointerMove:
		if !m.tabs.dragging {
			return ui.EventResult{}, false
		}
		m.tabStrip.offset = geometry.HorizontalOffsetForThumb(event.X - m.tabs.grab)
	case ui.PointerUp, ui.PointerExit:
		m.tabs.dragging = false
	default:
		return ui.EventResult{}, false
	}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (m *fileWindowManager) scrollTabs(event ui.ScrollEvent) (ui.EventResult, bool) {
	overTabs := pointInRect(event.X, event.Y, m.tabStrip.area) ||
		pointInRect(event.X, event.Y, m.tabs.geometry.HorizontalTrack)
	if !overTabs {
		return ui.EventResult{}, false
	}
	if m.tabStrip.max <= 0 {
		return ui.EventResult{Handled: true}, true
	}
	delta := event.DeltaX
	if delta == 0 {
		delta = event.DeltaY
	}
	previous := m.tabStrip.offset
	m.tabStrip.offset = min(max(m.tabStrip.offset+delta, 0), m.tabStrip.max)
	return ui.EventResult{Handled: true, Repaint: m.tabStrip.offset != previous}, true
}

func (m *fileWindowManager) clickTab(id ui.ID) bool {
	switch id.Scope() {
	case hitDocumentTabList:
		index, ok := id.In(hitDocumentTabList)
		if ok {
			if d := m.tabDocument(index); d != nil {
				m.activate(d.cacheKey)
			}
		}
		return true
	case hitDocumentTabCloseList:
		index, ok := id.In(hitDocumentTabCloseList)
		if ok {
			m.closeTab(index)
		}
		return true
	default:
		return false
	}
}

func (m *fileWindowManager) textInput(context app.TextInputContext) *app.TextInput {
	if d := m.activeDocument(); d != nil {
		return d.textInput(context)
	}
	return nil
}

func (m *fileWindowManager) pointer(event ui.PointerEvent) ui.EventResult {
	hoverChanged := m.trackTabPointer(event)
	if result, handled := m.tabScrollbarPointer(event); handled {
		if hoverChanged {
			result.Repaint = true
		}
		return result
	}
	if d := m.activeDocument(); d != nil {
		result := d.pointer(event)
		if hoverChanged {
			result.Repaint = true
		}
		return result
	}
	return ui.EventResult{}
}

func (m *fileWindowManager) key(event ui.KeyEvent) ui.EventResult {
	if event.Release {
		return ui.EventResult{}
	}
	if event.Target.Scope() == hitDocumentTabList || event.Target.Scope() == hitDocumentTabCloseList {
		return m.tabKey(event)
	}
	if d := m.activeDocument(); d != nil {
		return d.key(event)
	}
	return ui.EventResult{}
}

func (m *fileWindowManager) tabKey(event ui.KeyEvent) ui.EventResult {
	index := -1
	if index, _ = event.Target.In(hitDocumentTabList); index < 0 {
		index, _ = event.Target.In(hitDocumentTabCloseList)
	}
	if index < 0 || index >= len(m.order) {
		return ui.EventResult{}
	}
	switch event.Key {
	case ui.KeyEnter, ui.KeySpace:
		if event.Target.Scope() == hitDocumentTabCloseList {
			m.closeTab(index)
		} else {
			m.activate(m.order[index])
		}
		return ui.EventResult{Handled: true, Repaint: true, Focus: event.Target, FocusVisible: true}
	case ui.KeyDelete:
		m.closeTab(index)
		return ui.EventResult{Handled: true, Repaint: true}
	case ui.KeyLeft:
		index = max(index-1, 0)
	case ui.KeyRight:
		index = min(index+1, len(m.order)-1)
	case ui.KeyHome:
		index = 0
	case ui.KeyEnd:
		index = len(m.order) - 1
	default:
		return ui.EventResult{}
	}
	key := m.order[index]
	m.activate(key)
	focus := hitDocumentTabList.At(index)
	if event.Target.Scope() == hitDocumentTabCloseList {
		focus = hitDocumentTabCloseList.At(index)
	}
	return ui.EventResult{Handled: true, Repaint: true, Focus: focus, FocusVisible: true}
}

func (m *fileWindowManager) shortcut(event ui.KeyEvent) ui.EventResult {
	if event.Release {
		return ui.EventResult{}
	}
	if d := m.activeDocument(); d != nil && d.confirm != "" {
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if event.Modifiers&(ui.ModifierControl|ui.ModifierMeta) != 0 && strings.EqualFold(event.Text, "w") {
		m.closeActiveTab()
		return ui.EventResult{Handled: true, Repaint: true}
	}
	if d := m.activeDocument(); d != nil {
		return d.shortcut(event)
	}
	return ui.EventResult{}
}

func (m *fileWindowManager) scroll(event ui.ScrollEvent) ui.EventResult {
	if result, handled := m.scrollTabs(event); handled {
		return result
	}
	if d := m.activeDocument(); d != nil {
		return d.scroll(event)
	}
	return ui.EventResult{}
}
