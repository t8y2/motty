#!/bin/bash
# Build the release packages of Motty from a clean checkout, checksum them, and
# optionally upload them to a GitHub release.
#
#   tools/release.sh [--ref REF] [--out DIR] [--platforms mac,linux,windows]
#                    [--installer FILE] [--viva-ref REF]
#                    [--upload TAG [--notes FILE]]
#   tools/release.sh --sums TAG --expect-viva SHA
#   tools/release.sh --verify TAG --expect-viva SHA
#   tools/release.sh --check-dir DIR --version V [--pubkey KEY]
#
# --sums is the last step of a release, and the gate in front of publishing it:
# it signs the whole matrix, writes SHA256SUMS, and then refuses the release
# unless it carries exactly the fourteen assets of the matrix, SHA256SUMS matches
# the files, every signature verifies against the key compiled into the app, the
# title is "Motty <tag>", the release is still a draft, and its notes name the
# expected Viva commit. --verify runs the same checks on a release without
# touching it, which is what the workflow runs before it publishes. --check-dir
# runs the file checks on a directory, for tests and for looking at a build
# before it is uploaded.
#
# The packages come from REF (default HEAD), built in a clean temporary clone so
# uncommitted changes never leak in, with the sibling ../viva at its main branch
# (--viva-ref picks another ref; the release workflow passes the exact commit it
# resolved once, so every package of a release uses the same frame).
#
# --platforms picks what to build; by default everything this machine can: the
# macOS packages (.dmg and .zip) need Apple's tools and so a Mac, the Windows
# ones are cross-built anywhere. A release made on several machines, as
# .github/workflows/release.yml does, builds one platform on each.
#
# The Windows packages are NSIS installers, one per architecture, and NSIS
# (makensis) must be able to run. The arm64 installer needs makensis 3.10 or
# newer; if makensis is missing or crashes, that architecture is skipped with a
# note, and a prebuilt amd64 installer can be passed with --installer.
#
# --upload TAG puts the packages on release TAG of the repository's origin; TAG
# must be v plus the version in viva.toml, and the run refuses to upload unless
# every package of the platforms it built is there. A release that does not exist
# yet is created as a DRAFT pointing at the commit that was built, with the notes
# from --notes. The commit must already be pushed. SHA256SUMS is written when all
# three platforms were built in this run; otherwise run --sums TAG once they are
# all uploaded. Nothing published from here: publishing belongs to the workflow,
# after --sums has verified the release.
#
# The packages of a release are signed with the update key in
# MOTTY_UPDATE_SIGNING_KEY (the minisign private key itself, not a path), and the
# signatures are uploaded as SIGNATURES. The signatures for a whole release are
# written by --sums, not by the jobs that build it: a job sees only its own
# platform's packages, so a manifest it uploaded would be replaced by the next
# job's and leave that platform without a signature. Finalizing without the key
# is refused: a release nobody can update in place is a release with a broken
# update path.
#
# Signing is optional and comes from the environment, which viva package reads:
# with APPLE_SIGNING_IDENTITY (or APPLE_CERTIFICATE and APPLE_CERTIFICATE_PASSWORD)
# the macOS bundles and images are signed, and with notarytool credentials as well
# (APPLE_NOTARY_PROFILE, or APPLE_API_KEY/_KEY_ID/_ISSUER, or APPLE_ID/
# APPLE_PASSWORD/APPLE_TEAM_ID) the .dmg is notarized and stapled; with
# WINDOWS_CERTIFICATE_FILE (or WINDOWS_CERTIFICATE) and WINDOWS_CERTIFICATE_PASSWORD,
# or WINDOWS_SIGN_COMMAND, the Windows programs and the installer are signed. See
# `viva help package` and docs/release.md. Without them nothing is signed. The
# Linux packages are never signed.
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
ref=HEAD viva_ref=main out="" installer="" tag="" notes="" sums_tag="" platforms=""
verify_tag="" check_dir="" check_version="" pubkey="" expect_viva=""
while [ $# -gt 0 ]; do
	case "$1" in
	--ref) ref=$2; shift 2 ;;
	--viva-ref) viva_ref=$2; shift 2 ;;
	--out) out=$2; shift 2 ;;
	--platforms) platforms=$2; shift 2 ;;
	--installer) installer=$2; shift 2 ;;
	--upload) tag=$2; shift 2 ;;
	--notes) notes=$2; shift 2 ;;
	--sums) sums_tag=$2; shift 2 ;;
	--verify) verify_tag=$2; shift 2 ;;
	--check-dir) check_dir=$2; shift 2 ;;
	--version) check_version=$2; shift 2 ;;
	--pubkey) pubkey=$2; shift 2 ;;
	--expect-viva) expect_viva=$2; shift 2 ;;
	-h|--help) sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

