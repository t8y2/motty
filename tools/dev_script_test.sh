#!/usr/bin/env bash
# Exercises the development entry point without building Motty or opening a
# window. These checks cover one-shot dependency handling, signing failure, and
# watch-mode process ownership.
set -eu
cd "$(dirname "$0")/.."

root=$(mktemp -d)
trap 'status=$?; if [ "${MOTTY_TEST_KEEP:-0}" = 1 ]; then printf "kept test directory: %s\n" "$root"; else rm -rf "$root"; fi; exit "$status"' EXIT
case_dir=$root/stub-case
stub_dir=$root/stubs
mkdir -p "$stub_dir" "$case_dir"

cat >"$stub_dir/go" <<'EOF'
#!/usr/bin/env bash
set -eu
printf 'go %s\n' "$*" >>"$MOTTY_TEST_LOG"
while [ "$#" -gt 0 ]; do
	if [ "$1" = -o ]; then
		cat >"$2" <<'SCRIPT'
#!/usr/bin/env bash
printf 'launch\n' >>"$MOTTY_TEST_CHILD_LOG"
printf 'motty: started pid=%s\n' "$$"
sleep "${MOTTY_TEST_CHILD_SLEEP:-30}"
exit "${MOTTY_TEST_CHILD_EXIT:-0}"
SCRIPT
		chmod +x "$2"
		exit 0
	fi
	shift
done
exit 2
EOF

cat >"$stub_dir/fswatch" <<'EOF'
#!/usr/bin/env bash
set -eu
printf 'fswatch\n' >>"$MOTTY_TEST_LOG"
printf 'change\n'
exec sleep 30
EOF

cat >"$stub_dir/uname" <<'EOF'
#!/usr/bin/env bash
printf 'Darwin\n'
EOF

cat >"$stub_dir/codesign" <<'EOF'
#!/usr/bin/env bash
set -eu
printf 'codesign %s\n' "$*" >>"$MOTTY_TEST_LOG"
case "$*" in
*"--force"*)
	if [ "${MOTTY_TEST_SIGN_FAIL:-0}" = 1 ]; then
		exit 7
	fi
	;;
esac
EOF

cat >"$stub_dir/security" <<'EOF'
#!/usr/bin/env bash
set -eu
if [ "${1:-}" != find-identity ]; then
	printf 'unexpected security command: %s\n' "${1:-}" >&2
	exit 2
fi
printf '  1) 1111111111111111111111111111111111111111 "Developer ID Application: Script Test"\n'
EOF

chmod +x "$stub_dir/go" "$stub_dir/uname" "$stub_dir/codesign" "$stub_dir/security"
export PATH="$stub_dir:/usr/bin:/bin:/usr/sbin:/sbin"
export MOTTY_DEV_BIN=$case_dir/motty-dev
export MOTTY_DEV_LOG=$case_dir/launch.log
export MOTTY_DEV_SIGN_IDENTITY='Developer ID Application: Script Test'
export MOTTY_DEV_SIGN_SECURITY=$stub_dir/security
export MOTTY_DEV_SIGN_UNAME=$stub_dir/uname
export MOTTY_DEV_SIGN_CODESIGN=$stub_dir/codesign
export MOTTY_TEST_LOG=$case_dir/operations.log
export MOTTY_TEST_CHILD_LOG=$case_dir/children.log
: >"$MOTTY_TEST_LOG"
: >"$MOTTY_TEST_CHILD_LOG"

assert_eq() {
	local got=$1 want=$2 label=$3
	if [ "$got" != "$want" ]; then
		printf '%s: got %s, want %s\n' "$label" "$got" "$want" >&2
		exit 1
	fi
}

assert_contains() {
	local text=$1 want=$2 label=$3
	case "$text" in
	*"$want"*) ;;
	*) printf '%s: output does not contain %s\n' "$label" "$want" >&2; exit 1 ;;
	esac
}

assert_not_contains() {
	local text=$1 unwanted=$2 label=$3
	case "$text" in
	*"$unwanted"*) printf '%s: output contains %s\n' "$label" "$unwanted" >&2; exit 1 ;;
	esac
}

