#!/bin/sh
# Copies a GitHub release to the motty Cloudflare R2 bucket, which is served from
# https://dl.motty.page. GitHub Releases stays the source of record; this copy
# exists so a download does not have to cross a slow link.
#
#   releases/<tag>/<asset>    the exact release bytes, cached for a year
#   releases/latest/<stable>  the same bytes under a version-less name, five minute cache
#   latest.json               version, tag, and every file's size, digest and signature
#
# releases/latest is not a tag: it is the projection of latest.json, so a format
# a new release drops loses its alias here instead of serving old bytes forever.
#
# The work is two steps, so a release can copy its bytes before it is public and
# announce them after. --copy uploads the assets and confirms they are on the
# mirror; --announce writes latest.json, the file an installed copy updates from,
# and refuses unless those bytes are already there. Running neither flag does
# both, which is the manual re-mirror path for a release that is already public.
#
# Usage: tools/mirror-release.sh TAG [--dir DIR] [--copy | --announce]
#   TAG        release tag, v0.1.2
#   --dir DIR  work on what DIR already holds instead of downloading TAG
#   --copy     copy the assets to the bucket, leaving latest.json alone
#   --announce write latest.json, after checking the copy is complete
#
# Needs CLOUDFLARE_API_TOKEN (Workers R2 Storage: Edit) and CLOUDFLARE_ACCOUNT_ID
# in CI, or a local `wrangler login`. WRANGLER overrides the wrangler command.
set -eu

bucket=motty
mirror=https://dl.motty.page

tag=${1:-}
[ -n "$tag" ] || { echo "usage: tools/mirror-release.sh TAG [--dir DIR] [--copy | --announce]" >&2; exit 2; }
case $tag in v*) ;; *) echo "tag must look like v0.1.2, not $tag" >&2; exit 2 ;; esac
version=${tag#v}
version_re=$(printf '%s' "$version" | sed 's/\./\\./g')
shift

dir=
mode=all
while [ $# -gt 0 ]; do
	case $1 in
	--dir | -d)
		shift
		dir=${1:-}
		[ -n "$dir" ] || { echo "--dir needs a directory" >&2; exit 2; }
		[ -d "$dir" ] || { echo "no such directory: $dir" >&2; exit 2; }
		;;
	--copy)
		[ "$mode" = all ] || { echo "--copy and --announce are separate steps" >&2; exit 2; }
		mode=copy
		;;
	--announce)
		[ "$mode" = all ] || { echo "--copy and --announce are separate steps" >&2; exit 2; }
		mode=announce
		;;
	*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
	shift
done

scratch=
scratch_list=
upload_log=
previous_raw=
previous_names=
cleanup() {
	[ -z "$scratch_list" ] || rm -f "$scratch_list"
	[ -z "$upload_log" ] || rm -f "$upload_log"
	[ -z "$previous_raw" ] || rm -f "$previous_raw"
	[ -z "$previous_names" ] || rm -f "$previous_names"
	[ -z "$scratch" ] || rm -rf "$scratch"
}
trap cleanup EXIT INT TERM

if [ -z "$dir" ]; then
	scratch=$(mktemp -d)
	dir=$scratch
	echo "Downloading $tag"
	gh release download "$tag" --dir "$dir"
fi

wrangler() { ${WRANGLER:-npx --yes wrangler@4.152.0} "$@"; }

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

size_of() { wc -c <"$1" | tr -d ' '; }

verify_checksums() {
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$1" && sha256sum --check --quiet SHA256SUMS)
	else
		(cd "$1" && shasum -a 256 --check --quiet SHA256SUMS)
	fi
}

content_type() {
	case $1 in
	*.dmg) echo application/x-apple-diskimage ;;
	*.zip) echo application/zip ;;
	*.tar.gz) echo application/gzip ;;
	*.deb) echo application/vnd.debian.binary-package ;;
	*.exe) echo application/vnd.microsoft.portable-executable ;;
	*.json) echo application/json ;;
	*) echo 'text/plain; charset=utf-8' ;;
	esac
}