sums() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi; }
sums_packages() { if [ -f SIGNATURES ]; then sums motty* SIGNATURES; else sums motty*; fi; }

manifest_version() { sed -n 's/^version *= *"\([^"]*\)".*/\1/p' "$here/viva.toml" | head -1; }

# The matrix of a release: twelve packages and two manifests. Keep this in one
# place; --sums verifies a release against it and the build jobs check the
# platforms they were asked for.
release_assets() {
	cat <<-EOF
	motty-$1-darwin-arm64.dmg
	motty-$1-darwin-amd64.dmg
	motty-$1-darwin-arm64.zip
	motty-$1-darwin-amd64.zip
	motty-$1-linux-arm64.tar.gz
	motty-$1-linux-amd64.tar.gz
	motty-$1-windows-arm64-setup.exe
	motty-$1-windows-amd64-setup.exe
	motty-$1-windows-arm64.zip
	motty-$1-windows-amd64.zip
	motty_$1_arm64.deb
	motty_$1_amd64.deb
	SHA256SUMS
	SIGNATURES
	EOF
}

# check_release_dir DIR VERSION [PUBKEY] refuses a directory that is not the
# whole matrix, whose SHA256SUMS does not match its files, or whose package
# signatures do not verify against the release key (or PUBKEY, for tests).
check_release_dir() {
	local dir=$1 version=$2 key=${3:-}
	local expected present name missing=0 extra=0
	expected=$(release_assets "$version" | sort)
	present=$(cd "$dir" && ls -1 | sort)
	if [ "$expected" != "$present" ]; then
		missing=$(comm -23 <(printf '%s\n' "$expected") <(printf '%s\n' "$present"))
		extra=$(comm -13 <(printf '%s\n' "$expected") <(printf '%s\n' "$present"))
		[ -z "$missing" ] || echo "   missing from the release: $(echo $missing)" >&2
		[ -z "$extra" ] || echo "   not part of the release matrix: $(echo $extra)" >&2
		return 1
	fi
	# SHA256SUMS must cover every package and SIGNATURES, not just some of them.
	local listed
	listed=$(awk '{print $2}' "$dir/SHA256SUMS" | sort)
	if [ "$listed" != "$(printf '%s\n' "$expected" | grep -v '^SHA256SUMS$' | sort)" ]; then
		echo "   SHA256SUMS does not list exactly the packages of the release" >&2
		return 1
	fi
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$dir" && sha256sum -c --quiet SHA256SUMS) || { echo "   SHA256SUMS does not match the files" >&2; return 1; }
	else
		(cd "$dir" && shasum -a 256 -c SHA256SUMS >/dev/null) || { echo "   SHA256SUMS does not match the files" >&2; return 1; }
	fi
	local packages sigs
	# One signature per package, never per manifest: the count comes from the
	# matrix so a new package format cannot leave this check behind.
	packages=$(printf '%s\n' "$expected" | awk '$0 != "SHA256SUMS" && $0 != "SIGNATURES" {n++} END {print n+0}')
	sigs=$(wc -l <"$dir/SIGNATURES" | tr -d ' ')
	[ "$sigs" = "$packages" ] || { echo "   SIGNATURES has $sigs entries, not one per package ($packages)" >&2; return 1; }
	local args=(-version "$version" -dir "$dir")
	[ -z "$key" ] || args+=(-pubkey "$key")
	(cd "$here" && go run ./tools/updatesign verify "${args[@]}") || {
		echo "   a package signature does not verify against the release key" >&2
		return 1
	}
	echo "   $dir is the complete, signed matrix of $version"
}

