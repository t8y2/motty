# Batch Host Authentication Editing

Status: revised scope confirmed in the 2026-11-03 conversation for PR #366 and issue #361. The earlier password-only implementation is not sufficient.

## Behavior

- The selected-host action opens one batch editor. Each field is explicitly enabled for this operation; disabled fields preserve each selected host's value. Capture target IDs on opening, show eligible/skipped counts, and leave everything unchanged on cancel or no enabled fields.
- SSH fields: login username, authentication policy (Automatic, Password, Private Key, SSH Agent), pinned identity (including all keys), saved login password, one-time code seed, and SSH agent forwarding. Non-SSH hosts are skipped for these. A chosen identity must be available. Saved passwords are usable only for Automatic/Password; switching to Key/Agent does not erase an old password. Changing only the policy keeps a pinned identity dormant through restart and subsequent edits. A username or policy change is never inferred from entering a password.
- VNC/RDP retain bulk saved-password support. Telnet/serial have no saved authentication in Motty and are skipped.
- An enabled OTP field accepts a valid `otpauth://` seed per selected SSH host; an explicit Remove action can delete it. A blank field without Remove preserves the saved value. Values are masked and omitted from host metadata, status and logs.
- For an encrypted private key, validate the entered passphrase with `keyidentity.Load`. This is a saved unlocking value, not a change to the key file. Require one pinned encrypted key when setting it. Store a vault override under each selected host, bound to the exact key path; the selected host reads this before the existing key-path secret. Other hosts, including unselected hosts using the same key, retain their existing behavior. Explicitly clearing an override returns to the shared fallback. Deleting a host removes its override; rejecting an override forgets only that host's value.
- Empty username can deliberately return to the system-login default. Reject a batch that would introduce duplicate effective destinations after resolving the system login, without rejecting pre-existing duplicates unrelated to the batch. Agent forwarding is separate from Agent authentication and changes only if enabled. Never change MCP agent access or remote server passwords.
- Validate all targets and secrets before writing. Secret changes use one `vault.Apply`; a failure leaves the dialog open and does not update host metadata. A config-save failure is reported, not called success. Existing sessions follow the single-host editor's reconnect semantics.

## Acceptance

- Mixed selections change only enabled fields on eligible hosts, keeping other host fields and untouched secrets; skipped items are reported.
- Selected and unselected hosts sharing one encrypted key use different saved-passphrase sources after restart and through terminal, SFTP, host test and MCP agent paths; the unselected host is unaffected.
- Mode/key/username changes preserve unrelated saved credentials; wrong passphrase, invalid OTP, unavailable identity, duplicate destination, cancel, locked vault and failed save do not silently partially apply a batch.
- English and Chinese UI works at the narrowest supported host-panel width; full Go tests, focused race tests, vet and build run before the PR is marked ready.
