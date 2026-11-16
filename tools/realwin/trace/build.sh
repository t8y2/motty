#!/bin/bash
# Build a windows/amd64 Motty with timing probes, WITHOUT touching this checkout or ../viva.
# Both trees are copied to a scratch directory and patched there. Re-run it after either
# repository changes: ../viva moves under you, and a stale copy stops compiling.
#
#   trace/build.sh [OUT.exe]        default OUT: $TMPDIR/motty-trace.exe
#
# Probes (all inert unless the matching env var names a file):
#   VIVA_TRACE        window thread: key down/char, redraw post/begin/end, paint, Present
#   MOTTY_PUMP_TRACE  output drained by the UI thread, in bytes
#   MOTTY_SHELL_TRACE local shell: bytes read from the pty, writes to it
# Every line starts with milliseconds on one shared clock (epoch microseconds mod 1e9).
set -euo pipefail
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
motty=$(cd "$here/../../.." && pwd)
viva=${VIVA_DIR:-$motty/../viva}
out=${1:-${TMPDIR:-/tmp}/motty-trace.exe}
tmp=${TMPDIR:-/tmp}; tmp=${tmp%/}
scratch=$(mktemp -d "$tmp/motty-trace.XXXXXX")
trap '[ -n "${KEEP:-}" ] || rm -rf "$scratch"' EXIT

rsync -a --exclude=.git --exclude=.gocache --exclude=.gomodcache --exclude=build --exclude=website \
	--exclude=v42 --exclude=viva --exclude=private "$viva/" "$scratch/viva/"
rsync -a --exclude=.git --exclude=website --exclude=.private --exclude=build --exclude=dist "$motty/" "$scratch/motty/"
sed -i.bak "s#=> ../viva#=> $scratch/viva#" "$scratch/motty/go.mod" && rm "$scratch/motty/go.mod.bak"

python3 -I "$here/patch.py" "$scratch/viva" "$scratch/motty"

(cd "$scratch/motty" && GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags=-H=windowsgui -o "$out" ./cmd/motty)
echo "built $out ($(wc -c <"$out" | tr -d ' ') bytes)"
