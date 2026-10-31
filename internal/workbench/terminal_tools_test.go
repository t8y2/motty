package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/highlight"
	"motty/internal/sshconn"
	"motty/internal/term"
)

// buildWith prepares row 0 of a screen holding input under a palette with rules.
func buildWith(t testing.TB, input string, rules []highlight.Rule) *rowEntry {
	t.Helper()
	screen := term.New(40, 3, 10)
	screen.Write([]byte(input))
	look := darkPalette
	look.highlight = highlight.Compile(rules)
	var cache rowCache
	cache.begin()
	return cache.row(screen.Row(0), 40, -1, &look, false)
}

func TestHighlightColorsTheMatchedWordsAndKeepsEveryOtherCell(t *testing.T) {
	entry := buildWith(t, "ok: Permission denied now", highlight.Builtin())
	if entry.text != "ok: Permission denied now" {
		t.Fatalf("text = %q", entry.text)
	}
	green, red := ui.RGB(140, 230, 168), ui.RGB(255, 150, 150)
	colorAt := func(column int32) any {
		for _, run := range entry.runs {
			if column >= run.Column && column < run.Column+run.Cells {
				return run.Color
			}
		}
		return nil
	}
	if colorAt(0) != green || colorAt(1) != green {
		t.Errorf("ok is %v, want green", colorAt(0))
	}
	if colorAt(4) != darkPalette.termFG || colorAt(3) != darkPalette.termFG {
		t.Errorf("Permission is %v, want the terminal's own text", colorAt(4))
	}
	if colorAt(15) != red || colorAt(20) != red {
		t.Errorf("denied is %v, want red", colorAt(15))
	}
	if colorAt(22) != darkPalette.termFG {
		t.Errorf("now is %v, want the terminal's own text", colorAt(22))
	}
}

func TestHighlightLeavesTextAProgramAlreadyColoredAlone(t *testing.T) {
	entry := buildWith(t, "\x1b[34merror\x1b[0m error", highlight.Builtin())
	red := ui.RGB(255, 150, 150)
	first, last := entry.runs[0], entry.runs[len(entry.runs)-1]
	if first.Color == red {
		t.Errorf("a word the program colored blue was recolored: %+v", first)
	}
	if last.Color != red {
		t.Errorf("the plain word is %+v, want red", last)
	}
}

func TestHighlightFollowsWideCharactersToTheirCells(t *testing.T) {
	entry := buildWith(t, "你好 error", highlight.Builtin())
	red := ui.RGB(255, 150, 150)
	for _, run := range entry.runs {
		if run.Column >= 5 && run.Color != red {
			t.Errorf("run at cell %d is %+v, want red: the word starts at cell 5 after two wide characters", run.Column, run)
		}
		if run.Column < 4 && run.Color == red {
			t.Errorf("a wide character was colored: %+v", run)
		}
	}
}

func TestHighlightingNothingCostsNoExtraWork(t *testing.T) {
	var cache rowCache
	screen := term.New(40, 3, 10)
	screen.Write([]byte("nothing to see"))
	look := darkPalette
	look.highlight = highlight.Compile(highlight.Builtin())
	cache.begin()
	cache.row(screen.Row(0), 40, -1, &look, false)
	if cells := cache.markCells(screen.Row(0), 40, look.highlight); cells != nil {
		t.Fatalf("a line with no match returned marks: %v", cells)
	}
}

// schemeCard is the Settings card showing the color scheme with this id.
func schemeCard(t *testing.T, id string) ui.ID {
	t.Helper()
	for index := range colorSchemes {
		if colorSchemes[index].id == id {
			return hitSettingsSchemeList.At(index + 1)
		}
	}
	t.Fatalf("no color scheme %q", id)
	return ui.ID{}
}

