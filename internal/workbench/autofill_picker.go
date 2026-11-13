package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/autofill"
)

// The password prompt assist. su prints a bare Password: line and asks for
// root's password, which the host itself does not keep; instead of a rule the
// user has to write, the terminal recognises that prompt by itself and offers
// the passwords already saved: the host's own first, then every shared one.
//
// The offer never sends anything. Enter sends only the row the user chose, a
// typed character closes the list and reaches the shell unchanged, and Escape
// closes it. The list is a Viva popover painted over the grid at the cursor, as
// the command completion list is.

const passwordAssistID = "password-assist"

// passwordAssistRule is the built-in prompt the assist answers. It is enabled
// and matches only a bare password line, so a sudo prompt (which names sudo)
// stays with the ghost hint in password_hint.go, and programs that merely
// mention a password are not taken for a prompt.
var passwordAssistRule = newPasswordAssistRule()

func newPasswordAssistRule() *autofill.Rule {
	rule := &autofill.Rule{ID: passwordAssistID, Name: "password prompt", Pattern: suPromptPattern, Enabled: true}
	_ = rule.Compile() // the pattern is a constant; the prompt tests guard it
	return rule
}

// passwordAssistOn is the user's password prompt assist switch. It covers the
// sudo hint and the password offer at once.
func (state *State) passwordAssistOn() bool { return !state.passwordHintOff }

// passwordCandidate is one saved password the offer lists. credentialID is
// empty for the host's own password; a shared credential used by the host is
// listed once, as the host's entry.
type passwordCandidate struct {
	credentialID string
	name         string
	detail       string
	host         bool
}

// passwordCandidates is what the offer lists: the host's own password first,
// then every shared password. A candidate whose secret cannot be read is still
// listed, so the user learns it exists; sending it reports the vault's state.
func (state *State) passwordCandidates(session *Session) []passwordCandidate {
	m := state.messages.Autofill
	var candidates []passwordCandidate
	seen := make(map[string]bool, len(state.credentials))
	if id, ok := strings.CutPrefix(state.passwordName(session), credentialSecretName("")); ok {
		if entry, found := state.credentialByID(id); found {
			candidates = append(candidates, passwordCandidate{
				credentialID: entry.ID, name: entry.Name, detail: m.PickHostDetail, host: true,
			})
			seen[entry.ID] = true
		}
	} else if session.endpoint.Host != "" {
		if _, result := state.vaultSecret(state.passwordName(session)); result == vaultFound {
			name := session.host.Label
			if name == "" {
				name = session.Address
			}
			detail := m.PickHostDetail
			if session.endpoint.User != "" {
				detail = session.endpoint.User
			}
			candidates = append(candidates, passwordCandidate{name: name, detail: detail, host: true})
		}
	}
	for _, entry := range state.credentials {
		if seen[entry.ID] {
			continue
		}
		candidates = append(candidates, passwordCandidate{credentialID: entry.ID, name: entry.Name, detail: m.PickSharedDetail})
	}
	return candidates
}

// autofillPick is the open offer. selected is -1 until the user moves into the
// list, so a plain Enter still reaches the shell.
type autofillPick struct {
	sessionID  uint32
	line       string
	candidates []passwordCandidate
	selected   int
	top        int
}

// openPasswordPick offers the saved passwords for the prompt. The session must
// be the one the user is looking at: an offer in a background tab would be
// answered into whatever terminal has the keyboard. The prompt line is recorded
// with it, so a changed line closes the offer instead of answering the wrong
// program.
func (state *State) openPasswordPick(session *Session, line string) {
	if session == nil || session.term == nil || session.term.shell == nil ||
		session.ID != state.activeSessionID || state.activeSection != sectionTerminal ||
		!state.passwordAssistOn() {
		return
	}
	candidates := state.passwordCandidates(session)
	if len(candidates) == 0 {
		return
	}
	state.closeCompletionList()
	state.autofillRules.pick = autofillPick{sessionID: session.ID, line: line, candidates: candidates, selected: -1}
	// The offer stands for this prompt: closing it must not have it open again
	// on the same line at the next pause.
	session.term.autofill.Fired(passwordAssistID, line)
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}

