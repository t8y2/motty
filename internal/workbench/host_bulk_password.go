package workbench

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/configstore"
	"motty/internal/otp"
	"motty/internal/vault"
)

var bulkPasswordHits = tree.Feature("bulk-host-password")

var (
	hitBulkPasswordClose        = tree.ID(bulkPasswordHits, "close")
	hitBulkPasswordCancel       = tree.ID(bulkPasswordHits, "cancel")
	hitBulkPasswordConfirm      = tree.ID(bulkPasswordHits, "review")
	hitBulkPasswordSave         = tree.ID(bulkPasswordHits, "save")
	hitBulkPasswordBack         = tree.ID(bulkPasswordHits, "back")
	hitBulkPasswordField        = tree.ID(bulkPasswordHits, "password")
	hitBulkApplyPassword        = tree.ID(bulkPasswordHits, "apply-password")
	hitBulkApplyUser            = tree.ID(bulkPasswordHits, "apply-user")
	hitBulkUserField            = tree.ID(bulkPasswordHits, "user")
	hitBulkApplyAuth            = tree.ID(bulkPasswordHits, "apply-auth")
	hitBulkAuthTrigger          = tree.ID(bulkPasswordHits, "auth-trigger")
	hitBulkApplyIdentity        = tree.ID(bulkPasswordHits, "apply-identity")
	hitBulkIdentityTrigger      = tree.ID(bulkPasswordHits, "identity-trigger")
	hitBulkApplyOTP             = tree.ID(bulkPasswordHits, "apply-otp")
	hitBulkOTPField             = tree.ID(bulkPasswordHits, "otp")
	hitBulkOTPRemove            = tree.ID(bulkPasswordHits, "remove-otp")
	hitBulkApplyPassphrase      = tree.ID(bulkPasswordHits, "apply-passphrase")
	hitBulkPassphraseField      = tree.ID(bulkPasswordHits, "passphrase")
	hitBulkPassphraseKeyTrigger = tree.ID(bulkPasswordHits, "passphrase-key-trigger")
	hitBulkPassphraseRemove     = tree.ID(bulkPasswordHits, "remove-passphrase")
	hitBulkApplyAgentForward    = tree.ID(bulkPasswordHits, "apply-agent-forward")
	hitBulkAgentForwardValue    = tree.ID(bulkPasswordHits, "agent-forward-value")
)

type bulkHostPassword struct {
	ids               []uint32
	catalogIDs        map[uint32]string
	eligible, skipped int
	editor            text.Editor
	user              text.Editor
	otp               text.Editor
	passphrase        text.Editor
	fields            [4]ui.Field
	bound             bool
	applyPassword     bool
	applyUser         bool
	applyAuth         bool
	auth              hostAuth
	applyIdentity     bool
	identity          string
	applyOTP          bool
	removeOTP         bool
	applyPassphrase   bool
	removePassphrase  bool
	passphraseKey     string
	applyAgentForward bool
	agentForward      bool
	review            *bulkAuthPlan
}

type bulkAuthPlan struct {
	selection     []uint32
	hosts         []Host
	updated       map[uint32]Host
	changes       []vault.Change
	expected      map[string]string
	impact        []string
	affected      map[uint32]bool
	sharedSkipped int
}

func (batch *bulkHostPassword) editorFor(id gfx.ID) *text.Editor {
	if batch.review != nil {
		return nil
	}
	switch id {
	case hitBulkUserField:
		return &batch.user
	case hitBulkPasswordField:
		return &batch.editor
	case hitBulkOTPField:
		return &batch.otp
	case hitBulkPassphraseField:
		return &batch.passphrase
	}
	return nil
}

func bulkAuthHostEligible(host Host) bool {
	return host.isSSH() || host.Protocol == protocolVNC || host.Protocol == protocolRDP
}

func (state *State) selectedHostsCanEditAuth() bool {
	for _, id := range state.hostSelections {
		if host, ok := state.hostByID(id); ok && bulkAuthHostEligible(host) {
			return true
		}
	}
	return false
}

