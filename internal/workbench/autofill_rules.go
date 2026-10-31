package workbench

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/autofill"
	"motty/internal/otp"
	"motty/internal/vault"
)

// Autofill rules. The autofill package decides when a rule may answer; this file
// keeps the user's rules, answers from the vault, asks before a rule first sends
// something in a session, and lists the rules in Settings. A rule's fixed text
// lives in the vault, never in the settings file.

const (
	maxAutofillName  = 80
	maxAutofillText  = 256
	maxAutofillScope = 16
)

var autofillHits = tree.Feature("autofill-rules")

var (
	hitSettingsAutofillAdd  = tree.ID(autofillHits, "settings-autofill-add")
	hitSettingsAutofillSudo = tree.ID(autofillHits, "settings-autofill-sudo")
	hitSettingsAutofillSu   = tree.ID(autofillHits, "settings-autofill-su")
	hitAutofillName         = tree.ID(autofillHits, "autofill-name")
	hitAutofillPattern      = tree.ID(autofillHits, "autofill-pattern")
	hitAutofillFixed        = tree.ID(autofillHits, "autofill-fixed")
	hitAutofillScope        = tree.ID(autofillHits, "autofill-scope")
	hitAutofillPassword     = tree.ID(autofillHits, "autofill-send-password")
	hitAutofillOTP          = tree.ID(autofillHits, "autofill-send-otp")
	hitAutofillText         = tree.ID(autofillHits, "autofill-send-text")
	hitAutofillEnter        = tree.ID(autofillHits, "autofill-enter")
	hitAutofillSubmit       = tree.ID(autofillHits, "autofill-submit")
	hitAutofillCancel       = tree.ID(autofillHits, "autofill-cancel")
	hitAutofillClose        = tree.ID(autofillHits, "autofill-close")
	hitAutofillDismiss      = tree.ID(autofillHits, "autofill-dismiss")

	hitAutofillAllow        = tree.ID(autofillHits, "autofill-confirm-allow")
	hitAutofillDeny         = tree.ID(autofillHits, "autofill-confirm-deny")
	hitAutofillConfirmClose = tree.ID(autofillHits, "autofill-confirm-close")
	hitAutofillConfirmAway  = tree.ID(autofillHits, "autofill-confirm-dismiss")
)

var (
	hitAutofillEditList   = tree.Sub(autofillHits, tree.Name("autofill-edit"))   // + rule index
	hitAutofillDeleteList = tree.Sub(autofillHits, tree.Name("autofill-delete")) // + rule index
	hitAutofillToggleList = tree.Sub(autofillHits, tree.Name("autofill-toggle")) // + rule index
)

// autofillUI is the state of the rules, of the dialog that edits one, and of the
// question before one first sends.
type autofillUI struct {
	rules []*autofill.Rule
	// armed is whether any rule is on, so a terminal with none watches nothing.
	armed bool

	// The dialog: the rule it edits (nil adds one) and the fields.
	editing                     *autofill.Rule
	name, pattern, fixed, scope text.Editor
	send                        autofill.Send
	enter                       bool
	dialogError                 string

	ask autofillAsk
}

// autofillAsk is a rule waiting for the user's word before it answers.
type autofillAsk struct {
	sessionID uint32
	rule      *autofill.Rule
	line      string
}

func autofillSecretName(rule *autofill.Rule) string { return "autofill:" + rule.ID }

func (state *State) refreshAutofillArmed() {
	state.autofillRules.armed = false
	for _, rule := range state.autofillRules.rules {
		if rule.Enabled {
			state.autofillRules.armed = true
		}
	}
}

// ---------------------------------------------------------------------------
// Watching a terminal

// scanAutofill looks at the line under the cursor once the shell has gone quiet.
func (state *State) scanAutofill(session *Session) {
	t := session.term
	wait, pending := t.autofill.Pending(state.now())
	if !pending || wait > 0 {
		return
	}
	screen := t.screen
	cursor := screen.Cursor()
	_, rows := screen.Size()
	if screen.Modes().AltScreen || screen.ViewOffset() != 0 || t.shell == nil || t.ended || cursor.Y < 0 || cursor.Y >= rows {
		t.autofill.Skip()
		return
	}
	line := screen.RowText(cursor.Y)
	host := session.host
	rule := t.autofill.Scan(line, state.autofillRules.rules, func(rule *autofill.Rule) bool {
		return rule.AppliesTo(host.Label, host.Group)
	})
	if rule == nil {
		return
	}
	if t.autofill.Allowed(rule.ID) {
		t.autofill.Fired(rule.ID, line)
		state.sendAutofill(session, rule)
		return
	}
	if state.modal != modalNone {
		t.autofill.Retry(state.now()) // the question waits for the dialog in the way
		return
	}
	t.autofill.Fired(rule.ID, line)
	state.autofillRules.ask = autofillAsk{sessionID: session.ID, rule: rule, line: line}
	state.modal = modalAutofillConfirm
	state.modalSession = session.ID
}

