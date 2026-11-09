package workbench

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/monitoring"
	"motty/internal/remotestats"
	"motty/internal/sshconn"
)

var dashboardHits = tree.Feature("monitor-dashboard")
var (
	hitDashboard              = tree.ID(dashboardHits, "navigation")
	hitDashboardNew           = tree.ID(dashboardHits, "new")
	hitDashboardToggle        = tree.ID(dashboardHits, "start-stop")
	hitDashboardSwitch        = tree.ID(dashboardHits, "switch")
	hitDashboardEdit          = tree.ID(dashboardHits, "edit")
	hitDashboardDelete        = tree.ID(dashboardHits, "delete")
	hitDashboardSimple        = tree.ID(dashboardHits, "simple")
	hitDashboardDetailed      = tree.ID(dashboardHits, "detailed")
	hitDashboardSearch        = tree.ID(dashboardHits, "search")
	hitDashboardInterval      = tree.ID(dashboardHits, "interval")
	hitDashboardRetention     = tree.ID(dashboardHits, "retention")
	hitDashboardDismiss       = tree.ID(dashboardHits, "dismiss")
	hitDashboardScroll        = tree.ID(dashboardHits, "scroll")
	hitDashboardThumb         = tree.ID(dashboardHits, "thumb")
	hitDashboardXThumb        = tree.ID(dashboardHits, "horizontal-thumb")
	hitDashboardDialogSave    = tree.ID(dashboardHits, "dialog-save")
	hitDashboardDialogCancel  = tree.ID(dashboardHits, "dialog-cancel")
	hitDashboardName          = tree.ID(dashboardHits, "name")
	hitDashboardNote          = tree.ID(dashboardHits, "note")
	hitDashboardHostQuery     = tree.ID(dashboardHits, "host-query")
	hitDashboardHostScroll    = tree.ID(dashboardHits, "host-scroll")
	hitDashboardHostThumb     = tree.ID(dashboardHits, "host-thumb")
	hitDashboardPanelList     = tree.Sub(dashboardHits, tree.Name("panel"))
	hitDashboardIntervalList  = tree.Sub(dashboardHits, tree.Name("interval-option"))
	hitDashboardRetentionList = tree.Sub(dashboardHits, tree.Name("retention-option"))
	hitDashboardHostList      = tree.Sub(dashboardHits, tree.Name("host-option"))
	hitDashboardMissingList   = tree.Sub(dashboardHits, tree.Name("missing-host"))
	hitDashboardTerminalList  = tree.Sub(dashboardHits, tree.Name("terminal"))
	hitDashboardRetryList     = tree.Sub(dashboardHits, tree.Name("retry"))
)
var hitDashboardViewList = tree.Sub(dashboardHits, tree.Name("host-view"))
var hitDashboardSimpleViewList = tree.Sub(dashboardHits, tree.Name("simple-view"))
var hitDashboardSudoList = tree.Sub(dashboardHits, tree.Name("sudo"))
var dashboardIntervals = [...]int{2, 5, 10, 30, 60}
var dashboardRetentions = [...]int{15, 60, 360, 1440}

type dashboardEditor struct {
	id                                   string
	name, note, query                    text.Editor
	selected                             []string
	error                                string
	scroll, content                      float32
	bounds                               gfx.Rect
	fromHosts                            bool
	page                                 int
	order                                components.ReorderListState
	groups                               []string
	group                                string
	groupFilter, groupOpen, selectedOnly bool
}
type dashboardState struct {
	panels          []monitoring.Panel
	active          string
	running         map[string]bool
	engine          monitoring.Engine
	sources         map[string]*dashboardSource
	rows            map[string]*dashboardRow
	search          text.Editor
	editor          dashboardEditor
	menu            int
	selectBounds    [3]gfx.Rect
	body            gfx.Rect
	scroll, content float32
	width           int
	refresh         monitorView
	emptyLayout     ui.View
	scrollX         float32
	dragging        bool
	dragX           bool
	grab            float32
	addHosts        []string
	addAnchor       gfx.Rect
	// visible reports whether the dashboard is the page the window last
	// painted, so a background panel does not repaint the window from another
	// section. Paint writes it; collector goroutines read it.
	visible atomic.Bool
}
type dashboardSource struct {
	session              Session
	interval, collecting int
	cancel               context.CancelFunc
	errors               chan error
	blocked              bool
	connected            bool
	retry                int
	retryAt              time.Duration
	timer                *time.Timer
}

