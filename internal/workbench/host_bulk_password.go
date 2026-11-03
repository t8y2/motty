package workbench

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/keyidentity"
	"motty/internal/otp"
	"motty/internal/vault"
)

var bulkPasswordHits = tree.Feature("bulk-host-password")

var (
	hitBulkPasswordDismiss       = tree.ID(bulkPasswordHits, "dismiss")
	hitBulkPasswordClose         = tree.ID(bulkPasswordHits, "close")
	hitBulkPasswordCancel        = tree.ID(bulkPasswordHits, "cancel")
	hitBulkPasswordConfirm       = tree.ID(bulkPasswordHits, "confirm")
	hitBulkPasswordField         = tree.ID(bulkPasswordHits, "password")
	hitBulkPasswordViewport      = tree.ID(bulkPasswordHits, "viewport")
	hitBulkPasswordScrollbar     = tree.ID(bulkPasswordHits, "scrollbar")
	hitBulkApplyPassword         = tree.ID(bulkPasswordHits, "apply-password")
	hitBulkApplyUser             = tree.ID(bulkPasswordHits, "apply-user")
	hitBulkUserField             = tree.ID(bulkPasswordHits, "user")
	hitBulkApplyAuth             = tree.ID(bulkPasswordHits, "apply-auth")
	hitBulkAuthTrigger           = tree.ID(bulkPasswordHits, "auth-trigger")
	hitBulkAuthDismiss           = tree.ID(bulkPasswordHits, "auth-dismiss")
	hitBulkAuthItemList          = tree.Sub(bulkPasswordHits, tree.Name("auth-item"))
	hitBulkApplyIdentity         = tree.ID(bulkPasswordHits, "apply-identity")
	hitBulkIdentityTrigger       = tree.ID(bulkPasswordHits, "identity-trigger")
	hitBulkIdentityDismiss       = tree.ID(bulkPasswordHits, "identity-dismiss")
	hitBulkIdentityAll           = tree.ID(bulkPasswordHits, "identity-all")
	hitBulkIdentityItemList      = tree.Sub(bulkPasswordHits, tree.Name("identity-item"))
	hitBulkApplyOTP              = tree.ID(bulkPasswordHits, "apply-otp")
	hitBulkOTPField              = tree.ID(bulkPasswordHits, "otp")
	hitBulkOTPRemove             = tree.ID(bulkPasswordHits, "remove-otp")
	hitBulkApplyPassphrase       = tree.ID(bulkPasswordHits, "apply-passphrase")
	hitBulkPassphraseField       = tree.ID(bulkPasswordHits, "passphrase")
	hitBulkPassphraseKeyTrigger  = tree.ID(bulkPasswordHits, "passphrase-key-trigger")
	hitBulkPassphraseKeyDismiss  = tree.ID(bulkPasswordHits, "passphrase-key-dismiss")
	hitBulkPassphraseRemove      = tree.ID(bulkPasswordHits, "remove-passphrase")
	hitBulkPassphraseKeyItemList = tree.Sub(bulkPasswordHits, tree.Name("passphrase-key-item"))
	hitBulkApplyAgentForward     = tree.ID(bulkPasswordHits, "apply-agent-forward")
	hitBulkAgentForwardValue     = tree.ID(bulkPasswordHits, "agent-forward-value")
)

type bulkHostPassword struct {
	ids               []uint32
	eligible          int
	skipped           int
	editor            text.Editor
	applyPassword     bool
	applyUser         bool
	user              text.Editor
	applyAuth         bool
	auth              hostAuth
	authOpen          bool
	applyIdentity     bool
	identity          string
	identityOpen      bool
	applyOTP          bool
	otp               text.Editor
	removeOTP         bool
	applyPassphrase   bool
	passphrase        text.Editor
	removePassphrase  bool
	passphraseKey     string
	passphraseKeyOpen bool
	applyAgentForward bool
	agentForward      bool
	scroll            float32
	viewport          ui.Rect
	maxScroll         float32
}

