#!/usr/bin/env bash
# Exercises the macOS development identity lifecycle with command stubs. The
# real-keychain path is a separate, explicitly enabled integration test.
set -eu
cd "$(dirname "$0")/.."
. tools/macos_dev_signing.sh

root=$(mktemp -d)
trap 'status=$?; set +e; if [ "${MOTTY_TEST_KEEP:-0}" = 1 ]; then printf "kept test directory: %s\n" "$root"; else rm -rf "$root"; fi; exit "$status"' EXIT
if [ -x /usr/bin/stat ]; then
	stat() { /usr/bin/stat "$@"; }
fi
id() { /usr/bin/id "$@"; }
sleep() { /bin/sleep "$@"; }
awk() { /usr/bin/awk "$@"; }
tr() { /usr/bin/tr "$@"; }
sed() { /usr/bin/sed "$@"; }
cat() { /bin/cat "$@"; }
head() { /usr/bin/head "$@"; }
chmod() { /bin/chmod "$@"; }
mkdir() { /bin/mkdir "$@"; }
mv() { /bin/mv "$@"; }
mktemp() { /usr/bin/mktemp "$@"; }
printf() { builtin printf "$@"; }
stub_dir=$root/stubs
mkdir -p "$stub_dir"

cat >"$stub_dir/security" <<'EOF'
#!/usr/bin/env bash
set -eu
if [ "$#" -eq 0 ]; then
	exit 2
fi
command_name=$1
shift
printf '%s\n' "$command_name" >>"$MOTTY_STUB_OPS"
if [ "${MOTTY_STUB_FAIL_COMMAND:-}" = "$command_name" ]; then
	printf 'stub %s failed\n' "$command_name" >&2
	exit 9
fi

last_argument() {
	local last=
	for argument in "$@"; do
		last=$argument
	done
	printf '%s\n' "$last"
}

case "$command_name" in
create-keychain)
	keychain=$(last_argument "$@")
	: >"$keychain"
	chmod 600 "$keychain"
	if [ "${MOTTY_STUB_BLOCK_CREATE:-0}" = 1 ]; then
		: >"$MOTTY_STUB_BLOCK_MARKER"
		while [ ! -e "$MOTTY_STUB_RELEASE" ]; do
			sleep 0.05
		done
	fi
	;;
set-keychain-settings|unlock-keychain|import|set-key-partition-list)
	;;
verify-cert)
	if [ "${MOTTY_STUB_TRUSTED:-0}" = 1 ]; then
		exit 0
	fi
	exit 1
	;;
add-trusted-cert)
	if [ "${MOTTY_STUB_FAIL_COMMAND:-}" = add-trusted-cert ]; then
		exit 9
	fi
	;;
find-identity)
	if [ -n "${MOTTY_STUB_CUSTOM_IDENTITY:-}" ]; then
		printf '  1) %s "%s"\n     1 valid identities found\n' \
			"$MOTTY_STUB_CUSTOM_IDENTITY_SHA1" "$MOTTY_STUB_CUSTOM_IDENTITY"
	else
		printf '  1) %s "Motty Development Signing"\n     1 valid identities found\n' \
			"$MOTTY_STUB_IDENTITY"
	fi
	;;
find-certificate)
	printf '%s\n' "$MOTTY_STUB_CERTIFICATE"
	;;
list-keychains)
	case " $* " in
	*" -s "*)
		setting=0
		: >"$MOTTY_STUB_SEARCH"
		for argument in "$@"; do
			if [ "$setting" -eq 1 ]; then
				printf '"%s"\n' "$argument" >>"$MOTTY_STUB_SEARCH"
			elif [ "$argument" = -s ]; then
				setting=1
			fi
		done
		;;
	*)
		if [ -f "$MOTTY_STUB_SEARCH" ]; then
			sed 's/^/    /' "$MOTTY_STUB_SEARCH"
		fi
		;;
	esac
	;;
*)
	printf 'unexpected security command: %s\n' "$command_name" >&2
	exit 3
	;;
esac
EOF