// requestAutofillFrames asks for the one frame that looks at a prompt once the
// output has paused. A terminal that is quiet asks for nothing.
func (state *State) requestAutofillFrames(canvas *ui.Canvas) {
	if !state.autofillRules.armed {
		return
	}
	now := state.now()
	for index := range state.sessions {
		t := state.sessions[index].term
		if t == nil {
			continue
		}
		if wait, pending := t.autofill.Pending(now); pending && wait > 0 {
			canvas.RequestFrameIn(wait)
		}
	}
}

func (state *State) clickAutofillConfirm(id ui.ID) {
	ask := state.autofillRules.ask
	switch id {
	case hitAutofillAllow:
		session, ok := state.sessionByIDPointer(ask.sessionID)
		state.closeModal()
		if ok && session.term != nil && session.term.shell != nil && session.term.screen != nil {
			// The prompt may be gone by now: only the same line is answered.
			cursor := session.term.screen.Cursor()
			if cursor.Y >= 0 && session.term.screen.RowText(cursor.Y) == ask.line && !session.term.screen.Modes().AltScreen {
				session.term.autofill.Allow(ask.rule.ID)
				state.sendAutofill(session, ask.rule)
			}
		}
	case hitAutofillDeny, hitAutofillConfirmClose, hitAutofillConfirmAway:
		state.closeModal()
		state.status = state.messages.Autofill.StatusDeclined
	default:
		return
	}
	state.showNextPrompt()
}

// sendAutofill answers the prompt with what the rule sends. The answer goes to
// the shell only, never to the broadcast peers, and is never recorded.
func (state *State) sendAutofill(session *Session, rule *autofill.Rule) {
	m := state.messages.Autofill
	var value string
	var result vaultResult
	switch rule.Send {
	case autofill.SendPassword:
		if session.endpoint.Host == "" {
			result = vaultMissing
		} else {
			value, result = state.vaultSecret(state.passwordName(session))
		}
	case autofill.SendOTP:
		if session.endpoint.Host == "" {
			result = vaultMissing
		} else {
			value, result = state.takeOTP(session)
		}
	case autofill.SendText:
		value, result = state.vaultSecret(autofillSecretName(rule))
	}
	switch result {
	case vaultFound:
	case vaultLocked:
		state.status = fmt.Sprintf(m.StatusLockedFmt, rule.Name)
		return
	default:
		state.status = fmt.Sprintf(m.StatusNoSecret, rule.Name)
		return
	}
	payload := []byte(value)
	if rule.Enter {
		payload = append(payload, '\r')
	}
	if session.term.sendSecret(payload) {
		state.status = fmt.Sprintf(m.StatusFiredFmt, rule.Name)
	}
}

type vaultResult uint8

const (
	vaultMissing vaultResult = iota
	vaultFound
	vaultLocked
)

// vaultSecret reads a secret without asking the user to unlock: an answer that
// needs the vault open waits for the user to open it.
func (state *State) vaultSecret(name string) (string, vaultResult) {
	vaultFile := state.secrets.vault
	if vaultFile == nil || vaultFile.Len() == 0 {
		return "", vaultMissing
	}
	value, found, err := vaultFile.Get(name)
	switch {
	case errors.Is(err, vault.ErrNeedsConversion):
		return "", vaultLocked
	case err != nil || !found:
		return "", vaultMissing
	}
	return value, vaultFound
}