func (state *State) openBulkHostPassword() {
	state.captureHostActionIDs()
	var batch bulkHostPassword
	for _, id := range state.hostActionIDs {
		host, ok := state.hostByID(id)
		if !ok {
			continue
		}
		batch.ids = append(batch.ids, id)
		if host.isSSH() || host.Protocol == protocolVNC || host.Protocol == protocolRDP {
			batch.eligible++
		} else {
			batch.skipped++
		}
	}
	if batch.eligible == 0 {
		state.showActionWarning(state.messages.BulkPasswordTitle, state.messages.BulkPasswordNoHosts)
		return
	}
	state.bulkPassword = batch
	state.dialogError = ""
	state.modal = modalBulkHostPassword
}

func (state *State) clickBulkHostPassword(id ui.ID) {
	switch {
	case id == hitBulkPasswordDismiss || id == hitBulkPasswordClose || id == hitBulkPasswordCancel:
		state.bulkPassword = bulkHostPassword{}
		state.dialogError = ""
		state.reveal.reset()
		state.modal = modalNone
	case id == hitBulkPasswordConfirm:
		state.submitBulkHostPassword()
	case id == hitBulkApplyPassword:
		state.bulkPassword.applyPassword = !state.bulkPassword.applyPassword
	case id == hitBulkApplyUser:
		state.bulkPassword.applyUser = !state.bulkPassword.applyUser
	case id == hitBulkApplyAuth:
		state.bulkPassword.applyAuth = !state.bulkPassword.applyAuth
	case id == hitBulkAuthTrigger:
		state.bulkPassword.authOpen = !state.bulkPassword.authOpen
		state.bulkPassword.identityOpen, state.bulkPassword.passphraseKeyOpen = false, false
	case id == hitBulkAuthDismiss:
		state.bulkPassword.authOpen = false
	case id.Scope() == hitBulkAuthItemList:
		if choice := hostAuth(item(id)); choice < authCount {
			state.bulkPassword.auth = choice
		}
		state.bulkPassword.authOpen = false
	case id == hitBulkApplyIdentity:
		state.bulkPassword.applyIdentity = !state.bulkPassword.applyIdentity
	case id == hitBulkIdentityTrigger:
		state.bulkPassword.identityOpen = !state.bulkPassword.identityOpen
		state.bulkPassword.authOpen, state.bulkPassword.passphraseKeyOpen = false, false
	case id == hitBulkIdentityDismiss:
		state.bulkPassword.identityOpen = false
	case id == hitBulkIdentityAll:
		state.bulkPassword.identity = ""
		state.bulkPassword.identityOpen = false
	case id.Scope() == hitBulkIdentityItemList:
		if identity, ok := state.identityByID(uint32(item(id))); ok {
			state.bulkPassword.identity = identity.Fingerprint
		}
		state.bulkPassword.identityOpen = false
	case id == hitBulkApplyOTP:
		state.bulkPassword.applyOTP = !state.bulkPassword.applyOTP
	case id == hitBulkOTPRemove:
		state.bulkPassword.removeOTP = !state.bulkPassword.removeOTP
	case id == hitBulkApplyPassphrase:
		state.bulkPassword.applyPassphrase = !state.bulkPassword.applyPassphrase
	case id == hitBulkPassphraseKeyTrigger:
		state.bulkPassword.passphraseKeyOpen = !state.bulkPassword.passphraseKeyOpen
		state.bulkPassword.authOpen, state.bulkPassword.identityOpen = false, false
	case id == hitBulkPassphraseKeyDismiss:
		state.bulkPassword.passphraseKeyOpen = false
	case id == hitBulkPassphraseRemove:
		state.bulkPassword.removePassphrase = !state.bulkPassword.removePassphrase
	case id.Scope() == hitBulkPassphraseKeyItemList:
		if identity, ok := state.identityByID(uint32(item(id))); ok {
			state.bulkPassword.passphraseKey = identity.Fingerprint
		}
		state.bulkPassword.passphraseKeyOpen = false
	case id == hitBulkApplyAgentForward:
		state.bulkPassword.applyAgentForward = !state.bulkPassword.applyAgentForward
	case id == hitBulkAgentForwardValue:
		state.bulkPassword.agentForward = !state.bulkPassword.agentForward
	}
}

