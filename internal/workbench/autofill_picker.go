package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/autofill"
)

// The password prompt assist recognises sudo, su and SSH/SCP account password
// prompts, including commands run inside an already connected terminal. It
// offers saved identities without requiring a separate autofill rule.
//
// The offer never sends anything. Enter sends only the row the user chose, a
// typed character closes the list and reaches the shell unchanged, and Escape
// closes it. The list is a Viva popover painted over the grid at the cursor, as
// the command completion list is.

// passwordAssistOn is the user's password prompt assist switch. It covers the
// password offers for every supported account prompt.
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
// then shared identities. If the prompt names an account, omit identities for
// other users; password-only identities remain usable. A candidate whose secret
// cannot be read is still listed; sending it reports the vault's state.
func (state *State) passwordCandidates(session *Session, user string) []passwordCandidate {
	m := state.messages.Autofill
	var candidates []passwordCandidate
	seen := make(map[string]bool, len(state.credentials))
	hostMatches := user == "" || session.endpoint.User == user
	if id, ok := strings.CutPrefix(state.passwordName(session), credentialSecretName("")); ok && hostMatches {
		if entry, found := state.credentialByID(id); found {
			detail := session.endpoint.User
			if detail == "" {
				detail = m.PickHostDetail
			}
			candidates = append(candidates, passwordCandidate{
				credentialID: entry.ID, name: entry.Name, detail: detail, host: true,
			})
			seen[entry.ID] = true
		}
	} else if hostMatches && session.endpoint.Host != "" {
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
		if seen[entry.ID] || user != "" && entry.User != "" && entry.User != user {
			continue
		}
		detail := entry.User
		if detail == "" {
			detail = m.PickSharedDetail
		}
		candidates = append(candidates, passwordCandidate{credentialID: entry.ID, name: entry.Name, detail: detail})
	}
	return candidates
}

// autofillPick is the open offer. selected is -1 until the user moves into the
// list, so a plain Enter still reaches the shell.
type autofillPick struct {
	sessionID  uint32
	prompt     terminalPasswordPrompt
	candidates []passwordCandidate
	selected   int
	top        int
	// The page Popover that carries the offer: open is its visibility, kept
	// between paints so its scope and presence stay stable. body/draw are
	// stored so an open offer allocates no closures.
	open       bool
	c          ui.C
	body       func()
	draw       func(*ui.DrawContext)
	slotHeight float32
	pickTop    int
	visible    int
}

// openPasswordPick offers the saved passwords for the prompt. The session must
// be the one the user is looking at: an offer in a background tab would be
// answered into whatever terminal has the keyboard. The prompt line is recorded
// with it, so a changed line closes the offer instead of answering the wrong
// program.
func (state *State) openPasswordPick(session *Session, prompt terminalPasswordPrompt) bool {
	if session == nil || session.term == nil || session.term.shell == nil ||
		session.ID != state.activeSessionID || state.activeSection != sectionTerminal ||
		!state.passwordAssistOn() {
		return false
	}
	candidates := state.passwordCandidates(session, autofill.PasswordPromptUser(prompt.line))
	if len(candidates) == 0 {
		return false
	}
	state.suspendAutofillPick()
	state.closeCompletionList()
	state.autofillRules.pick = autofillPick{sessionID: session.ID, prompt: prompt, candidates: candidates, selected: -1}
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
	return true
}

// closeAutofillPick forgets the offer; nothing is sent.
func (state *State) closeAutofillPick() {
	state.autofillRules.pick = autofillPick{}
}

// suspendAutofillPick leaves an unanswered prompt available when another tab or
// dialog temporarily owns the window. Explicit dismissal and manual input use
// closeAutofillPick instead, so repainting them never reopens the same list.
func (state *State) suspendAutofillPick() {
	pick := state.autofillRules.pick
	if session, ok := state.sessionByIDPointer(pick.sessionID); ok && session.term != nil {
		t := session.term
		if t.shell != nil && !t.ended && !t.autofill.HasInput() && t.passwordOffered == pick.prompt {
			t.passwordOffered = terminalPasswordPrompt{}
			t.autofill.Retry(state.now())
		}
	}
	state.closeAutofillPick()
}