# check_release_body TAG EXPECT_VIVA refuses a release whose title or notes are
# not what the workflow promises to publish.
check_release_body() {
	local tag=$1 want_viva=$2 body built
	if [ "$tag" != "v$(manifest_version)" ]; then
		echo "the release tag is $tag but viva.toml says version $(manifest_version): the tag must be v$(manifest_version)" >&2
		return 1
	fi
	body=$(gh release view "$tag" --json body --jq .body)
	if [ "$(gh release view "$tag" --json name --jq .name)" != "Motty $tag" ]; then
		echo "the release title is not \"Motty $tag\"" >&2
		return 1
	fi
	built=$(printf '%s\n' "$body" | sed -n 's/^Built against vivaui\/viva `\([0-9a-f]*\)`\.$/\1/p' | wc -l | tr -d ' ')
	if [ "$built" != 1 ]; then
		echo "the notes name $built Viva commits; exactly one \"Built against vivaui/viva <sha>.\" line belongs there" >&2
		return 1
	fi
	if [ "$(printf '%s\n' "$body" | sed -n 's/^Built against vivaui\/viva `\([0-9a-f]*\)`\.$/\1/p')" != "$want_viva" ]; then
		echo "the notes say this release was built against $(printf '%s\n' "$body" | sed -n 's/^Built against vivaui\/viva `\([0-9a-f]*\)`\.$/\1/p'), not $want_viva" >&2
		return 1
	fi
}

# A run that is going to upload must produce the whole matrix: a missing package
# would otherwise be published as a release nobody can install from.
strict=0
[ -z "$tag" ] || strict=1

if [ -n "$check_dir" ]; then
	[ -n "$check_version" ] || { echo "--check-dir needs --version" >&2; exit 2; }
	check_release_dir "$check_dir" "$check_version" "$pubkey"
	exit 0
fi

if [ -n "$verify_tag" ]; then
	[ -n "$expect_viva" ] || { echo "--verify needs --expect-viva <sha>: the notes and the build must name the same frame" >&2; exit 2; }
	version=$(manifest_version)
	[ -n "$version" ] || { echo "no version in viva.toml" >&2; exit 1; }
	# The body is cheap to read and the assets are not: refuse a release whose
	# notes or title are wrong before downloading the matrix.
	check_release_body "$verify_tag" "$expect_viva"
	scratch=$(mktemp -d)
	trap 'rm -rf "$scratch"' EXIT
	gh release download "$verify_tag" --dir "$scratch"
	check_release_dir "$scratch" "$version"
	if [ "$(gh release view "$verify_tag" --json isDraft --jq .isDraft)" = true ]; then
		echo "== draft $verify_tag verified; publishing it is the last step"
	else
		echo "== published $verify_tag verified against the matrix"
	fi
	exit 0
fi