# With no fswatch on PATH, one-shot still builds, signs, starts, and waits for
# its child. It must not claim a startup that signing prevented.
export MOTTY_TEST_CHILD_SLEEP=0.2
set +e
tools/dev.sh --once >"$case_dir/once.log" 2>&1
once_rc=$?
set -e
assert_eq "$once_rc" 0 "one-shot result"
assert_contains "$(cat "$case_dir/once.log")" "motty-dev reported a window" "one-shot startup report"
assert_contains "$(cat "$MOTTY_TEST_LOG")" "go build -tags dev" "one-shot build"
assert_contains "$(cat "$MOTTY_TEST_LOG")" "codesign --force" "one-shot signing"
assert_not_contains "$(cat "$MOTTY_TEST_LOG")" "fswatch" "one-shot fswatch"
assert_eq "$(wc -l <"$MOTTY_TEST_CHILD_LOG" | tr -d ' ')" 1 "one-shot child launches"

# A successful launch marker does not make a later child failure a successful
# one-shot run: automation must see the child's real exit status.
export MOTTY_TEST_CHILD_SLEEP=0
export MOTTY_TEST_CHILD_EXIT=7
: >"$MOTTY_TEST_CHILD_LOG"
set +e
tools/dev.sh --once >"$case_dir/child-failure.log" 2>&1
once_rc=$?
set -e
assert_eq "$once_rc" 7 "post-start child failure result"
assert_contains "$(cat "$case_dir/child-failure.log")" "motty-dev reported a window" "post-start failure launch report"
unset MOTTY_TEST_CHILD_EXIT

# The release build never enters this script, and the script has one build/sign
# path. Counting these tokens catches a second unsigned or ad-hoc path creeping
# back into the development entry point.
assert_eq "$(grep -c 'go build -tags dev' tools/dev.sh | tr -d ' ')" 1 "development build entry points"
assert_eq "$(grep -c 'macos_dev_signing_sign' tools/dev.sh | tr -d ' ')" 1 "development signing entry points"
assert_not_contains "$(cat tools/dev.sh)" 'codesign --force' "duplicate direct signing"
assert_not_contains "$(cat tools/dev.sh)" 'security find-identity' "automatic certificate selection"

export MOTTY_TEST_SIGN_FAIL=1
: >"$MOTTY_TEST_CHILD_LOG"
set +e
tools/dev.sh --once >"$case_dir/failure.log" 2>&1
once_rc=$?
set -e
assert_eq "$once_rc" 1 "failed signing result"
assert_contains "$(cat "$case_dir/failure.log")" "development signing failed" "failed signing message"
assert_not_contains "$(cat "$case_dir/failure.log")" "motty-dev reported a window" "failed signing must not launch"
assert_eq "$(wc -l <"$MOTTY_TEST_CHILD_LOG" | tr -d ' ')" 0 "failed signing child launches"
unset MOTTY_TEST_SIGN_FAIL

# Watch mode kills only the child it launched. An unrelated process in the same
# process group must survive both a restart and the watcher's own shutdown.
export MOTTY_TEST_CHILD_SLEEP=30
chmod +x "$stub_dir/fswatch"
sleep 30 &
unrelated=$!
(
	tools/dev.sh >"$case_dir/watch.log" 2>&1
) &
watcher=$!
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
	if [ "$(wc -l <"$MOTTY_TEST_CHILD_LOG" | tr -d ' ')" -ge 2 ]; then
		break
	fi
	sleep 0.05
done
kill "$watcher" 2>/dev/null || true
wait "$watcher" 2>/dev/null || true
assert_contains "$(cat "$case_dir/watch.log")" "change detected; restarting" "watch restart"
assert_eq "$(wc -l <"$MOTTY_TEST_CHILD_LOG" | tr -d ' ')" 2 "watch child launches"
if ! kill -0 "$unrelated" 2>/dev/null; then
	printf '%s\n' "watch mode killed an unrelated process" >&2
	exit 1
fi
kill "$unrelated" 2>/dev/null || true
wait "$unrelated" 2>/dev/null || true

printf 'development script tests passed\n'
