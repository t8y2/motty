package workbench

// A host dialog shows the secrets already saved for the host, so editing a host
// does not look as if its password had been lost; the field's eye button then
// shows or hides them.

// savedHostSecretName is the vault name of the secret a form field stands for,
// and false when there is none to look up.
func (state *State) savedHostSecretName(field hostField) (string, bool) {
	switch field {
	case fieldPassword:
		// An edit shows the host's own password; a copy starts from its source's.
		host, ok := state.hostByID(state.hostDialogID)
		if !ok || state.hostDialogMode == hostDialogAdd {
			return "", false
		}
		return hostPasswordSecretName(host)
	case fieldPassphrase:
		if !state.hostAsksKeyPassphrase() {
			return "", false
		}
		identity, ok := state.identityForFingerprint(state.editor.identity)
		if !ok {
			return "", false
		}
		return passphraseSecretName(identity.Path), true
	}
	return "", false
}

// loadSavedHostSecret puts the saved secret of a form field into it. A vault
// that cannot answer leaves the field empty, which saving reads as "keep what is
// saved".
func (state *State) loadSavedHostSecret(field hostField) {
	name, ok := state.savedHostSecretName(field)
	vaultFile := state.secrets.vault
	// An empty vault has nothing to read; asking it would still open the key
	// store for a key that does not exist yet.
	if !ok || vaultFile == nil || vaultFile.Len() == 0 || vaultFile.NeedsConversion() {
		return
	}
	value, found, err := vaultFile.Get(name)
	if err != nil || !found {
		return
	}
	if editor := state.editor.field(field); editor.String() == "" {
		editor.Insert(value)
		if field == fieldPassword {
			state.editor.passwordSaved = true
		}
	}
}

// prefillHostSecrets fills the password of the host being edited and the
// passphrase of its key, if the vault opens without being asked.
func (state *State) prefillHostSecrets() {
	state.loadSavedHostSecret(fieldPassword)
	state.prefillKeyPassphrase()
}

// prefillKeyPassphrase does the same for the key the form has just pinned. It
// asks once per key, so clearing the field is not undone by the next click.
func (state *State) prefillKeyPassphrase() {
	editor := &state.editor
	if !state.hostAsksKeyPassphrase() || editor.passphraseFor == editor.identity {
		return
	}
	editor.passphraseFor = editor.identity
	if editor.passphrase.String() == "" {
		state.loadSavedHostSecret(fieldPassphrase)
	}
}
