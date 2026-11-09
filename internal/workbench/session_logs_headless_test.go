//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
	"motty/internal/sessionlog"
)

// The session-log page through a headless window: the same paint and input paths
// a real window takes, at the standard and the stress size, in both languages and
// both themes.

// openSessionLogsWindow starts a headless window on a state that already shows
// the page with the seeded journal. OnWake is the production wake path, so
// typing into the native search field becomes a query exactly as it does in a
// window.
func openSessionLogsWindow(t *testing.T, state *State, size gfx.Size) *headless.Session {
	return openSessionLogsWindowWithInvalidator(t, state, size, nil)
}

func openSessionLogsWindowWithInvalidator(t *testing.T, state *State, size gfx.Size, invalidator *app.Invalidator) *headless.Session {
	t.Helper()
	view, err := headless.Open(app.Config{
		Title: "Session Logs", Width: int(size.Width), Height: int(size.Height), Tree: Tree(),
		Invalidator: invalidator,
		Paint:       state.Paint, OnPointer: state.Pointer, OnKey: state.Key,
		OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize, OnWake: state.Wake,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(context.ID); editor != nil {
				return &app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
			}
			return nil
		},
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { view.Close() })
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestSessionLogsNativeIdleMidnightRefresh(t *testing.T) {
	for _, zone := range []string{"UTC", "America/Santiago"} {
		t.Run(zone, func(t *testing.T) { testSessionLogsIdleMidnightRefresh(t, zone) })
	}
}

func testSessionLogsIdleMidnightRefresh(t *testing.T, zone string) {
	t.Helper()
	t.Setenv("TZ", zone)
	location, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatal(err)
	}
	var invalidator app.Invalidator
	options := sessionLogsTestOptions(t)
	options.Invalidate = invalidator.Invalidate
	before := time.Date(2026, 9, 5, 23, 59, 59, 0, location)
	var started atomic.Pointer[time.Time]
	options.WallNow = func() time.Time {
		if start := started.Load(); start != nil {
			return before.Add(time.Since(*start))
		}
		return before
	}
	state := newTestState(t, options)
	sessionLogsInsert(t, state, sessionlog.Record{ID: "before-midnight", RunID: "run", Host: "host", Protocol: sessionlog.ProtocolSSH, StartedAt: before.Add(-time.Hour)}, sessionlog.StatusEnded, sessionlog.ReasonUserClosed, before)
	state.sessionLogs.view.timeRange = sessionLogTimeToday
	state.openSessionLogs()
	sessionLogsWait(t, state)
	view := openSessionLogsWindowWithInvalidator(t, state, gfx.Size{Width: 1180, Height: 807}, &invalidator)
	if state.sessionLogs.view.matched != 1 {
		t.Fatal("initial today row missing")
	}
	start := time.Now()
	started.Store(&start)
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-view.Driver().Wakes():
			if err := view.Driver().Pump(); err != nil {
				t.Fatal(err)
			}
			if !state.sessionLogs.view.loading && state.sessionLogs.view.matched == 0 {
				return
			}
		case <-deadline.C:
			t.Fatal("idle native page did not refresh at midnight")
		}
	}
}

// sessionLogsSettle lets the page's queued query finish and repaints the frame
// with what it produced. A real window is woken by the completion; here the
// resize to the same size is the redraw that runs the wake path and repaints.
func sessionLogsSettle(t *testing.T, view *headless.Session, state *State) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		state.pumpSessionLogs()
		painted := &state.sessionLogs.view
		if !painted.loading && !painted.loadingMore && !painted.eventsLoad {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the session log page did not settle")
		}
		time.Sleep(time.Millisecond)
	}
	snapshot := view.Snapshot()
	if err := view.Resize(snapshot.Width, snapshot.Height); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
}

// windowHitInside reports whether every painted control of the page is inside
// the window, which is what keeps it clickable at the stress size.
func windowHitsInside(t *testing.T, view *headless.Session, size gfx.Size) {
	t.Helper()
	for _, hit := range view.Snapshot().Hits {
		if hit.ID.IsZero() || hit.Block {
			continue
		}
		bounds := hit.Bounds
		if bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.Width > size.Width || bounds.Y+bounds.Height > size.Height {
			t.Fatalf("hit %q at %+v leaves the %v window", hit.Label, bounds, size)
		}
	}
}