func (state *State) submitBulkHostPassword() {
	batch := &state.bulkPassword
	defer func() {
		if state.dialogError != "" {
			batch.scroll = 0
		}
	}()
	if state.saveBlocked || state.config == nil || state.pauseSave > 0 {
		state.dialogError = state.messages.BulkAuthSaveUnavailable
		return
	}
	if !batch.applyPassword && !batch.applyUser && !batch.applyAuth && !batch.applyIdentity && !batch.applyOTP && !batch.applyPassphrase && !batch.applyAgentForward {
		state.dialogError = state.messages.BulkAuthNoFields
		return
	}
	password := batch.editor.String()
	if batch.applyPassword && password == "" {
		state.dialogError = state.messages.BulkPasswordEmpty
		return
	}
	var otpValue string
	if batch.applyOTP && !batch.removeOTP && strings.TrimSpace(batch.otp.String()) != "" {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(batch.otp.String())), "otpauth://") {
			state.dialogError = state.messages.Tools.ErrOTPInvalid
			return
		}
		parsed, err := otp.Parse(batch.otp.String())
		if err != nil {
			state.dialogError = state.messages.Tools.ErrOTPInvalid
			return
		}
		otpValue = parsed.String()
	}
	var keyPath string
	if batch.applyPassphrase {
		identity, ok := state.identityForFingerprint(batch.passphraseKey)
		if !ok || identity.Error != "" || !identity.Encrypted {
			state.dialogError = state.messages.BulkAuthKeyRequired
			return
		}
		keyPath = identity.Path
		if !batch.removePassphrase {
			secret := []byte(batch.passphrase.String())
			_, err := keyidentity.Load(keyPath, secret)
			clear(secret)
			if err != nil {
				state.dialogError = state.messages.KeyPassphraseWrong
				return
			}
		}
	}
	if batch.applyIdentity && batch.identity != "" {
		identity, ok := state.identityForFingerprint(batch.identity)
		if !ok || identity.Error != "" {
			state.dialogError = state.messages.BulkAuthKeyRequired
			return
		}
	}
	updated := make(map[uint32]Host, len(batch.ids))
	changes := make([]vault.Change, 0, len(batch.ids)*3)
	changed := 0
	for _, id := range batch.ids {
		host, ok := state.hostByID(id)
		if !ok {
			state.dialogError = state.messages.BulkAuthMissingHost
			return
		}
		candidate := host
		if host.isSSH() {
			if batch.applyUser {
				candidate.User = strings.TrimSpace(batch.user.String())
			}
			if batch.applyAuth {
				candidate.Auth = batch.auth
			}
			if batch.applyIdentity && candidate.Auth == authPrivateKey {
				candidate.Identity = batch.identity
			}
			if batch.applyAgentForward {
				candidate.AgentForward = batch.agentForward
			}
			if batch.applyOTP && (batch.removeOTP || otpValue != "") {
				changes = append(changes, vault.Change{Name: otpName(host), Value: otpValue, Delete: batch.removeOTP})
			}
			if batch.applyPassphrase && candidate.Auth == authPrivateKey && candidate.Identity == batch.passphraseKey {
				changes = append(changes, vault.Change{Name: hostPassphraseSecretName(host.CatalogID), Value: encodeHostPassphrase(keyPath, batch.passphrase.String()), Delete: batch.removePassphrase})
			}
		}
		if batch.applyPassword {
			if name, eligible := hostPasswordSecretName(candidate); eligible {
				changes = append(changes, vault.Change{Name: name, Value: password})
			}
		}
		if candidate != host {
			updated[id] = candidate
			changed++
		}
	}
	previousKeys := make(map[uint32]hostConnection, len(state.hosts))
	for _, host := range state.hosts {
		if host.isSSH() {
			previousKeys[host.ID] = connectionKey(host.Address, state.login(host), host.Port)
		}
	}
	seen := make(map[hostConnection]uint32, len(state.hosts))
	for _, host := range state.hosts {
		originalID := host.ID
		if candidate, ok := updated[host.ID]; ok {
			host = candidate
		}
		if host.isSSH() {
			key := connectionKey(host.Address, state.login(host), host.Port)
			if previous, exists := seen[key]; exists && previousKeys[previous] != previousKeys[originalID] {
				state.dialogError = state.messages.BulkAuthDuplicate
				return
			}
			seen[key] = originalID
		}
	}
	if len(changes) == 0 && changed == 0 {
		state.dialogError = state.messages.BulkAuthNoChanges
		return
	}
	if len(changes) > 0 && state.secrets.vault == nil {
		state.dialogError = state.hostPasswordSaveError(vault.ErrUnavailable)
		return
	}
	previous := append([]Host(nil), state.hosts...)
	commit := func() error {
		if changed == 0 {
			return nil
		}
		for index, host := range state.hosts {
			if candidate, ok := updated[host.ID]; ok {
				state.hosts[index] = candidate
			}
		}
		state.refreshHostGroups()
		if err := state.saveWithResultMode(false); err != nil {
			copy(state.hosts, previous)
			state.refreshHostGroups()
			return err
		}
		return nil
	}
	var err error
	if len(changes) > 0 {
		err = state.secrets.vault.ApplyWithCommit(changes, commit)
	} else {
		err = commit()
	}
	if err != nil {
		state.dialogError = fmt.Sprintf(state.messages.BulkAuthSaveFailedFmt, err)
		if errors.Is(err, vault.ErrRollbackFailed) {
			state.dialogError += " " + state.messages.BulkAuthRollbackFailed
		}
		return
	}
	if changed > 0 {
		for index, host := range previous {
			if candidate, ok := updated[host.ID]; ok {
				state.replaceHost(index, host, candidate)
			}
		}
		state.hostDataChanged()
		state.sshSyncFlush()
		state.scheduleSync()
	} else if len(changes) > 0 {
		state.scheduleSync()
	}
	_, skipped := state.bulkAuthScope()
	state.status = fmt.Sprintf(state.messages.BulkAuthSavedFmt, changed, len(changes), skipped)
	state.bulkPassword = bulkHostPassword{}
	state.dialogError = ""
	state.reveal.reset()
	state.modal = modalNone
}