cat >"$stub_dir/openssl" <<'EOF'
#!/usr/bin/env bash
set -eu
if [ "$#" -eq 0 ]; then
	exit 2
fi
command_name=$1
shift
printf '%s\n' "openssl-$command_name" >>"$MOTTY_STUB_OPS"
if [ "${MOTTY_STUB_FAIL_COMMAND:-}" = "openssl-$command_name" ]; then
	printf 'stub openssl %s failed\n' "$command_name" >&2
	exit 9
fi

value_after() {
	local option=$1
	shift
	while [ "$#" -gt 0 ]; do
		if [ "$1" = "$option" ]; then
			printf '%s\n' "$2"
			return 0
		fi
		shift
	done
	return 1
}

case "$command_name" in
rand)
	counter_file=$MOTTY_STUB_RANDOM_COUNTER
	count=0
	if [ -f "$counter_file" ]; then
		count=$(cat "$counter_file")
	fi
	count=$((count + 1))
	printf '%s\n' "$count" >"$counter_file"
	if [ "$count" -eq 1 ]; then
		printf 'SECRET_KEYCHAIN_PASSWORD_123\n'
	else
		printf 'SECRET_P12_PASSWORD_456\n'
	fi
	;;
req)
	key=$(value_after -keyout "$@")
	certificate=$(value_after -out "$@")
	printf 'PRIVATE KEY FIXTURE\n' >"$key"
	printf '%s\n' "$MOTTY_STUB_CERTIFICATE" >"$certificate"
	chmod 600 "$key" "$certificate"
	;;
pkcs12)
	p12=$(value_after -out "$@")
	printf 'PKCS12 FIXTURE\n' >"$p12"
	chmod 600 "$p12"
	;;
x509)
	case " $* " in
	*" -fingerprint "*)
		case " $* " in
		*" -sha1 "*)
			printf 'SHA1 Fingerprint=%s\n' "$MOTTY_STUB_CERTIFICATE_SHA1"
			;;
		*" -sha256 "*)
			printf 'SHA256 Fingerprint=%s\n' "$MOTTY_STUB_FINGERPRINT"
			;;
		*)
			printf 'unexpected fingerprint digest\n' >&2
			exit 4
			;;
		esac
		;;
	*" -checkend "*)
		if [ "${MOTTY_STUB_EXPIRED:-0}" = 1 ]; then
			exit 1
		fi
		printf 'Certificate will not expire\n'
		;;
	*)
		printf 'unexpected openssl x509 arguments\n' >&2
		exit 4
		;;
	esac
	;;
*)
	printf 'unexpected openssl command: %s\n' "$command_name" >&2
	exit 3
	;;
esac
EOF

cat >"$stub_dir/uname" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "${MOTTY_STUB_UNAME:-Darwin}"
EOF

cat >"$stub_dir/codesign" <<'EOF'
#!/usr/bin/env bash
set -eu
command_name=$1
shift
printf '%s\n' "codesign-$command_name" >>"$MOTTY_STUB_OPS"
case "$command_name" in
--force)
	if [ "${MOTTY_STUB_FAIL_CODESIGN:-}" = sign ]; then
		exit 8
	fi
	printf 'sign\n' >"$MOTTY_STUB_LAST_SIGN"
	printf '%s\n' "$@" >"$MOTTY_STUB_SIGN_ARGS"
	;;
--verify)
	if [ "${MOTTY_STUB_FAIL_CODESIGN:-}" = verify ]; then
		exit 8
	fi
	printf '%s\n' "$@" >"$MOTTY_STUB_VERIFY_ARGS"
	;;
*)
	printf 'unexpected codesign command: %s\n' "$command_name" >&2
	exit 3
	;;
esac
EOF
chmod +x "$stub_dir/security" "$stub_dir/openssl" "$stub_dir/uname" "$stub_dir/codesign"

export MOTTY_DEV_SIGN_SECURITY=$stub_dir/security
export MOTTY_DEV_SIGN_OPENSSL=$stub_dir/openssl
export MOTTY_DEV_SIGN_UNAME=$stub_dir/uname
export MOTTY_DEV_SIGN_CODESIGN=$stub_dir/codesign