// hasHitLabel reports whether a control with that accessible name is painted.
// Labels are what assistive technology reads, not always what is drawn: a
// filter trigger draws its value and names itself after the filter.
func hasHitLabel(view *headless.Session, label string) bool {
	for _, hit := range view.Snapshot().Hits {
		if hit.Label == label {
			return true
		}
	}
	return false
}

// textAt finds the painted text value inside a row, for the icon-alignment
// check between two navigation rows.
func textAt(texts []gfx.Text, value string, row gfx.Rect) (gfx.Text, bool) {
	for _, text := range texts {
		if text.Value == value && text.Bounds.X >= row.X && text.Bounds.Y >= row.Y &&
			text.Bounds.X+text.Bounds.Width <= row.X+row.Width &&
			text.Bounds.Y+text.Bounds.Height <= row.Y+row.Height {
			return text, true
		}
	}
	return gfx.Text{}, false
}

// The page paints at both window sizes, in both themes and languages: readable
// text, a navigation row aligned with its siblings, and a detail panel that
// switches rows with the pointer.
func TestSessionLogsNativeLayout(t *testing.T) {
	sizes := []gfx.Size{{Width: 1400, Height: 900}, {Width: 1180, Height: 807}, {Width: 760, Height: 620}}
	for _, size := range sizes {
		for _, language := range []i18n.Language{i18n.English, i18n.Chinese} {
			for _, look := range []appearance{appearanceDark, appearanceLight} {
				name := fmt.Sprintf("%gx%g/%v/%v", size.Width, size.Height, language, look)
				t.Run(name, func(t *testing.T) {
					state := newTestState(t, sessionLogsTestOptions(t))
					sessionLogsSeed(t, state)
					state.setLanguage(language)
					state.setAppearance(look)
					state.openSessionLogs()
					sessionLogsWait(t, state)
					view := openSessionLogsWindow(t, state, size)
					m := state.messages.SessionLogs

					windowHitsInside(t, view, size)
					sessionLogsScreenshot(t, view, fmt.Sprintf("%gx%g-%v-%v-list", size.Width, size.Height, language, look))

					snapshot := view.Snapshot()
					nav, ok := view.Hit(m.Title)
					if !ok || nav.Role != gfx.RoleMenuItem || !nav.Selected {
						t.Fatalf("the sidebar row = %+v, ok=%v", nav, ok)
					}
					// The row matches the Monitor row it follows, and its label
					// starts where that row's label does.
					monitor, ok := view.Hit(state.messages.Dashboard.Title)
					if !ok {
						t.Fatal("the sidebar has no monitor row")
					}
					if nav.Bounds.Width != monitor.Bounds.Width || nav.Bounds.Height != monitor.Bounds.Height {
						t.Fatalf("the session-log row %+v does not match the monitor row %+v", nav.Bounds, monitor.Bounds)
					}
					if gap := nav.Bounds.Y - monitor.Bounds.Y; gap != 40 {
						t.Fatalf("the session-log row is %g below Monitor, want the sidebar's 40", gap)
					}
					navText, ok := textAt(snapshot.Texts, m.Title, nav.Bounds)
					if !ok {
						t.Fatalf("the sidebar row's label is not painted: %+v", snapshot.Texts)
					}
					monitorText, ok := textAt(snapshot.Texts, state.messages.Dashboard.Title, monitor.Bounds)
					if !ok {
						t.Fatal("the monitor row's label is not painted")
					}
					if navText.Bounds.X != monitorText.Bounds.X {
						t.Fatalf("the labels start at %g and %g, so the icons are not aligned", navText.Bounds.X, monitorText.Bounds.X)
					}

					// Everything the user needs to read is painted, and every
					// control says what it does.
					for _, want := range []string{m.Title, m.ColumnTarget, m.ColumnWhen, m.ColumnStatus} {
						if !view.HasText(want) {
							t.Errorf("%q is not painted", want)
						}
					}
					for _, want := range []string{m.Search, m.FilterTime, m.FilterStatus, m.FilterProtocol, m.FilterFavorite} {
						if !hasHitLabel(view, want) {
							t.Errorf("no control is named %q", want)
						}
					}
					for _, host := range []string{"web-01", "rack-02", "db-01"} {
						if !view.HasText(host) {
							t.Errorf("the row for %q is not painted", host)
						}
					}
					if state.sessionLogs.view.countText == "" || !view.HasText(state.sessionLogs.view.countText) {
						t.Errorf("the result count is not painted")
					}

					// Selecting one row and then another switches the detail panel.
					// A narrow card puts the panel in place of the list, so it is
					// left again first.
					if err := view.ClickLabel("web-01 · alice"); err != nil {
						t.Fatal(err)
					}
					sessionLogsSettle(t, view, state)
					if state.sessionLogs.view.selected != "log-00" || !state.sessionLogs.view.detailOpen {
						t.Fatalf("clicking a row selected %q, open=%v", state.sessionLogs.view.selected, state.sessionLogs.view.detailOpen)
					}
					if !view.HasText(m.Details) || !view.HasText(m.FieldStarted) {
						t.Fatalf("the detail panel is not painted: %+v", view.Texts())
					}
					if !view.HasText("10.0.0.1") {
						t.Fatalf("the detail panel does not show the record's address: %v", view.Texts())
					}
					sessionLogsScreenshot(t, view, fmt.Sprintf("%gx%g-%v-%v-detail", size.Width, size.Height, language, look))
					if hasHitLabel(view, m.BackToList) {
						if !hasHitLabel(view, m.BackToList) {
							t.Fatal("the narrow detail panel has no way back to the list")
						}
						if err := view.ClickLabel(m.BackToList); err != nil {
							t.Fatal(err)
						}
						if err := view.Rest(); err != nil {
							t.Fatal(err)
						}
						if state.sessionLogs.view.detailOpen {
							t.Fatal("the way back to the list did not close the detail panel")
						}
					}
					if err := view.ClickLabel("db-01 · bob"); err != nil {
						t.Fatal(err)
					}
					sessionLogsSettle(t, view, state)
					if state.sessionLogs.view.selected != "log-01" {
						t.Fatalf("clicking the second row selected %q", state.sessionLogs.view.selected)
					}
					detailTexts := view.Texts()
					if !contains(detailTexts, "10.0.0.2") {
						t.Fatalf("the detail panel did not follow the selection: %v", detailTexts)
					}
					if contains(detailTexts, "10.0.0.1") {
						t.Fatalf("the previous record's address is still painted: %v", detailTexts)
					}

					if dir := os.Getenv("MOTTY_SESSION_LOGS_SCREENSHOTS"); dir != "" {
						path := filepath.Join(dir, fmt.Sprintf("session-logs-%gx%g-%v-%v.png", size.Width, size.Height, language, look))
						if err := view.Screenshot(path); err != nil {
							t.Logf("screenshot unavailable without the offscreen renderer: %v", err)
						}
					}
				})
			}
		}
	}
}

