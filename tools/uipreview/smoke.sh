#!/usr/bin/env bash
# Prove this machine can render motty without a window: build the headless
# entry, run one session from an empty home, and check the parts a screenshot
# needs. This is the gate in .private/specs/pr-ui-preview-screenshots.md, step
# 1: it answers "can the real binary take a picture here" before any CI work.
#
#   tools/uipreview/smoke.sh [--out DIR] [--size 1400x900] [--scale 1.25]
#                            [--timeout 180] [--keep] [--skip-build]
#                            [--binary PATH]
#
# Runs on Linux (the CI target) and on macOS, where the offscreen renderer is
# linked only with -tags vivahead. The Linux-only library and font checks are
# skipped with a note elsewhere.
#
# What it checks, in order:
#   1. the headless entry builds for this OS;
#   2. the process reaches "ok ready" with no display and no window;
#   3. the frame has text, controls and layout facts worth photographing;
#   4. the PNG is written at the size the app reported and is not blank;
#   5. quit leaves no process behind.
#
# It writes nothing outside --out (default /tmp/motty-smoke): home and data
# directory live under it, so every run starts from a first launch. --keep
# leaves that home in place for inspection; without it the home is removed at
# the end and the PNG, stdout and stderr stay.
set -uo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
out=/tmp/motty-smoke
size=1400x900
scale=1.25
run_timeout=180
keep=0
build=1
binary=

while [ $# -gt 0 ]; do
	case $1 in
	--out) out=$2; shift 2 ;;
	--size) size=$2; shift 2 ;;
	--scale) scale=$2; shift 2 ;;
	--timeout) run_timeout=$2; shift 2 ;;
	--keep) keep=1; shift ;;
	--skip-build) build=0; shift ;;
	--binary) binary=$2; build=0; shift 2 ;;
	-h | --help) sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	*) echo "smoke: unknown argument $1" >&2; exit 2 ;;
	esac
done

if ! [[ $size =~ ^[0-9]+x[0-9]+$ ]]; then
	echo "smoke: --size wants WIDTHxHEIGHT, got $size" >&2
	exit 2
fi

rm -rf "$out"
mkdir -p "$out/home" "$out/data"
home=$out/home
data=$out/data
bin=${binary:-$out/motty-head}
png=$out/first-run.png
stdout=$out/stdout.txt
stderr=$out/stderr.txt
commands=$out/commands.txt

failures=0
warnings=0
say() { printf '%s\n' "$*"; }
pass() { printf '  ok    %s\n' "$*"; }
warn() { printf '  warn  %s\n' "$*"; warnings=$((warnings + 1)); }
fail() { printf '  FAIL  %s\n' "$*"; failures=$((failures + 1)); }
section() { printf '\n%s\n' "$*"; }

# run_limited SECONDS COMMAND...: kill the command when it overruns. timeout
# and gtimeout are not everywhere (macOS ships neither), so fall back to a
# watchdog of our own.
run_limited() {
	local seconds=$1
	shift
	if command -v timeout >/dev/null 2>&1; then
		timeout "$seconds" "$@"
		return $?
	fi
	if command -v gtimeout >/dev/null 2>&1; then
		gtimeout "$seconds" "$@"
		return $?
	fi
	# A background job in a non-interactive shell reads /dev/null unless it is
	# redirected, so the child's stdin is spelled out: it is this script's own
	# stdin, which the caller pointed at the commands file.
	"$@" <&0 &
	local pid=$!
	(
		sleep "$seconds"
		kill -TERM "$pid" 2>/dev/null
		sleep 2
		kill -KILL "$pid" 2>/dev/null
	) >/dev/null 2>&1 &
	local dog=$!
	local status=0
	wait "$pid" || status=$?
	kill "$dog" 2>/dev/null
	return "$status"
}

# --- what this machine is ------------------------------------------------------

section "machine"
say "  $(uname -srm), bash $BASH_VERSION"
say "  $(go version 2>/dev/null || echo 'go: not found')"
(
	cd "$root"
	dirty=no
	[ -z "$(git status --porcelain 2>/dev/null)" ] || dirty=yes
	say "  repo $(git rev-parse --short HEAD 2>/dev/null || echo unknown) dirty=$dirty"
)