# motty-0.1.2-darwin-arm64.dmg -> motty-darwin-arm64.dmg
# motty_0.1.2_amd64.deb        -> motty_amd64.deb
stable_name() {
	printf '%s' "$1" | sed -E "s/-${version_re}-/-/; s/_${version_re}_/_/"
}

# Uploads over a long haul fail and recover on their own; three attempts with a
# growing pause is what a home connection and a CI runner both need.
upload() { # key, file, content type, cache control
	attempt=1
	while [ "$attempt" -le 3 ]; do
		if wrangler r2 object put "$bucket/$1" --file "$2" --content-type "$3" --cache-control "$4" --remote \
			>/dev/null 2>"$upload_log"; then
			echo "  $1"
			return 0
		fi
		echo "  $1: attempt $attempt failed, retrying" >&2
		sleep $((attempt * 3))
		attempt=$((attempt + 1))
	done
	echo "failed to upload $1" >&2
	tail -5 "$upload_log" >&2
	return 1
}

# Removes a version-less key this release no longer ships. A failure is reported
# but does not stop the mirror: the release is already published, and a stale
# alias is less harmful than latest.json pointing at nothing.
remove_alias() { # stable name
	attempt=1
	while [ "$attempt" -le 3 ]; do
		if wrangler r2 object delete "$bucket/releases/latest/$1" --remote -y \
			>/dev/null 2>"$upload_log"; then
			echo "  releases/latest/$1 (removed)"
			return 0
		fi
		sleep $((attempt * 3))
		attempt=$((attempt + 1))
	done
	echo "warning: could not remove releases/latest/$1" >&2
	tail -5 "$upload_log" >&2
	return 0
}

if [ -f "$dir/SHA256SUMS" ]; then
	echo "Verifying SHA256SUMS"
	verify_checksums "$dir" || { echo "checksum mismatch: refusing to mirror" >&2; exit 1; }
else
	echo "warning: no SHA256SUMS in $dir" >&2
fi

# SIGNATURES is the release's per-package minisign signatures, one
# "name<TAB>base64" line each. The feed carries the signature of every package so
# the app can check it before installing.
signatures=$dir/SIGNATURES
[ -f "$signatures" ] || signatures=
signature_of() {
	[ -n "$signatures" ] || return 0
	awk -F"$(printf '\t')" -v name="$1" '$1 == name { print $2; exit }' "$signatures"
}