func TestSchemeChangesTheTerminalNotTheWindow(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	window, dark := state.pal.workspace, state.termPal.terminal
	epoch := state.termPal.epoch

	state.Click(hitSettings)
	state.Click(schemeCard(t, "dracula"))
	if state.activeScheme() != "dracula" {
		t.Fatalf("scheme = %q", state.activeScheme())
	}
	if state.termPal.terminal == dark || state.termPal.terminal != ui.RGB(0x28, 0x2a, 0x36) {
		t.Errorf("terminal background = %v", state.termPal.terminal)
	}
	if state.termPal.termFG != ui.RGB(0xf8, 0xf8, 0xf2) || state.termPal.ansi[1] != [3]uint8{0xff, 0x55, 0x55} {
		t.Errorf("text %v, red %v", state.termPal.termFG, state.termPal.ansi[1])
	}
	if state.pal.workspace != window {
		t.Error("choosing a scheme repainted the window")
	}
	if state.termPal.epoch == epoch {
		t.Error("the epoch did not move, so prepared lines would keep their old colors")
	}

	// A line prepared before the change is built again.
	screen := term.New(40, 3, 10)
	screen.Write([]byte("\x1b[31mred\x1b[0m"))
	var cache rowCache
	cache.begin()
	before := cache.row(screen.Row(0), 40, -1, &darkPalette, false).runs[0].Color
	look := state.termPal
	cache.begin()
	after := cache.row(screen.Row(0), 40, -1, &look, false).runs[0].Color
	if before == after || after != ui.RGB(0xff, 0x55, 0x55) {
		t.Errorf("red before %v, after %v", before, after)
	}

	// It is kept, and Default comes back with the first card.
	restarted := newTestState(t, options)
	if restarted.activeScheme() != "dracula" {
		t.Fatalf("restored scheme = %q", restarted.activeScheme())
	}
	state.Click(hitSettingsSchemeList.At(0))
	if state.activeScheme() != defaultSchemeID || state.termPal.terminal != state.pal.terminal {
		t.Errorf("default scheme: %q, %v", state.activeScheme(), state.termPal.terminal)
	}
}

func TestEachLookKeepsItsOwnTerminalScheme(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	if state.appearance != appearanceDark {
		t.Fatal("the window opens dark")
	}

	state.Click(hitSettings)
	state.Click(schemeCard(t, "dracula"))
	darkTerminal := state.termPal.terminal

	state.Click(hitTheme) // the window turns light and so does the terminal
	if state.appearance != appearanceLight || state.activeScheme() != defaultSchemeID {
		t.Fatalf("light look: %v scheme %q", state.appearance, state.activeScheme())
	}
	if state.termPal.terminal == darkTerminal || state.termPal.terminal != state.pal.terminal {
		t.Errorf("the terminal stayed %v in a light window", state.termPal.terminal)
	}
	state.Click(schemeCard(t, "one-light"))

	state.Click(hitTheme)
	if state.activeScheme() != "dracula" || state.termPal.terminal != darkTerminal {
		t.Errorf("dark look came back as %q %v", state.activeScheme(), state.termPal.terminal)
	}

	restarted := newTestState(t, options)
	if restarted.terminalSchemes != [2]string{"dracula", "one-light"} {
		t.Errorf("restored schemes = %q", restarted.terminalSchemes)
	}
}

func TestASchemeSavedBeforeLooksHadOneGoesToTheLookItSuits(t *testing.T) {
	for _, test := range []struct {
		saved       string
		dark, light string
	}{
		{"dracula", "dracula", defaultSchemeID},
		{"github-light", defaultSchemeID, "github-light"},
		{"default", defaultSchemeID, defaultSchemeID},
		{"no-such-scheme", defaultSchemeID, defaultSchemeID},
	} {
		_, options := newWorld(t)
		if err := saveState(options.StatePath, savedState{Hosts: sampleHosts, TerminalScheme: test.saved}); err != nil {
			t.Fatal(err)
		}
		state := newTestState(t, options)
		if got := state.terminalSchemes; got != [2]string{test.dark, test.light} {
			t.Errorf("%q restored as %q", test.saved, got)
		}
	}
}

func TestEveryColorSchemeIsLegible(t *testing.T) {
	seen := map[string]bool{}
	for _, scheme := range colorSchemes {
		if seen[scheme.id] || strings.TrimSpace(scheme.name) == "" {
			t.Errorf("scheme %q is duplicated or unnamed", scheme.id)
		}
		seen[scheme.id] = true
		for _, slot := range highlight.Slots() {
			ink := scheme.ansi[slot.ANSI()]
			if contrast(ink, scheme.background) < 2.0 {
				t.Errorf("%s: highlight %s has contrast %.1f against its background", scheme.name, slot.Name(), contrast(ink, scheme.background))
			}
		}
		if contrast(scheme.foreground, scheme.background) < 3.5 {
			t.Errorf("%s: text contrast %.1f", scheme.name, contrast(scheme.foreground, scheme.background))
		}
	}
}