certificate_sha1=89ABCDEF0123456789ABCDEF0123456789ABCDEF
identity=$certificate_sha1
fingerprint=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
custom_identity='Developer ID Application: Test'
custom_sha1=0123456789ABCDEF0123456789ABCDEF01234567
certificate='-----BEGIN CERTIFICATE-----
STUB CERTIFICATE
-----END CERTIFICATE-----'

new_case() {
	local name=$1
	MOTTY_CASE=$root/$name
	MOTTY_SIGNING_DIR="$MOTTY_CASE/identity with space"
	export MOTTY_DEV_SIGNING_DIR=$MOTTY_SIGNING_DIR
	export MOTTY_STUB_OPS=$MOTTY_CASE/operations.log
	export MOTTY_STUB_SEARCH=$MOTTY_CASE/search-list.txt
	export MOTTY_STUB_RANDOM_COUNTER=$MOTTY_CASE/random-counter
	export MOTTY_STUB_IDENTITY=$identity
	export MOTTY_STUB_CERTIFICATE_SHA1=$certificate_sha1
	export MOTTY_STUB_FINGERPRINT=$fingerprint
	export MOTTY_STUB_CUSTOM_IDENTITY=
	export MOTTY_STUB_CUSTOM_IDENTITY_SHA1=$custom_sha1
	export MOTTY_STUB_CERTIFICATE=$certificate
	export MOTTY_STUB_FAIL_COMMAND=
	export MOTTY_STUB_EXPIRED=0
	export MOTTY_STUB_TRUSTED=1
	export MOTTY_STUB_BLOCK_CREATE=0
	export MOTTY_STUB_BLOCK_MARKER=$MOTTY_CASE/create-blocked
	export MOTTY_STUB_RELEASE=$MOTTY_CASE/release-create
	export MOTTY_STUB_UNAME=Darwin
	export MOTTY_STUB_FAIL_CODESIGN=
	export MOTTY_STUB_LAST_SIGN=$MOTTY_CASE/last-sign
	export MOTTY_STUB_SIGN_ARGS=$MOTTY_CASE/sign-args
	export MOTTY_STUB_VERIFY_ARGS=$MOTTY_CASE/verify-args
	mkdir -p "$MOTTY_CASE"
	: >"$MOTTY_STUB_OPS"
	printf '"%s"\n' "$MOTTY_CASE/original.keychain-db" >"$MOTTY_STUB_SEARCH"
}

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
	*) fail "$label: output does not contain '$want': $text" ;;
	esac
}

assert_not_contains() {
	local text=$1 unwanted=$2 label=$3
	case "$text" in
	*"$unwanted"*) fail "$label: output contains '$unwanted'" ;;
	esac
}

assert_mode() {
	local path=$1 want=$2
	local mode
	mode=$(stat -f '%Lp' "$path" 2>/dev/null || stat -c '%a' "$path")
	assert_eq "$mode" "$want" "mode of $path"
}

assert_absent() {
	local path=$1
	if [ -e "$path" ] || [ -L "$path" ]; then
		fail "$path unexpectedly exists"
	fi
}

assert_present() {
	local path=$1
	if [ ! -e "$path" ]; then
		fail "$path is missing"
	fi
}

ensure() {
	local log=$1
	local rc xtrace=0
	case $- in
	*x*) xtrace=1 ;;
	esac
	set +e
	set +x
	macos_dev_signing_ensure_default >"$log" 2>&1
	rc=$?
	set -e
	if [ "$xtrace" -eq 1 ]; then {
		set -x
	}; fi
	ENSURE_RC=$rc
}

resolved_signing_dir() {
	(cd -P "$MOTTY_SIGNING_DIR" && pwd -P)
}

count_command() {
	local command_name=$1
	awk -v command_name="$command_name" '$0 == command_name { n++ } END { print n + 0 }' "$MOTTY_STUB_OPS"
}