# Every asset this release ships, as "<stable name><TAB><asset name>". SIGNATURES
# stays with the release rather than under a stable name: latest.json carries the
# signatures per file.
scratch_list=$(mktemp)
upload_log=$(mktemp)
for file in "$dir"/*; do
	[ -f "$file" ] || continue
	name=$(basename "$file")
	case $name in
	latest.json | SIGNATURES) continue ;;
	esac
	printf '%s\t%s\n' "$(stable_name "$name")" "$name" >>"$scratch_list"
done

# The mirror holds a package only when the versioned URL answers with the size
# the file has here. The feed must never name bytes that were not uploaded, so
# --announce refuses instead of writing a latest.json the update would fail on.
verify_uploaded() {
	missing=0
	while IFS="$(printf '\t')" read -r stable name; do
		[ -n "$name" ] || continue
		want=$(size_of "$dir/$name")
		got=$(curl -fsSI "$mirror/releases/$tag/$name" | tr -d '\r' \
			| awk 'tolower($1) == "content-length:" { print $2 }' | tail -1)
		if [ "$got" != "$want" ]; then
			echo "mirror does not hold releases/$tag/$name at $want bytes (got ${got:-nothing})" >&2
			missing=1
		fi
	done <"$scratch_list"
	[ "$missing" -eq 0 ] || { echo "the copy is incomplete; run --copy before --announce" >&2; return 1; }
}

if [ "$mode" != announce ]; then
	# What the feed listed before this run: after the uploads, every name in here
	# that this release does not ship loses its alias. The cache-buster matters,
	# because a five-minute CDN copy could otherwise hide the last release's list.
	previous_raw=$(mktemp)
	previous_names=$(mktemp)
	if curl -fsS "$mirror/latest.json?ts=$(date +%s)" -o "$previous_raw" 2>/dev/null; then
		grep -o '"name": *"[^"]*"' "$previous_raw" \
			| sed 's/.*"name": *"//; s/"$//' | sort -u >"$previous_names" || true
	else
		echo "warning: could not read the previous feed; stale aliases are left in place" >&2
	fi

	echo "Uploading $tag to $mirror"
	for file in "$dir"/*; do
		[ -f "$file" ] || continue
		name=$(basename "$file")
		case $name in
		latest.json) continue ;;
		SIGNATURES)
			# The signatures themselves stay with the release, not in the alias
			# directory: latest.json carries them per file.
			upload "releases/$tag/$name" "$file" "$(content_type "$name")" 'public, max-age=31536000, immutable'
			continue
			;;
		esac
		stable=$(stable_name "$name")
		upload "releases/$tag/$name" "$file" "$(content_type "$name")" 'public, max-age=31536000, immutable'
		upload "releases/latest/$stable" "$file" "$(content_type "$stable")" 'public, max-age=300'
	done

	if [ -s "$previous_names" ]; then
		new_names=$(mktemp)
		cut -f1 "$scratch_list" | sort -u >"$new_names"
		while IFS= read -r name; do
			[ -n "$name" ] || continue
			if ! grep -qxF "$name" "$new_names"; then
				remove_alias "$name"
			fi
		done <"$previous_names"
		rm -f "$new_names"
	fi

	# The bucket is a download host, not a site: keep its files out of search results.
	robots_txt=$(mktemp)
	printf 'User-agent: *\nDisallow: /\n' >"$robots_txt"
	upload robots.txt "$robots_txt" 'text/plain; charset=utf-8' 'public, max-age=86400'
	rm -f "$robots_txt"

	verify_uploaded
fi

if [ "$mode" != copy ]; then
	# A copy and an announce in one run already confirmed the bytes above; a lone
	# announce has to confirm them itself before it can point the feed at them.
	if [ "$mode" = announce ]; then
		verify_uploaded
	fi

	repo=${GITHUB_REPOSITORY:-}
	[ -n "$repo" ] || repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
	# The release notes, as one JSON string, for the update dialog. A release without
	# them, or a failed lookup, gives an empty string: the dialog says so.
	notes_json=$(gh release view "$tag" --repo "$repo" --json body --jq '.body | tojson' 2>/dev/null) || notes_json=
	[ -n "$notes_json" ] || notes_json='""'

	{
		printf '{\n'
		printf '  "version": "%s",\n' "$version"
		printf '  "tag": "%s",\n' "$tag"
		printf '  "publishedAt": "%s",\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
		printf '  "source": "https://github.com/%s/releases/tag/%s",\n' "$repo" "$tag"
		printf '  "mirror": "%s",\n' "$mirror"
		printf '  "notes": %s,\n' "$notes_json"
		printf '  "files": [\n'
		first=1
		while IFS="$(printf '\t')" read -r stable name; do
			[ -n "$name" ] || continue
			if [ "$first" -eq 1 ]; then first=0; else printf ',\n'; fi
			signature=$(signature_of "$name")
			if [ -n "$signature" ]; then
				printf '    {"name": "%s", "size": %s, "sha256": "%s", "signature": "%s", "url": "%s/releases/latest/%s"}' \
					"$stable" "$(size_of "$dir/$name")" "$(sha256_of "$dir/$name")" "$signature" "$mirror" "$stable"
			else
				printf '    {"name": "%s", "size": %s, "sha256": "%s", "url": "%s/releases/latest/%s"}' \
					"$stable" "$(size_of "$dir/$name")" "$(sha256_of "$dir/$name")" "$mirror" "$stable"
			fi
		done <"$scratch_list"
		printf '\n  ]\n}\n'
	} >"$dir/latest.json"

	upload latest.json "$dir/latest.json" "application/json" 'public, max-age=300'

	echo "Done: $mirror/latest.json"
fi

if [ "$mode" = copy ]; then
	echo "Copied $tag; latest.json is left for --announce"
fi
