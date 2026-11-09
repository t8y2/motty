package workbench

import (
	"reflect"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

func TestAIMentionKeyboardWrapFilterEscapeAndNativeAccept(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	state.ai.open = true
	state.ai.input = *text.NewEditor("compare @")
	paintAt(state, panelWide)
	key := func(key gfx.Key) {
		t.Helper()
		if result := state.Key(gfx.KeyEvent{Target: hitAIInput, Key: key}); !result.Handled {
			t.Fatalf("key %v not handled", key)
		}
		paintAt(state, panelWide)
	}
	key(gfx.KeyUp)
	if state.ai.panel.mentionIndex != 1 {
		t.Fatal("Up did not wrap to the last suggestion")
	}
	key(gfx.KeyDown)
	if state.ai.panel.mentionIndex != 0 {
		t.Fatal("Down did not wrap to the first suggestion")
	}
	key(gfx.KeyDown)
	state.aiPanelText(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextInsertNewline})
	if got := state.ai.input.String(); got != "compare @"+mentionSlug(state.sessions[1].Title)+" " || state.ai.run != nil {
		t.Fatalf("native Enter = %q, run=%v", got, state.ai.run)
	}
	state.ai.input = *text.NewEditor("@")
	paintAt(state, panelWide)
	key(gfx.KeyDown)
	// A query change must reset the highlight before the next native event,
	// even if the paint for that edit has not run yet.
	state.ai.input = *text.NewEditor("@" + mentionSlug(state.sessions[0].Title)[:1])
	state.aiPanelText(app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextInsertTab})
	if got := state.ai.input.String(); got != "@"+mentionSlug(state.sessions[0].Title)+" " {
		t.Fatalf("filtered Tab = %q", got)
	}
	state.ai.input = *text.NewEditor("@")
	paintAt(state, panelWide)
	key(gfx.KeyEscape)
	if state.ai.input.String() != "@" || len(state.ai.panel.mentions) != 0 {
		t.Fatal("Escape changed the prompt or failed to dismiss suggestions")
	}
	state.aiPanelText(app.TextEvent{Kind: app.TextInsert, Text: "P"})
	paintAt(state, panelWide)
	if len(state.ai.panel.mentions) != 1 {
		t.Fatal("typing after Escape did not reopen matching suggestions")
	}
}

func TestAIMentionKeysLeaveChineseCompositionToTheInputMethod(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	state.ai.open = true
	state.ai.input = *text.NewEditor("@")
	paintAt(state, panelWide)
	state.aiPanelText(app.TextEvent{Kind: app.TextSetMarked, Text: "ni", MarkedSelection: app.TextRange{Start: 2, End: 2}})
	paintAt(state, panelWide)
	before := state.ai.input.String()
	for _, key := range []gfx.Key{gfx.KeyUp, gfx.KeyDown, gfx.KeyEnter, gfx.KeyEscape} {
		if _, handled := state.aiPanelKey(gfx.KeyEvent{Target: hitAIInput, Key: key}); handled {
			t.Fatalf("key %v was taken from the input method", key)
		}
	}
	if len(state.ai.panel.mentions) != 0 || state.ai.run != nil || state.ai.input.String() != before {
		t.Fatal("composition opened suggestions, sent a message, or changed the preedit")
	}
}

func TestAITargetSelectUsesTheTabsHostIcons(t *testing.T) {
	state := targetTestWorld(t, quietModel{"ok"})
	state.sessions[0].host.Icon = hostIconAuto
	state.sessions[0].host.System = sshconn.SystemUbuntu
	state.sessions[1].host.Icon = hostIconGlobe
	options := state.aiTargetOptions()
	for index, session := range state.sessions {
		expected := state.hostIconArt(session.host.Icon, session.host.System, session.host.isDesktop())
		if !reflect.DeepEqual(options[index+1].Icon, expected) || !strings.Contains(options[index+1].Hint, session.Address) {
			t.Fatalf("selector did not use the tab's icon or address for %s", session.Title)
		}
	}
}