// takeOTP is the current code of an account's secret. A counter-based secret
// moves on with every code used, or the next one would repeat.
func (state *State) takeOTP(session *Session) (string, vaultResult) {
	name := state.otpSecretName(session)
	value, result := state.vaultSecret(name)
	if result != vaultFound {
		return "", result
	}
	secret, err := otp.Parse(value)
	if err != nil {
		return "", vaultMissing
	}
	code := secret.Code(state.options.WallNow())
	if secret.Kind == otp.HOTP {
		if err := state.secrets.vault.Set(name, secret.Next().String()); err != nil {
			state.status = fmt.Sprintf(state.messages.VaultSaveFailedFmt, state.vaultErrorText(err))
		}
	}
	return code, vaultFound
}

// ---------------------------------------------------------------------------
// The rule dialog

func (state *State) openAutofillDialog(rule *autofill.Rule) {
	form := &state.autofillRules
	form.editing = rule
	form.name, form.pattern, form.fixed, form.scope = text.Editor{}, text.Editor{}, text.Editor{}, text.Editor{}
	form.send, form.enter = autofill.SendPassword, true
	form.dialogError = ""
	if rule != nil {
		form.name.Insert(rule.Name)
		form.pattern.Insert(rule.Pattern)
		form.scope.Insert(strings.Join(rule.Scope, ", "))
		form.send, form.enter = rule.Send, rule.Enter
	}
	state.modal = modalAutofillRule
}

// The prompts of the two commands that ask for a password after login. sudo
// tags its prompt, and the answer is the account's own password. su prints a bare
// Password: line, and the answer is root's, which is not saved anywhere else, so
// that preset sends a fixed text. Both are anchored at the start of the line, so
// a program that only mentions a password is not taken for the prompt.
const (
	sudoPromptPattern = `(?i)^\[sudo\][^\n]*(?:password|密码)[^\n]*[:：]\s*$`
	suPromptPattern   = `(?i)^(?:password|密码|口令)\s*[:：]\s*$`
)

// openAutofillSudoPreset opens the rule dialog filled in for sudo's prompt.
func (state *State) openAutofillSudoPreset() {
	state.openAutofillDialog(nil)
	form := &state.autofillRules
	form.name.Insert(state.messages.Autofill.PresetSudoName)
	form.pattern.Insert(sudoPromptPattern)
	form.send = autofill.SendPassword
}

// openAutofillSuPreset opens the rule dialog filled in for su's prompt.
func (state *State) openAutofillSuPreset() {
	state.openAutofillDialog(nil)
	form := &state.autofillRules
	form.name.Insert(state.messages.Autofill.PresetSuName)
	form.pattern.Insert(suPromptPattern)
	form.send = autofill.SendText
}

func (state *State) clickAutofillDialog(id ui.ID) {
	form := &state.autofillRules
	switch id {
	case hitAutofillSubmit:
		state.submitAutofillRule()
	case hitAutofillCancel, hitAutofillClose, hitAutofillDismiss:
		state.closeModal()
	case hitAutofillPassword:
		form.send = autofill.SendPassword
	case hitAutofillOTP:
		form.send = autofill.SendOTP
	case hitAutofillText:
		form.send = autofill.SendText
	case hitAutofillEnter:
		form.enter = !form.enter
	}
}

func (state *State) submitAutofillRule() {
	form, m := &state.autofillRules, state.messages.Autofill
	fail := func(message string) { form.dialogError = message }
	name := cleanSnippetText(form.name.String(), maxAutofillName)
	pattern := strings.TrimSpace(form.pattern.String())
	if name == "" {
		fail(m.ErrorName)
		return
	}
	if _, err := autofill.Validate(pattern); err != nil {
		switch syntaxMessage, isSyntax := autofill.IsSyntax(err); {
		case errors.Is(err, autofill.ErrEmpty):
			fail(m.ErrorPattern)
		case errors.Is(err, autofill.ErrTooLong):
			fail(m.ErrorTooLong)
		case errors.Is(err, autofill.ErrMatchesEmpty):
			fail(m.ErrorMatchesAll)
		case isSyntax:
			fail(fmt.Sprintf(m.ErrorInvalidFmt, syntaxMessage))
		default:
			fail(fmt.Sprintf(m.ErrorInvalidFmt, err))
		}
		return
	}
	if form.editing == nil && len(form.rules) >= autofill.MaxRules {
		fail(m.ErrorTooMany)
		return
	}
	scope, unknown := state.parseAutofillScope(form.scope.String())
	if unknown != "" {
		fail(fmt.Sprintf(m.ErrorScopeFmt, unknown))
		return
	}
	fixed := strings.TrimRight(form.fixed.String(), "\r\n")
	if utf8.RuneCountInString(fixed) > maxAutofillText {
		fixed = string([]rune(fixed)[:maxAutofillText])
	}
	rule := &autofill.Rule{Name: name, Pattern: pattern, Send: form.send, Enter: form.enter, Scope: scope}
	if edited := form.editing; edited != nil {
		rule.ID, rule.Enabled = edited.ID, edited.Enabled
	} else {
		rule.ID = state.nextAutofillID()
	}
	hadText := form.editing != nil && form.editing.Send == autofill.SendText
	if rule.Send == autofill.SendText && (fixed != "" || !hadText) {
		if fixed == "" {
			fail(m.ErrorFixedText)
			return
		}
		if message := state.storeAutofillText(rule, fixed); message != "" {
			fail(message)
			return
		}
	}
	if hadText && rule.Send != autofill.SendText && state.secrets.vault != nil {
		_ = state.secrets.vault.Delete(autofillSecretName(rule))
	}
	_ = rule.Compile()
	if form.editing != nil {
		for index, existing := range form.rules {
			if existing == form.editing {
				form.rules[index] = rule
			}
		}
	} else {
		form.rules = append(form.rules, rule)
	}
	state.closeModal()
	state.refreshAutofillArmed()
	state.save()
}

