#!/usr/bin/env bash
# Checks how the development launcher classifies a child's output without
# opening a window. Run through tools/dev_script_test.sh.
set -eu
cd "$(dirname "$0")/.."
. tools/dev_launch.sh

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

write() {
	local name=$1 body=$2
	printf '#!/usr/bin/env bash\n%s\n' "$body" >"$dir/$name"
	chmod +x "$dir/$name"
}

write started 'echo "motty: started pid=$$"; exec sleep 30'
write handoff "echo 'Motty Dev: launch handed to the running copy; this development build did not start.' >&2; echo 'motty: secondary launch handed off' >&2"
write failed "echo boom >&2; exit 7"
write silent 'exec sleep 30'
write chatter 'while :; do echo noise; sleep 0.2; done'

expect() {
	local binary=$1 want=$2 want_rc=$3
	set +e
	dev_launch "$dir/$binary" "$dir/$binary.log" 1 >/dev/null
	local rc=$?
	set -e
	if [ "$DEV_LAUNCH_RESULT" != "$want" ] || [ "$rc" -ne "$want_rc" ]; then
		printf '%s: result=%s rc=%s, want %s/%s\n' "$binary" "$DEV_LAUNCH_RESULT" "$rc" "$want" "$want_rc" >&2
		exit 1
	fi
	case "$want" in
	started)
		if [ -z "$DEV_LAUNCH_PID" ] || [ -z "$DEV_LAUNCH_DRAIN" ]; then
			printf '%s: started without a child and output drainer\n' "$binary" >&2
			exit 1
		fi
		;;
	handoff)
		if ! grep -q "motty: secondary launch handed off" "$dir/$binary.log"; then
			printf '%s: handoff was not logged\n' "$binary" >&2
			exit 1
		fi
		;;
	failed)
		if ! grep -q "startup failed with status 7" "$dir/$binary.log"; then
			printf '%s: failure was not logged\n' "$binary" >&2
			exit 1
		fi
		;;
	timeout)
		if ! grep -q "no window after 1s" "$dir/$binary.log"; then
			printf '%s: timeout was not logged\n' "$binary" >&2
			exit 1
		fi
		;;
	esac
	dev_launch_stop
}

expect started started 0
expect handoff handoff 0
expect failed failed 1
expect silent timeout 1
expect chatter timeout 1
printf 'launcher classifications passed\n'
