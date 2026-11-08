# Development builds

Motty has two kinds of build: the release build, which is what users install,
and the development build, which you run while changing the code. This page
covers how to launch one, where its data lives, how it is signed on macOS, and
which storage format it writes.

## Launch

Build and launch one development binary with:

```sh
make run
```

The binary is built with `-tags dev`, so its window and tray say `Motty Dev`
and its Dock icon carries a red `DEV` badge. The logo and wordmark inside the
window are the release ones, so the interface itself looks like what ships and
the running copy is still told apart at a glance.

Use `make dev` to rebuild and restart when a Go file in this repository or
`../viva` changes. Watch mode needs `fswatch`; one-shot mode does not.
`tools/dev.sh --once` is the same entry point as `make run`.

## Inspecting the interface

A development build has an inspector: press Command+Option+Shift+I (Control+Alt+Shift+I on
Windows and Linux) to cycle it. It first names the control under the pointer
(its identity, role, accessible name, bounds and state), then outlines every
control of the frame, then turns off. It is the real-window view of what
`tree` lists in a headless run, and it is not part of a release build.

A headless script can record an interaction as frames with the input drawn on
them, so a clip shows where a press landed and which keys were typed:

```text
click "New Host"
record /tmp/new-host 800 60 input
```

## Data directories

Development builds use `motty-dev` under the platform's configuration
directory, separate from the release build's unchanged `motty` directory.
Their vault keys also use a separate system-secret namespace, so the two
default copies can run side by side without upgrading each other's databases.

When an unused default development profile finds release data, its first-run
dialog offers **Import copy** or **Start empty**. Import reads the release
profile without changing it, copies hosts, shared settings and trusted keys,
and optionally re-encrypts saved passwords under the development key. It never
copies open sessions or cloud logins. Closing the dialog postpones the choice;
failures stay visible and unfinished imports can resume without replacing
current hosts or passwords. Existing profiles and explicit `MOTTY_DATA_DIR`
profiles are not auto-imported, but they can still restore an encrypted backup
through Backup & sync.

`MOTTY_DATA_DIR` still overrides the directory. Use disposable data and test
credentials for destructive tests: profiles within one build variant still
share its system-secret namespace. Never point a development build at
production data or configure production cloud sync unless that shared access is
intentional. Portable development builds use `motty-dev-data`, not
`motty-data`.

## macOS signing

On macOS the standard entry creates or reuses a private local development
signing identity under
`~/Library/Application Support/motty/development-signing`. The first access
may require one system Keychain authorization. Subsequent content-different
rebuilds reuse that authorization while the same identity and unlocked Keychain
remain available. This does not bypass a locked Keychain, a changed ACL or an
explicit identity change. A signing or verification failure stops the launch
instead of silently running an unsigned build.

Use an explicit certificate without creating the default identity with
`MOTTY_DEV_SIGN_IDENTITY='Developer ID Application: …' make run`. Use
`MOTTY_DEV_SIGN_IDENTITY=none make run` only for an intentional unsigned run;
the entry warns that Keychain authorization may repeat. `go run ./cmd/motty`
is release behaviour, while `go run -tags dev ./cmd/motty` bypasses the signing
helper and is intended only for diagnosis.

## Storage compatibility

Configuration epoch 23 introduces lossless field updates. Compatible additions
to JSON objects no longer require a format bump; unknown fields and records
survive saves, sync and backup restoration. Conflicting edits fail rather than
overwrite unread data. Arrays remain atomic, so incompatible element changes
still require a new epoch. Application writer versions are diagnostic only.

Older epochs (including 19–22) upgrade once; already released older binaries
cannot read epoch 23. Do not edit the database's format number to bypass this
protection. See
[storage compatibility](../tools/README.md#storage-compatibility).