// closeAutofillPick forgets the offer; nothing is sent.
func (state *State) closeAutofillPick() {
	state.autofillRules.pick = autofillPick{}
}

// autofillPickSession returns the session the offer belongs to while it still
// stands: the same prompt line under the cursor, not in a program that owns the
// screen, and not behind a dialog or a scrolled view.
func (state *State) autofillPickSession() (*Session, bool) {
	pick := &state.autofillRules.pick
	if len(pick.candidates) == 0 || !state.passwordAssistOn() {
		return nil, false
	}
	session, ok := state.sessionByIDPointer(pick.sessionID)
	if !ok || session.term == nil || session.term.screen == nil || session.term.shell == nil || session.term.ended {
		return nil, false
	}
	// The offer belongs to what the user is looking at: a password must never go
	// to a tab that moved to the background while the list was open.
	if session.ID != state.activeSessionID || state.activeSection != sectionTerminal {
		return nil, false
	}
	t := session.term
	cursor := t.screen.Cursor()
	_, rows := t.screen.Size()
	if t.screen.Modes().AltScreen || t.screen.ViewOffset() != 0 || cursor.Y < 0 || cursor.Y >= rows ||
		t.screen.RowText(cursor.Y) != pick.line {
		return nil, false
	}
	return session, true
}

// autofillPickKey handles keys while the offer is open. Escape closes it and
// sends nothing. Arrows and the page keys move through the list; Enter sends the
// chosen password. Anything else, a typed character included, closes the offer
// and goes to the shell, so typing the password by hand is unchanged.
func (state *State) autofillPickKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	pick := &state.autofillRules.pick
	if len(pick.candidates) == 0 {
		return gfx.EventResult{}, false
	}
	if state.modal != modalNone {
		state.closeAutofillPick()
		return gfx.EventResult{}, false
	}
	session, ok := state.autofillPickSession()
	if !ok {
		state.closeAutofillPick()
		return gfx.EventResult{}, false
	}
	if event.Release || event.Key.IsModifier() {
		return gfx.EventResult{}, false
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	count := len(pick.candidates)
	if event.Key == gfx.KeyEscape {
		state.closeAutofillPick()
		return handled, true
	}
	if event.Key == gfx.KeyEnter {
		if event.Modifiers != 0 || event.Repeat {
			return handled, true // a held Enter must not answer twice
		}
		if pick.selected < 0 || pick.selected >= count {
			state.closeAutofillPick()
			return gfx.EventResult{}, false // the user's own Enter, as without an offer
		}
		state.sendPasswordCandidate(session, pick.candidates[pick.selected])
		return handled, true
	}
	if event.Modifiers == 0 {
		switch event.Key {
		case gfx.KeyUp:
			pick.moveList(-1, count)
			return handled, true
		case gfx.KeyDown:
			pick.moveList(1, count)
			return handled, true
		case gfx.KeyPageUp:
			pick.moveList(-passwordPickRows, count)
			return handled, true
		case gfx.KeyPageDown:
			pick.moveList(passwordPickRows, count)
			return handled, true
		case gfx.KeyHome:
			pick.selectList(0, count)
			return handled, true
		case gfx.KeyEnd:
			pick.selectList(count-1, count)
			return handled, true
		}
	}
	state.closeAutofillPick()
	return gfx.EventResult{}, false
}

func (pick *autofillPick) moveList(delta, count int) {
	if delta > 0 {
		pick.selectList(pick.selected+delta, count)
		return
	}
	pick.selectList(max(pick.selected+delta, 0), count)
}

// selectList clamps the choice into the list and scrolls it into view.
func (pick *autofillPick) selectList(index, count int) {
	if count <= 0 {
		pick.selected, pick.top = -1, 0
		return
	}
	pick.selected = min(max(index, 0), count-1)
	if pick.selected < pick.top {
		pick.top = pick.selected
	}
	if pick.selected >= pick.top+passwordPickRows {
		pick.top = pick.selected - passwordPickRows + 1
	}
	if pick.top < 0 {
		pick.top = 0
	}
}

