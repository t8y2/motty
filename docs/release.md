# Releasing Motty

A release is a version tag. Pushing `v0.1.2` runs `.github/workflows/release.yml`,
which builds every package, signs what it has the secrets to sign, and uploads
everything to a **draft** release. You read the draft and publish it.

```sh
# 1. bump version in viva.toml, commit, push the branch
# 2. tag exactly v<version> and push the tag
git tag v0.1.2 && git push origin v0.1.2
# 3. when the workflow is green, review the draft on GitHub and publish it
gh release edit v0.1.2 --draft=false
```

`tools/release.sh` refuses a tag that is not `v` plus the version in `viva.toml`.

## What each job does

| Job | Runs on | Builds |
| --- | --- | --- |
| Pin viva | Ubuntu | resolves `vivaui/viva` main to one commit |
| Draft release | Ubuntu | creates the draft if there is none |
| macOS | macOS 15 (arm64) | `.dmg` and `.zip` for arm64 and amd64; signed, notarized and stapled |
| Linux and Windows | Ubuntu | `.tar.gz` and `.deb` (amd64, arm64), and an NSIS installer for Windows amd64 and arm64; the Windows programs and the installers signed |
| Checksums | Ubuntu | signs every package of the release and writes `SIGNATURES` and `SHA256SUMS` |

The two build jobs run at the same time and upload to the same draft. Re-running
a failed job replaces its files. The *Checksums* job is the only one that sees
the whole matrix, so it is where the packages are signed into one `SIGNATURES`
and where `SHA256SUMS` is written: a build job that signed only its own platform
would have its manifest replaced by the other job's upload. The Linux packages
are not code-signed.

## The framework commit

Motty is built against the sibling Viva checkout, so a release has to name which
frame it was built on. The *Pin viva* job runs first and asks GitHub for
`vivaui/viva` main's commit; both build jobs then check out that exact commit and
pass it to `tools/release.sh --viva-ref`, and the checksums job writes a
`Built against vivaui/viva <sha>.` line into the release notes. Main's tip at the
start of the release is what gets used: a push to Viva while the release runs
cannot hand the Mac job and the Linux job different frames, and the commit stays
readable from the release afterwards.

A local run uses whatever `../viva` holds unless told otherwise, so pass the same
commit to compare with CI: `tools/release.sh --platforms linux --viva-ref <sha>`.

The macOS runner's architecture does not decide the output architecture: the
release script builds both arm64 and amd64, and Viva cross-builds the cgo/Metal
bridge for the other one. The deployment target is the `[mac] min_system` in
`viva.toml` (default 12.0), not the runner's macOS version. `viva` has a test that
builds a cgo program for the other Mac architecture and checks its Mach-O slice.

## Secrets

The platform certificates are optional: a platform without its secrets is built
unsigned and the log says so in a warning. The update signing key is not — a
release is not finalized without it. Set them in the repository's *Settings >
Secrets and variables > Actions*, or with `gh secret set NAME`.

**Update signing**

| Secret | What |
| --- | --- |
| `MOTTY_UPDATE_SIGNING_KEY` | the contents of the file `tools/updatesign generate` writes (for example `~/.motty/updater-signing.key`), a minisign private key. The matching public key is compiled into `internal/updatesign` |

Keep the private key in a password manager or on an offline medium as well as in
the secret. It is the trust root for every 0.1.2-or-later installation: if it is
lost, those installations can no longer update in place. Rotating it is a
migration — the release that carries the new public key has to be signed with the
old key — not a quiet secret replacement. The decision is
[0001](decisions/0001-updater-signing.md).

**macOS**