func (d *dashboardState) panelDefinitions() []monitoring.Panel {
	panels := slices.Clone(d.panels)
	for i := range panels {
		panels[i].Hosts = slices.Clone(panels[i].Hosts)
	}
	return panels
}

func dashboardHostEligible(h Host) bool { return h.isSSH() && h.ID != localHostID }

func (s *State) dashboardPanel() *monitoring.Panel {
	for i := range s.dashboard.panels {
		if s.dashboard.panels[i].ID == s.dashboard.active {
			return &s.dashboard.panels[i]
		}
	}
	return nil
}
func (s *State) dashboardHost(id string) (Host, bool) {
	for _, h := range s.hosts {
		if h.CatalogID == id {
			return h, true
		}
	}
	return Host{}, false
}

func (s *State) reconcileDashboard() {
	d := &s.dashboard
	if d.sources == nil {
		d.sources = make(map[string]*dashboardSource)
	}
	for _, p := range d.panels {
		d.engine.Set(p, d.running[p.ID])
	}
	wanted := d.engine.Wanted()
	for id, source := range d.sources {
		if _, ok := wanted[id]; !ok {
			s.closeDashboardSource(source)
			delete(d.sources, id)
		}
	}
	for id, interval := range wanted {
		host, ok := s.dashboardHost(id)
		if !ok {
			d.engine.SetStatus(id, monitoring.Missing)
			continue
		}
		if !dashboardHostEligible(host) {
			if source := d.sources[id]; source != nil {
				s.closeDashboardSource(source)
				delete(d.sources, id)
			}
			d.engine.SetStatus(id, monitoring.Unsupported)
			continue
		}
		source := d.sources[id]
		if source == nil {
			source = &dashboardSource{session: Session{ID: s.nextSessionID, HostID: host.ID, Title: host.Label, Address: s.sessionAddress(host), host: host}, interval: interval}
			s.nextSessionID++
			d.sources[id] = source
			s.connectDashboardSource(source)
		} else if source.interval != interval {
			source.interval = interval
			s.dashboard.engine.BeginSource(id)
			if source.cancel != nil {
				source.cancel()
				source.cancel = nil
				source.collecting = 0
				source.errors = nil
			}
		}
	}
	s.pumpDashboard()
}

func (s *State) connectDashboardSource(source *dashboardSource) {
	source.session.sudoCommands, source.session.sudoPassword = false, ""
	s.dashboard.engine.BeginSource(source.session.host.CatalogID)
	if source.cancel != nil {
		source.cancel()
		source.cancel = nil
	}
	source.collecting = 0
	source.retryAt = 0
	source.errors = nil
	if source.session.conn != nil {
		source.session.conn.Cancel()
		source.session.conn = nil
	}
	if host, ok := s.dashboardHost(source.session.host.CatalogID); ok {
		source.session.host = host
		source.session.HostID = host.ID
		source.session.Title = host.Label
		source.session.Address = s.sessionAddress(host)
	}
	if source.timer != nil {
		source.timer.Stop()
		source.timer = nil
	}
	v := &source.session
	v.phase = sshconn.PhaseConnecting
	v.version = 0
	v.answered = 0
	v.otpSent = false
	v.attempt = 0
	v.questions = nil
	v.err = nil
	v.pendingSecretName = ""
	v.pendingSecretValue = ""
	v.endpoint = sshconn.Endpoint{Host: v.host.Address, Port: v.host.Port, User: s.login(v.host)}
	v.role = sshconn.EndpointTarget
	config, ok := s.sshConfigFor(v, v.host)
	if !ok {
		v.phase = sshconn.PhaseFailed
		source.blocked = true
		s.dashboard.engine.SetStatus(v.host.CatalogID, monitoring.Failed)
		return
	}
	v.conn, v.shared = nil, false
	if s.shareConnections == shareConnectionsOn {
		if shared, _ := s.shareFrom(v, v.host); shared != nil {
			v.conn, v.shared = shared, true
		}
	}
	if v.conn == nil {
		v.conn = s.options.Dial(config)
	}
	source.blocked = false
	source.retryAt = 0
	s.dashboard.engine.SetStatus(v.host.CatalogID, monitoring.Connecting)
}
func (s *State) closeDashboardSource(source *dashboardSource) {
	source.session.sudoCommands, source.session.sudoPassword = false, ""
	s.dashboard.engine.BeginSource(source.session.host.CatalogID)
	source.collecting = 0
	source.errors = nil
	if source.cancel != nil {
		source.cancel()
		source.cancel = nil
	}
	if source.timer != nil {
		source.timer.Stop()
		source.timer = nil
	}
	if s.modalSession == source.session.ID {
		s.closeModal()
	}
	if s.modal == modalSudoSFTP && s.sudoSFTP.dashboard && s.sudoSFTP.sessionID == source.session.ID {
		s.closeModal()
	}
	if source.session.conn != nil {
		source.session.conn.Cancel()
		source.session.conn = nil
	}
}
func (s *State) closeDashboard() {
	for _, source := range s.dashboard.sources {
		s.closeDashboardSource(source)
	}
	s.dashboard.sources = nil
}

