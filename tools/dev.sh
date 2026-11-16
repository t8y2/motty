#!/usr/bin/env bash
# Build, sign and launch one Motty development build. The default watch mode
# rebuilds and restarts it whenever a Go file here or in ../viva changes.
set -u
cd "$(dirname "$0")/.."
. tools/dev_launch.sh
. tools/macos_dev_signing.sh

bin=${MOTTY_DEV_BIN:-/tmp/motty-dev}
launch_log=${MOTTY_DEV_LOG:-/tmp/motty-dev.log}

# Colour only a terminal: a redirected log or NO_COLOR stays plain.
use_color=0
if [ -t 1 ] && [ -z "${NO_COLOR+x}" ] && [ "${TERM:-}" != dumb ]; then
	use_color=1
fi

say() {
	local tone=$1
	shift
	local mark='==>' text="$*"
	if [ "$use_color" -eq 1 ]; then
		local prefix='' reset=$'\033[0m'
		case "$tone" in
		building) prefix=$'\033[1;34m' ;; # blue
		ok) prefix=$'\033[1;32m' ;;       # green
		wait) prefix=$'\033[1;33m' ;;     # yellow
		change) prefix=$'\033[1;36m' ;;   # cyan
		fail) prefix=$'\033[1;31m' ;;     # red
		dim) prefix=$'\033[2m' ;;         # dim
		esac
		mark="${prefix}==>${reset}"
		text="${prefix}${text}${reset}"
	fi
	printf '%s %s\n' "$mark" "$text"
}

stop() {
	dev_launch_stop
}

build_sign_launch() {
	say building "building motty-dev from $PWD"
	SECONDS=0
	if ! go build -tags dev -o "$bin" ./cmd/motty; then {
		say fail "build failed" >&2
		return 1
	}; fi
	say ok "build finished in ${SECONDS}s"
	if ! macos_dev_signing_sign "$bin" "${MOTTY_DEV_SIGN_IDENTITY:-}"; then {
		say fail "development signing failed; the build was not launched" >&2
		return 1
	}; fi
	: >"$launch_log"
	dev_launch "$bin" "$launch_log"
	case "$DEV_LAUNCH_RESULT" in
	started)
		if [ -n "${MOTTY_DATA_DIR:-}" ]; then
			say ok "motty-dev reported a window; pid $DEV_LAUNCH_PID (data: $MOTTY_DATA_DIR)"
		else
			say ok "motty-dev reported a window; pid $DEV_LAUNCH_PID (data: default)"
		fi
		;;
	handoff)
		say wait "launch handed to the running copy; this development build did not start"
		say dim "quit the running copy before retrying"
		;;
	*)
		say fail "motty-dev failed to start; see $launch_log" >&2
		return 1
		;;
	esac
}

one_shot() {
	trap 'stop; exit 0' INT TERM
	if ! build_sign_launch; then {
		return 1
	}; fi
	if [ "$DEV_LAUNCH_RESULT" = started ] && [ -n "$DEV_LAUNCH_PID" ]; then {
		local status=0 pid=$DEV_LAUNCH_PID drain=$DEV_LAUNCH_DRAIN
		wait "$pid"
		status=$?
		DEV_LAUNCH_PID=
		[ -z "$drain" ] || wait "$drain" 2>/dev/null || true
		DEV_LAUNCH_DRAIN=
		return "$status"
	}; fi
}

watch() {
	command -v fswatch >/dev/null 2>&1 || {
		say fail "watch mode needs fswatch (brew install fswatch)" >&2
		return 2
	}
	trap 'stop; exit 0' INT TERM
	build_sign_launch
	say dim "watching . and ../viva for Go changes (Ctrl-C to stop)"
	while read -r _; do
		say change "change detected; restarting"
		stop
		build_sign_launch
	done < <(fswatch -o -l 0.3 -e '.*' -i '\.go$' -i 'go\.(mod|sum)$' . ../viva)
}

case "${1:-}" in
"")
	watch
	;;
--once)
	one_shot
	;;
*)
	printf 'usage: %s [--once]\n' "$0" >&2
	exit 2
	;;
esac
