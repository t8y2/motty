#!/bin/bash
# Drive and measure Motty on the physical Windows machine. See tools/realwin/README.md.
#
#   realwin.sh setup               install the helper scripts on the machine (once)
#   realwin.sh status              GPU, OS, running probe, work directory
#   realwin.sh build               build the instrumented motty (windows/amd64) locally
#   realwin.sh push [EXE]          stop the probe, upload a build as motty-trace.exe
#   realwin.sh start [--keep]      (re)start the probe with an empty data dir and fresh logs
#   realwin.sh stop                stop the probe only
#   realwin.sh run SCENARIO        play scenarios/SCENARIO.steps against the probe window
#   realwin.sh shot [NAME]         screenshot to ./out/NAME.png
#   realwin.sh fetch [DIR]         copy trace.log pump.log shell.log marks.log to DIR (default ./out/run)
#   realwin.sh report [DIR] [--from MARK --to MARK]
#   realwin.sh clean               remove everything this tool put on the machine
#
# The probe is a separate binary with its own data directory; the user's installed Motty,
# hosts and settings are never touched. An already-running probe is replaced, nothing else.
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$here/lib.sh"
out="$here/out"; mkdir -p "$out"
cmd=${1:-}; shift || true

case "$cmd" in
setup) rw_setup && rw_put "$here/remote/itask.ps1" itask.ps1 ;;
status) rw_desktop "$here/remote/status.ps1" >/dev/null; rw_ps "$here/remote/status.ps1" ;;
build) "$here/trace/build.sh" "${1:-$out/motty-trace.exe}" ;;
push)
	# A running exe cannot be overwritten; stop the probe (only it) first.
	rw_ps "$here/remote/stop.ps1" >/dev/null
	rw_put "${1:-$out/motty-trace.exe}" motty-trace.exe && echo pushed ;;
start)
	flag=""; [ "${1:-}" = "--keep" ] && flag="-Keep"
	rw_desktop "$here/remote/launch.ps1" "$flag" ; sleep 4; rw_ps "$here/remote/status.ps1" | grep -E "process|FAILED" || echo "probe not running" ;;
stop) rw_ps "$here/remote/stop.ps1" ;;
run)
	name=${1:?scenario name, e.g. typing}; file="$here/scenarios/$name.steps"
	[ -f "$file" ] || { echo "no such scenario: $file"; ls "$here/scenarios"; exit 2; }
	rw_put "$file" "$name.steps" && rw_put "$here/remote/drive.ps1" drive.ps1 \
		&& rw_desktop "$here/remote/drive.ps1" "-Steps $RW_WORK_WIN\\$name.steps" ;;
shot)
	name=${1:-shot}
	rw_desktop "$here/remote/shot.ps1" "-Name $name" && rw_get "$name.png" "$out/$name.png" && echo "$out/$name.png" ;;
fetch)
	dir=${1:-$out/run}; mkdir -p "$dir"
	# The probe keeps its logs open for writing, so copy first and read the copy.
	rw_ssh "cd /d $RW_WORK_WIN && for %f in (trace pump shell marks) do if exist %f.log copy /y %f.log %f.copy >nul" >/dev/null
	for f in trace pump shell marks; do rw_get "$f.copy" "$dir/$f.log" 2>/dev/null || true; done
	wc -l "$dir"/*.log ;;
report)
	dir=${1:-$out/run}; shift || true
	python3 -I "$here/trace/analyze.py" "$dir" "$@" ;;
clean) rw_ps "$here/remote/stop.ps1"; rw_ssh "rmdir /s /q $RW_WORK_WIN" ; echo cleaned ;;
*) sed -n '2,20p' "$0"; exit 2 ;;
esac
