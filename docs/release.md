# Releasing Motty

A release is a version tag. The commit the tag points at carries the version bump
and the release notes in `docs/releases/<version>.md`; pushing `v<version>` builds
every package, signs what it has the secrets to sign, verifies the finished
matrix, and publishes it. The tag is the approval — the notes were read in the
commit, and the workflow checks the artifacts before anything goes public.

```sh
# 1. bump viva.toml and internal/update/update.go, write
#    docs/releases/<version>.md, commit and push
# 2. tag exactly v<version> and push the tag
git tag v0.1.3 && git push origin v0.1.3
# 3. when the workflow is green the packages are on the mirror, the release is
#    public and the update feed names it
```

`tools/release.sh` refuses a tag that is not `v` plus the version in `viva.toml`,
and the draft job refuses a tag whose commit has no notes file.

## What each job does

| Job | Runs on | Does |
| --- | --- | --- |
| Pin viva | Ubuntu | resolves `vivaui/viva` main to one commit |
| Draft release | Ubuntu | creates the draft with the notes from `docs/releases/<version>.md` |
| macOS | macOS 15 (arm64) | `.dmg` and `.zip` for arm64 and amd64; signed, notarized and stapled |
| Linux and Windows | Ubuntu | `.tar.gz` and `.deb` (amd64, arm64), and an NSIS installer for Windows amd64 and arm64; the Windows programs and the installers signed |
| Checksums | Ubuntu | signs every package of the release, writes `SIGNATURES` and `SHA256SUMS`, and verifies the whole release |
| Publish | Ubuntu | copies the packages to the mirror, publishes the verified draft, then flips `latest.json` |

The *Publish* job runs in that order on purpose: the bytes go to the mirror while
the release is still a draft, the release page goes public only once they are
confirmed there, and `latest.json` — the file an installed copy updates from —
changes last. Nothing public ever names a file the mirror does not hold, and a
release that fails at any step before the last leaves the update feed on the
previous version.

The two build jobs run at the same time and upload to the same draft. Re-running
a failed job replaces its files. The *Checksums* job is the only one that sees
the whole matrix, so it is where the packages are signed into one `SIGNATURES`
and where `SHA256SUMS` is written: a build job that signed only its own platform
would have its manifest replaced by the other job's upload. The Linux packages
are not code-signed.

## What the gate checks

The tag is a promise that the artifacts are right, so the *Checksums* job proves
it before *Publish* runs. `tools/release.sh --sums TAG --expect-viva SHA` stops
the release unless

- the draft holds exactly the twelve assets of the matrix and nothing else;
- `SHA256SUMS` lists every package and matches the uploaded bytes;
- `SIGNATURES` has one line per package and each signature verifies against the
  key compiled into the app, bound to that file name and version;
- the title is `Motty <tag>` and the notes name the Viva commit that was built;
- the release is still a draft (a published release is never rewritten).

`--verify TAG --expect-viva SHA` runs the same checks on a release without
signing or uploading anything, and `--check-dir DIR --version V` runs the file
checks on a build directory. What the gate cannot see is the app itself: tests
and, when a change touches windows, input or updates, a headless run are still
what keeps a release worth publishing.

If a job fails, the draft stays and nothing is published. Fix the cause, delete
the draft and the tag, and tag again; once the *Publish* job has published the
release a fix is a new patch version, not an edit. The one step with a different
recovery is *Announce*: if the copy and the publish went through but `latest.json`
did not, the release is public on the previous feed — run *Actions → Mirror
release → Run workflow* with the tag to finish it without touching the release.

## The release notes

`docs/releases/<version>.md` is the text of the GitHub release, in the same
commit as the version bump and the tag. The draft job copies it in, and the
checksums job appends the `Built against vivaui/viva <sha>.` line, so the file
itself never carries that line. Write it for the people who read the update
dialog — what they can do now that they could not before — in Chinese, with the
`## 新功能 / 改进 / 修复 / 安装` sections the earlier releases use. The notes in
the update dialog are the `notes` field of `latest.json`, copied from this body.

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
0001 (`.private/decisions/0001-updater-signing.md`, not in the repository).

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

A by-hand run only ever leaves a draft. Sign and verify it with
`tools/release.sh --sums v<version> --expect-viva <sha>` when every platform is
uploaded, and check a release at any time — including one that shipped — with
`tools/release.sh --verify v<version> --expect-viva <sha>`.

On a Mac that is signed in to a keychain with the certificate, `APPLE_SIGNING_IDENTITY`
and a profile made once with `xcrun notarytool store-credentials motty` are enough;
no `.p12` is needed. See `viva help package` for every variable.

## The changelog page

`website/lib/changelog.ts` holds one entry per release, newest first, with the
English and Chinese text side by side. The website's `/en/changelog` and
`/zh/changelog` render it, and the nav links to it. Once a release is published,
add its entry there with the publish date and push: the site rebuilds and the
entry goes live. The entry is the release notes written for readers, in both
languages, so keep it to what a user notices.

The release title carries the tag as-is (`Motty v0.1.2`, not `Motty 0.1.2`), so
the version on GitHub reads the same as in the app and in the changelog.

## The Cloudflare mirror

The *Publish* job is what puts a release on the `motty` R2 bucket on Cloudflare,
in three steps: `--copy` uploads every asset while the release is still a draft,
then the draft is published, then `--announce` writes `latest.json`. Splitting it
that way is what keeps the public release page, the website's download links and
the update feed from ever pointing at a file the mirror does not hold yet, and it
keeps the feed — the one thing installed copies act on — as the last thing that
changes. `--announce` also refuses unless the copied bytes are on the mirror: the
versioned URL has to answer with the size the file has locally.

The bucket answers on `https://dl.motty.page`; the GitHub release stays the source
of record and the copy is only a faster path to the same bytes. `.github/workflows/mirror.yml`
is the repair path, not the trigger: *Actions → Mirror release → Run workflow*
with the tag does the copy and the announce again for a release that is already
public.

`tools/mirror-release.sh TAG` does the work and can be run locally:

```sh
tools/mirror-release.sh v0.1.2                          # copy and announce (repair)
tools/mirror-release.sh v0.1.2 --copy                   # copy only, feed untouched
tools/mirror-release.sh v0.1.2 --announce               # feed only, refuses a missing copy
tools/mirror-release.sh v0.1.2 --dir build/release-0.1.2   # work on what is already there
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
do not match it; `--announce` in turn refuses while any package is missing from
the bucket, so the feed cannot name bytes that were never uploaded.

The mirror steps need `CLOUDFLARE_API_TOKEN` with *Workers R2 Storage: Edit* (the
Pages token from docs/website.md covers the site only) and `CLOUDFLARE_ACCOUNT_ID`
(the same value the website workflow uses).

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
