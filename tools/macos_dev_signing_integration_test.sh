#!/usr/bin/env bash
# Verify that separately rebuilt Mach-O programs with the same development
# identity share a Keychain authorization. This test is opt-in because it uses
# the real code-signing identity and creates a temporary system keychain.
set -eu
cd "$(dirname "$0")/.."

if [ "${MOTTY_DEV_SIGNING_INTEGRATION:-0}" != 1 ]; then
	printf 'skipped macOS signing integration test; set MOTTY_DEV_SIGNING_INTEGRATION=1 to run it\n'
	exit 0
fi
if [ "$(/usr/bin/uname -s)" != Darwin ]; then
	printf 'macOS signing integration test only runs on macOS\n' >&2
	exit 1
fi

unset MOTTY_DEV_SIGN_SECURITY MOTTY_DEV_SIGN_OPENSSL MOTTY_DEV_SIGN_UNAME MOTTY_DEV_SIGN_CODESIGN
. tools/macos_dev_signing.sh

clang=${MOTTY_DEV_SIGNING_CLANG:-/usr/bin/clang}
security=/usr/bin/security
codesign=/usr/bin/codesign
root=$(mktemp -d)
root_real=$(cd -P "$root" && pwd -P)
keychain=$root/keychain/integration.keychain-db
service=motty-dev-signing-integration
account=probe-secret

