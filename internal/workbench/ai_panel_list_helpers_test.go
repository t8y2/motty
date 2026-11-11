package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

// aiListScroll is the conversation List's scroll state after the last paint.
func aiListScroll(tb testing.TB, state *State) *ui.ScrollState {
	tb.Helper()
	scroll := state.ai.panel.messageScroll
	if scroll == nil {
		tb.Fatal("the conversation List has not been painted")
	}
	return scroll
}

// aiHistoryListScroll is the history List's scroll state after the last paint.
func aiHistoryListScroll(tb testing.TB, state *State) *ui.ScrollState {
	tb.Helper()
	scroll := state.ai.panel.historyScroll
	if scroll == nil {
		tb.Fatal("the history List has not been painted")
	}
	return scroll
}

// aiListFollows reports whether the conversation is at its newest row and not
// held away from it by a scrollbar drag.
func aiListFollows(state *State) bool {
	scroll := state.ai.panel.messageScroll
	return scroll != nil && scroll.AtEnd() && !state.ai.panel.followSuppressed
}

// aiListOffset is the conversation's offset, zero when it has no List (the
// empty state or a closed panel).
func aiListOffset(state *State) float32 {
	if scroll := state.ai.panel.messageScroll; scroll != nil {
		return scroll.Offset
	}
	return 0
}