| Secret | What |
| --- | --- |
| `APPLE_CERTIFICATE` | the *Developer ID Application* certificate **with its private key**, exported from Keychain Access as a `.p12`, then `base64 -i certificate.p12` |
| `APPLE_CERTIFICATE_PASSWORD` | the password you gave the `.p12` when exporting |
| `APPLE_ID`, `APPLE_PASSWORD`, `APPLE_TEAM_ID` | your Apple ID, an app-specific password for it (account.apple.com > Sign-In and Security) and the team ID (the `(TEAMID)` in the identity's name) |
| `APPLE_API_KEY_P8`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER` | instead of the three above: an App Store Connect API key (the contents of the `.p8`, its key ID and the issuer ID). Easier to revoke on its own |

The certificate is imported into a keychain created for the job and deleted with
it; no other keychain is touched. A certificate without notarization credentials
fails the job rather than shipping an image Gatekeeper would block.

**Windows**

| Secret | What |
| --- | --- |
| `WINDOWS_CERTIFICATE`, `WINDOWS_CERTIFICATE_PASSWORD` | a code signing certificate as a `.pfx` (`base64 -i code-signing.pfx`) and its password |
| `WINDOWS_SIGN_COMMAND` | instead: a command that signs `{file}` in place, for a key that lives in a token or a service such as Azure Trusted Signing. It runs on the Ubuntu runner, which has no `signtool`, so it must be something that runs there |

Current certificates mostly keep the key where a file cannot reach it; check what
your certificate authority issues before buying one. Packages are timestamped by
DigiCert's server unless `WINDOWS_TIMESTAMP_URL` says otherwise.

**Viva**

Motty builds against the `main` branch of `vivaui/viva`, checked out beside it. If that
repository is private, add `REED_REPO_TOKEN`, a token that can read it. Because
`main` moves, a rebuild of an old tag can differ from the original build; tag only
when Viva's `main` is what you want to ship.

## Building by hand

```sh
tools/release.sh                       # everything this machine can build, in build/release-<version>
tools/release.sh --platforms linux,windows --out /tmp/out
APPLE_SIGNING_IDENTITY="Developer ID Application: Name (TEAMID)" \
APPLE_NOTARY_PROFILE=motty tools/release.sh --platforms mac
```

On a Mac that is signed in to a keychain with the certificate, `APPLE_SIGNING_IDENTITY`
and a profile made once with `xcrun notarytool store-credentials motty` are enough;
no `.p12` is needed. See `viva help package` for every variable.

## The changelog page

`website/lib/changelog.ts` holds one entry per release, newest first, with the
English and Chinese text side by side. The website's `/en/changelog` and
`/zh/changelog` render it, and the nav links to it. When a release is published,
add its entry there and push: the site rebuilds and the entry goes live. The
entry is the release notes written for readers, not the generated changelog, so
keep it to what a user notices.

The release title carries the tag as-is (`Motty v0.1.2`, not `Motty 0.1.2`), so
the version on GitHub reads the same as in the app and in the changelog.

## The Cloudflare mirror

Publishing the draft (`gh release edit v0.1.2 --draft=false`) also triggers
`.github/workflows/mirror.yml`, which copies every asset to the `motty` R2 bucket
on Cloudflare. The bucket answers on `https://dl.motty.page`; the GitHub release
stays the source of record and the copy is only a faster path to the same bytes.

`tools/mirror-release.sh TAG` does the work and can be run locally:

```sh
tools/mirror-release.sh v0.1.2              # downloads the release with gh, then uploads
tools/mirror-release.sh v0.1.2 --dir build/release-0.1.2   # upload what is already there
```

Object layout:

| Key | Cache | What |
| --- | --- | --- |
| `latest.json` | five minutes | version, tag, and every file's name, size and SHA-256; the only file at the bucket root besides `robots.txt` |
| `releases/<tag>/<asset>` | a year, immutable | the exact release asset |
| `releases/latest/<stable>` | five minutes | the same bytes, version out of the name (`motty-0.1.2-darwin-arm64.dmg` → `motty-darwin-arm64.dmg`) |

Everything a release is made of lives under `releases/`; the root only carries
the feed and `robots.txt`. `releases/latest` is not a tag: it is the projection
of `latest.json`, and the uploader deletes the alias of a name the new release no
longer ships, so an old format cannot keep serving stale bytes.

The website's download page links at `/releases/latest/<stable>`, so it needs no
rebuild per version; `latest.json` is what a script should read. The page also
lists only what `latest.json` holds, so adding or dropping a package format with
a release shows up on the next site build without editing the page. The uploader
runs `sha256sum -c SHA256SUMS` first and refuses to mirror a release whose files
do not match it.

The workflow needs `CLOUDFLARE_API_TOKEN` with *Workers R2 Storage: Edit* (the
Pages token from docs/website.md covers the site only) and `CLOUDFLARE_ACCOUNT_ID`
(the same value the website workflow uses). Re-run a mirror from *Actions →
Mirror release → Run workflow* with the tag.

## In-app updates

The app updates itself from the mirror: it reads `https://dl.motty.page/latest.json`
(GitHub's latest release is only the fallback, and carries no checksums, so it can
send the user to the page but cannot install). The release notes in the update
dialog are the `notes` field of that file, which `tools/mirror-release.sh` copies
from the GitHub release body.

Every package carries a minisign signature from the release key, and the feed
copies it into the `signature` field of that file. The signature's signed comment
names the version and the file, so verification holds the bytes, the version the
feed announced and the file the app resolved together; a changed feed cannot pass
an older package off as a newer one. The app verifies this before it replaces
anything and refuses a release without a signature.

What the app downloads is chosen by name, so these must stay as they are:

- the stable asset names in `update.StableName` (`motty-darwin-<arch>.zip`,
  `motty-linux-<arch>.tar.gz`, `motty-windows-amd64-setup.exe`) match what
  `viva package` produces, after the mirror takes the version out;
- `latest.json` keeps `version`, `tag` (`v` plus the version) and, per file, `name`,
  `size`, `sha256` and `signature`;
- the release carries `SIGNATURES`, one `name<TAB>base64` line per package, and
  the mirror only folds a signature in for a file it names.

The operating system check stays as a second layer: the program in the macOS zip
must be signed with the same Developer ID team as the copy it replaces, and a
Windows installer that is signed must have the same subject as a signed running
copy. An unsigned Windows installer is accepted on the strength of the update
signature alone, which is what lets Windows update without a code-signing
certificate.

Only a user-owned installation replaces itself (an app bundle in a writable folder,
the per-user Windows installer, a Linux `install.sh` prefix). An installation the
system owns — a distribution package, or a program under `Program Files` — is never
replaced by the app: the dialog says to update it with the package manager or to run
the new installer, and a portable copy is sent to the release page. No update path
asks for administrator rights.

The replacement is made by a helper that outlives the app, so it never touches a
file the running app holds. On macOS the helper is a copy of the whole bundle,
not of the executable: a Developer ID signature covers the bundle's `Info.plist`,
and the executable on its own no longer verifies, so the system kills it as it
starts (exit 137, `amfid` logs `Code=-420`) and nothing is replaced. The app
watches the helper for a moment after starting it and, if it dies at once, shows
the failure instead of quitting. A development build signed with `--identifier`
alone does not bind the `Info.plist`, so it cannot show this failure.

## The package matrix

Every release carries the packages below for every platform and architecture:

| Platform | arm64 | amd64 |
| --- | --- | --- |
| macOS | `.dmg`, `.zip` | `.dmg`, `.zip` |
| Windows | `.exe` installer | `.exe` installer |
| Linux | `.deb`, `.tar.gz` | `.deb`, `.tar.gz` |

The macOS `.zip` holds the same signed bundle; it is what the
in-app updater downloads, so a release without it would leave macOS unable to
update in place. `viva package` staples the ticket to the `.dmg`, not to a
bundle it also zips, so the zip's first launch checks the signature online.

A signed release also carries `SIGNATURES`, the minisign signature of every
package. That is 12 release assets: the packages above, `SHA256SUMS` and
`SIGNATURES`.

`tools/release.sh` refuses to upload when one of those is missing, so a run with
a broken makensis stops instead of publishing a release with a hole in it. The
installers are built on Linux; makensis on Apple Silicon crashes (NSIS issue
#1333), which is why `--platforms windows` on a Mac only warns.

## What has been checked

The macOS signing, notarization and stapling were run for real, with a Developer ID
and Apple's notary service, on a throwaway app: Gatekeeper reports *Notarized
Developer ID* for both the `.dmg` and the stapled `.app`. The Windows signing was run
for real with `osslsigncode` and a self-signed certificate, including the
installer and a DigiCert timestamp, and the signed installer installed. Not run: the
workflow itself on GitHub, the import of a `.p12` for a certificate Apple trusts,
`signtool`, and a certificate from a Windows certificate authority.

The update signature was checked against a real build: `tools/release.sh
--platforms linux` signed a release directory with the key, `tools/updatesign
verify` accepted every package, and a changed byte, another version and another
file name were each refused. Not run: the signing step on GitHub's runners, a
Windows installation replacing itself in place, and the update dialog against a
signed feed.