func sessionLogsScreenshot(t *testing.T, view *headless.Session, name string) {
	t.Helper()
	dir := os.Getenv("MOTTY_SESSIONLOG_SCREENSHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := view.Screenshot(filepath.Join(dir, name+".png")); err != nil {
		t.Fatal(err)
	}
}

func TestSessionLogsNativeP1Actions(t *testing.T) {
	state := sessionLogsState(t, true)
	state.setLanguage(i18n.Chinese)
	state.setAppearance(appearanceDark)
	size := gfx.Size{Width: 1180, Height: 807}
	view := openSessionLogsWindow(t, state, size)
	m := state.messages.SessionLogs
	if err := view.ClickLabel(state.sessionLogs.view.page[1].favoriteLabel); err != nil {
		t.Fatal(err)
	}
	sessionLogsWaitFor(t, state, func() bool { r, _ := state.sessionLogRecord("log-05"); return r.Favorite })
	sessionLogsSettle(t, view, state)
	if state.sessionLogs.view.selected != "" {
		t.Fatal("native favorite click also selected the row")
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(state.sessionLogs.view.page[1].favoriteLabel); err != nil {
		t.Fatal(err)
	}
	sessionLogsWaitFor(t, state, func() bool { r, _ := state.sessionLogRecord("log-05"); return !r.Favorite })
	sessionLogsSettle(t, view, state)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(state.sessionLogs.view.page[1].favoriteLabel); err != nil {
		t.Fatal(err)
	}
	sessionLogsWaitFor(t, state, func() bool { r, _ := state.sessionLogRecord("log-05"); return r.Favorite })
	sessionLogsSettle(t, view, state)
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(m.More); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	windowHitsInside(t, view, size)
	sessionLogsScreenshot(t, view, "p1-more-menu")
	for _, hit := range view.Snapshot().Hits {
		if hit.ID == hitSessionLogsMore && hit.Disabled {
			t.Fatal("open menu disabled its trigger")
		}
	}
	// The popup's dismiss layer owns presses outside its scope, including the
	// trigger; use its painted coordinates for the native outside-click path.
	anchor := state.sessionLogs.view.moreBounds
	if err := view.Click(anchor.X+anchor.Width/2, anchor.Y+anchor.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.sessionLogs.view.moreMenu {
		t.Fatal("more trigger did not close its menu")
	}
	if err := view.ClickLabel(m.More); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(m.ClearAll); err != nil {
		t.Fatal(err)
	}
	sessionLogsWaitFor(t, state, func() bool { return state.form.description != m.Loading })
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.form.description, "6") || !strings.Contains(state.form.description, "1") {
		t.Fatalf("native confirmation hides scope: %q", state.form.description)
	}
	sessionLogsScreenshot(t, view, "p1-clear-confirmation")
	if err := view.ClickLabel(state.messages.Cancel); err != nil {
		t.Fatal(err)
	}
	if len(sessionLogRows(t, state)) != 6 {
		t.Fatal("canceling a native cleanup changed the records")
	}
}

func TestSessionLogsNativeP2QueryActions(t *testing.T) {
	state := sessionLogsState(t, true)
	state.setLanguage(i18n.English)
	state.setAppearance(appearanceLight)
	size := gfx.Size{Width: 760, Height: 620}
	view := openSessionLogsWindow(t, state, size)
	m := state.messages.SessionLogs
	if err := view.ClickLabel(m.FilterSort); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(m.SortLongest); err != nil {
		t.Fatal(err)
	}
	sessionLogsSettle(t, view, state)
	if state.sessionLogs.view.query.Sort != sessionlog.SortDurationDesc || !view.HasText(m.ColumnDuration) {
		t.Fatal("duration ordering hides its comparison value in the narrow table")
	}
	windowHitsInside(t, view, size)
	sessionLogsScreenshot(t, view, "p2-duration-order")
	if err := view.ClickLabel(m.FilterTime); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(m.TimeCustom); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(m.DateFrom); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("cmd+a"); err != nil {
		t.Fatal(err)
	}
	if err := view.Type("not-a-date"); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(m.ApplyRange); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalForm || state.form.dialogError == "" {
		t.Fatal("invalid native range did not retain its validation state")
	}
	sessionLogsScreenshot(t, view, "p2-custom-invalid")
	for _, label := range []string{m.DateFrom, m.DateThrough} {
		if err := view.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
		if err := view.Keys("cmd+a"); err != nil {
			t.Fatal(err)
		}
		if err := view.Type("2026-10-30"); err != nil {
			t.Fatal(err)
		}
	}
	if err := view.ClickLabel(m.ApplyRange); err != nil {
		t.Fatal(err)
	}
	sessionLogsSettle(t, view, state)
	if state.modal != modalNone || len(state.sessionLogs.view.page) != 3 || state.sessionLogs.view.total != 6 {
		t.Fatal("native custom date range did not reach the query")
	}
	windowHitsInside(t, view, size)
	sessionLogsScreenshot(t, view, "p2-custom-range")
}

// Typing into the native field filters the page, Escape empties it, and the
// keyboard walks the filters and rows in the order they are painted.
func TestSessionLogsNativeKeyboardFlow(t *testing.T) {
	size := gfx.Size{Width: 1400, Height: 900}
	state := newTestState(t, sessionLogsTestOptions(t))
	sessionLogsSeed(t, state)
	state.openSessionLogs()
	sessionLogsWait(t, state)
	view := openSessionLogsWindow(t, state, size)
	m := state.messages.SessionLogs

	if err := view.ClickLabel(m.Search); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if focused := view.Snapshot().Focused; focused != hitSessionLogsSearch {
		t.Fatalf("clicking the search box focused %v", focused)
	}
	if err := view.Type("db-01"); err != nil {
		t.Fatal(err)
	}
	sessionLogsSettle(t, view, state)
	if got := state.sessionLogs.view.appliedSearch; got != "db-01" {
		t.Fatalf("the typed search did not reach the query: %q", got)
	}
	if got := len(state.sessionLogs.view.page); got != 1 {
		t.Fatalf("the typed search left %d rows", got)
	}
	// Escape in the field clears it and brings the rows back.
	if err := view.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	sessionLogsSettle(t, view, state)
	if state.sessionLogs.view.search.RuneCount() != 0 || len(state.sessionLogs.view.page) != 6 {
		t.Fatalf("Escape left search=%q rows=%d", state.sessionLogs.view.search.String(), len(state.sessionLogs.view.page))
	}
	// Tab leaves the field and reaches the page's own controls.
	if err := view.ClickLabel(m.Search); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("tab"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	focused := view.Snapshot().Focused
	if !tree.Within(focused.Scope(), sessionLogHits) {
		t.Fatalf("Tab left the page: %v", focused)
	}
	// The filters open from the keyboard and apply a choice.
	if err := view.ClickLabel(m.FilterStatus); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if state.sessionLogs.view.filterMenu == sessionLogFilterNone {
		t.Fatal("the status filter did not open")
	}
	if err := view.Keys("down"); err != nil {
		t.Fatal(err)
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	// Arrow navigation puts the keyboard in the list: the first Down reaches
	// the first choice.
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	focused = view.Snapshot().Focused
	if index, ok := focused.In(hitSessionLogsFilterList); !ok || index != 0 {
		t.Fatalf("Down did not reach the filter's first choice: %v", focused)
	}
	if err := view.ClickLabel(state.messages.SessionLogs.StatusFailed); err != nil {
		t.Fatal(err)
	}
	sessionLogsSettle(t, view, state)
	if state.sessionLogs.view.filterMenu != sessionLogFilterNone {
		t.Fatal("the filter menu stayed open after a choice")
	}
	if state.sessionLogs.view.query.Status == nil || *state.sessionLogs.view.query.Status != sessionlog.StatusFailed {
		t.Fatalf("the filter choice did not reach the query: %+v", state.sessionLogs.view.query)
	}
}

// A warm paint does no database work and allocates nothing: the page's strings
// are prepared off the paint path, and only the query completion replaces them.
func TestSessionLogsNativePaintIsInert(t *testing.T) {
	size := gfx.Size{Width: 1400, Height: 900}
	state := newTestState(t, sessionLogsTestOptions(t))
	sessionLogsSeed(t, state)
	state.openSessionLogs()
	sessionLogsWait(t, state)
	view := openSessionLogsWindow(t, state, size)
	if err := view.ClickLabel("web-01 · alice"); err != nil {
		t.Fatal(err)
	}
	sessionLogsSettle(t, view, state)

	generation := state.sessionLogs.requestGeneration
	loading := state.sessionLogs.view.loading
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(20, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("the native page paints %v allocations once warm", allocations)
	}
	if state.sessionLogs.requestGeneration != generation || state.sessionLogs.view.loading != loading {
		t.Fatal("a paint asked the store for a page")
	}
	select {
	case result := <-state.sessionLogs.results:
		t.Fatalf("a paint produced a store result: %+v", result)
	default:
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	windowHitsInside(t, view, size)
}
