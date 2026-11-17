package workbench

import (
	"testing"
	"time"

	"motty/internal/cmdhistory"
)

// A command another host knows joins the automatic list after the host's own
// matches, so the list offers the same cross-host commands the inline
// suggestion already draws from (#862).
func TestTheAutomaticListOffersCommandsFromOtherHosts(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.inlineOff = true // automatic-list mode
	w.state.history.on = true
	now := time.Now()
	w.state.history.ghost = map[string][]cmdhistory.Entry{
		historyKey(w.session): {{Command: "docker compose up -d", Used: now, Count: 2}},
		"": {
			{Command: "docker compose up -d", Used: now, Count: 2},
			{Command: "docker compose pull", Used: now.Add(-time.Hour), Count: 1},
		},
	}
	w.type_("docker compose ")
	w.state.openCompletions()
	list := &w.state.completion.list
	if !list.active {
		t.Fatal("no list")
	}
	own, other := -1, -1
	for index, candidate := range list.candidates {
		switch candidate.Text {
		case "docker compose up -d":
			own = index
		case "docker compose pull":
			other = index
		}
	}
	if other < 0 {
		t.Fatalf("the list does not offer the other host's command: %+v", list.candidates)
	}
	if own < 0 || own > other {
		t.Fatalf("the host's own command is not before the other host's: %+v", list.candidates)
	}
	if insert := list.candidates[other].Insert; insert != "pull" {
		t.Fatalf("the other host's row inserts %q, want the suffix", insert)
	}
}

// The automatic list asks for the merged history, not only the host's own:
// without that request the cross-host rows would never arrive in this mode.
func TestTheAutomaticListReadsTheMergedHistory(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.inlineOff = true // automatic-list mode
	w.state.history.on = true
	store := w.state.historyStore()
	if store == nil {
		t.Fatal("no history store")
	}
	now := time.Now()
	if err := store.Record(historyKey(w.session), "docker compose up -d", now); err != nil {
		t.Fatal(err)
	}
	if err := store.Record("other-host", "systemctl restart nginx", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	w.state.forgetCompletionHistory()
	deadline := time.Now().Add(waitBudget())
	for time.Now().Before(deadline) {
		w.state.Wake()
		_, host := w.state.history.ghost[historyKey(w.session)]
		_, every := w.state.history.ghost[""]
		if host && every {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok := w.state.history.ghost[""]; !ok {
		t.Fatal("the automatic list never read the merged history")
	}
	w.type_("systemctl re")
	w.state.openCompletions()
	list := &w.state.completion.list
	if !list.active {
		t.Fatal("no list")
	}
	for _, candidate := range list.candidates {
		if candidate.Text == "systemctl restart nginx" {
			return
		}
	}
	t.Fatalf("the other host's command is missing: %+v", list.candidates)
}

// A host that keeps no history never asks for the merged read, so it cannot
// draw another host's commands. The eligibility check already keeps such a
// host out of the list and the ghost entirely.
func TestAHostThatKeepsNoHistoryAsksForNoMergedHistory(t *testing.T) {
	w := newCompletionWorld(t)
	w.state.history.inlineOff = true // automatic-list mode
	w.state.history.on = true
	w.session.host.NoHistory = true
	w.state.Wake()
	if w.state.completion.historyPending[""] {
		t.Fatal("a host that keeps no history asked for the merged history")
	}
	if w.state.completion.list.active {
		t.Fatal("a host that keeps no history opened the list")
	}
}