if [ -n "$sums_tag" ]; then
	[ -n "$expect_viva" ] || { echo "--sums needs --expect-viva <sha>: the notes and the build must name the same frame" >&2; exit 2; }
	version=$(manifest_version)
	[ -n "$version" ] || { echo "no version in viva.toml" >&2; exit 1; }
	# A published release is final: its assets are what installed copies already
	# downloaded, and rewriting them would break the signatures the feed carries.
	[ "$(gh release view "$sums_tag" --json isDraft --jq .isDraft)" = true ] || {
		echo "$sums_tag is already published: a published release is never rewritten" >&2
		exit 1
	}
	scratch=$(mktemp -d)
	workdir=$(mktemp -d)
	trap 'rm -rf "$scratch" "$workdir"' EXIT
	gh release download "$sums_tag" --dir "$scratch" --pattern 'motty*'
	# Sign here, once, with the whole matrix in hand: a build job only sees the
	# packages of its own platform, so a manifest written there is overwritten by
	# the next job's upload and leaves that platform out of the release.
	[ -n "${MOTTY_UPDATE_SIGNING_KEY:-}" ] || {
		echo "MOTTY_UPDATE_SIGNING_KEY is not set: refusing to finalize a release that cannot be updated in place" >&2
		exit 1
	}
	key=$workdir/update.key
	(umask 077 && printf '%s\n' "$MOTTY_UPDATE_SIGNING_KEY" >"$key")
	(cd "$here" && go run ./tools/updatesign sign -key "$key" -version "$version" -dir "$scratch")
	(cd "$scratch" && sums_packages > SHA256SUMS && cat SHA256SUMS)
	# Nothing is uploaded until the whole thing is provably the matrix we mean
	# to publish: a half-signed or wrongly named release stays a draft.
	check_release_body "$sums_tag" "$expect_viva"
	check_release_dir "$scratch" "$version"
	gh release upload "$sums_tag" --clobber "$scratch/SIGNATURES" "$scratch/SHA256SUMS"
	exit 0
fi

if [ -z "$platforms" ]; then
	if [ "$(uname -s)" = Darwin ]; then platforms=mac,linux,windows; else platforms=linux,windows; fi
fi
want() { case ",$platforms," in *",$1,"*) return 0 ;; *) return 1 ;; esac; }
for platform in ${platforms//,/ }; do
	case "$platform" in
	mac|linux|windows) ;;
	*) echo "unknown platform: $platform (mac, linux, windows)" >&2; exit 2 ;;
	esac
done
if want mac && [ "$(uname -s)" != Darwin ]; then
	echo "the macOS packages are built on macOS; this is $(uname -s). Use --platforms linux,windows here" >&2
	exit 1
fi
viva_repo=$(cd "$here/../viva" 2>/dev/null && pwd) || { echo "the Viva checkout is expected at ../viva" >&2; exit 1; }