# First creation, reuse, permissions, saved binding data and readable logs.
new_case create-reuse
ensure "$MOTTY_CASE/create.log"
assert_eq "$ENSURE_RC" 0 "first initialization"
resolved=$(resolved_signing_dir)
assert_mode "$resolved" 700
assert_mode "$resolved/identity.conf" 600
assert_mode "$resolved/development-signing.keychain-db" 600
assert_mode "$resolved/keychain-password" 600
assert_absent "$resolved/init.lock"
assert_contains "$(cat "$resolved/identity.conf")" "identifier=dev.motty.app.development" "configuration identifier"
assert_contains "$(cat "$resolved/identity.conf")" "identity_sha1=$identity" "configuration identity"
assert_contains "$(cat "$resolved/identity.conf")" "certificate_sha256=$fingerprint" "configuration certificate"
assert_eq "$(head -1 "$MOTTY_STUB_SEARCH")" "\"$resolved/development-signing.keychain-db\"" "private keychain search-list position"
assert_contains "$(cat "$MOTTY_STUB_SEARCH")" "$MOTTY_CASE/original.keychain-db" "existing search-list entry"
assert_eq "$(count_command create-keychain)" 1 "initial keychain creations"
assert_eq "$(count_command openssl-req)" 1 "initial certificate generations"
assert_eq "$(count_command verify-cert)" 1 "initial certificate trust checks"
assert_eq "$(count_command add-trusted-cert)" 0 "already trusted certificates"
assert_not_contains "$(cat "$MOTTY_CASE/create.log")" "SECRET_KEYCHAIN_PASSWORD_123" "initialization output"
assert_not_contains "$(cat "$MOTTY_CASE/create.log")" "SECRET_P12_PASSWORD_456" "initialization output"
assert_not_contains "$(cat "$MOTTY_STUB_OPS")" "SECRET_KEYCHAIN_PASSWORD_123" "operation log"
assert_not_contains "$(cat "$MOTTY_STUB_OPS")" "SECRET_P12_PASSWORD_456" "operation log"
before=$(cksum "$resolved/identity.conf")
ensure "$MOTTY_CASE/reuse.log"
assert_eq "$ENSURE_RC" 0 "identity reuse"
assert_eq "$(cksum "$resolved/identity.conf")" "$before" "reuse must not rewrite configuration"
assert_eq "$(count_command create-keychain)" 1 "reuse must not create another keychain"
assert_eq "$(count_command openssl-req)" 1 "reuse must not generate another certificate"
assert_eq "$(count_command add-trusted-cert)" 0 "reuse must not alter trust settings"
assert_eq "$(grep -Fxc "\"$resolved/development-signing.keychain-db\"" "$MOTTY_STUB_SEARCH")" 1 "deduplicated search-list entry"

# An existing entry that is not first must be moved to the front without
# recreating the identity.
new_case search-list-order
ensure "$MOTTY_CASE/create.log"
assert_eq "$ENSURE_RC" 0 "search-list fixture initialization"
resolved=$(resolved_signing_dir)
{
	printf '"%s"\n' "$MOTTY_CASE/original.keychain-db"
	printf '"%s"\n' "$resolved/development-signing.keychain-db"
} >"$MOTTY_STUB_SEARCH"
ensure "$MOTTY_CASE/reorder.log"
assert_eq "$ENSURE_RC" 0 "search-list reordering"
assert_eq "$(head -1 "$MOTTY_STUB_SEARCH")" "\"$resolved/development-signing.keychain-db\"" "reordered search-list position"
assert_eq "$(count_command create-keychain)" 1 "search-list reordering must not recreate the keychain"

# Shell tracing must not expose generated passwords even though it is enabled
# before the signing helper starts doing secret work.
new_case xtrace-secrets
set +e
bash -x tools/macos_dev_signing.sh load >"$MOTTY_CASE/xtrace.log" 2>&1
xtrace_rc=$?
set -e
assert_eq "$xtrace_rc" 0 "xtrace initialization"
assert_not_contains "$(cat "$MOTTY_CASE/xtrace.log")" "SECRET_KEYCHAIN_PASSWORD_123" "xtrace output"
assert_not_contains "$(cat "$MOTTY_CASE/xtrace.log")" "SECRET_P12_PASSWORD_456" "xtrace output"