# The offscreen renderer draws text through the platform's engine. On Linux
# that engine is pango/cairo, opened with dlopen at the first label, so a
# missing library shows up as "load system text library" rather than as a
# blank picture.
if [ "$(uname -s)" = Linux ]; then
	section "libraries (Linux text engine)"
	if command -v ldconfig >/dev/null 2>&1; then
		for lib in libpango-1.0.so.0 libpangocairo-1.0.so.0 libcairo.so.2 libgobject-2.0.so.0 libfontconfig.so.1; do
			if ldconfig -p 2>/dev/null | grep -q -- "$lib"; then pass "$lib"; else fail "$lib missing (apt install libpango1.0-0 libcairo2 libfontconfig1)"; fi
		done
	else
		warn "no ldconfig; cannot check the text engine libraries"
	fi
	if command -v fc-list >/dev/null 2>&1; then
		cjk=$(fc-list :lang=zh 2>/dev/null | wc -l)
		if [ "$cjk" -gt 0 ]; then pass "$cjk Chinese font faces"; else warn "no Chinese fonts; a Chinese window will show tofu (install fonts-noto-cjk)"; fi
	else
		warn "no fc-list; cannot check Chinese fonts"
	fi
else
	section "libraries"
	say "  skipped: only meaningful on Linux"
fi

# --- build ---------------------------------------------------------------------

section "build"
if [ "$build" = 1 ]; then
	started=$(date +%s)
	if ! ( cd "$root" && go build -tags vivahead -o "$bin" ./cmd/motty ) >"$out/build.log" 2>&1; then
		fail "go build -tags vivahead ./cmd/motty"
		sed 's/^/        /' "$out/build.log" | tail -20
		printf '\nverdict: cannot build; nothing else was tried\n'
		exit 2
	fi
	pass "built in $(( $(date +%s) - started ))s, $(du -h "$bin" | cut -f1)"
else
	[ -x "$bin" ] || { fail "no binary at $bin and --skip-build was given"; exit 2; }
	pass "using $bin"
fi

# --- run -----------------------------------------------------------------------

section "run"
cat >"$commands" <<EOF
size
settle 50
shot $png
texts
hits
layout
quit
EOF

started=$(date +%s)
(
	export HOME=$home MOTTY_DATA_DIR=$data VIVA_HEADLESS=1
	run_limited "$run_timeout" "$bin" <"$commands" >"$stdout" 2>"$stderr"
)
status=$?
elapsed=$(( $(date +%s) - started ))
say "  exit $status after ${elapsed}s; replies in $stdout"

ready=$(head -1 "$stdout")
case $ready in
"ok ready "*) pass "reached: $ready" ;;
*) fail "no 'ok ready' on the first line; got '${ready:-nothing}'" ;;
esac
if [ "$status" = 124 ] || [ "$status" = 137 ] || [ "$status" = 143 ]; then fail "had to be killed after ${run_timeout}s (see $stderr)"; fi
# The app logs its start and its normal exit on stderr; anything else is worth
# quoting, because a fatal error only appears there (headless mode has no
# message box).
if grep -qvE 'motty: (started|window closed|exiting normally)' "$stderr" 2>/dev/null; then
	warn "stderr has more than the usual start and exit lines:"
	grep -vE 'motty: (started|window closed|exiting normally)' "$stderr" | sed -n '1,8p' | sed 's/^/        /'
fi

# The replies a screenshot pipeline depends on. texts and hits answer with
# their data lines and a bare "ok", so the counts come from the line prefixes
# the protocol defines. A blank frame still has a size, so "size" alone proves
# nothing: these two say the layout produced content.
texts=$(grep -c '^x=' "$stdout")
hits=$(grep -c '^id=' "$stdout")
problems=$(sed -n 's/^ok \([0-9]\{1,\}\) problems$/\1/p' "$stdout" | tail -1)
if [ "$texts" -gt 5 ]; then pass "$texts painted texts"; else fail "texts: got $texts"; fi
if [ "$hits" -gt 5 ]; then pass "$hits hit targets"; else fail "hits: got $hits"; fi
if [ "${problems:-0}" -gt 0 ]; then warn "$problems layout problems; run 'layout' by hand to read them"; else pass "no layout problems"; fi