commit=$(git -C "$here" rev-parse --verify "$ref^{commit}")
commit_time=$(git -C "$here" show -s --format=%cI "$commit")
short=$(git -C "$here" rev-parse --short "$commit")
# The version of the commit being packaged, not of the working tree: --ref may
# name another commit while viva.toml is dirty or ahead.
version=$(git -C "$here" show "$commit:viva.toml" 2>/dev/null | sed -n 's/^version *= *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$version" ] || { echo "no version in viva.toml at $short" >&2; exit 1; }
if [ -n "$tag" ] && [ "$tag" != "v$version" ]; then
	echo "the release tag is $tag but viva.toml says version $version: the tag must be v$version" >&2
	exit 1
fi
out=${out:-$here/build/release-$version}
mkdir -p "$out"
out=$(cd "$out" && pwd)
[ -z "$installer" ] || [ -f "$installer" ] || { echo "no such installer: $installer" >&2; exit 1; }
if [ -n "$installer" ] && [ -n "$tag" ]; then
	echo "--installer cannot be uploaded: a release asset must be built and verified here" >&2
	exit 1
fi

work=$(mktemp -d)
cleanup() {
	git -C "$viva_repo" worktree remove --force "$work/viva" 2>/dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

echo "== Motty $version from $short ($platforms)"
git -C "$viva_repo" worktree add -q --detach "$work/viva" "$viva_ref"
# Go 1.26 does not discover a linked worktree's .git file when stamping VCS
# information. A local shared clone has a real .git directory and the same
# committed source, without downloading or copying the object database.
git clone -q --shared --no-checkout "$here" "$work/motty"
git -C "$work/motty" checkout -q --detach "$commit"
(cd "$work/viva" && go build -o "$work/viva-cli" ./cmd/viva)

cd "$work/motty"
# The Windows local terminal loads the bundled ConPTY pair from conpty/<arch>.
# The same files go into the installer and the portable zip; OpenConsole.exe
# matches the kernel architecture, so an x64 build also carries the arm64 host
# for Windows on ARM.
windows_conpty_files() {
	case "$1" in
	amd64) cat <<-'EOF'
	internal/localshell/conpty/LICENSE.txt=conpty/LICENSE.txt
	internal/localshell/conpty/x64/arm64/OpenConsole.exe=conpty/x64/arm64/OpenConsole.exe
	internal/localshell/conpty/x64/conpty.dll=conpty/x64/conpty.dll
	internal/localshell/conpty/x64/x64/OpenConsole.exe=conpty/x64/x64/OpenConsole.exe
	EOF
	;;
	arm64) cat <<-'EOF'
	internal/localshell/conpty/LICENSE.txt=conpty/LICENSE.txt
	internal/localshell/conpty/arm64/arm64/OpenConsole.exe=conpty/arm64/arm64/OpenConsole.exe
	internal/localshell/conpty/arm64/conpty.dll=conpty/arm64/conpty.dll
	EOF
	;;
	esac
}
build_package() {
	local target=$1 arch=$2 program
	shift 2
	program="$work/program-$target-$arch/motty"
	[ "$target" != windows ] || program="$program.exe"
	# Build once, check the executable itself (including foreign targets), then
	# package those same bytes. Explicit returns also work inside the Windows
	# conditional below, where bash suppresses errexit inside called functions.
	"$work/viva-cli" build --target "$target" --arch "$arch" -o "$program" || return
	# Package uses a supplied program as it is, signature included, so the
	# Windows executable is signed here, before the check reads its bytes.
	[ "$target" != windows ] || "$work/viva-cli" sign --target windows --program "$program" || return
	go run ./tools/checkbuild -program "$program" -revision "$commit" -time "$commit_time" -target "$target" -arch "$arch" || return
	local -a conpty=() portable=()
	if [ "$target" = windows ]; then
		local entry
		while IFS= read -r entry; do
			conpty+=(--file "$entry")
			portable+=(-file "$entry")
		done < <(windows_conpty_files "$arch")
		[ ${#conpty[@]} -gt 0 ] || { echo "   no conpty files for windows/$arch" >&2; return 1; }
	fi
	# The portable copy holds these same signed bytes (see internal/datadir).
	if [ "$target" = windows ]; then
		go run ./tools/portablezip -program "$program" -time "$commit_time" \
			-o "$out/motty-$version-windows-$arch.zip" "${portable[@]+"${portable[@]}"}" || return
		unzip -l "$out/motty-$version-windows-$arch.zip" > "$work/zip-list-$arch.txt" || return
		if ! grep -q ' conpty/' "$work/zip-list-$arch.txt"; then
			echo "   the portable zip is missing its conpty files" >&2
			return 1
		fi
	fi
	"$work/viva-cli" package --target "$target" --arch "$arch" --program "$program" "${conpty[@]+"${conpty[@]}"}" "$@"
}
package() { build_package "$@" 2>&1 | sed 's/^/   /'; }

if want mac; then
	echo "== macOS"
	for arch in arm64 amd64; do
		package darwin "$arch" -o "$work/mac-$arch/Motty.app" --format zip,dmg
		cp "$work/mac-$arch/"motty-*.zip "$work/mac-$arch/"motty-*.dmg "$out/"
	done
fi
if want linux; then
	echo "== Linux"
	for arch in amd64 arm64; do
		package linux "$arch" --format tar.gz,deb -o "$out"
	done
fi
if want windows; then
	echo "== Windows"
	for arch in amd64 arm64; do
		if [ -n "$installer" ] && [ "$arch" = amd64 ]; then
			cp "$installer" "$out/"
			echo "   Windows amd64 installer copied from $installer"
			# The zip is made from a program built here, whatever made the installer.
			package windows "$arch" --format zip -o "$work/unused-zip-$arch"
		elif package windows "$arch" --format nsis -o "$out" 2>&1 | tee "$work/nsis-$arch.log"; then
			echo "   Windows $arch installer built"
		else
			echo "   no Windows $arch installer: $(tail -1 "$work/nsis-$arch.log" | cut -c1-110)"
			echo "   (makensis needs to work; a prebuilt amd64 installer can be passed with --installer)"
			[ "$strict" != 1 ] || exit 1
		fi
	done
fi

rm -rf "$out/Motty.app"

if [ "$strict" = 1 ]; then
	missing=0
	have() { compgen -G "$out/$1" >/dev/null || { echo "   missing package: $1" >&2; missing=1; }; }
	if want mac; then
		have 'motty-*-darwin-arm64.dmg'
		have 'motty-*-darwin-amd64.dmg'
		# The zip is what the in-app updater downloads (update.StableName).
		have 'motty-*-darwin-arm64.zip'
		have 'motty-*-darwin-amd64.zip'
	fi
	if want linux; then
		have 'motty-*-linux-arm64.tar.gz'
		have 'motty-*-linux-amd64.tar.gz'
		have 'motty_*_arm64.deb'
		have 'motty_*_amd64.deb'
	fi
	if want windows; then
		have 'motty-*-windows-amd64-setup.exe'
		have 'motty-*-windows-arm64-setup.exe'
		have 'motty-*-windows-amd64.zip'
		have 'motty-*-windows-arm64.zip'
	fi
	[ "$missing" = 0 ] || { echo "refusing to upload an incomplete release" >&2; exit 1; }
fi

# The update signature is what lets a Windows build update itself without a
# code-signing certificate, and what binds a package to its version everywhere.
signing_key=
if [ -n "${MOTTY_UPDATE_SIGNING_KEY:-}" ]; then
	signing_key=$work/update.key
	(umask 077 && printf '%s\n' "$MOTTY_UPDATE_SIGNING_KEY" >"$signing_key")
elif [ -n "$tag" ]; then
	echo "MOTTY_UPDATE_SIGNING_KEY is not set: refusing to upload a release that cannot be updated in place" >&2
	exit 1
else
	echo "warning: MOTTY_UPDATE_SIGNING_KEY is not set; the packages will not be installable in place" >&2
fi
if [ -n "$signing_key" ]; then
	echo "== signing packages"
	(cd "$work/motty" && go run ./tools/updatesign sign -key "$signing_key" -version "$version" -dir "$out")
	(cd "$work/motty" && go run ./tools/updatesign verify -version "$version" -dir "$out")
fi

if want mac && want linux && want windows; then
	(cd "$out" && rm -f SHA256SUMS && sums_packages > SHA256SUMS)
else
	echo "   no SHA256SUMS: this run builds only $platforms; run --sums TAG when every platform is uploaded"
fi
echo "== packages in $out"
ls -l "$out" | awk 'NR>1 {printf "   %10d  %s\n", $5, $9}'

[ -n "$tag" ] || exit 0

# The packages were built in a clone whose origin is this checkout's path, not
# GitHub, so gh cannot tell which repository to talk to from there. The release
# belongs to the checkout that has the GitHub remote.
cd "$here"

echo "== release $tag"
assets=("$out"/motty*)
# Only a run that built every platform in one place uploads a manifest. The
# finalize step signs the whole matrix into one SIGNATURES; a per-platform one
# would be overwritten by the other build job and leave its packages unsigned.
if [ -f "$out/SHA256SUMS" ]; then
	assets+=("$out/SHA256SUMS")
	[ ! -f "$out/SIGNATURES" ] || assets+=("$out/SIGNATURES")
fi
if gh release view "$tag" >/dev/null 2>&1; then
	gh release upload "$tag" --clobber "${assets[@]}"
else
	if ! git -C "$here" branch -r --contains "$commit" | grep -q .; then
		echo "commit $short is not on any remote branch; push it first" >&2
		exit 1
	fi
	[ -n "$notes" ] || { echo "a release needs notes: pass --notes FILE (docs/releases/<version>.md)" >&2; exit 1; }
	gh release create "$tag" --draft --title "Motty $tag" --target "$commit" --notes-file "$notes" "${assets[@]}"
fi
echo "   still a draft: the workflow publishes it after --sums verified the release"
