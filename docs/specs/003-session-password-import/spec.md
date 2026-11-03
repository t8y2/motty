# Session Password Import and MobaXterm Encoding

Status: approved scope from the 2026-11-03 conversation; implementation and source-specific encrypted-password feasibility remain to be verified.

## Intent

Imported connections should retain a usable saved password when the selected export contains a readable SSH password. MobaXterm session names and groups in issue #351 should survive import without Chinese text corruption.

## Scope

- Electerm bookmark JSON and Netcatty CSV may supply SSH login passwords. The user must explicitly choose to save available passwords after seeing the number of eligible new and existing hosts. Choosing connection-only import preserves the old behavior. Cancel makes no changes.
- A repeated import matches an existing SSH host by address, user and effective port. It may fill a missing password, but must never replace an existing password, even if the imported value differs or another window writes it after preview. The absent check and write share the vault lock. It does not alter other host fields. An imported duplicate with incompatible authentication is not eligible.
- A newly imported SSH host with a usable password stores it under the host's stable catalog ID in the existing vault, not in host metadata. Passwords for non-SSH sessions, private-key passphrases and private keys remain out of scope. Automatic and password SSH authentication may use the saved password; explicitly key/agent-only hosts are ineligible.
- Vault errors must not leave newly imported hosts saved without the passwords the user chose to save. The dialog stays open on failure; the user may retry or choose connection-only import. A config-save failure restores pre-import host state and the vault's prior encrypted records without publishing tombstones; an unsuccessful vault restore is disclosed as a partial failure.
- Duplicate passwords in the same import do not silently pick a winner: conflicting values for one endpoint are ineligible and counted as conflicts. Identical duplicates can contribute one password.
- Results, diagnostic output, error text, logs and documentation never contain imported secret values. Do not alter the source export or delete it; warn that a plaintext source file still needs secure handling.
- Fix #351 without changing the decode behavior of unrelated formats. Respect UTF-8 and UTF-16 BOMs; recognize a supported legacy MobaXterm Chinese encoding only after validating it against a byte fixture. Do not silently drop undecodable bytes. Preserve source ordering and existing file/archive size limits.
- When BOM-less bytes are valid as both UTF-8 and GB18030 but decode differently, import pauses before saving and shows a comparison dialog with explicit encoding choices, including differing login names. Ambiguous multiple-file imports are rejected for separate per-file selection. This collision cannot be solved reliably by autodetection. Malformed BOM-marked UTF-16 is rejected.

## Separate follow-up

Research PuTTY, Xshell, MobaXterm and WindTerm credential storage per version and input format. An encrypted source is not implemented until a safe, verifiable migration route and licensed test vectors exist. PuTTY proxy credentials must not be mistaken for an SSH host login password. Record supported/unsupported conditions and the exact missing input for each client.

## Acceptance

- A first import can save distinct Electerm/Netcatty SSH passwords for distinct hosts; reopening the editor and connecting read the same vault secret. Connection-only and cancel paths save no secret.
- A repeated import fills only missing compatible SSH secrets; existing secrets and metadata remain unchanged. A conflict or vault failure reports counts/errors without exposing credentials or partial host imports.
- A GB18030 MobaXterm fixture with Chinese names/groups imports intact, including an ambiguous byte sequence through explicit selection, while UTF-8, UTF-16 and damaged bytes keep explicit, tested behavior.
- Parser, workbench, vault-failure and English/Chinese UI tests pass. The public issue attachment is not copied into the repository unless inspected and sanitized.