func (s *State) pumpDashboard() {
	for id, source := range s.dashboard.sources {
		host, exists := s.dashboardHost(id)
		if !exists || !dashboardHostEligible(host) {
			s.closeDashboardSource(source)
			delete(s.dashboard.sources, id)
			status := monitoring.Missing
			if exists {
				status = monitoring.Unsupported
			}
			s.dashboard.engine.SetStatus(id, status)
			continue
		}
		if s.sessionAddress(host) != source.session.Address {
			s.closeDashboardSource(source)
			source.connected = false
			source.retry = 0
			s.connectDashboardSource(source)
		}
		v := &source.session
		if source.retryAt > 0 && s.now() >= source.retryAt {
			s.connectDashboardSource(source)
		}
		if v.conn == nil {
			continue
		}
		status := v.conn.Status()
		if status.Version != v.version || status.Phase != v.phase {
			v.version, v.phase, v.attempt = status.Version, status.Phase, status.Attempt
			v.hostKey, v.questions, v.identity = status.HostKey, status.Questions, status.IdentityPath
			v.role, v.err = status.EndpointRole, status.Err
			if status.Endpoint.Host != "" {
				v.endpoint = status.Endpoint
			}
			s.adoptSystem(v, status)
			if s.modalSession == v.ID && !isPromptPhase(v.phase) {
				s.closeModal()
			}
			if v.phase == sshconn.PhaseConnected {
				source.connected = true
				source.retry = 0
				s.rememberSecret(v)
			}
			if v.phase == sshconn.PhaseClosed || v.phase == sshconn.PhaseFailed {
				v.sudoCommands, v.sudoPassword = false, ""
				s.dashboard.engine.BeginSource(id)
				if source.cancel != nil {
					source.cancel()
					source.cancel = nil
					source.collecting = 0
				}
				s.dashboard.engine.SetStatus(id, monitoring.Failed)
				if source.connected && (errors.Is(status.Err, sshconn.ErrDisconnected) || source.retry > 0) && !reconnectTerminalError(status.Err) && s.connectionReconnect.permits(source.retry+1) {
					source.retry++
					delay := reconnectDelay(source.retry)
					source.retryAt = s.now() + delay
					if source.timer != nil {
						source.timer.Stop()
					}
					source.timer = time.AfterFunc(delay, func() {
						if s.options.Invalidate != nil {
							s.options.Invalidate()
						}
					})
				}
			}
		}
		if source.errors != nil {
			select {
			case err := <-source.errors:
				source.blocked = true
				if source.cancel != nil {
					source.cancel()
					source.cancel = nil
				}
				source.collecting = 0
				state := monitoring.Failed
				if errors.Is(err, remotestats.ErrUnsupported) {
					state = monitoring.Unsupported
				}
				s.dashboard.engine.SetStatus(id, state)
			default:
			}
		}
		if v.phase == sshconn.PhaseConnected && !source.blocked && source.collecting == 0 {
			s.startDashboardCollector(id, source)
		}
	}
}