# The hit ids are the tree paths a plan would target, not screen coordinates.
if grep -qE '^id=[A-Za-z][A-Za-z0-9/._-]* ' "$stdout"; then
	pass "hit ids are tree paths, e.g. $(grep -oE '^id=[A-Za-z][A-Za-z0-9/._-]*' "$stdout" | sed -n '2p' | cut -d= -f2)"
else
	fail "hit ids are not tree paths (a plan could not address them)"
fi

# --- picture -------------------------------------------------------------------

section "picture"
# The app decides the window size; --size only says what was expected.
read -r app_w app_h app_s <<<"$(sed -nE 's/^ok ([0-9]+)x([0-9]+) scale=([0-9.]+)$/\1 \2 \3/p' "$stdout" | tail -1)"
if [ -z "${app_w:-}" ]; then
	app_w=${size%x*}
	app_h=${size#*x}
	app_s=$scale
	warn "no size reply; assuming ${app_w}x${app_h} at ${app_s}x"
elif [ "${app_w}x${app_h}" != "$size" ]; then
	warn "app reports ${app_w}x${app_h}, --size said $size"
fi
device_width=$(awk -v w="$app_w" -v s="$app_s" 'BEGIN{printf "%d", (w*s)+0.5}')
device_height=$(awk -v h="$app_h" -v s="$app_s" 'BEGIN{printf "%d", (h*s)+0.5}')

if [ ! -f "$png" ]; then
	fail "no PNG at $png"
else
	# A proxy, not a pixel diff: IHDR gives the size; the decompressed IDAT
	# bytes say whether the frame has content. A flat black frame filters to
	# one or two distinct byte values, while any drawn text or chrome brings
	# hundreds. The text and hit counts above remain the real evidence.
	if detail=$(python3 - "$png" "$device_width" "$device_height" <<'PY'
import struct, sys, zlib
path, want_w, want_h = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
data = open(path, "rb").read()
if data[:8] != b"\x89PNG\r\n\x1a\n":
    sys.exit("not a PNG")
pos, idat, w, h = 8, b"", 0, 0
while pos < len(data):
    n = struct.unpack(">I", data[pos:pos + 4])[0]
    kind = data[pos + 4:pos + 8]
    chunk = data[pos + 8:pos + 8 + n]
    pos += 12 + n
    if kind == b"IHDR":
        w, h = struct.unpack(">II", chunk[:8])
    elif kind == b"IDAT":
        idat += chunk
    elif kind == b"IEND":
        break
if (w, h) != (want_w, want_h):
    sys.exit(f"size {w}x{h}, want {want_w}x{want_h}")
raw = zlib.decompress(idat)
seen = [0] * 256
for b in raw:
    seen[b] += 1
distinct = sum(1 for n in seen if n)
print(f"{w}x{h}, {len(data) // 1024} KiB, {distinct} distinct byte values")
if distinct < 32 or len(data) < 8000:
    sys.exit("blank frame: the renderer drew nothing")
PY
	); then
		pass "PNG $detail"
	else
		fail "PNG $detail (see $png)"
	fi
fi

# --- tidy ----------------------------------------------------------------------

section "tidy"
if pgrep -f "$bin" >/dev/null 2>&1; then
	fail "a process is still running"
else
	pass "no process left behind"
fi
if [ "$keep" = 0 ]; then
	rm -rf "$home" "$data"
	say "  removed home and data (--keep keeps them)"
fi

# --- verdict -------------------------------------------------------------------

section "verdict"
say "  artifacts: $png, $stdout, $stderr"
if [ "$failures" -gt 0 ]; then
	printf '  FAILED: %d failure(s), %d warning(s)\n' "$failures" "$warnings"
	printf '  The real-binary recorder is not viable here yet; read the failures above before\n'
	printf '  planning around it (see .private/specs/pr-ui-preview-screenshots.md, step 1).\n'
	exit 1
fi
printf '  PASS: %d warning(s)\n' "$warnings"
printf '  A headless run rendered on this machine. The recorder can use the real binary;\n'
printf '  keep this output as the smoke record.\n'
exit 0
