# Source this file. Helpers for driving the physical Windows machine.
#
# Endpoint comes from MOTTY_WIN_REAL_HOST / MOTTY_WIN_REAL_USER in
# tools/realwin/local.sh, which is git-ignored.
#
# An SSH login on Windows lives in session 0 and cannot see the desktop. Anything that
# touches windows, input or the screen therefore runs as a one-shot scheduled task in
# the user's interactive session (rw_desktop). Plain commands run over SSH (rw_ssh).
here=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]:-$0}")" && pwd)
if [ -f "$here/local.sh" ]; then . "$here/local.sh"; fi
: "${MOTTY_WIN_REAL_HOST:?set MOTTY_WIN_REAL_HOST (see tools/realwin/local.example.sh)}"
: "${MOTTY_WIN_REAL_USER:?set MOTTY_WIN_REAL_USER}"
RW_TARGET="$MOTTY_WIN_REAL_USER@$MOTTY_WIN_REAL_HOST"
RW_SSH_OPTS=(-o BatchMode=yes -o ConnectTimeout=10)
# Remote work directory, relative to the user's profile. Everything we create lives here.
RW_DIR='mt-probe'
RW_HOME="C:/Users/$MOTTY_WIN_REAL_USER"
RW_WORK="$RW_HOME/$RW_DIR"
RW_WORK_WIN="C:\\Users\\$MOTTY_WIN_REAL_USER\\$RW_DIR"

# rw_ssh CMD...   run a cmd.exe command line; output converted from GBK
rw_ssh() { ssh "${RW_SSH_OPTS[@]}" "$RW_TARGET" "$@" 2>&1 | iconv -f GBK -t UTF-8 2>/dev/null || true; }

# rw_put LOCAL REMOTE_NAME   copy a file into the work directory
rw_put() {
	rw_ssh "if not exist $RW_WORK_WIN mkdir $RW_WORK_WIN" >/dev/null
	scp "${RW_SSH_OPTS[@]}" "$1" "$RW_TARGET:$RW_WORK/$2" >/dev/null || { echo "rw_put: scp failed for $1" >&2; return 1; }
}

# rw_get REMOTE_NAME LOCAL   copy a file out of the work directory
rw_get() { scp "${RW_SSH_OPTS[@]}" "$RW_TARGET:$RW_WORK/$1" "$2" >/dev/null || { echo "rw_get: scp failed for $1" >&2; return 1; }; }

# rw_ps SCRIPT.ps1 [args...]   run a PowerShell file over SSH (no desktop access)
rw_ps() {
	local script=$1; shift
	rw_put "$script" "$(basename "$script")" || return 1
	rw_ssh "powershell -NoProfile -ExecutionPolicy Bypass -File $RW_WORK_WIN\\$(basename "$script") $*"
}

# rw_desktop SCRIPT.ps1 [args...]   run a PowerShell file in the interactive desktop session
# and wait for it. Needs remote/itask.ps1, which rw_setup installs.
rw_desktop() {
	local script=$1; shift
	rw_put "$script" "$(basename "$script")" || return 1
	rw_ssh "powershell -NoProfile -ExecutionPolicy Bypass -File $RW_WORK_WIN\\itask.ps1 -Script $RW_WORK_WIN\\$(basename "$script") -ScriptArgs \"$*\""
}

# rw_setup   install the helper scripts in remote/ (run once, safe to repeat)
rw_setup() {
	for f in "$here"/remote/*.ps1; do rw_put "$f" "$(basename "$f")" || return 1; done
	echo "installed $(ls "$here"/remote/*.ps1 | wc -l | tr -d ' ') helpers in $RW_WORK_WIN"
}