// autofillPickSession returns the session the offer belongs to while it still
// stands: the same prompt line under the cursor, not in a program that owns the
// screen, and not behind a dialog or a scrolled view.
func (state *State) autofillPickSession() (*Session, bool) {
	pick := &state.autofillRules.pick
	if len(pick.candidates) == 0 || !state.passwordAssistOn() || state.modal != modalNone {
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
	if t.autofill.HasInput() || t.passwordPromptAtCursor() != pick.prompt {
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
		state.suspendAutofillPick()
		return gfx.EventResult{}, false
	}
	session, ok := state.autofillPickSession()
	if !ok {
		state.suspendAutofillPick()
		return gfx.EventResult{}, false
	}
	// The window dispatcher also receives keys from find and file fields. Those
	// keys cannot select or confirm a password; keep the offer suspended until
	// the user returns to this terminal or explicitly clicks a candidate.
	owner := state.terminalPageSession(event.Target)
	if (owner == nil || owner.ID != session.ID) && event.Target.Scope() != hitAutofillPickItem {
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
		state.confirmPasswordPick(session, pick.selected)
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

// confirmPasswordPick drains output received since the list was shown before
// checking its prompt again. A delayed click or Enter must not answer a shell
// that has already left the password prompt.
func (state *State) confirmPasswordPick(session *Session, index int) {
	pick := state.autofillRules.pick
	if session.term.takeOutput != nil {
		session.term.takeOutput()
	}
	session, ok := state.autofillPickSession()
	if !ok || session.ID != pick.sessionID || len(session.term.rx) != 0 ||
		state.autofillRules.pick.prompt != pick.prompt || index < 0 || index >= len(pick.candidates) {
		state.closeAutofillPick()
		return
	}
	state.sendPasswordCandidate(session, pick.candidates[index])
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
			state.suspendAutofillPick()
			return false
		}
		index := item(id)
		if index < 0 || index >= len(pick.candidates) {
			return false
		}
		state.confirmPasswordPick(session, index)
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

// declareAutofillPick declares the offer as a page Popover over the grid,
// anchored to the cursor cell and flipped above it when the terminal has no room
// below. The rows stay hand-painted in the popover's body slot.
func (state *State) declareAutofillPick(c ui.C, session *Session, bounds gfx.Rect, geometry terminalGeometry) {
	pick := &state.autofillRules.pick
	if len(pick.candidates) == 0 || pick.sessionID != session.ID || session.ID != state.activeSessionID ||
		state.modal != modalNone || state.activeSection != sectionTerminal {
		pick.open = false
		return
	}
	if _, ok := state.autofillPickSession(); !ok {
		pick.open = false
		return
	}
	cursor := session.term.presentedCursor()
	cellWidth, cellHeight := state.cellWidth, state.cellHeight
	x := geometry.left + float32(cursor.X)*cellWidth
	y := geometry.top + float32(cursor.Y)*cellHeight
	area := geometry.area
	x = min(max(x, area.X+2), area.X+max(area.Width-2, 2))
	y = min(max(y, area.Y+2), area.Y+max(area.Height-2, 2))
	anchorRect := gfx.Rect{X: x, Y: y, Width: max(cellWidth, 2), Height: max(cellHeight, 2)}

	top := min(pick.top, len(pick.candidates)-1)
	visible := min(len(pick.candidates)-top, passwordPickRows)
	height := 2*passwordPickSurfacePad + passwordPickGroupHeight + 2*passwordPickSeparatorHeight +
		passwordPickHintHeight + float32(visible)*passwordPickRowHeight
	// The page Popover reserves its border and padding in the layout; the old
	// surface was given a fixed height. Shortening the slot by that inset makes
	// the surface and the rows land where the old popover put them.
	const border = float32(1)
	padding := state.Theme().Metrics.PadSurface
	pick.pickTop, pick.visible = top, visible
	pick.slotHeight = height - 2*(border+padding)
	pick.c = c
	if pick.body == nil {
		pick.body = state.declareAutofillPickBody
	}
	if pick.draw == nil {
		pick.draw = state.drawAutofillPickBody
	}
	anchor := ui.Slot(c).Absolute().Left(anchorRect.X-bounds.X).Top(anchorRect.Y-bounds.Y).
		Size(anchorRect.Width, anchorRect.Height)
	pick.open = true
	spec := ui.PopoverSpec{
		Scope:            autofillPickScope.ID(tree.Name("popover")),
		DismissID:        hitAutofillPickDismiss,
		Side:             components.PopoverBottom,
		Align:            ui.Start,
		Offset:           2,
		Width:            state.passwordPickWidth(c.Canvas(), pick.candidates),
		KeepOpenOnAnchor: true,
	}
	if ui.PopoverWith(c, anchor, &pick.open, spec, pick.body) {
		pick.open = false
	}
}

// declareAutofillPickBody declares the one slot that paints the offer.
func (state *State) declareAutofillPickBody() {
	pick := &state.autofillRules.pick
	ui.Slot(pick.c).FillWidth().Height(pick.slotHeight).Draw(pick.draw)
}

// drawAutofillPickBody paints the rows into the surface rectangle the old
// popover exposed as its Content. Expanding the slot by the page's border and
// padding recovers that surface.
func (state *State) drawAutofillPickBody(d *ui.DrawContext) {
	pick := &state.autofillRules.pick
	const border = float32(1)
	padding := state.Theme().Metrics.PadSurface
	content := gfx.Rect{
		X: d.Rect.X - (border + padding), Y: d.Rect.Y - (border + padding),
		Width: d.Rect.Width + 2*(border+padding), Height: d.Rect.Height + 2*(border+padding),
	}
	state.paintPasswordPickRows(d.Canvas, content, pick, pick.pickTop, pick.visible)
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