func (state *State) bulkAuthScope() (eligible, skipped int) {
	batch := &state.bulkPassword
	selected := batch.applyPassword || batch.applyUser || batch.applyAuth || batch.applyIdentity ||
		batch.applyOTP || batch.applyPassphrase || batch.applyAgentForward
	if !selected {
		return batch.eligible, batch.skipped
	}
	for _, id := range batch.ids {
		host, ok := state.hostByID(id)
		if !ok {
			skipped++
			continue
		}
		auth, identity := host.Auth, host.Identity
		if host.isSSH() {
			if batch.applyAuth {
				auth = batch.auth
			}
			if batch.applyIdentity && auth == authPrivateKey {
				identity = batch.identity
			}
		}
		candidate := host
		candidate.Auth = auth
		if batch.applyUser && host.isSSH() || batch.applyAuth && host.isSSH() ||
			batch.applyIdentity && host.isSSH() && auth == authPrivateKey ||
			batch.applyOTP && host.isSSH() || batch.applyAgentForward && host.isSSH() ||
			batch.applyPassphrase && host.isSSH() && auth == authPrivateKey && identity == batch.passphraseKey ||
			batch.applyPassword && hostSupportsSavedPassword(candidate) {
			eligible++
		} else {
			skipped++
		}
	}
	return eligible, skipped
}