// storeAutofillText keeps a rule's fixed text in the vault, and says what went
// wrong in words if it cannot.
func (state *State) storeAutofillText(rule *autofill.Rule, value string) string {
	m := state.messages.Autofill
	vaultFile := state.secrets.vault
	switch {
	case vaultFile == nil:
		return fmt.Sprintf(m.ErrorVaultFmt, "—")
	case vaultFile.NeedsConversion():
		return m.ErrorVaultLocked
	default:
		if err := vaultFile.Prepare(); err != nil {
			return fmt.Sprintf(m.ErrorVaultFmt, state.vaultErrorText(err))
		}
	}
	if err := vaultFile.Set(autofillSecretName(rule), value); err != nil {
		return fmt.Sprintf(m.ErrorVaultFmt, state.vaultErrorText(err))
	}
	return ""
}

// parseAutofillScope splits the scope field into host and group names. It
// returns the first name that matches no host or group.
func (state *State) parseAutofillScope(value string) (scope []string, unknown string) {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		known := false
		for _, host := range state.hosts {
			if strings.EqualFold(host.Label, part) || strings.EqualFold(host.Group, part) {
				known = true
				break
			}
		}
		if !known {
			return nil, part
		}
		if len(scope) < maxAutofillScope {
			scope = append(scope, part)
		}
	}
	return scope, ""
}

// nextAutofillID is an id no other device will pick for a rule of its own.
func (state *State) nextAutofillID() string {
	var raw [4]byte
	_, _ = rand.Read(raw[:])
	return "r" + hex.EncodeToString(raw[:])
}

func (state *State) toggleAutofillRule(index int) {
	rules := state.autofillRules.rules
	if index < 0 || index >= len(rules) {
		return
	}
	rules[index].Enabled = !rules[index].Enabled
	state.refreshAutofillArmed()
	state.save()
}

func (state *State) deleteAutofillRule(index int) {
	rules := state.autofillRules.rules
	if index < 0 || index >= len(rules) {
		return
	}
	if rules[index].Send == autofill.SendText && state.secrets.vault != nil {
		_ = state.secrets.vault.Delete(autofillSecretName(rules[index]))
	}
	state.autofillRules.rules = append(rules[:index:index], rules[index+1:]...)
	state.refreshAutofillArmed()
	state.save()
}

func (state *State) sendKindName(send autofill.Send) string {
	m := state.messages.Autofill
	switch send {
	case autofill.SendOTP:
		return m.SendOTP
	case autofill.SendText:
		return m.SendText
	}
	return m.SendPassword
}

// ---------------------------------------------------------------------------
// Painting

