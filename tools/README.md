# Tools

Developer-only helpers. Nothing here is compiled into the application or shipped
in a package.

## Entry points

| Path | What it does |
| --- | --- |
| `headless_drive.py` | Drives a `-tags vivahead` build through the headless line protocol: `click`, `type`, `key`, `waitfor`, `shot`, `texts`, `layout`, and the checks `tree` and `expect` (`expect text "Connected" within 5000`, `expect layout clean`). A script runs in the language it was written for (a `# lang: zh-CN` line, else Chinese when its commands contain Chinese, else English) and a failed run ends with each failed command and its line; `--fail-fast` stops at the first. `--build` and `--watch` add the `dev` tag, so the interface they iterate has the component gallery. |
| `dev.sh` | Builds, signs and launches a real `-tags dev` window. `--once` runs one build; the default mode rebuilds and restarts when Go files here or in `../viva` change and needs `fswatch`. `make run` and `make dev` are shortcuts. |
| `dev_launch.sh` | Shared launch observation for `dev.sh`. It waits for the application's real window-created message instead of treating a child PID as ready, and distinguishes start, hand-off, timeout and failure. |
| `macos_dev_signing.sh` | Creates or reuses the private local macOS development identity, signs the executable, and verifies its identifier and certificate-bound designated requirement. `MOTTY_DEV_SIGN_IDENTITY` overrides the identity; `none` explicitly disables signing. |
| `macos_dev_signing_integration_test.sh` | Opt-in real macOS Keychain test. It compiles two materially different probes, signs both with the development identity, and checks CDHash, requirement reuse, authorization reuse and rejection of an ad-hoc control in an isolated Keychain. |
| `release.sh` | Builds the release packages, checksums, signing and optional upload. See `docs/release.md`. |
| `realwin/` | Drives and times a probe build of Motty on a physical Windows machine over SSH, in the logged-in desktop session: scenarios, screenshots, and key-to-screen and frame-pacing traces from a patched scratch copy. Endpoint comes from a git-ignored `local.sh`. See `realwin/README.md`. |
| `testslice/` | Prints the `-run` pattern for one shard of a package's tests, picked by test-name hash, so `make race-workbench` can spread the workbench suite over the parallel jobs in `tests.yml`. |
| `competitor_issues.py` | Fetches closed issues (and pull requests, dropped) from the Netcatty and NyaTerm repositories into JSON for requirement mining. `--state`, `--label`, `--summary`. |

Run the first two from the repository root.

## Development launch and signing

Use `make run` for one build and `make dev` for the edit loop. Development uses
the platform's `motty-dev` directory and a separate system-secret namespace;
release continues to use `motty`. Each default directory has its own
single-instance claim. An explicit `MOTTY_DATA_DIR` still wins, but two copies
pointing at the same directory share a claim, not live cross-process refresh.
The old `development_workspace` record remains readable for existing profiles.

An unused default development profile offers a first-run import when release
data exists. **Import copy** reads the release configuration and trusted keys;
the optional password checkbox authorizes reading the release secret store and
re-encrypting those secrets in the development vault. No source file or key is
written, and cloud credentials and workspace state are excluded. **Start empty**
records the opt-out; closing the dialog postpones it until another start.
Existing profiles and explicit directory overrides do not trigger discovery.

Failures remain in the dialog. A progress marker committed with the imported
records lets a failed password/key step resume after restart without replacing
hosts or passwords already in development. Users can retry without passwords
if the source vault is unavailable, or keep the partial import explicitly.
An encrypted backup can also be imported through Backup & sync. Use a separate
cloud destination for development. Directory overrides within one variant do
not create extra system-secret namespaces.

The import tests use temporary source and destination profiles, never real user
data. With `tools/e2esshd` listening on loopback port 22483, validate the actual
dialog, saved-password SSH login and restart with:

```sh
MOTTY_IMPORT_SSH_PORT=22483 go test -tags 'dev vivahead' ./internal/workbench -run TestDevelopmentImport -count=1
```

On macOS, the standard entry initializes a private self-signed identity and
private Keychain under
`~/Library/Application Support/motty/development-signing` when needed. The first
access may ask for system authorization. A later content-different rebuild
signed with the same identity and an unlocked Keychain reuses that authorization;
this is not a promise to suppress prompts after locking, ACL changes or an
explicit identity change. A sign or verification error stops the launch.

Set `MOTTY_DEV_SIGN_IDENTITY='Developer ID Application: …'` to use an existing
certificate without initializing the default identity. Set it to `none` only
for an intentional unsigned run; the helper prints the resulting authorization
warning. `go run -tags dev ./cmd/motty` bypasses these helpers; `go run
./cmd/motty` is release behaviour.

The helper preserves incomplete, expired, mismatched or interrupted signing
material for manual repair. Do not delete the identity directory or rotate the
vault key to avoid an authorization prompt. Inspect the reported path and
configuration first; the integration test never reads the real `motty vault`
item, data key or release certificate. Run the real-keychain check only when
explicitly intended:

```sh
MOTTY_DEV_SIGNING_INTEGRATION=1 bash tools/macos_dev_signing_integration_test.sh
```

The default stub checks do not need a user Keychain:

```sh
bash -n tools/dev.sh tools/dev_launch.sh tools/macos_dev_signing.sh \
  tools/macos_dev_signing_integration_test.sh
bash tools/dev_launch_test.sh
bash tools/macos_dev_signing_test.sh
bash tools/dev_script_test.sh
```

## Storage compatibility

`PRAGMA user_version` describes SQLite tables. `meta.format` and the encrypted
backup's `format` are the minimum reader/writer compatibility epoch, not a
feature counter. Epoch 23 excludes earlier, lossy whole-object writers.
`meta.writer` and a backup's `writer` identify the editing application and do
not prevent a compatible application from reading it.

Adding optional object fields does not raise the epoch when their absence keeps
the old meaning. The workbench projects its typed DTOs before and after an edit;
the store applies only that difference to the latest JSON in a transaction.
Unknown object fields, nested fields, envelopes and record kinds survive.
Missing, null and explicit clearing remain distinct. Disjoint concurrent edits
merge; competing edits of the same value fail and roll back the whole change.
Reload before retrying a conflicting edit. No background worker or polling is
needed.

Arrays are atomic values, not positional object patches. An unchanged array
retains all its raw data. A changed array with unread fields or concurrent edits
is rejected rather than guessed at by index. Changes to array element schemas,
existing value meanings, required fields, credential ownership or protocol
invariants need a compatibility review and, when incompatible, a new epoch.
Do not claim arbitrary downgrade compatibility.

Opening a pre-23 database makes a one-time compatibility transition without
rewriting its records. Existing old binaries cannot be retrofitted: use a build
that supports the current epoch, never lower the stored number. Already-open
compatible writers recheck the epoch inside writes, so a later incompatible
upgrade cannot be overwritten by a stale connection.

Backup restoration stores raw records rather than round-tripping them through
UI structs. Before replacement it creates a consistent SQLite snapshot in
`config.db.before-restore`, preserving prior safety copies. Unknown records and
fields also survive cloud round trips; sync retains its record-level
last-writer-wins conflict policy. Backups deliberately exclude cloud credentials
and device workspace state. Data migrations that become irreversible must add
their own pre-migration snapshot and rollback procedure.

Focused checks:

```sh
go test ./internal/configstore ./internal/workbench -run 'Test(JSONChange|PatchTransaction|OpenWriter|SnapshotReopens|Compatible|CompatibilityEpoch)' -count=1
go test -tags dev ./cmd/motty ./internal/workbench -run 'Test(Development|Claim|Compatible|CompatibilityEpoch)' -count=1
```

## End-to-end

`e2e/` holds the headless command scripts. They are not Go tests: each one names
its fixture, environment variables and commands in its header. Run them from the
repository root, for example:

```sh
make head
(cd tools/e2esshd && go build -o /tmp/e2esshd . && /tmp/e2esshd 2222 &)
python3 tools/headless_drive.py "$(mktemp -d)" tools/e2e/tray_e2e.txt
```

`e2e/mcp_e2e.py` is the Python harness for the MCP server and stdio bridge; it
also expects the loopback SSH fixture on port 2222 and a built `/tmp/motty-head`.
`headless_drive.py` sets an explicit `MOTTY_DATA_DIR` under the temporary HOME
because headless startup refuses the default directory. That file isolation is
not system-secret isolation and does not make real credentials safe to test.

`e2e/portable_e2e.py` runs a portable copy: the headless build is copied beside a
`motty-data` folder and started without `MOTTY_DATA_DIR`. It checks that the data
stays in that folder, that a password can be saved at once with its key in a
`vault.key` file there, and that a restart signs in with it. Set `MOTTY_E2E_PORT` when the loopback SSH fixture is not on 2222.

`e2e/run_agent_mcp_e2e.sh` runs everything for `motty mcp-serve` (the MCP server
that needs no window) by itself: it builds, starts a fixture, drives the window side
(`agent_mcp_setup_e2e.txt`, `agent_mcp_activity_e2e.txt`) and the real `motty
mcp-serve` over stdio (`agent_mcp_e2e.py`), with its own HOME and data directory.

Fixtures:

- `e2esshd/` — loopback-only SSH/SFTP/SCP fixture with its own Go module.
- `fakes3/` — loopback S3 endpoint for the cloud-sync scripts.

## Live servers

`live/` runs the RFB and RDP tests against real servers rather than fixtures:

- `live/vnc_live.sh` — TigerVNC in Docker, or KasmVNC on the shared Linux test
  machine.
- `live/rdp_live.sh` — xrdp in Docker, or the Windows test machine.
- `live/vnc/` and `live/rdp/` — container contexts and helper scripts used by the
  two commands.

Docker builds these contexts; the scripts remove their containers and images when
they finish.