// sendPasswordCandidate sends the chosen saved password to the shell. It is not
// typed input: it does not echo, enter the recording or reach the broadcast.
func (state *State) sendPasswordCandidate(session *Session, candidate passwordCandidate) {
	m := state.messages.Autofill
	state.closeAutofillPick()
	if session == nil || session.term == nil {
		return
	}
	name := state.passwordName(session)
	if candidate.credentialID != "" {
		name = credentialSecretName(candidate.credentialID)
	}
	value, result := state.vaultSecret(name)
	switch result {
	case vaultFound:
	case vaultLocked:
		state.status = fmt.Sprintf(m.PickLockedFmt, candidate.name)
		return
	default:
		state.status = fmt.Sprintf(m.PickMissingFmt, candidate.name)
		return
	}
	if session.term.sendSecret(append([]byte(value), '\r')) {
		state.status = fmt.Sprintf(m.PickSentFmt, candidate.name)
	}
}

// clickAutofillPick handles the list's controls.
func (state *State) clickAutofillPick(id gfx.ID) bool {
	pick := &state.autofillRules.pick
	if len(pick.candidates) == 0 {
		return false
	}
	switch {
	case id == hitAutofillPickDismiss:
		state.closeAutofillPick()
		return true
	case id.Scope() == hitAutofillPickItem:
		session, ok := state.autofillPickSession()
		if !ok {
			state.closeAutofillPick()
			return false
		}
		index := item(id)
		if index < 0 || index >= len(pick.candidates) {
			return false
		}
		state.sendPasswordCandidate(session, pick.candidates[index])
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Painting

var autofillPickHits = tree.Feature("password-prompt-assist")

var (
	hitAutofillPickDismiss = tree.ID(autofillPickHits, "password-prompt-dismiss")
	hitAutofillPickItem    = tree.Sub(autofillPickHits, tree.Name("password-prompt-item")) // + candidate index
)

// autofillPickScope names the popover's focus scope. The grid keeps the
// keyboard and the input method; the list is operated by the key route above.
var autofillPickScope = tree.Sub(autofillPickHits, tree.Name("password-prompt-scope"))

const (
	// passwordPickRows is how many passwords the popover lists at once; the rest
	// scroll with the arrow keys.
	passwordPickRows = 6
	// The surface keeps the command and menu components' rhythm: a 4px surface
	// inset, a 28px group heading, 9px separators, 32px rows with an 8px
	// leading inset, a 16px icon and an 8px gap before the label.
	passwordPickSurfacePad      = float32(4)
	passwordPickGroupHeight     = float32(28)
	passwordPickSeparatorHeight = float32(9)
	passwordPickRowHeight       = float32(32)
	passwordPickHintHeight      = float32(28)
	passwordPickRowInset        = float32(8)
	passwordPickTextStart       = float32(32)
	passwordPickTextEnd         = float32(12)
	passwordPickIconSize        = float32(16)
	passwordPickMinWidth        = float32(300)
	passwordPickMaxWidth        = float32(420)
)

// paintAutofillPick draws the offer over the grid, anchored to the cursor cell
// and flipped above it when the terminal has no room below.
func (state *State) paintAutofillPick(canvas *gfx.Canvas, session *Session, geometry terminalGeometry) {
	pick := &state.autofillRules.pick
	if len(pick.candidates) == 0 || pick.sessionID != session.ID || session.ID != state.activeSessionID ||
		state.modal != modalNone || state.activeSection != sectionTerminal {
		return
	}
	if _, ok := state.autofillPickSession(); !ok {
		return
	}
	cursor := session.term.presentedCursor()
	cellWidth, cellHeight := state.cellWidth, state.cellHeight
	x := geometry.left + float32(cursor.X)*cellWidth
	y := geometry.top + float32(cursor.Y)*cellHeight
	area := geometry.area
	x = min(max(x, area.X+2), area.X+max(area.Width-2, 2))
	y = min(max(y, area.Y+2), area.Y+max(area.Height-2, 2))
	anchor := gfx.Rect{X: x, Y: y, Width: max(cellWidth, 2), Height: max(cellHeight, 2)}

	top := min(pick.top, len(pick.candidates)-1)
	visible := min(len(pick.candidates)-top, passwordPickRows)
	height := 2*passwordPickSurfacePad + passwordPickGroupHeight + 2*passwordPickSeparatorHeight +
		passwordPickHintHeight + float32(visible)*passwordPickRowHeight
	popover := components.BeginPopover(canvas, state.Theme(), area, anchor, components.PopoverOptions{
		Layer:       dialogLayer,
		Scope:       autofillPickScope.ID(tree.Name("popover")),
		DismissID:   hitAutofillPickDismiss,
		Side:        components.PopoverBottom,
		Align:       components.PopoverAlignStart,
		SideOffset:  2,
		ContentSize: gfx.Size{Width: state.passwordPickWidth(canvas, pick.candidates), Height: height},
	})
	// The title the popover would paint is empty: the list carries the command
	// surface's own group heading, separators and rows, so the two headings and
	// the rows share one grid.
	layout := popover.Begin("", "")
	state.paintPasswordPickRows(canvas, layout.Content, pick, top, visible)
	popover.End()
}

func (state *State) paintPasswordPickRows(canvas *gfx.Canvas, content gfx.Rect, pick *autofillPick, top, visible int) {
	pal := state.pal
	list := gfx.Rect{
		X: content.X + passwordPickSurfacePad, Y: content.Y + passwordPickSurfacePad,
		Width: max(content.Width-2*passwordPickSurfacePad, 1), Height: max(content.Height-2*passwordPickSurfacePad, 1),
	}
	y := list.Y
	heading := gfx.Rect{X: list.X + passwordPickRowInset, Y: y, Width: max(list.Width-2*passwordPickRowInset, 1), Height: passwordPickGroupHeight}
	canvas.Label(state.messages.Autofill.PickTitle, heading,
		gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.muted})
	y += passwordPickGroupHeight
	canvas.Fill(gfx.Rect{X: content.X, Y: y + 4, Width: content.Width, Height: 1}, pal.line)
	y += passwordPickSeparatorHeight

	nameStyle := gfx.TextStyle{Size: metrics.FontSizeM, Weight: gfx.WeightRegular, Color: pal.text}
	metaStyle := gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightRegular, Color: pal.muted}
	for row := 0; row < visible; row++ {
		index := top + row
		if index >= len(pick.candidates) {
			break
		}
		candidate := pick.candidates[index]
		bounds := gfx.Rect{X: list.X, Y: y + float32(row)*passwordPickRowHeight, Width: list.Width, Height: passwordPickRowHeight}
		selected := index == pick.selected
		if selected {
			// The menu's active row color, so a chosen row is as legible as one in
			// the app's menus.
			canvas.Rounded(bounds, radius.item, pal.surfaceHot)
		}
		icon := lucideIcons[iconKey]
		if candidate.host {
			icon = lucideIcons[iconTerminal]
		}
		iconColor := pal.muted
		if selected {
			iconColor = pal.text
		}
		iconRect := gfx.Rect{
			X: bounds.X + passwordPickRowInset, Y: bounds.Y + (bounds.Height-passwordPickIconSize)/2,
			Width: passwordPickIconSize, Height: passwordPickIconSize,
		}
		canvas.DrawIcon(icon, iconRect, iconColor)
		canvas.HitWithOptions(hitAutofillPickItem.At(index), bounds, candidate.name, gfx.HitOptions{
			// The grid keeps the keyboard: a press must not focus the row.
			Role: gfx.RoleOption, SkipTab: true, SkipFocus: true, Selected: selected, Description: candidate.detail,
		})
		metaWidth := min(canvas.TextWidth(candidate.detail, metrics.FontSizeS), max(bounds.Width*0.45, 10))
		meta := gfx.Rect{
			X: bounds.X + bounds.Width - passwordPickTextEnd - metaWidth, Y: bounds.Y,
			Width: metaWidth, Height: bounds.Height,
		}
		label := gfx.Rect{
			X: bounds.X + passwordPickTextStart, Y: bounds.Y,
			Width: max(meta.X-12-(bounds.X+passwordPickTextStart), 10), Height: bounds.Height,
		}
		canvas.Label(elide(canvas, candidate.name, metrics.FontSizeM, gfx.WeightRegular, label.Width), label, nameStyle)
		canvas.LabelAligned(elide(canvas, candidate.detail, metrics.FontSizeS, gfx.WeightRegular, meta.Width), meta, metaStyle, gfx.AlignRight)
	}
	y += float32(visible) * passwordPickRowHeight
	canvas.Fill(gfx.Rect{X: content.X, Y: y + 4, Width: content.Width, Height: 1}, pal.line)
	y += passwordPickSeparatorHeight
	state.paintPasswordPickHint(canvas, gfx.Rect{X: list.X, Y: y, Width: list.Width, Height: passwordPickHintHeight})
}