func (state *State) paintBulkHostPassword(canvas *ui.Canvas, viewport ui.Rect) {
	m, theme := state.messages, state.Theme()
	batch := &state.bulkPassword
	eligible, skipped := state.bulkAuthScope()
	contentHeight := float32(530)
	if state.dialogError != "" {
		contentHeight += 36
	}
	bodyHeight := min(contentHeight, max(float32(310), viewport.Height-170))
	dialog := components.BeginDialog(canvas, theme, viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitBulkPasswordDismiss, CloseID: hitBulkPasswordClose, InitialFocusID: hitBulkApplyUser,
		ContentSize: ui.Size{Width: 680}, FitBody: true, BodyHeight: bodyHeight,
	})
	layout := dialog.Begin(m.BulkPasswordTitle, fmt.Sprintf(m.BulkPasswordDescriptionFmt, eligible, skipped))
	area := components.BeginScrollArea(canvas, theme, layout.Body, ui.Size{Height: contentHeight}).Offset(0, batch.scroll).
		Viewport(hitBulkPasswordViewport, m.BulkPasswordTitle).VerticalScrollbar(hitBulkPasswordScrollbar, m.BulkPasswordTitle, "")
	geometry := area.BeginContent()
	batch.scroll, batch.viewport, batch.maxScroll = geometry.OffsetY, geometry.Viewport, geometry.MaxOffsetY
	body := geometry.Content
	if geometry.MaxOffsetY > 0 {
		body.Width -= 12
	}
	checkbox := func(id ui.ID, label string, checked bool, y float32) {
		rect := ui.Rect{X: body.X + 10, Y: y + 21, Width: 195, Height: 30}
		components.BeginCheckbox(canvas, theme, id, rect, checked).Label(label).Paint()
	}
	field := func(id ui.ID, label string, editor *text.Editor, secure bool, y float32, width float32) {
		bounds := ui.Rect{X: body.X + 218, Y: y, Width: width, Height: 58}
		content := components.BeginField(canvas, theme, bounds).Paint(label, "", "")
		value := editor.String()
		focused := canvas.Focused() == id
		state.paintSecretInput(canvas, content, components.InputSnapshot{
			ID: id, Label: label, Value: value, Secure: secure,
			Focused: focused, PlatformEditing: focused && value != "", Invalid: state.dialogError != "",
		})
	}
	width := max(body.Width-235, float32(120))
	y := body.Y + 4
	if state.dialogError != "" {
		line := state.chrome.dialog.Begin(canvas, theme, ui.Rect{X: body.X + 12, Y: y, Width: body.Width - 24, Height: 28})
		line.Column().Fill().Children(func() {
			line.Text(state.dialogError).FontSize(metrics.FontSizeS).Color(state.pal.danger).LineHeight(24).Ellipsis()
		})
		line.End()
		y += 36
	}
	checkbox(hitBulkApplyUser, m.FieldUser, batch.applyUser, y)
	field(hitBulkUserField, m.FieldUser, &batch.user, false, y, width)
	y += 62
	checkbox(hitBulkApplyAuth, m.FieldAuthentication, batch.applyAuth, y)
	authBounds := ui.Rect{X: body.X + 218, Y: y + 20, Width: width, Height: 34}
	y += 62
	checkbox(hitBulkApplyIdentity, m.FieldIdentity, batch.applyIdentity, y)
	identityBounds := ui.Rect{X: body.X + 218, Y: y + 20, Width: width, Height: 34}
	y += 62
	checkbox(hitBulkApplyPassword, m.FieldPassword, batch.applyPassword, y)
	field(hitBulkPasswordField, m.FieldPassword, &batch.editor, true, y, width)
	y += 62
	checkbox(hitBulkApplyOTP, m.Tools.OTPField, batch.applyOTP, y)
	field(hitBulkOTPField, m.Tools.OTPField, &batch.otp, true, y, width-85)
	otpAction := m.Tools.OTPRemove
	if batch.removeOTP {
		otpAction = m.Tools.OTPKeep
	}
	components.BeginButton(canvas, theme, hitBulkOTPRemove, ui.Rect{X: body.X + body.Width - 97, Y: y + 22, Width: 85, Height: 32}).Small().Outline(otpAction)
	y += 62
	checkbox(hitBulkApplyPassphrase, m.KeyPassphraseLabel, batch.applyPassphrase, y)
	passphraseKeyBounds := ui.Rect{X: body.X + 218, Y: y + 20, Width: width, Height: 34}
	y += 62
	field(hitBulkPassphraseField, m.KeyPassphraseLabel, &batch.passphrase, true, y, width-85)
	passphraseAction := m.Tools.OTPRemove
	if batch.removePassphrase {
		passphraseAction = m.Tools.OTPKeep
	}
	components.BeginButton(canvas, theme, hitBulkPassphraseRemove, ui.Rect{X: body.X + body.Width - 97, Y: y + 22, Width: 85, Height: 32}).Small().Outline(passphraseAction)
	y += 62
	checkbox(hitBulkApplyAgentForward, m.Tools.AgentForward, batch.applyAgentForward, y)
	components.BeginCheckbox(canvas, theme, hitBulkAgentForwardValue, ui.Rect{X: body.X + 220, Y: y + 21, Width: width, Height: 30}, batch.agentForward).
		Label(m.Tools.AgentForward).Paint()
	area.EndContent()
	state.paintBulkAuthSelect(canvas, authBounds)
	state.paintBulkIdentitySelect(canvas, identityBounds, false)
	state.paintBulkIdentitySelect(canvas, passphraseKeyBounds, true)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitBulkPasswordCancel, Label: m.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitBulkPasswordConfirm, Label: m.BulkPasswordConfirm},
	)
	dialog.End()
}