func (s *State) startDashboardCollector(id string, source *dashboardSource) {
	system := source.session.host.System
	if !monitorSupported(system) {
		source.blocked = true
		s.dashboard.engine.SetStatus(id, monitoring.Unsupported)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	source.cancel = cancel
	source.collecting = source.interval
	faults := make(chan error, 1)
	source.errors = faults
	// Disks and interfaces change slowly; their details keep the sidebar's
	// five-second floor even when the sample cadence is faster, so a fast
	// panel does not run df and interface counters every two seconds.
	runner := commandRunnerFor(&source.session)
	worker := startMonitorWorker(runner, nil, system, source.interval)
	details := startMonitorDetails(runner, nil, system, max(source.interval, 5))
	var detailResults <-chan remotestats.DetailResult
	if details != nil {
		detailResults = details.Results()
	}
	engine, wake, visible := &s.dashboard.engine, s.options.Invalidate, &s.dashboard.visible
	// A sample only needs a repaint while the page is on screen. Connection
	// progress still wakes the window through the session's own notify relay,
	// and a collector error below always wakes it.
	notifySample := func() {
		if wake != nil && visible.Load() {
			wake()
		}
	}
	generation := engine.BeginSource(id)
	go func() {
		defer worker.Close()
		if details != nil {
			defer details.Close()
		}
		for {
			select {
			case <-ctx.Done():
				return
			case r, ok := <-worker.Results():
				if !ok {
					return
				}
				if ctx.Err() != nil {
					return
				}
				if r.Err != nil {
					select {
					case faults <- r.Err:
					default:
					}
					if wake != nil {
						wake()
					}
					return
				}
				if r.Details != nil {
					engine.DetailsFrom(id, generation, *r.Details, false)
				}
				engine.PublishFrom(id, generation, time.Now(), r.Sample)
				notifySample()
			case r, ok := <-detailResults:
				if !ok {
					detailResults = nil
					continue
				}
				if ctx.Err() != nil {
					return
				}
				engine.DetailsFrom(id, generation, r.Details, r.Err != nil)
				notifySample()
			}
		}
	}()
}

func (s *State) openDashboardEditor(edit bool) {
	e := dashboardEditor{}
	if edit {
		if p := s.dashboardPanel(); p != nil {
			e.id = p.ID
			e.name.Insert(p.Name)
			e.note.Insert(p.Note)
			e.selected = slices.Clone(p.Hosts)
		}
	}
	s.dashboard.editor = e
	s.dashboard.menu = 0
	s.modal = modalDashboard
}
func (s *State) submitDashboard() {
	d, e, m := &s.dashboard, &s.dashboard.editor, s.messages.Dashboard
	name := strings.TrimSpace(e.name.String())
	if name == "" || utf8.RuneCountInString(name) > 40 {
		e.error = m.NameRequired
		e.page = 0
		return
	}
	for _, p := range d.panels {
		if p.ID != e.id && strings.EqualFold(p.Name, name) {
			e.error = m.DuplicateName
			e.page = 0
			return
		}
	}
	if len(e.selected) == 0 {
		e.error = m.HostsRequired
		e.page = 0
		return
	}
	p := monitoring.Panel{ID: e.id, Name: name, Note: string([]rune(strings.TrimSpace(e.note.String()))[:min(160, utf8.RuneCountInString(strings.TrimSpace(e.note.String())))]), Hosts: slices.Clone(e.selected), Interval: 5, Retention: 60}
	index := -1
	for i := range d.panels {
		if d.panels[i].ID == p.ID {
			index = i
			p.Simple = d.panels[i].Simple
			p.Interval = d.panels[i].Interval
			p.Retention = d.panels[i].Retention
		}
	}
	if index < 0 {
		p.ID = rand.Text()
		d.panels = append(d.panels, p)
	} else {
		d.panels[index] = p
	}
	d.active = p.ID
	if e.fromHosts {
		s.activeSection = sectionDashboard
	}
	d.search = text.Editor{}
	d.scroll = 0
	s.pruneDashboardRows(p.ID, p.Hosts)
	s.closeModal()
	s.reconcileDashboard()
	s.save()
}
func (s *State) deleteDashboard() {
	p := s.dashboardPanel()
	if p == nil {
		return
	}
	id := p.ID
	m := s.messages.Dashboard
	s.openForm(formDialog{title: m.Delete, description: m.DeletePrompt, submit: m.Delete, danger: true, run: func(s *State, _ *formDialog) string {
		d := &s.dashboard
		d.panels = slices.DeleteFunc(d.panels, func(p monitoring.Panel) bool { return p.ID == id })
		delete(d.running, id)
		d.engine.Remove(id)
		d.active = ""
		if len(d.panels) > 0 {
			d.active = d.panels[0].ID
		}
		s.pruneDashboardRows(id, nil)
		d.search = text.Editor{}
		d.scroll = 0
		s.reconcileDashboard()
		s.save()
		return ""
	}})
}
func (s *State) dashboardEditorFor(hit gfx.ID) *text.Editor {
	if s.modal == modalDashboard {
		if s.dashboard.editor.page != 0 {
			return nil
		}
		switch hit {
		case hitDashboardName:
			return &s.dashboard.editor.name
		case hitDashboardNote:
			return &s.dashboard.editor.note
		case hitDashboardHostQuery:
			return &s.dashboard.editor.query
		}
	}
	if s.modal == modalNone && s.activeSection == sectionDashboard && hit == hitDashboardSearch {
		return &s.dashboard.search
	}
	return nil
}
func (s *State) clickDashboard(id gfx.ID) bool {
	d := &s.dashboard
	if s.clickDashboardManagement(id) {
		return true
	}
	if s.modal == modalDashboard {
		switch {
		case id == hitDashboardGroupFilter:
			d.editor.groupOpen = !d.editor.groupOpen
		case id == hitDashboardGroupDismiss:
			d.editor.groupOpen = false
		case id == hitDashboardSelectedOnly:
			d.editor.selectedOnly = !d.editor.selectedOnly
			d.editor.scroll = 0
		case id.Scope() == hitDashboardGroupList:
			i := item(id)
			if i >= 0 && i <= len(d.editor.groups) {
				d.editor.groupFilter = i > 0
				if i > 0 {
					d.editor.group = d.editor.groups[i-1]
				}
				d.editor.groupOpen = false
				d.editor.scroll = 0
			}
		case id == hitDashboardPickTab || id == hitDashboardOrderTab:
			d.editor.order.Cancel()
			d.editor.groupOpen = false
			d.editor.page = 0
			if id == hitDashboardOrderTab {
				d.editor.page = 1
			}
			d.editor.scroll = 0
		case id.Scope() == hitDashboardMissingList:
			if i := item(id); i < len(d.editor.selected) {
				if host, ok := s.dashboardHost(d.editor.selected[i]); !ok || !dashboardHostEligible(host) {
					d.editor.selected = slices.Delete(d.editor.selected, i, i+1)
				}
			}
		case id == hitDashboardDialogSave:
			s.submitDashboard()
		case id == hitDashboardDialogCancel:
			s.closeModal()
			s.showNextPrompt()
		case id.Scope() == hitDashboardHostList:
			h, ok := s.hostByID(uint32(item(id)))
			if ok && h.isSSH() && h.ID != localHostID {
				if slices.Contains(d.editor.selected, h.CatalogID) {
					d.editor.selected = slices.DeleteFunc(d.editor.selected, func(v string) bool { return v == h.CatalogID })
				} else {
					d.editor.selected = append(d.editor.selected, h.CatalogID)
				}
			}
		}
		return true
	}
	if id == hitDashboard && s.modal == modalNone {
		s.activeSection = sectionDashboard
		s.save()
		return true
	}
	if s.activeSection != sectionDashboard || s.modal != modalNone {
		return false
	}
	p := s.dashboardPanel()
	switch {
	case id.Scope() == hitDashboardSudoList && p != nil:
		if h, ok := s.hostByID(uint32(item(id))); ok {
			if source := d.sources[h.CatalogID]; source != nil && source.session.phase == sshconn.PhaseConnected {
				s.toggleSudoCommands(&source.session, false)
			}
		}
	case id.Scope() == hitDashboardSimpleViewList && p != nil:
		n := item(id)
		if h, ok := s.hostByID(uint32(n / 3)); ok {
			row := s.dashboardRow(p, h.CatalogID, int(d.body.Width))
			row.simplePage = n % 3
		}

	case id.Scope() == hitDashboardViewList && p != nil:
		n := item(id)
		if h, ok := s.hostByID(uint32(n / 3)); ok {
			row := s.dashboardRow(p, h.CatalogID, int(d.body.Width))
			row.page = n % 3
		}

	case id == hitDashboardNew:
		s.openDashboardEditor(false)
	case id == hitDashboardSwitch:
		if d.menu == 1 {
			d.menu = 0
		} else {
			d.menu = 1
		}
	case id == hitDashboardInterval:
		if d.menu == 2 {
			d.menu = 0
		} else {
			d.menu = 2
		}
	case id == hitDashboardRetention:
		if d.menu == 3 {
			d.menu = 0
		} else {
			d.menu = 3
		}
	case id == hitDashboardDismiss:
		d.menu = 0
	case id == hitDashboardSort:
		s.openDashboardOrder()
	case id == hitDashboardEdit:
		d.menu = 0
		s.openDashboardEditor(true)
	case id == hitDashboardDelete:
		d.menu = 0
		s.deleteDashboard()
	case id.Scope() == hitDashboardPanelList:
		if i := item(id); i < len(d.panels) {
			d.active = d.panels[i].ID
			d.search = text.Editor{}
			d.scroll = 0
			d.menu = 0
			s.save()
		}
	case id == hitDashboardToggle && p != nil:
		if d.running == nil {
			d.running = make(map[string]bool)
		}
		d.running[p.ID] = !d.running[p.ID]
		s.reconcileDashboard()
	case (id == hitDashboardSimple || id == hitDashboardDetailed) && p != nil:
		p.Simple = id == hitDashboardSimple
		d.scroll = 0
		s.save()
	case id.Scope() == hitDashboardIntervalList && p != nil:
		if i := item(id); i < len(dashboardIntervals) {
			p.Interval = dashboardIntervals[i]
			d.refresh.refreshReset = true
			d.menu = 0
			s.reconcileDashboard()
			s.save()
		}
	case id.Scope() == hitDashboardRetentionList && p != nil:
		if i := item(id); i < len(dashboardRetentions) {
			p.Retention = dashboardRetentions[i]
			d.menu = 0
			s.reconcileDashboard()
			s.save()
		}
	case id.Scope() == hitDashboardTerminalList:
		if h, ok := s.hostByID(uint32(item(id))); ok {
			s.openHostSession(h)
		}
	case id.Scope() == hitDashboardRetryList:
		if h, ok := s.hostByID(uint32(item(id))); ok {
			if source := d.sources[h.CatalogID]; source != nil {
				if source.cancel != nil {
					source.cancel()
					source.cancel = nil
				}
				source.collecting = 0
				source.errors = nil
				source.retry = 0
				s.connectDashboardSource(source)
				s.pumpDashboard()
			}
		}
	default:
		return false
	}
	return true
}

func (s *State) dashboardStatus(status monitoring.Status, running bool, hasSample bool) string {
	m := s.messages.Dashboard
	if !running {
		if hasSample {
			return m.Stopped
		}
		return m.Waiting
	}
	switch status {
	case monitoring.Live:
		return m.Live
	case monitoring.Failed:
		return m.Failed
	case monitoring.Unsupported:
		return m.Unsupported
	case monitoring.Missing:
		return m.Missing
	default:
		return m.Connecting
	}
}
func (s *State) dashboardRetention(minutes int) string {
	m := s.messages.Dashboard
	if minutes < 60 {
		return fmt.Sprintf(m.Minutes, minutes)
	}
	return fmt.Sprintf(m.Hours, minutes/60)
}

func (s *State) pruneDashboardRows(panel string, hosts []string) {
	prefix := panel + "/"
	for key := range s.dashboard.rows {
		if strings.HasPrefix(key, prefix) && !slices.Contains(hosts, strings.TrimPrefix(key, prefix)) {
			delete(s.dashboard.rows, key)
		}
	}
}
