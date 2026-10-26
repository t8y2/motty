package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/flex"
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
	state.passwordFields = make([]text.Editor, len(questions))
	state.passwordRects = make([]ui.Rect, len(questions))
	state.passwordBoxes = make([]flex.Box, len(questions))
	state.passwordError = ""
	state.offerToSave()
	if passwordQuestionIndex(questions) < 0 {
		// Nothing here is the account password, so there is nothing worth
		// saving under the password's name.
		state.secrets.canSave = false
	}
	if session.attempt > 1 {
		state.passwordError = state.messages.PasswordRetry
		if state.forgetRefusedSecret(session, passwordSecretName(session.endpoint)) {
			state.passwordError = state.messages.PasswordSavedRefused
		}
	}
	state.passwordDescription = passwordDialogAddress(state, session)
	state.prefillPasswordAnswers(session)
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
	return fmt.Sprintf(state.messages.PasswordDescription, address)
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
func (state *State) prefillPasswordAnswers(session *Session) {
	if len(state.passwordQuestions) < 2 || session.attempt > 1 {
		return
	}
	vaultFile := state.secrets.vault
	if vaultFile == nil || vaultFile.Len() == 0 || !vaultFile.Unlocked() {
		return
	}
	if index := passwordQuestionIndex(state.passwordQuestions); index >= 0 {
		if value, found, err := vaultFile.Get(passwordSecretName(session.endpoint)); err == nil && found {
			state.passwordFields[index].Insert(value)
		}
	}
	if index := otpQuestionIndex(state.passwordQuestions); index >= 0 && !session.otpSent {
		value, found, err := vaultFile.Get(otpSecretName(session.endpoint))
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
	session.conn.SubmitAnswers(answers)
	if index := passwordQuestionIndex(state.passwordQuestions); index >= 0 && index < len(answers) {
		state.stageSecret(session, passwordSecretName(session.endpoint), answers[index])
	}
	if otpQuestionIndex(state.passwordQuestions) >= 0 {
		// A refused code is the user's to retype, not one to refill.
		session.otpSent = true
	}
	state.answered(session)
}