func (state *State) bulkSelectBounds(trigger ui.Rect, rows int) ui.Rect {
	height := float32(8 + min(rows, 7)*32)
	top := trigger.Y + trigger.Height + 4
	window := state.windowRect()
	if top+height > window.Y+window.Height-8 {
		top = trigger.Y - height - 4
	}
	return ui.Rect{X: trigger.X, Y: top, Width: trigger.Width, Height: height}
}

func (state *State) paintBulkAuthSelect(canvas *ui.Canvas, trigger ui.Rect) {
	batch := &state.bulkPassword
	if trigger.Y+trigger.Height < batch.viewport.Y || trigger.Y > batch.viewport.Y+batch.viewport.Height {
		return
	}
	list := state.bulkSelectBounds(trigger, int(authCount))
	selectControl := components.BeginSelect(canvas, state.Theme(), trigger, components.SelectOptions{
		TriggerID: hitBulkAuthTrigger, Label: state.messages.FieldAuthentication, Value: state.authLabel(batch.auth),
		Open: batch.authOpen, Content: list, Layer: dialogLayer + 1, Scope: dialogScope,
		DismissID: hitBulkAuthDismiss, Viewport: state.windowRect(),
	})
	defer selectControl.End()
	for choice := authAutomatic; choice < authCount; choice++ {
		selectControl.AddItem(components.SelectItem{ID: hitBulkAuthItemList.At(int(choice)), Label: state.authLabel(choice), Selected: batch.auth == choice})
	}
}

func (state *State) paintBulkIdentitySelect(canvas *ui.Canvas, trigger ui.Rect, passphrase bool) {
	batch := &state.bulkPassword
	if trigger.Y+trigger.Height < batch.viewport.Y || trigger.Y > batch.viewport.Y+batch.viewport.Height {
		return
	}
	selected, label := batch.identity, state.messages.AuthAllKeys
	open, triggerID, dismissID, items := batch.identityOpen, hitBulkIdentityTrigger, hitBulkIdentityDismiss, hitBulkIdentityItemList
	if passphrase {
		selected, label = batch.passphraseKey, state.messages.AuthKeyUnavailable
		open, triggerID, dismissID, items = batch.passphraseKeyOpen, hitBulkPassphraseKeyTrigger, hitBulkPassphraseKeyDismiss, hitBulkPassphraseKeyItemList
	}
	for _, identity := range state.identities {
		if identity.Fingerprint == selected && selected != "" {
			label = identity.Name
			break
		}
	}
	list := state.bulkSelectBounds(trigger, len(state.identities)+1)
	selectControl := components.BeginSelect(canvas, state.Theme(), trigger, components.SelectOptions{
		TriggerID: triggerID, Label: state.messages.FieldIdentity, Value: label,
		Open: open, Content: list, Layer: dialogLayer + 1, Scope: dialogScope,
		DismissID: dismissID, Viewport: state.windowRect(), Scroll: state.menuScrollFor(dismissID),
	})
	defer selectControl.End()
	if !passphrase {
		selectControl.AddItem(components.SelectItem{ID: hitBulkIdentityAll, Label: state.messages.AuthAllKeys, Selected: selected == ""})
	}
	for _, identity := range state.identities {
		if passphrase && !identity.Encrypted {
			continue
		}
		selectControl.AddItem(components.SelectItem{ID: items.At(int(identity.ID)), Label: identity.Name,
			Selected: selected == identity.Fingerprint, Disabled: identity.Error != "" || identity.Fingerprint == ""})
	}
}

func (state *State) scrollBulkHostPassword(event ui.ScrollEvent) ui.EventResult {
	batch := &state.bulkPassword
	if !pointInRect(event.X, event.Y, batch.viewport) {
		return ui.EventResult{Handled: true}
	}
	previous := batch.scroll
	batch.scroll = max(0, min(batch.maxScroll, previous+event.StepY(32)))
	return ui.EventResult{Handled: true, Repaint: batch.scroll != previous}
}