func (state *State) paintAutofillDialog(canvas *ui.Canvas, viewport ui.Rect) {
	m, pal, theme := state.messages.Autofill, state.pal, state.Theme()
	form := &state.autofillRules
	title := m.DialogAddTitle
	if form.editing != nil {
		title = m.DialogEditTitle
	}
	body := float32(68*3 + 58 + 28) // name, pattern and scope; the sending choice; the box
	if form.send == autofill.SendText {
		body += 68
	}
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitAutofillDismiss, CloseID: hitAutofillClose, InitialFocusID: hitAutofillName,
		ContentSize: ui.Size{Width: 600}, FitBody: true, BodyHeight: withError(body, form.dialogError),
	})
	layout := dialog.Begin(title, "")
	kinds := [...]struct {
		id   ui.ID
		send autofill.Send
	}{{hitAutofillPassword, autofill.SendPassword}, {hitAutofillOTP, autofill.SendOTP}, {hitAutofillText, autofill.SendText}}
	var name, pattern, fixed, scope, enter flex.Box
	var choices [len(kinds)]flex.Box
	f := state.settingsRow.Begin(canvas, theme, layout.Body)
	f.Column().Fill().PadSides(2, 4, 0, 4).Children(func() {
		name = f.Slot().Height(62).MarginSides(0, 0, 6, 0)
		pattern = f.Slot().Height(62).MarginSides(0, 0, 6, 0)
		f.Text(m.Send).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.muted).LineHeight(18).MarginSides(0, 0, 4, 2)
		f.Row().Gap(8).Height(30).MarginSides(0, 0, 8, 0).Children(func() {
			for index, kind := range kinds {
				border, width := withAlpha(pal.line, 0.9), float32(1)
				if form.send == kind.send {
					border, width = pal.accent, 2
				}
				choices[index] = f.Row().Grow(1).Height(30).Items(flex.Center).PadX(12).Background(pal.terminal).
					Radius(radius.control).Border(width, border).Children(func() {
					f.Text(state.sendKindName(kind.send)).FontSize(metrics.FontSizeM).Color(pal.text)
				})
			}
		})
		if form.send == autofill.SendText {
			fixed = f.Slot().Height(62).MarginSides(0, 0, 6, 0)
		}
		scope = f.Slot().Height(62).MarginSides(0, 0, 6, 0)
		enter = state.declareDialogCheckbox(f, m.Enter, 4, 0)
		if form.dialogError != "" {
			f.Text(form.dialogError).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.danger).LineHeight(18).MarginSides(15, 0, 0, 0)
		}
	})
	f.End()
	field := func(box flex.Box, id ui.ID, label, placeholder, value string, secret bool) {
		content := components.BeginField(canvas, theme, box.Rect()).Paint(label, "", "")
		focused := canvas.Focused() == id
		state.paintSecretInput(canvas, content, components.InputSnapshot{
			ID: id, Label: label, Value: value, Placeholder: placeholder, Secure: secret,
			Focused: focused, PlatformEditing: focused && value != "", Invalid: form.dialogError != "",
		})
	}
	field(name, hitAutofillName, m.Name, m.NamePlaceholder, form.name.String(), false)
	field(pattern, hitAutofillPattern, m.Pattern, m.PatternPlaceholder, form.pattern.String(), false)
	for index, kind := range kinds {
		canvas.HitWithOptions(kind.id, choices[index].Rect(), state.sendKindName(kind.send), ui.HitOptions{Role: ui.RoleButton, Selected: form.send == kind.send})
	}
	if form.send == autofill.SendText {
		placeholder := ""
		if form.editing != nil && form.editing.Send == autofill.SendText {
			placeholder = m.FixedTextHint
		}
		field(fixed, hitAutofillFixed, m.FixedText, placeholder, form.fixed.String(), true)
	}
	field(scope, hitAutofillScope, m.Scope, m.ScopePlaceholder, form.scope.String(), false)
	state.paintDialogCheckbox(canvas, hitAutofillEnter, m.Enter, form.enter, enter)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitAutofillCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAutofillSubmit, Label: m.Save},
	)
	dialog.End()
}