// paintPasswordPickHint draws the keycaps and what each key does. Keycaps are
// the menu's: a point smaller than a standalone Kbd and 18px tall.
func (state *State) paintPasswordPickHint(canvas *gfx.Canvas, row gfx.Rect) {
	pal := state.pal
	capTheme := state.Theme()
	capTheme.Metrics.FontSizeS = min(capTheme.Metrics.FontSizeS, 10)
	labelStyle := gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.muted}
	x := row.X + passwordPickRowInset
	capY := row.Y + (row.Height-18)/2
	add := func(key, label string) {
		cap := components.Kbd(canvas, capTheme, gfx.Rect{X: x, Y: capY, Height: 18}, key)
		x = cap.X + cap.Width + 4
		width := canvas.TextWidth(label, metrics.FontSizeS)
		canvas.Label(label, gfx.Rect{X: x, Y: row.Y, Width: width, Height: row.Height}, labelStyle)
		x += width + 10
	}
	add("↑↓", state.messages.Autofill.PickSelect)
	add("Enter", state.messages.Autofill.PickSend)
	add("Esc", state.messages.Autofill.PickDismiss)
}

// passwordPickWidth measures the widest row and the keycap hint, within the
// list's bounds.
func (state *State) passwordPickWidth(canvas *gfx.Canvas, candidates []passwordCandidate) float32 {
	width := max(passwordPickMinWidth,
		2*passwordPickSurfacePad+passwordPickRowInset+state.passwordPickHintWidth(canvas)+passwordPickTextEnd)
	for _, candidate := range candidates {
		row := 2*passwordPickSurfacePad + passwordPickTextStart +
			canvas.TextWidth(candidate.name, metrics.FontSizeM) + 12 +
			canvas.TextWidth(candidate.detail, metrics.FontSizeS) + passwordPickTextEnd
		width = max(width, row)
	}
	return min(width, passwordPickMaxWidth)
}

// passwordPickHintWidth is the painted width of the keycap row.
func (state *State) passwordPickHintWidth(canvas *gfx.Canvas) float32 {
	capTheme := state.Theme()
	capTheme.Metrics.FontSizeS = min(capTheme.Metrics.FontSizeS, 10)
	pairs := [3][2]string{
		{"↑↓", state.messages.Autofill.PickSelect},
		{"Enter", state.messages.Autofill.PickSend},
		{"Esc", state.messages.Autofill.PickDismiss},
	}
	var width float32
	for index, pair := range pairs {
		capWidth := max(canvas.TextWidthWeight(pair[0], capTheme.Metrics.FontSizeS, gfx.WeightMedium)+8, 18)
		width += capWidth + 4 + canvas.TextWidth(pair[1], metrics.FontSizeS)
		if index < len(pairs)-1 {
			width += 10
		}
	}
	return width
}
