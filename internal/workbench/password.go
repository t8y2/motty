package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/otp"
	"motty/internal/sshconn"
)

// The password dialog answers one keyboard-interactive round. A round is one
// question for the plain password method and for most keyboard-interactive
// servers, but a PAM stack may ask the account password and a one-time code
// together, so the dialog holds one field per question, in the server's order.

// preparePassword lays out the questions the open prompt waits for. It runs
// before autofill, which may answer a single question on its own.
func (state *State) preparePassword(session *Session) {
	questions := session.questions
	if len(questions) == 0 {
		// The plain password method carries no Server Question; it is one
		// secret prompt with the default label.
		questions = []sshconn.Question{{Secret: true}}
	}
	state.passwordQuestions = questions
	vncCredentials := isVNCCredentialsQuestion(session)
	if vncCredentials {
		state.passwordQuestions = append([]sshconn.Question(nil), questions...)
		state.passwordQuestions[0].Text = state.messages.Desktop.VNCSystemUser
	}
	state.passwordFields = make([]text.Editor, len(questions))
	passwordName := state.passwordName(session)
	var accountError error
	if vncCredentials {
		owner := state.secretOwner(session.host, session.endpoint, session.role)
		username := strings.TrimPrefix(session.endpoint.User, vncAccountPrefix)
		if username == "" && session.attempt == 1 {
			saved, ok, err := state.singleSavedVNCAccount(owner)
			accountError = err
			if ok {
				username = saved
			} else if !state.passwordShared(session) {
				passwordName = "" // no unique account to prefill, including an empty one
			}
		}
		state.passwordFields[0].Insert(username)
		if passwordName != "" {
			endpoint := session.endpoint
			endpoint.User = vncAccountPrefix + username
			passwordName = state.passwordSecret(session.host, endpoint, session.role)
		}
	}
	state.passwordRects = make([]gfx.Rect, len(questions))
	state.passwordBoxes = make([]ui.Box, len(questions))
	state.passwordError = ""
	state.offerToSave()
	passwordIndex := passwordQuestionIndex(questions)
	if passwordIndex < 0 || state.passwordShared(session) {
		// Nothing here is the account password, so there is nothing worth
		// saving under the password's name; and a saved password is changed in
		// Settings, not by whatever one host was last answered with.
		state.secrets.canSave = false
	}
	switch {
	case session.attempt > 1 && passwordIndex >= 0 && otpQuestionIndex(questions) < 0:
		// Only a round that asks for the password alone can blame it. A round
		// that still carries a one-time code may have refused either factor.
		state.passwordError = state.messages.PasswordRetry
		if state.forgetRefusedSecret(session, state.passwordName(session)) {
			state.passwordError = state.messages.PasswordSavedRefused
		}
	case session.attempt > 1 && passwordIndex >= 0:
		// A round that only asks for a later factor keeps the dialog quiet: the
		// server moved on, and the password was not what it refused.
		state.passwordError = state.messages.PasswordRetryFactor
	}
	if accountError != nil {
		state.passwordError = state.vaultErrorText(accountError)
	}
	state.passwordDescription = passwordDialogAddress(state, session)
	state.prefillPasswordAnswers(session, passwordName)
}

// singleSavedVNCAccount recovers an account entered only at login. Multiple
// system accounts remain ambiguous; legacy passwords are never candidates.
func (state *State) singleSavedVNCAccount(owner string) (string, bool, error) {
	vaultFile := state.secrets.vault
	if vaultFile == nil || vaultFile.Len() == 0 || vaultFile.NeedsConversion() {
		return "", false, nil
	}
	names, err := vaultFile.Names()
	if err != nil {
		return "", false, err
	}
	prefix := hostSecretName(owner, "vnc-system-password:")
	username, found := "", false
	for _, name := range names {
		if account, ok := strings.CutPrefix(name, prefix); ok {
			if found {
				return "", false, nil
			}
			username, found = account, true
		}
	}
	return username, found, nil
}