cleanup() {
	local status=$? cleanup_path
	set +e
	if [ -e "$keychain" ]; then
		cleanup_path=$(cd "$(dirname "$keychain")" 2>/dev/null && pwd -P)
		case "$cleanup_path" in
		"$root_real"/*) "$security" delete-keychain "$keychain" >/dev/null 2>&1 ;;
		*)
			printf 'refusing to delete an unexpected keychain path: %s\n' "$keychain" >&2
			;;
		esac
	fi
	if [ "${MOTTY_TEST_KEEP:-0}" = 1 ]; then
		printf 'kept integration test directory: %s\n' "$root"
	else
		rm -rf "$root"
	fi
	exit "$status"
}
trap cleanup EXIT

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

assert_eq() {
	local got=$1 want=$2 label=$3
	if [ "$got" != "$want" ]; then
		fail "$label: got '$got', want '$want'"
	fi
}

assert_contains() {
	local text=$1 want=$2 label=$3
	case "$text" in
	*"$want"*) ;;
	*) fail "$label: output does not contain '$want'" ;;
	esac
}

assert_not_contains() {
	local text=$1 unwanted=$2 label=$3
	case "$text" in
	*"$unwanted"*) fail "$label: output contains '$unwanted'" ;;
	esac
}

cdhash() {
	"$codesign" -dvvv "$1" 2>&1 |
		sed -n 's/^CDHash=//p' |
		sed -n '$p'
}

designated_requirement() {
	"$codesign" -d -r- "$1" 2>&1 |
		sed -n 's/^designated => //p'
}

run_probe() {
	local label=$1 output=$root/$1.out
	shift
	local pid watchdog status

	"$@" >"$output" 2>"$root/$label.err" &
	pid=$!
	(
		sleep 10
		kill "$pid" 2>/dev/null || true
	) &
	watchdog=$!
	set +e
	wait "$pid"
	status=$?
	set -e
	kill "$watchdog" 2>/dev/null || true
	wait "$watchdog" 2>/dev/null || true
	PROBE_STATUS=$status
	PROBE_OUTPUT=$output
}

source_file=$PWD/tools/macos_keychain_probe.c
one=$root/probe-one
two=$root/probe-two
adhoc=$root/probe-adhoc

compiler_flags=(-O0 -Wall -Wextra -Wno-deprecated-declarations)

if ! "$clang" "${compiler_flags[@]}" -DPROBE_BUILD_TAG=101 \
	-framework CoreFoundation -framework Security \
	"$source_file" -o "$one"; then
	fail "cannot compile the first keychain probe"
fi
if ! "$clang" "${compiler_flags[@]}" -DPROBE_BUILD_TAG=202 \
	-framework CoreFoundation -framework Security \
	"$source_file" -o "$two"; then
	fail "cannot compile the second keychain probe"
fi
if ! "$clang" "${compiler_flags[@]}" -DPROBE_BUILD_TAG=303 \
	-framework CoreFoundation -framework Security \
	"$source_file" -o "$adhoc"; then
	fail "cannot compile the ad-hoc control probe"
fi

assert_eq "$("$one" --build-tag)" 101 "first probe build marker"
assert_eq "$("$two" --build-tag)" 202 "second probe build marker"
if ! macos_dev_signing_sign "$one"; then
	fail "cannot sign the first probe with the default development identity"
fi
if ! macos_dev_signing_sign "$two"; then
	fail "cannot sign the second probe with the default development identity"
fi
if ! "$codesign" --force --sign - --identifier "$MDS_IDENTIFIER" "$adhoc"; then
	fail "cannot create the ad-hoc control signature"
fi

one_cdhash=$(cdhash "$one")
two_cdhash=$(cdhash "$two")
one_requirement=$(designated_requirement "$one")
two_requirement=$(designated_requirement "$two")
if [ -z "$one_cdhash" ] || [ -z "$two_cdhash" ]; then
	fail "cannot read the signed probe CDHashes"
fi
if [ "$one_cdhash" = "$two_cdhash" ]; then
	fail "the two rebuilds unexpectedly have the same CDHash"
fi
assert_eq "$two_requirement" "$one_requirement" "designated requirement across rebuilds"
assert_contains "$one_requirement" "identifier \"$MDS_IDENTIFIER\"" "development identifier in requirement"
assert_contains "$one_requirement" \
	"certificate leaf = H\"$(printf '%s' "$MDS_CERTIFICATE_SHA1" | tr '[:upper:]' '[:lower:]')\"" \
	"certificate identity in requirement"

mkdir -p "$(dirname "$keychain")"
keychain_password=$("${MOTTY_DEV_SIGN_OPENSSL:-/usr/bin/openssl}" rand -hex 32)
secret=$("${MOTTY_DEV_SIGN_OPENSSL:-/usr/bin/openssl}" rand -hex 24)
if [ -z "$keychain_password" ] || [ -z "$secret" ]; then
	fail "cannot generate isolated keychain material"
fi
if ! "$security" create-keychain -p "$keychain_password" "$keychain" ||
	! "$security" set-keychain-settings -lut 3600 "$keychain" ||
	! "$security" unlock-keychain -p "$keychain_password" "$keychain" ||
	! "$security" add-generic-password -a "$account" -s "$service" -w "$secret" \
		-T "" -T "$one" "$keychain"; then
	fail "cannot create the isolated keychain item"
fi
chmod 600 "$keychain"

acl=$("$security" dump-keychain -a "$keychain" 2>&1)
assert_contains "$acl" "$one (OK)" "signed first probe ACL entry"
assert_contains "$acl" \
	"certificate leaf = H\"$(printf '%s' "$MDS_CERTIFICATE_SHA1" | tr '[:upper:]' '[:lower:]')\"" \
	"designated requirement in keychain ACL"
assert_not_contains "$acl" "$two (OK)" "second probe must rely on the shared requirement, not a path entry"

run_probe first "$one" "$keychain" "$service" "$account"
assert_eq "$PROBE_STATUS" 0 "first signed probe access"
assert_eq "$(cat "$PROBE_OUTPUT")" "$secret" "first signed probe secret"

run_probe rebuilt "$two" "$keychain" "$service" "$account"
assert_eq "$PROBE_STATUS" 0 "rebuilt signed probe access"
assert_eq "$(cat "$PROBE_OUTPUT")" "$secret" "rebuilt signed probe secret"

run_probe adhoc "$adhoc" "$keychain" "$service" "$account"
if [ "$PROBE_STATUS" -eq 0 ]; then
	fail "the ad-hoc control inherited the signed probe authorization"
fi
assert_not_contains "$(cat "$PROBE_OUTPUT")" "$secret" "ad-hoc probe must not receive the secret"

printf 'macOS development signing integration tests passed\n'
printf '  first CDHash:  %s\n' "$one_cdhash"
printf '  rebuild CDHash: %s\n' "$two_cdhash"
printf '  requirement:   %s\n' "$one_requirement"
printf '  ad-hoc status: %s\n' "$PROBE_STATUS"