func (state *State) paintAutofillConfirm(canvas *ui.Canvas, viewport ui.Rect) {
	m, pal, theme := state.messages.Autofill, state.pal, state.Theme()
	ask := state.autofillRules.ask
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitAutofillConfirmAway, CloseID: hitAutofillConfirmClose, InitialFocusID: hitAutofillDeny,
		ContentSize: ui.Size{Width: 560}, FitBody: true, BodyHeight: 92,
	})
	address := ""
	if session, ok := state.sessionByIDPointer(ask.sessionID); ok {
		address = session.Address
	}
	name := ""
	if ask.rule != nil {
		name = ask.rule.Name
	}
	kind := ""
	if ask.rule != nil {
		kind = state.sendKindName(ask.rule.Send)
	}
	layout := dialog.Begin(m.ConfirmTitle, fmt.Sprintf(m.ConfirmFmt, name, address))
	f := state.partLayout.Begin(canvas, theme, layout.Body)
	f.Column().Fill().Children(func() {
		f.Text(fmt.Sprintf(m.ConfirmSendsFmt, kind)).FontSize(metrics.FontSizeM).Color(pal.text).LineHeight(20)
		f.Row().Height(58).Shrink(0).Items(flex.Center).PadX(11).MarginSides(8, 0, 0, 0).
			Border(1, pal.line).Background(pal.surface).Radius(radius.control).Children(func() {
			f.Text(m.ConfirmLine + ": " + clipboardPreview(ask.line)).FontSize(metrics.FontSizeS).Color(pal.text)
		})
	})
	f.End()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitAutofillDeny, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAutofillAllow, Label: m.ConfirmAllow},
	)
	dialog.End()
}

// paintAutofillSettings is the Autofill rules group of Settings.
func (state *State) paintAutofillSettings(page *settingsPage) {
	m := state.messages.Autofill
	rules := state.autofillRules.rules
	page.group(m.Group, func() {
		for index, rule := range rules {
			scope := m.ScopeAll
			if len(rule.Scope) > 0 {
				scope = fmt.Sprintf(m.ScopeFmt, strings.Join(rule.Scope, ", "))
			}
			send := state.sendKindName(rule.Send)
			if rule.Enter {
				send += " + " + m.WithEnter
			}
			status, toggle := m.StateOff, m.TurnOn
			if rule.Enabled {
				status, toggle = m.StateOn, m.TurnOff
			}
			page.actionRow(rule.Name, status+" · "+send+" · "+scope+" · "+rule.Pattern, false,
				components.ButtonAction{ID: hitAutofillToggleList.At(index), Label: toggle, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitAutofillEditList.At(index), Label: m.Edit, Variant: components.ButtonOutline},
				components.ButtonAction{ID: hitAutofillDeleteList.At(index), Label: m.Delete, Variant: components.ButtonOutline})
		}
		full := len(rules) >= autofill.MaxRules
		page.actionRow(m.Row, m.RowDescription, full,
			components.ButtonAction{ID: hitSettingsAutofillSudo, Label: m.PresetSudo, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitSettingsAutofillSu, Label: m.PresetSu, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitSettingsAutofillAdd, Label: m.Add, Variant: components.ButtonOutline})
	})
}

// ---------------------------------------------------------------------------
// The state file

type savedAutofill struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Pattern string   `json:"pattern"`
	Send    string   `json:"send"`
	Enter   bool     `json:"enter,omitempty"`
	Enabled bool     `json:"enabled,omitempty"`
	Scope   []string `json:"scope,omitempty"`
}

func (state *State) savedAutofillRules() []savedAutofill {
	var saved []savedAutofill
	for _, rule := range state.autofillRules.rules {
		saved = append(saved, savedAutofill{
			ID: rule.ID, Name: rule.Name, Pattern: rule.Pattern, Send: rule.Send.String(),
			Enter: rule.Enter, Enabled: rule.Enabled, Scope: rule.Scope,
		})
	}
	return saved
}

// restoreAutofill loads the rules of a saved state. A rule that is damaged, or
// whose pattern would not be accepted today, is dropped: it could answer a
// prompt it was never meant to.
func (state *State) restoreAutofill(saved savedState) {
	clear(state.autofillRules.rules)
	state.autofillRules.rules = state.autofillRules.rules[:0]
	seen := map[string]bool{}
	for _, entry := range saved.Autofill {
		rule := &autofill.Rule{
			ID: entry.ID, Name: cleanSnippetText(entry.Name, maxAutofillName), Pattern: entry.Pattern,
			Send: autofill.SendFromString(entry.Send), Enter: entry.Enter, Enabled: entry.Enabled, Scope: entry.Scope,
		}
		if rule.ID == "" || seen[rule.ID] || rule.Name == "" || rule.Send == 0 ||
			len(state.autofillRules.rules) >= autofill.MaxRules || rule.Compile() != nil {
			continue
		}
		seen[rule.ID] = true
		state.autofillRules.rules = append(state.autofillRules.rules, rule)
	}
	state.refreshAutofillArmed()
}