func contrast(a, b [3]uint8) float32 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relativeLuminance(c [3]uint8) float32 {
	linear := func(v uint8) float32 {
		f := float32(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		x := (f + 0.055) / 1.055
		return x * x * (x*0.5 + 0.5) // close enough to x^2.4 for a threshold test
	}
	return 0.2126*linear(c[0]) + 0.7152*linear(c[1]) + 0.0722*linear(c[2])
}

func TestAddingAHighlightRuleThroughTheDialogCompilesKeepsAndRestoresIt(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.Click(hitSettings)
	state.Click(hitSettingsHighlightAdd)
	if state.modal != modalHighlightRule {
		t.Fatalf("modal = %d", state.modal)
	}
	state.Click(hitHighlightSubmit)
	if state.modal != modalHighlightRule || state.highlighting.dialogError != state.messages.Tools.HighlightErrorEmpty {
		t.Fatalf("an empty rule: modal %d, error %q", state.modal, state.highlighting.dialogError)
	}
	typeInto(state, hitHighlightPattern, "5\\d\\d")
	state.Click(hitHighlightRegex)
	state.Click(hitHighlightColorList.At(2)) // Green in the dialog's order: Red, Yellow, Green
	state.Click(hitHighlightSubmit)
	if state.modal != modalNone || len(state.highlighting.rules) != 1 {
		t.Fatalf("modal %d, rules %v", state.modal, state.highlighting.rules)
	}
	rule := state.highlighting.rules[0]
	if rule.Pattern != "5\\d\\d" || !rule.Regex || rule.Slot != highlight.Green {
		t.Fatalf("rule = %+v", rule)
	}
	if !state.highlights.Mark([]byte("HTTP 503"), make([]highlight.Slot, 8)) {
		t.Fatal("the new rule is not in the compiled set")
	}

	restarted := newTestState(t, options)
	if len(restarted.highlighting.rules) != 1 || restarted.highlighting.rules[0] != rule {
		t.Fatalf("restored rules = %+v", restarted.highlighting.rules)
	}

	state.Click(hitHighlightDeleteList.At(0))
	if len(state.highlighting.rules) != 0 {
		t.Fatalf("rules after delete = %v", state.highlighting.rules)
	}
}

func TestAHighlightRuleThatCannotWorkIsRefusedWithAReason(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	for pattern, want := range map[string]string{
		"(":    "Not a valid pattern",
		"x*":   state.messages.Tools.HighlightErrorEverywhere,
		"  ":   state.messages.Tools.HighlightErrorEmpty,
		"ab**": "Not a valid pattern",
	} {
		state.openHighlightDialog()
		state.highlighting.regex = true
		typeInto(state, hitHighlightPattern, pattern)
		state.Click(hitHighlightSubmit)
		if state.modal != modalHighlightRule || !strings.HasPrefix(state.highlighting.dialogError, want) {
			t.Errorf("%q: modal %d, error %q, want %q", pattern, state.modal, state.highlighting.dialogError, want)
		}
		state.Click(hitHighlightCancel)
	}
	if len(state.highlighting.rules) != 0 {
		t.Fatalf("rules = %v", state.highlighting.rules)
	}
}

func TestTheBuiltInHighlightsCanBeTurnedOffAndStayOff(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	if state.highlights.Empty() {
		t.Fatal("the built-in rules are not on by default")
	}
	state.Click(hitSettings)
	state.Click(hitSettingsHighlightToggle)
	if !state.highlights.Empty() {
		t.Fatal("turning them off left rules compiled")
	}
	if restarted := newTestState(t, options); !restarted.highlights.Empty() {
		t.Fatal("the choice was not kept")
	}
	state.Click(hitSettingsHighlightToggle)
	if state.highlights.Empty() {
		t.Fatal("turning them on found no rules")
	}
}

func TestADamagedStateFileCannotInstallAnUnusableRuleOrSnippet(t *testing.T) {
	_, options := newWorld(t)
	saved := savedState{
		Hosts: sampleHosts,
		Highlights: []savedHighlight{
			{Pattern: "(", Regex: true, Color: "red"},
			{Pattern: "fine", Color: "teal"},
			{Pattern: "ok", Color: "green"},
		},
		Snippets:       []savedSnippet{{Name: "", Command: "x"}, {Name: "n", Command: "\x1b[2Jls\n"}, {Name: "m", Command: ""}},
		TerminalScheme: "no-such-scheme",
	}
	if err := saveState(options.StatePath, saved); err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, options)
	if len(state.highlighting.rules) != 1 || state.highlighting.rules[0].Pattern != "ok" {
		t.Errorf("rules = %+v", state.highlighting.rules)
	}
	if len(state.snippets.list) != 1 || state.snippets.list[0].Command != "[2Jls" {
		t.Errorf("snippets = %+v: control characters must not survive into keystrokes", state.snippets.list)
	}
	if state.activeScheme() != defaultSchemeID {
		t.Errorf("scheme = %q", state.activeScheme())
	}
}