// passwordDialogAddress names the account the dialog is asking about.
func passwordDialogAddress(state *State, session *Session) string {
	address := session.Address
	if session.endpoint.Host != "" {
		address = fmt.Sprintf("%s@%s:%d", session.endpoint.User, session.endpoint.Host, session.endpoint.Port)
	}
	if session.host.isDesktop() && session.endpoint.User == vncAccount {
		address = fmt.Sprintf("VNC %s:%d", session.endpoint.Host, session.endpoint.Port)
	}
	if account, ok := strings.CutPrefix(session.endpoint.User, rdpAccountPrefix); ok && session.host.Protocol == protocolRDP {
		address = fmt.Sprintf("RDP %s@%s:%d", account, session.endpoint.Host, session.endpoint.Port)
	}
	if account, ok := strings.CutPrefix(session.endpoint.User, vncAccountPrefix); ok && session.host.Protocol == protocolVNC {
		address = fmt.Sprintf("VNC %s@%s:%d", account, session.endpoint.Host, session.endpoint.Port)
	}
	return fmt.Sprintf(state.messages.PasswordDescription, address)
}

func isVNCCredentialsQuestion(session *Session) bool {
	return session.host.Protocol == protocolVNC && strings.HasPrefix(session.endpoint.User, vncAccountPrefix) &&
		len(session.questions) == 2 && !session.questions[0].Secret && session.questions[1].Secret
}

// passwordQuestionIndex is the question whose answer is the account password:
// the first secret prompt that is not asking for a one-time code. -1 means no
// answer here is worth saving as the password.
func passwordQuestionIndex(questions []sshconn.Question) int {
	for index, question := range questions {
		if question.Secret && !otp.LooksLikeQuestion(question.Text) {
			return index
		}
	}
	return -1
}

// otpQuestionIndex is the question asking for a one-time code, if one does.
func otpQuestionIndex(questions []sshconn.Question) int {
	for index, question := range questions {
		if question.Secret && otp.LooksLikeQuestion(question.Text) {
			return index
		}
	}
	return -1
}

// prefillPasswordAnswers fills what the vault can answer without sending it:
// a multi-question round is confirmed by the user, and a code sent by itself
// would be spent if the dialog was dismissed. A single question keeps the
// existing autofill path, which may answer and submit on its own.
func (state *State) prefillPasswordAnswers(session *Session, passwordName string) {
	if len(state.passwordQuestions) < 2 || session.attempt > 1 {
		return
	}
	vaultFile := state.secrets.vault
	if vaultFile == nil || vaultFile.Len() == 0 || vaultFile.NeedsConversion() {
		return
	}
	if index := passwordQuestionIndex(state.passwordQuestions); index >= 0 && passwordName != "" {
		if value, found, err := vaultFile.Get(passwordName); err == nil && found {
			state.passwordFields[index].Insert(value)
		}
	}
	if index := otpQuestionIndex(state.passwordQuestions); index >= 0 && !session.otpSent {
		value, found, err := vaultFile.Get(state.otpSecretName(session))
		if err != nil || !found {
			return
		}
		secret, err := otp.Parse(value)
		if err != nil || secret.Kind != otp.TOTP {
			// A counter-based code used by another application would be
			// advanced behind its back; it stays the user's to type.
			return
		}
		state.passwordFields[index].Insert(secret.Code(state.options.WallNow()))
	}
}

// submitPassword sends every answer of the round and forgets them.
func (state *State) submitPassword() {
	session := state.promptSession()
	if session == nil || session.conn == nil {
		state.closeModal()
		return
	}
	answers := make([]string, len(state.passwordFields))
	for index := range state.passwordFields {
		answers[index] = state.passwordFields[index].String()
	}
	if session.host.Protocol == protocolVNC && (session.endpoint.User == vncAccount || strings.HasPrefix(session.endpoint.User, vncAccountPrefix)) {
		if index := passwordQuestionIndex(state.passwordQuestions); index >= 0 && index < len(answers) && answers[index] == "" {
			state.passwordError = state.messages.Desktop.ErrVNCPassword
			return
		}
	}
	if isVNCCredentialsQuestion(session) {
		// Name the saved secret after the account the user actually submitted,
		// before publishing the answers to the connection goroutine.
		session.endpoint.User = vncAccountPrefix + answers[0]
	}
	session.conn.SubmitAnswers(answers)
	if index := passwordQuestionIndex(state.passwordQuestions); index >= 0 && index < len(answers) {
		state.stageSecret(session, state.passwordName(session), answers[index])
	}
	if otpQuestionIndex(state.passwordQuestions) >= 0 {
		// A refused code is the user's to retype, not one to refill.
		session.otpSent = true
	}
	state.answered(session)
}