# A new certificate that is not trusted yet gets a private code-signing trust
# anchor; an existing trusted identity is left alone.
new_case trust
export MOTTY_STUB_TRUSTED=0
ensure "$MOTTY_CASE/create.log"
assert_eq "$ENSURE_RC" 0 "initialization with untrusted certificate"
assert_eq "$(count_command add-trusted-cert)" 1 "certificate trust additions"
assert_contains "$(cat "$MOTTY_STUB_OPS")" "add-trusted-cert" "trust command"

# Existing material that no longer matches its saved certificate is preserved.
new_case mismatch
ensure "$MOTTY_CASE/create.log"
assert_eq "$ENSURE_RC" 0 "mismatch fixture initialization"
resolved=$(resolved_signing_dir)
wrong=BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB
awk -v value="$wrong" '
	/^certificate_sha256=/ { print "certificate_sha256=" value; next }
	{ print }
' "$resolved/identity.conf" >"$MOTTY_CASE/identity.new"
chmod 600 "$MOTTY_CASE/identity.new"
mv "$MOTTY_CASE/identity.new" "$resolved/identity.conf"
before=$(cksum "$resolved/identity.conf")
ensure "$MOTTY_CASE/mismatch.log"
assert_eq "$ENSURE_RC" 1 "fingerprint mismatch"
assert_contains "$(cat "$MOTTY_CASE/mismatch.log")" "does not match the saved fingerprint" "mismatch diagnosis"
assert_eq "$(cksum "$resolved/identity.conf")" "$before" "mismatch must preserve configuration"
assert_eq "$(count_command create-keychain)" 1 "mismatch must not recreate the keychain"

# An expired certificate stops loading and is not replaced.
new_case expired
ensure "$MOTTY_CASE/create.log"
assert_eq "$ENSURE_RC" 0 "expiry fixture initialization"
resolved=$(resolved_signing_dir)
export MOTTY_STUB_EXPIRED=1
before=$(cksum "$resolved/development-signing.keychain-db")
ensure "$MOTTY_CASE/expired.log"
assert_eq "$ENSURE_RC" 1 "expired certificate"
assert_contains "$(cat "$MOTTY_CASE/expired.log")" "expired or not yet valid" "expiry diagnosis"
assert_eq "$(cksum "$resolved/development-signing.keychain-db")" "$before" "expiry must preserve the keychain"
assert_eq "$(count_command create-keychain)" 1 "expiry must not recreate the keychain"

# A symlinked signing directory is rejected before any system command.
new_case unsafe-directory
mkdir "$MOTTY_CASE/real-identity"
chmod 700 "$MOTTY_CASE/real-identity"
ln -s "$MOTTY_CASE/real-identity" "$MOTTY_CASE/linked-identity"
export MOTTY_DEV_SIGNING_DIR=$MOTTY_CASE/linked-identity
ensure "$MOTTY_CASE/unsafe.log"
assert_eq "$ENSURE_RC" 1 "symlinked signing directory"
assert_contains "$(cat "$MOTTY_CASE/unsafe.log")" "symbolic link" "symlink diagnosis"
assert_eq "$(wc -l <"$MOTTY_STUB_OPS" | tr -d ' ')" 0 "system commands after unsafe directory"

# Unsafe material links and permissions are rejected without replacement.
new_case unsafe-material
ensure "$MOTTY_CASE/create.log"
assert_eq "$ENSURE_RC" 0 "unsafe-material fixture initialization"
resolved=$(resolved_signing_dir)
mv "$resolved/identity.conf" "$MOTTY_CASE/identity.real"
ln -s "$MOTTY_CASE/identity.real" "$resolved/identity.conf"
ensure "$MOTTY_CASE/link.log"
assert_eq "$ENSURE_RC" 1 "symlinked configuration"
assert_contains "$(cat "$MOTTY_CASE/link.log")" "symbolic link" "material symlink diagnosis"
assert_present "$MOTTY_CASE/identity.real"