func TestSnippetsAreAddedEditedSentAndKept(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)

	state.Click(hitSnippets)
	state.Click(hitSnippetAdd)
	state.Click(hitSnippetSubmit)
	if state.modal != modalSnippet || state.snippets.dialogError != state.messages.Tools.SnippetErrorName {
		t.Fatalf("modal %d, error %q", state.modal, state.snippets.dialogError)
	}
	typeInto(state, hitSnippetName, "Restart nginx")
	state.Click(hitSnippetSubmit)
	if state.snippets.dialogError != state.messages.Tools.SnippetErrorCommand {
		t.Fatalf("error = %q", state.snippets.dialogError)
	}
	typeInto(state, hitSnippetCommand, "systemctl restart nginx")
	state.Click(hitSnippetSubmit)
	if state.modal != modalNone || len(state.snippets.list) != 1 {
		t.Fatalf("modal %d, snippets %+v", state.modal, state.snippets.list)
	}
	if got := state.snippets.list[0]; got.Name != "Restart nginx" || got.Command != "systemctl restart nginx" || !got.Run {
		t.Fatalf("snippet = %+v", got)
	}

	// It is on the command palette, and choosing it types into the active shell.
	state.openCommandPalette()
	found := false
	for index := 0; index < int(state.commandPalette.entryCount); index++ {
		if strings.Contains(state.commandPalette.entries[index].label, "Restart nginx") {
			found = true
		}
	}
	if !found {
		t.Fatal("the snippet is not on the command palette")
	}
	state.closeCommandPalette()
	state.Click(hitSnippetSendList.At(0))
	if got := shell.typed(); got != "systemctl restart nginx\r" {
		t.Fatalf("the shell heard %q", got)
	}
	if !strings.Contains(state.status, "Restart nginx") {
		t.Fatalf("status = %q", state.status)
	}

	// Edited without Enter, it only types.
	state.Click(hitSnippetEdit)
	if state.modal != modalSnippet || state.snippets.name.String() != "Restart nginx" {
		t.Fatalf("editing opened modal %d with name %q", state.modal, state.snippets.name.String())
	}
	state.Click(hitSnippetRun)
	state.Click(hitSnippetSubmit)
	state.Click(hitSnippetSendList.At(0))
	if got := shell.typed(); got != "systemctl restart nginx\rsystemctl restart nginx" {
		t.Fatalf("the shell heard %q", got)
	}

	if restarted := newTestState(t, options); len(restarted.snippets.list) != 1 || restarted.snippets.list[0].Run {
		t.Fatalf("restored snippets = %+v", restarted.snippets.list)
	}
	state.Click(hitSnippetRemove)
	if len(state.snippets.list) != 0 {
		t.Fatalf("snippets after delete = %+v", state.snippets.list)
	}
}

func TestASnippetWithNowhereToGoSaysSo(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.snippets.list = []snippet{{Name: "n", Command: "ls", Run: true}}
	state.Click(hitSnippetSendList.At(0))
	if state.status != state.messages.Tools.StatusSnippetNoTerminal {
		t.Fatalf("status = %q", state.status)
	}
}

// twoPanes connects a host and splits it, so the tab has two live shells.
func twoPanes(t *testing.T) (state *State, first, second *fakeShell, firstID, secondID uint32) {
	t.Helper()
	world, options := newSharingWorld(t)
	state = newTestState(t, options)
	world.connectFirst(t, state)
	firstSession, _ := state.sessionByIDPointer(state.activeSessionID)
	firstID = firstSession.ID
	state.Click(hitPaneSplitHorizontal)
	secondID = state.activeSessionID
	if secondID == firstID || len(world.shared) != 1 {
		t.Fatalf("split did not make a second pane: %d %d", firstID, secondID)
	}
	return state, world.conns[0].shell(t), world.shared[0].shell(t), firstID, secondID
}

func insertText(state *State, session uint32, value string) {
	state.terminalText(session, app.TextEvent{Kind: app.TextInsert, Text: value})
}

