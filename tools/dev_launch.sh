#!/usr/bin/env bash
# Launch observation shared by tools/dev.sh. The application reports that its
# first window exists; a child PID alone does not mean it became usable.

# The state exists before the first launch so a signal that arrives while a
# build is failing still stops cleanly under set -u.
DEV_LAUNCH_PID=
DEV_LAUNCH_DRAIN=
DEV_LAUNCH_STATUS=
DEV_LAUNCH_RESULT=failed
DEV_LAUNCH_ACTIVE=0

dev_launch() {
	local binary=$1 logfile=$2 timeout=${3:-15}
	local fifo=${TMPDIR:-/tmp}/motty-dev-output.$$.$RANDOM
	local line deadline remaining
	local exit_trap
	DEV_LAUNCH_PID=
	DEV_LAUNCH_DRAIN=
	DEV_LAUNCH_STATUS=
	DEV_LAUNCH_RESULT=failed
	DEV_LAUNCH_ACTIVE=0
	if [ -e "$fifo" ]; then
		return 1
	fi
	if ! mkfifo "$fifo"; then
		printf '%s\n' "could not create launch pipe $fifo" >>"$logfile"
		DEV_LAUNCH_STATUS=1
		return 1
	fi
	# The read-write descriptor lets the child open its side before a reader
	# exists. The read-only descriptor becomes the actual EOF source once the
	# read-write descriptor is closed below.
	exec 8<>"$fifo"
	exec 9<"$fifo"
	(
		# Do not run a caller's EXIT cleanup when this background child exits.
		trap - EXIT
		exec 9<&-
		exec "$binary" >&8 2>&1
	) &
	DEV_LAUNCH_PID=$!
	DEV_LAUNCH_ACTIVE=1
	exec 8<&-
	if [ -e "$fifo" ]; then
		rm -f "$fifo"
	fi
	: >>"$logfile"
	deadline=$((SECONDS + timeout))

	while :; do
		remaining=$((deadline - SECONDS))
		if [ "$remaining" -le 0 ]; then
			if dev_launch_child_running; then
				DEV_LAUNCH_RESULT=timeout
				dev_launch_terminate
				printf 'no window after %ss\n' "$timeout" >>"$logfile"
				return 1
			fi
			break
		fi
		if IFS= read -r -t "$remaining" line <&9; then
			printf '%s\n' "$line"
			printf '%s\n' "$line" >>"$logfile"
			case "$line" in
			*"motty: started"*)
				DEV_LAUNCH_RESULT=started
				# The drain runs for the application's lifetime. Do not let it inherit
				# and later run the caller's EXIT cleanup if it is stopped immediately.
				exit_trap=$(trap -p EXIT)
				trap - EXIT
				dev_launch_drain "$logfile" &
				DEV_LAUNCH_DRAIN=$!
				if [ -n "$exit_trap" ]; then
					eval "$exit_trap"
				fi
				return 0
				;;
			esac
			continue
		else
			if dev_launch_child_running; then
				# Bash 3.2 reports a timeout as status 1. Keep looping until
				# the one shared deadline, so uninterrupted output cannot reset
				# it, and a closed output pipe cannot spin.
				sleep 0.05
				continue
			fi
		fi
		break
	done
	if wait "$DEV_LAUNCH_PID"; then
		DEV_LAUNCH_STATUS=0
	else
		DEV_LAUNCH_STATUS=$?
	fi
	DEV_LAUNCH_PID=
	exec 9<&-
	if [ -e "$fifo" ]; then
		rm -f "$fifo"
	fi
	DEV_LAUNCH_ACTIVE=0
	if grep -q "motty: secondary launch handed off" "$logfile"; then
		DEV_LAUNCH_RESULT=handoff
		DEV_LAUNCH_STATUS=0
		return 0
	fi
	if [ "$DEV_LAUNCH_STATUS" -eq 0 ]; then
		DEV_LAUNCH_RESULT=timeout
		printf '%s\n' "exited before reporting that a window was created" >>"$logfile"
		return 1
	fi
	DEV_LAUNCH_RESULT=failed
	printf 'startup failed with status %s\n' "$DEV_LAUNCH_STATUS" >>"$logfile"
	return 1
}

dev_launch_child_running() {
	local process_state
	if ! kill -0 "$DEV_LAUNCH_PID" 2>/dev/null; then
		return 1
	fi
	process_state=$(ps -o stat= -p "$DEV_LAUNCH_PID" 2>/dev/null) || return 1
	set -- $process_state
	case "${1:-Z}" in
	Z*) return 1 ;;
	esac
	return 0
}

dev_launch_drain() {
	local logfile=$1 line
	trap - EXIT
	while IFS= read -r line <&9; do
		printf '%s\n' "$line"
		printf '%s\n' "$line" >>"$logfile"
	done
}

dev_launch_terminate() {
	if [ "$DEV_LAUNCH_ACTIVE" -eq 1 ]; then
		exec 9<&-
		DEV_LAUNCH_ACTIVE=0
	fi
	if [ -n "$DEV_LAUNCH_PID" ]; then
		kill "$DEV_LAUNCH_PID" 2>/dev/null || true
		wait "$DEV_LAUNCH_PID" 2>/dev/null || true
		DEV_LAUNCH_PID=
	fi
	if [ -n "$DEV_LAUNCH_DRAIN" ]; then
		kill "$DEV_LAUNCH_DRAIN" 2>/dev/null || true
		wait "$DEV_LAUNCH_DRAIN" 2>/dev/null || true
		DEV_LAUNCH_DRAIN=
	fi
}

dev_launch_stop() {
	dev_launch_terminate
}