new_case unsafe-mode
ensure "$MOTTY_CASE/create.log"
assert_eq "$ENSURE_RC" 0 "unsafe-mode fixture initialization"
resolved=$(resolved_signing_dir)
chmod 644 "$resolved/keychain-password"
ensure "$MOTTY_CASE/mode.log"
assert_eq "$ENSURE_RC" 1 "unsafe password-file permission"
assert_contains "$(cat "$MOTTY_CASE/mode.log")" "mode 600" "permission diagnosis"
assert_present "$resolved/keychain-password"

# A retained lock, including one from an interrupted run, requires manual repair.
new_case stale-lock
mkdir "$MOTTY_SIGNING_DIR"
chmod 700 "$MOTTY_SIGNING_DIR"
resolved=$(resolved_signing_dir)
mkdir "$MOTTY_SIGNING_DIR/init.lock"
chmod 700 "$MOTTY_SIGNING_DIR/init.lock"
ensure "$MOTTY_CASE/lock.log"
assert_eq "$ENSURE_RC" 1 "stale initialization lock"
assert_contains "$(cat "$MOTTY_CASE/lock.log")" "initialization lock remains" "stale-lock diagnosis"
assert_present "$MOTTY_SIGNING_DIR/init.lock"
assert_eq "$(wc -l <"$MOTTY_STUB_OPS" | tr -d ' ')" 0 "system commands with stale lock"

# A failed command removes only this initialization attempt; a retry then starts
# from clean material rather than requiring manual lock repair.
new_case failed-initialization
export MOTTY_STUB_FAIL_COMMAND=import
ensure "$MOTTY_CASE/failure.log"
assert_eq "$ENSURE_RC" 1 "failed identity import"
assert_contains "$(cat "$MOTTY_CASE/failure.log")" "importing the development identity failed" "failed-import diagnosis"
assert_absent "$MOTTY_SIGNING_DIR/init.lock"
assert_absent "$MOTTY_SIGNING_DIR/keychain-password"
assert_absent "$MOTTY_SIGNING_DIR/development-signing.keychain-db"
assert_absent "$MOTTY_SIGNING_DIR/identity.conf"
assert_eq "$(find "$MOTTY_SIGNING_DIR" -maxdepth 2 \( -name key.pem -o -name 'identity.p12' -o -name '.init.*' \) | wc -l | tr -d ' ')" 0 "failed initialization temporary files"
export MOTTY_STUB_FAIL_COMMAND=
ensure "$MOTTY_CASE/retry.log"
assert_eq "$ENSURE_RC" 0 "retry after failed initialization"
assert_eq "$(count_command create-keychain)" 2 "failed initialization cleanup and retry"

# Two concurrent initializers: one holds the lock while the other refuses.
new_case concurrent
export MOTTY_STUB_BLOCK_CREATE=1
(
	macos_dev_signing_ensure_default >"$MOTTY_CASE/first.log" 2>&1
) &
first_pid=$!
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
	if [ -e "$MOTTY_STUB_BLOCK_MARKER" ]; then
		break
	fi
	sleep 0.05
done
assert_present "$MOTTY_STUB_BLOCK_MARKER"
ensure "$MOTTY_CASE/second.log"
assert_eq "$ENSURE_RC" 1 "second concurrent initializer"
assert_contains "$(cat "$MOTTY_CASE/second.log")" "initialization lock" "concurrent-lock diagnosis"
: >"$MOTTY_STUB_RELEASE"
set +e
wait "$first_pid"
first_rc=$?
set -e
assert_eq "$first_rc" 0 "first concurrent initializer"
assert_absent "$MOTTY_SIGNING_DIR/init.lock"
assert_eq "$(count_command create-keychain)" 1 "concurrent keychain creations"