func TestBroadcastTypesIntoEveryPaneOfTheTabUntilItIsTurnedOff(t *testing.T) {
	state, first, second, firstID, secondID := twoPanes(t)

	insertText(state, secondID, "a")
	if first.typed() != "" || second.typed() != "a" {
		t.Fatalf("before broadcast: first %q, second %q", first.typed(), second.typed())
	}

	state.Click(hitBroadcast)
	if state.broadcast == 0 || !state.broadcasting(firstID) || !state.broadcasting(secondID) {
		t.Fatalf("broadcast = %d", state.broadcast)
	}
	insertText(state, secondID, "ls")
	if first.typed() != "ls" || second.typed() != "als" {
		t.Fatalf("while broadcasting: first %q, second %q", first.typed(), second.typed())
	}
	insertText(state, firstID, "x")
	if first.typed() != "lsx" || second.typed() != "alsx" {
		t.Fatalf("typing in the other pane: first %q, second %q", first.typed(), second.typed())
	}

	// A paste and an Enter go along; a pane that is not live is skipped.
	state.terminalText(firstID, app.TextEvent{Kind: app.TextPaste, Text: "echo hi"})
	if !strings.HasSuffix(second.typed(), "echo hi") {
		t.Fatalf("paste did not reach the other pane: %q", second.typed())
	}
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitTerminalList.At(int(firstID))})
	if !strings.HasSuffix(second.typed(), "\r") {
		t.Fatalf("Enter did not reach the other pane: %q", second.typed())
	}

	state.Click(hitBroadcast)
	if state.broadcast != 0 {
		t.Fatal("broadcast stayed on")
	}
	before := first.typed()
	insertText(state, secondID, "q")
	if first.typed() != before {
		t.Fatalf("typing after broadcast was off reached the other pane: %q", first.typed())
	}
}

func TestBroadcastNeedsAtLeastTwoPanes(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.Click(hitBroadcast)
	if state.broadcast != 0 || state.status != state.messages.Tools.StatusBroadcastNeedsSplit {
		t.Fatalf("broadcast %d, status %q", state.broadcast, state.status)
	}
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitBroadcast); ok {
		t.Fatal("a tab with one pane offers a broadcast button")
	}
}

func TestBroadcastStaysInItsTabAndNeverCarriesProtocolReplies(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	firstSession, _ := state.sessionByIDPointer(state.activeSessionID)
	state.Click(hitPaneSplitHorizontal)
	secondID := state.activeSessionID
	state.Click(hitBroadcast)

	// Another tab, on a connection of its own.
	state.shareConnections = shareConnectionsOff
	openEmptyTab(state)
	state.Click(hitHosts)
	openHost(state)
	otherConn := world.conns[len(world.conns)-1]
	otherConn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	otherShell := otherConn.shell(t)
	otherID := state.activeSessionID
	if otherID == secondID || state.broadcasting(otherID) {
		t.Fatalf("the new tab broadcasts: %d", otherID)
	}
	insertText(state, otherID, "private")
	if strings.Contains(world.conns[0].shell(t).typed(), "private") || strings.Contains(world.shared[0].shell(t).typed(), "private") {
		t.Fatal("typing in another tab reached the broadcasting tab")
	}
	insertText(state, firstSession.ID, "shared")
	if strings.Contains(otherShell.typed(), "shared") {
		t.Fatal("broadcast leaked into another tab")
	}

	// Replies a terminal sends for its program stay in their pane.
	session, _ := state.sessionByIDPointer(firstSession.ID)
	peer := world.shared[0].shell(t)
	before := peer.typed()
	session.term.sendProtocol([]byte("\x1b[0n"))
	if peer.typed() != before {
		t.Fatal("a protocol reply was broadcast")
	}
}

// A screen of log lines is prepared again whenever it scrolls, so what the
// highlight rules add to preparing one line is what a busy terminal pays.
func BenchmarkPrepareALogLine(b *testing.B) {
	for name, rules := range map[string][]highlight.Rule{"no rules": nil, "builtin rules": highlight.Builtin()} {
		b.Run(name, func(b *testing.B) {
			screen := term.New(120, 3, 10)
			screen.Write([]byte("2026-10-17T10:11:12 host sshd[123]: Failed password for root from 192.168.1.20 port 22 ssh2"))
			look := darkPalette
			look.highlight = highlight.Compile(rules)
			var cache rowCache
			row := screen.Row(0)
			b.ReportAllocs()
			for b.Loop() {
				cache.begin()
				row.Version++ // as if the line had changed
				cache.row(row, 120, -1, &look, false)
			}
		})
	}
}