func (state *State) openBulkHostPassword() {
	batch := bulkHostPassword{catalogIDs: make(map[uint32]string, len(state.hostActionIDs))}
	for _, id := range state.hostActionIDs {
		if host, ok := state.hostByID(id); ok && !slices.Contains(batch.ids, id) {
			batch.ids = append(batch.ids, id)
			batch.catalogIDs[id] = host.CatalogID
			if bulkAuthHostEligible(host) {
				batch.eligible++
			} else {
				batch.skipped++
			}
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

func (state *State) clickBulkHostPassword(id gfx.ID) {
	batch := &state.bulkPassword
	switch id {
	case hitBulkPasswordClose, hitBulkPasswordCancel:
		state.closeModal()
	case hitBulkPasswordBack:
		batch.review = nil
		state.dialogError = ""
	case hitBulkPasswordSave:
		state.confirmBulkHostPassword()
	case hitBulkPasswordConfirm:
		if batch.review == nil {
			state.submitBulkHostPassword()
		}
	default:
		if batch.review != nil {
			return
		}
		if flag := batch.check(id); flag != nil {
			*flag = !*flag
		}
	}
}

func (batch *bulkHostPassword) check(id gfx.ID) *bool {
	switch id {
	case hitBulkApplyUser:
		return &batch.applyUser
	case hitBulkApplyAuth:
		return &batch.applyAuth
	case hitBulkApplyIdentity:
		return &batch.applyIdentity
	case hitBulkApplyPassword:
		return &batch.applyPassword
	case hitBulkApplyOTP:
		return &batch.applyOTP
	case hitBulkOTPRemove:
		return &batch.removeOTP
	case hitBulkApplyPassphrase:
		return &batch.applyPassphrase
	case hitBulkPassphraseRemove:
		return &batch.removePassphrase
	case hitBulkApplyAgentForward:
		return &batch.applyAgentForward
	case hitBulkAgentForwardValue:
		return &batch.agentForward
	}
	return nil
}

func (state *State) submitBulkHostPassword() {
	plan, failure := state.planBulkHostPassword()
	state.dialogError = failure
	state.bulkPassword.review = plan
}

func (state *State) planBulkHostPassword() (*bulkAuthPlan, string) {
	batch, m := &state.bulkPassword, state.messages
	if state.saveBlocked || state.config == nil || state.pauseSave > 0 || state.appAccessBlocked() {
		return nil, m.BulkAuthSaveUnavailable
	}
	if !batch.hasFields() {
		return nil, m.BulkAuthNoFields
	}
	password := batch.editor.String()
	if batch.applyPassword && password == "" {
		return nil, m.BulkPasswordEmpty
	}
	var otpValue string
	if seed := strings.TrimSpace(batch.otp.String()); batch.applyOTP && !batch.removeOTP && seed != "" {
		if !strings.HasPrefix(strings.ToLower(seed), "otpauth://") {
			return nil, m.Tools.ErrOTPInvalid
		}
		parsed, err := otp.Parse(seed)
		if err != nil {
			return nil, m.Tools.ErrOTPInvalid
		}
		otpValue = parsed.String()
	}
	var keyPath string
	if batch.applyPassphrase {
		identity, ok := state.identityForFingerprint(batch.passphraseKey)
		if !ok || identity.Error != "" || !identity.Encrypted {
			return nil, m.BulkAuthKeyRequired
		}
		keyPath = identity.Path
		if !batch.removePassphrase {
			secret := []byte(batch.passphrase.String())
			err := state.keyPassphraseOpens(identity, secret)
			clear(secret)
			if err != nil {
				return nil, m.KeyPassphraseWrong
			}
		}
	}
	if batch.applyIdentity && batch.identity != "" {
		if identity, ok := state.identityForFingerprint(batch.identity); !ok || identity.Error != "" {
			return nil, m.BulkAuthKeyRequired
		}
	}
	plan := &bulkAuthPlan{
		selection: slices.Clone(state.hostSelections), updated: make(map[uint32]Host),
		expected: make(map[string]string), affected: make(map[uint32]bool),
	}
	for _, id := range batch.ids {
		host, ok := state.hostByID(id)
		if !ok || host.CatalogID != batch.catalogIDs[id] {
			return nil, m.BulkAuthMissingHost
		}
		plan.hosts = append(plan.hosts, host)
		candidate := batch.candidate(host)
		if candidate.User != host.User || candidate.Auth != host.Auth || candidate.Identity != host.Identity || candidate.AgentForward != host.AgentForward {
			plan.updated[id] = candidate
			plan.affected[id] = true
		}
		add := func(label string, change vault.Change, matchingPath string) string {
			vaultFile := state.secrets.vault
			if vaultFile == nil {
				return state.hostPasswordSaveError(vault.ErrUnavailable)
			}
			previous, found, err := vaultFile.Get(change.Name)
			if err != nil {
				return state.hostPasswordSaveError(err)
			}
			if change.Delete && !found || !change.Delete && found && previous == change.Value {
				return ""
			}
			if matchingPath != "" {
				if _, matches := decodeHostPassphrase(previous, matchingPath); !matches {
					return ""
				}
			}
			format := m.BulkAuthAddFmt
			if found {
				plan.expected[change.Name] = previous
				format = m.BulkAuthOverwriteFmt
				if change.Delete {
					format = m.BulkAuthRemoveFmt
				}
			}
			plan.impact = append(plan.impact, fmt.Sprintf(format, host.Label+" · "+state.sessionAddress(host), label))
			plan.affected[id] = true
			plan.changes = append(plan.changes, change)
			return ""
		}
		if batch.applyPassword {
			if name, eligible := hostPasswordSecretName(candidate); eligible {
				if candidate.Credential != "" {
					plan.sharedSkipped++
				} else if failure := add(m.FieldPassword, vault.Change{Name: name, Value: password}, ""); failure != "" {
					return nil, failure
				}
			}
		}
		if host.isSSH() && batch.applyOTP && (batch.removeOTP || otpValue != "") {
			if failure := add(m.Tools.OTPField, vault.Change{Name: otpName(host), Value: otpValue, Delete: batch.removeOTP}, ""); failure != "" {
				return nil, failure
			}
		}
		if host.isSSH() && batch.applyPassphrase && candidate.Auth == authPrivateKey && candidate.Identity == batch.passphraseKey {
			change := vault.Change{Name: hostPassphraseSecretName(host.CatalogID), Delete: batch.removePassphrase}
			matchingPath := ""
			if change.Delete {
				matchingPath = keyPath
			} else {
				change.Value = encodeHostPassphrase(keyPath, batch.passphrase.String())
			}
			if failure := add(m.KeyPassphraseLabel, change, matchingPath); failure != "" {
				return nil, failure
			}
		}
	}
	if state.bulkAuthDuplicate(plan.updated) {
		return nil, m.BulkAuthDuplicate
	}
	if len(plan.affected) == 0 {
		if plan.sharedSkipped != 0 {
			return nil, m.BulkAuthNoChanges + " " + fmt.Sprintf(m.BulkAuthSharedFmt, plan.sharedSkipped)
		}
		return nil, m.BulkAuthNoChanges
	}
	return plan, ""
}

func (batch *bulkHostPassword) hasFields() bool {
	return batch.applyPassword || batch.applyUser || batch.applyAuth || batch.applyIdentity || batch.applyOTP || batch.applyPassphrase || batch.applyAgentForward
}

func (batch *bulkHostPassword) candidate(host Host) Host {
	if !host.isSSH() {
		return host
	}
	if batch.applyUser {
		host.User = strings.TrimSpace(batch.user.String())
	}
	if batch.applyAuth {
		host.Auth = batch.auth
	}
	if batch.applyIdentity && host.Auth == authPrivateKey {
		host.Identity = batch.identity
	}
	if batch.applyAgentForward {
		host.AgentForward = batch.agentForward
	}
	return host
}

func (state *State) bulkAuthDuplicate(updated map[uint32]Host) bool {
	seen := make(map[hostConnection]hostConnection)
	for _, host := range state.hosts {
		if !host.isSSH() {
			continue
		}
		before := connectionKey(host.Address, state.login(host), host.Port)
		if candidate, ok := updated[host.ID]; ok {
			host = candidate
		}
		after := connectionKey(host.Address, state.login(host), host.Port)
		if previous, exists := seen[after]; exists && previous != before {
			return true
		}
		seen[after] = before
	}
	return false
}

func (state *State) confirmBulkHostPassword() {
	batch, m := &state.bulkPassword, state.messages
	if batch.review == nil {
		return
	}
	reviewed := batch.review
	batch.review = nil
	plan, failure := state.planBulkHostPassword()
	if failure != "" {
		state.dialogError = failure
		return
	}
	if !reflect.DeepEqual(plan, reviewed) {
		state.dialogError = m.BulkAuthChanged
		return
	}
	previousHosts := slices.Clone(state.hosts)
	previousSaved := state.lastSaved
	commit := func() error {
		if len(plan.updated) == 0 {
			return nil
		}
		previousForwards := slices.Clone(state.forwards)
		for _, host := range previousHosts {
			if candidate, ok := plan.updated[host.ID]; ok && host.User != candidate.User {
				state.updateHostForwardEndpoints(host, candidate)
			}
		}
		for index, host := range state.hosts {
			if candidate, ok := plan.updated[host.ID]; ok {
				state.hosts[index] = candidate
			}
		}
		state.refreshHostGroups()
		if err := state.saveWithResultModeRecovery(false, false, false); err != nil {
			copy(state.hosts, previousHosts)
			copy(state.forwards, previousForwards)
			state.refreshHostGroups()
			return err
		}
		return nil
	}
	var err error
	if len(plan.changes) != 0 {
		err = state.secrets.vault.ApplyCheckedWithCommit(plan.changes, plan.expected, commit)
	} else {
		err = commit()
	}
	if err != nil {
		switch {
		case errors.Is(err, vault.ErrChanged):
			state.dialogError = m.BulkAuthChanged
		default:
			state.dialogError = fmt.Sprintf(m.BulkAuthSaveFailedFmt, state.vaultErrorText(err))
			if errors.Is(err, vault.ErrRollbackFailed) {
				state.dialogError += " " + m.BulkAuthRollbackFailed
			}
			if errors.Is(err, configstore.ErrConflict) {
				_ = state.refreshShared()
			}
		}
		return
	}
	if len(plan.updated) != 0 {
		for _, host := range previousHosts {
			if candidate, ok := plan.updated[host.ID]; ok {
				state.refreshHostReferences(host, candidate)
			}
		}
		state.hostDataChanged()
		state.configurationSaved(changedSavedFields(previousSaved, state.lastSaved), false, false)
		state.sshSyncFlush()
	}
	state.scheduleSync()
	state.status = fmt.Sprintf(m.BulkAuthSavedFmt, len(plan.affected), len(plan.changes), len(batch.ids)-len(plan.affected))
	state.closeModal()
}

func (state *State) bulkAuthScope() (eligible, skipped int) {
	batch := &state.bulkPassword
	if !batch.hasFields() {
		return batch.eligible, batch.skipped
	}
	for _, id := range batch.ids {
		host, ok := state.hostByID(id)
		candidate := batch.candidate(host)
		_, password := hostPasswordSecretName(candidate)
		if ok && (host.isSSH() && (batch.applyUser || batch.applyAuth || batch.applyOTP || batch.applyAgentForward ||
			candidate.Auth == authPrivateKey && (batch.applyIdentity || batch.applyPassphrase && candidate.Identity == batch.passphraseKey)) ||
			batch.applyPassword && password && candidate.Credential == "") {
			eligible++
		} else {
			skipped++
		}
	}
	return eligible, skipped
}

func (state *State) declareBulkHostPassword(c ui.C) {
	batch, m := &state.bulkPassword, state.messages
	if !batch.bound {
		for index, editor := range []*text.Editor{&batch.user, &batch.editor, &batch.otp, &batch.passphrase} {
			batch.fields[index].Bind(editor)
			batch.fields[index].Secure = index != 0
		}
		batch.bound = true
	}
	focus := hitBulkApplyUser
	if batch.review != nil {
		focus = hitBulkPasswordBack
	}
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.BulkPasswordTitle, Width: 580, Height: 620, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitBulkPasswordClose, InitialFocusID: focus,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().PadSides(4, 10, 4, 10).Gap(8).Children(func() {
			if state.dialogError != "" {
				ui.Wrap(c, state.dialogError).FillWidth().FontSize(metrics.FontSizeS).Color(state.pal.danger)
			}
			if review := batch.review; review != nil {
				ui.Wrap(c, fmt.Sprintf(m.BulkAuthImpactFmt, len(review.affected), len(batch.ids)-len(review.affected))).FillWidth().Weight(gfx.WeightSemibold)
				if review.sharedSkipped > 0 {
					ui.Wrap(c, fmt.Sprintf(m.BulkAuthSharedFmt, review.sharedSkipped)).FillWidth().FontSize(metrics.FontSizeS).Color(state.pal.muted)
				}
				ui.Wrap(c, m.BulkAuthRecovery).FillWidth().FontSize(metrics.FontSizeS).Color(state.pal.muted)
				ui.Scroll(c).Grow(1).FillWidth().Gap(6).Children(func() {
					for _, line := range review.impact {
						ui.Wrap(c, line).FillWidth().FontSize(metrics.FontSizeS)
					}
				})
				return
			}
			eligible, skipped := state.bulkAuthScope()
			ui.Wrap(c, fmt.Sprintf(m.BulkPasswordDescriptionFmt, eligible, skipped)).FillWidth().FontSize(metrics.FontSizeS).Color(state.pal.muted)
			ui.Scroll(c).Grow(1).FillWidth().Gap(8).Children(func() { state.declareBulkAuthFields(c) })
		})
	})
	dialog.Footer(func() {
		button := func(id gfx.ID, label string, primary bool) {
			control := ui.Button(c, label).Hit(id, label)
			if !primary {
				control.Secondary()
			}
			if control.Clicked() {
				c.After(func() { state.clickBulkHostPassword(id) })
			}
		}
		button(hitBulkPasswordCancel, m.Cancel, false)
		if batch.review != nil {
			button(hitBulkPasswordBack, m.BulkAuthBack, false)
			button(hitBulkPasswordSave, m.BulkAuthSave, true)
		} else {
			button(hitBulkPasswordConfirm, m.BulkAuthReview, true)
		}
	})
	if dialog.Dismissed() {
		c.After(state.closeModal)
	}
}

func (state *State) declareBulkAuthFields(c ui.C) {
	batch, m := &state.bulkPassword, state.messages
	check := func(id gfx.ID, title string) {
		ui.Checkbox(c, state.pageFlag(*batch.check(id)), title).Hit(id, title)
	}
	field := func(index int, id gfx.ID, label string, disabled bool) {
		ui.TextField(c, &batch.fields[index], label, label, ui.WithID(id)).FillWidth().Disabled(disabled)
	}
	identityLabel := func(fingerprint string) string {
		if identity, ok := state.identityForFingerprint(fingerprint); ok {
			return identity.Name
		}
		return m.AuthAllKeys
	}
	keys := []string{""}
	var encrypted []string
	for _, identity := range state.identities {
		if identity.Error == "" && identity.Fingerprint != "" {
			keys = append(keys, identity.Fingerprint)
			if identity.Encrypted {
				encrypted = append(encrypted, identity.Fingerprint)
			}
		}
	}
	check(hitBulkApplyUser, m.FieldUser)
	field(0, hitBulkUserField, m.FieldUser, !batch.applyUser)
	check(hitBulkApplyAuth, m.FieldAuthentication)
	ui.Select(c, &batch.auth, []hostAuth{authAutomatic, authPassword, authPrivateKey, authAgent}, state.authLabel, ui.WithID(hitBulkAuthTrigger)).FillWidth().Disabled(!batch.applyAuth)
	check(hitBulkApplyIdentity, m.FieldIdentity)
	ui.Select(c, &batch.identity, keys, identityLabel, ui.WithID(hitBulkIdentityTrigger)).FillWidth().Disabled(!batch.applyIdentity)
	check(hitBulkApplyPassword, m.FieldPassword)
	field(1, hitBulkPasswordField, m.FieldPassword, !batch.applyPassword)
	check(hitBulkApplyOTP, m.Tools.OTPField)
	field(2, hitBulkOTPField, m.Tools.OTPField, !batch.applyOTP || batch.removeOTP)
	check(hitBulkOTPRemove, m.Tools.OTPRemove)
	check(hitBulkApplyPassphrase, m.KeyPassphraseLabel)
	ui.Select(c, &batch.passphraseKey, encrypted, func(fingerprint string) string {
		if fingerprint == "" {
			return m.BulkAuthKeyRequired
		}
		return identityLabel(fingerprint)
	}, ui.WithID(hitBulkPassphraseKeyTrigger)).FillWidth().Disabled(!batch.applyPassphrase)
	field(3, hitBulkPassphraseField, m.KeyPassphraseLabel, !batch.applyPassphrase || batch.removePassphrase)
	check(hitBulkPassphraseRemove, m.Tools.OTPRemove)
	check(hitBulkApplyAgentForward, m.Tools.AgentForward)
	check(hitBulkAgentForwardValue, m.BulkAuthForwardEnabled)
}