# Signing with the default identity binds the requested identifier and the
# SHA-1 leaf certificate identity, then verifies that exact requirement.
new_case sign-default
printf '#!/bin/sh\n' >"$MOTTY_CASE/motty-dev"
chmod +x "$MOTTY_CASE/motty-dev"
set +e
macos_dev_signing_sign "$MOTTY_CASE/motty-dev" >"$MOTTY_CASE/sign.log" 2>&1
sign_rc=$?
set -e
assert_eq "$sign_rc" 0 "default development signing"
assert_contains "$(cat "$MOTTY_STUB_SIGN_ARGS")" "--identifier" "default signing identifier option"
assert_contains "$(cat "$MOTTY_STUB_SIGN_ARGS")" "dev.motty.app.development" "default signing identifier"
assert_contains "$(cat "$MOTTY_STUB_SIGN_ARGS")" "--keychain" "default signing keychain option"
assert_contains "$(cat "$MOTTY_STUB_SIGN_ARGS")" "=designated => identifier \"dev.motty.app.development\" and certificate leaf = H\"$certificate_sha1\"" "embedded designated requirement"
assert_contains "$(cat "$MOTTY_STUB_VERIFY_ARGS")" "identifier \"dev.motty.app.development\" and certificate leaf = H\"$certificate_sha1\"" "verified designated requirement"
assert_eq "$(sed -n '1p' "$MOTTY_STUB_VERIFY_ARGS")" "--strict" "verification strict option"
assert_eq "$(sed -n '2p' "$MOTTY_STUB_VERIFY_ARGS")" "-R=identifier \"dev.motty.app.development\" and certificate leaf = H\"$certificate_sha1\"" "verification requirement without leading equals"
assert_not_contains "$(cat "$MOTTY_STUB_VERIFY_ARGS")" "$fingerprint" "verification must use the SHA-1 certificate fingerprint"
assert_eq "$(count_command codesign---force)" 1 "default signing calls"
assert_eq "$(count_command codesign---verify)" 1 "default verification calls"

# With an explicit identity, the helper signs but does not initialize or unlock
# the default private identity.
new_case sign-custom
printf '#!/bin/sh\n' >"$MOTTY_CASE/motty-dev"
chmod +x "$MOTTY_CASE/motty-dev"
export MOTTY_STUB_CUSTOM_IDENTITY=$custom_identity
set +e
macos_dev_signing_sign "$MOTTY_CASE/motty-dev" "$custom_identity" >"$MOTTY_CASE/sign.log" 2>&1
sign_rc=$?
set -e
assert_eq "$sign_rc" 0 "explicit identity signing"
assert_absent "$MOTTY_CASE/identity with space"
assert_contains "$(cat "$MOTTY_STUB_SIGN_ARGS")" "$custom_identity" "explicit signing identity"
assert_contains "$(cat "$MOTTY_STUB_SIGN_ARGS")" "--identifier" "explicit signing identifier option"
assert_contains "$(cat "$MOTTY_STUB_SIGN_ARGS")" "=designated => identifier \"dev.motty.app.development\" and certificate leaf = H\"$custom_sha1\"" "explicit embedded requirement"
assert_contains "$(cat "$MOTTY_STUB_VERIFY_ARGS")" "identifier \"dev.motty.app.development\" and certificate leaf = H\"$custom_sha1\"" "explicit verification requirement"
assert_not_contains "$(cat "$MOTTY_STUB_VERIFY_ARGS")" "$certificate_sha1" "explicit verification must not use the default certificate"

new_case sign-custom-missing
printf '#!/bin/sh\n' >"$MOTTY_CASE/motty-dev"
chmod +x "$MOTTY_CASE/motty-dev"
set +e
macos_dev_signing_sign "$MOTTY_CASE/motty-dev" "$custom_identity" >"$MOTTY_CASE/sign.log" 2>&1
sign_rc=$?
set -e
assert_eq "$sign_rc" 1 "unknown explicit identity"
assert_contains "$(cat "$MOTTY_CASE/sign.log")" "exactly one valid code-signing certificate" "unknown explicit identity diagnosis"
assert_eq "$(count_command codesign---force)" 0 "unknown explicit identity signing"

