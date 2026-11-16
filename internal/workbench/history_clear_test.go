package workbench

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func holdHistoryWriter(t *testing.T, path string) func() {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("BEGIN EXCLUSIVE"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			if _, err := db.Exec("COMMIT"); err != nil {
				t.Error(err)
			}
			db.Close()
		})
	}
	t.Cleanup(release)
	return release
}

func TestClearPausesJoinedWindowsAndFlushesEveryOldQueue(t *testing.T) {
	a, b := newCompletionWorld(t), newCompletionWorld(t)
	path := filepath.Join(t.TempDir(), "shared.db")
	group := NewGroup()
	for _, w := range []*completionWorld{a, b} {
		w.state.options.HistoryPath = path
		w.state.options.Group = group
		w.state.history.on = true
		group.join(w.state)
	}
	source, other := a.state.historyStore(), b.state.historyStore()
	release := holdHistoryWriter(t, path)
	if err := other.Record(historyKey(b.session), "before-clear queued", time.Now()); err != nil {
		t.Fatal(err)
	}
	a.state.startHistoryClear(source)
	job := a.state.history.clear
	b.state.rememberCommand(b.session.ID, "during-clear skipped")
	c := newCompletionWorld(t)
	c.state.options.HistoryPath = path
	c.state.options.Group = group
	c.state.history.on = true
	wake := make(chan struct{}, 1)
	c.state.options.Invalidate = func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	group.join(c.state)
	if !c.state.historyClearing() {
		t.Fatal("joined window did not pause")
	}
	c.state.rememberCommand(c.session.ID, "joined-during-clear skipped")
	release()
	select {
	case <-job.done:
	case <-time.After(3 * time.Second):
		t.Fatal("clear timed out")
	}
	select {
	case <-wake:
	case <-time.After(3 * time.Second):
		t.Fatal("joined window did not receive completion wake")
	}
	for _, w := range []*completionWorld{a, b, c} {
		w.state.adoptHistoryClear()
	}
	if count, err := source.Len(); err != nil || count != 0 {
		t.Fatalf("old/new-during-clear writes survived: %d %v", count, err)
	}
	c.state.rememberCommand(c.session.ID, "after-clear learned")
	entries, err := c.state.historyStore().List("")
	if err != nil || len(entries) != 1 || entries[0].Command != "after-clear learned" {
		t.Fatalf("learning did not resume: %v %v", entries, err)
	}
}

func TestClearJobNotifiesAWindowJoiningAfterCompletion(t *testing.T) {
	job := &historyClearJob{done: make(chan struct{})}
	job.finish()
	wake := make(chan struct{}, 1)
	job.subscribe(func() { wake <- struct{}{} })
	select {
	case <-wake:
	default:
		t.Fatal("completed job did not wake a late subscriber")
	}
}

func TestWindowOpenedAfterAFinishedClearIsNotPausedOrNotified(t *testing.T) {
	a := newCompletionWorld(t)
	path := filepath.Join(t.TempDir(), "shared.db")
	group := NewGroup()
	a.state.options.HistoryPath = path
	a.state.options.Group = group
	a.state.history.on = true
	group.join(a.state)
	a.state.startHistoryClear(a.state.historyStore())
	select {
	case <-a.state.history.clear.done:
	case <-time.After(3 * time.Second):
		t.Fatal("clear timed out")
	}
	a.state.adoptHistoryClear()

	b := newCompletionWorld(t)
	b.state.options.HistoryPath = path
	b.state.options.Group = group
	b.state.history.on = true
	group.join(b.state)
	if b.state.historyClearing() {
		t.Fatal("a window opened after the clear finished is paused")
	}
	b.state.status = ""
	b.state.adoptHistoryClear()
	if b.state.status != "" {
		t.Fatalf("a window that did not clear announced %q", b.state.status)
	}
}