# Explicitly unsigned runs are still accepted, but warn about authorization.
new_case sign-none
printf '#!/bin/sh\n' >"$MOTTY_CASE/motty-dev"
chmod +x "$MOTTY_CASE/motty-dev"
set +e
macos_dev_signing_sign "$MOTTY_CASE/motty-dev" none >"$MOTTY_CASE/sign.log" 2>&1
sign_rc=$?
set -e
assert_eq "$sign_rc" 0 "explicit unsigned signing"
assert_contains "$(cat "$MOTTY_CASE/sign.log")" "signing disabled" "unsigned warning"
assert_contains "$(cat "$MOTTY_CASE/sign.log")" "authorization may repeat" "unsigned authorization warning"
assert_eq "$(wc -l <"$MOTTY_STUB_OPS" | tr -d ' ')" 0 "unsigned run system commands"

# A failed sign or verification stops the launch attempt.
new_case sign-failure
printf '#!/bin/sh\n' >"$MOTTY_CASE/motty-dev"
chmod +x "$MOTTY_CASE/motty-dev"
ensure "$MOTTY_CASE/create.log"
assert_eq "$ENSURE_RC" 0 "sign-failure fixture initialization"
export MOTTY_STUB_FAIL_CODESIGN=sign
set +e
macos_dev_signing_sign "$MOTTY_CASE/motty-dev" >"$MOTTY_CASE/sign.log" 2>&1
sign_rc=$?
set -e
assert_eq "$sign_rc" 1 "failed signing"
assert_contains "$(cat "$MOTTY_CASE/sign.log")" "signing the development executable failed" "signing failure diagnosis"
assert_eq "$(grep -c '^codesign---verify$' "$MOTTY_STUB_OPS" || true)" 0 "verification after signing failure"

new_case verify-failure
printf '#!/bin/sh\n' >"$MOTTY_CASE/motty-dev"
chmod +x "$MOTTY_CASE/motty-dev"
ensure "$MOTTY_CASE/create.log"
assert_eq "$ENSURE_RC" 0 "verify-failure fixture initialization"
export MOTTY_STUB_FAIL_CODESIGN=verify
set +e
macos_dev_signing_sign "$MOTTY_CASE/motty-dev" >"$MOTTY_CASE/sign.log" 2>&1
sign_rc=$?
set -e
assert_eq "$sign_rc" 1 "failed verification"
assert_contains "$(cat "$MOTTY_CASE/sign.log")" "verifying the development signature failed" "verification failure diagnosis"

# Non-macOS platforms skip both signing and default identity creation.
new_case wrong-platform
export MOTTY_STUB_UNAME=Linux
ensure "$MOTTY_CASE/platform.log"
assert_eq "$ENSURE_RC" 1 "non-macOS platform"
assert_contains "$(cat "$MOTTY_CASE/platform.log")" "only available on macOS" "platform diagnosis"
assert_absent "$MOTTY_SIGNING_DIR"
set +e
macos_dev_signing_sign "$MOTTY_CASE/nonexistent" >"$MOTTY_CASE/platform-sign.log" 2>&1
sign_rc=$?
set -e
assert_eq "$sign_rc" 0 "non-macOS signing"
assert_eq "$(wc -c <"$MOTTY_CASE/platform-sign.log" | tr -d ' ')" 0 "non-macOS signing output"
assert_eq "$(wc -l <"$MOTTY_STUB_OPS" | tr -d ' ')" 0 "non-macOS signing system commands"
printf '#!/bin/sh\n' >"$MOTTY_CASE/existing-binary"
chmod +x "$MOTTY_CASE/existing-binary"
set +e
macos_dev_signing_sign "$MOTTY_CASE/existing-binary" >"$MOTTY_CASE/platform-existing.log" 2>&1
sign_rc=$?
set -e
assert_eq "$sign_rc" 0 "non-macOS signing of an existing binary"
assert_eq "$(wc -c <"$MOTTY_CASE/platform-existing.log" | tr -d ' ')" 0 "non-macOS existing-binary output"
assert_eq "$(wc -l <"$MOTTY_STUB_OPS" | tr -d ' ')" 0 "non-macOS existing-binary system commands"

printf 'macOS development signing tests passed\n'
