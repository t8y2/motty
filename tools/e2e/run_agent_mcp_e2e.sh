#!/bin/sh
# End to end for `motty mcp-serve`, the MCP server that needs no window, against
# the loopback SSH fixture:
#
#   tools/e2e/run_agent_mcp_e2e.sh
#
# The window side (the headless build) saves two hosts, opens one to agents, trusts
# the fixture's key and lets agents run commands; the real `motty mcp-serve` is then
# driven over stdio by agent_mcp_e2e.py; the window side shows and clears what the
# agent did. Everything stays on this machine: the server listens on 127.0.0.1
# only, and the run has its own HOME and data directory, so neither the real saved
# hosts nor the system's secret store are touched (no password is saved, so the
# vault is never opened).
set -e
cd "$(dirname "$0")/../.."

head=${MOTTY_HEAD:-/tmp/motty-head}
bin=${MOTTY_BIN:-/tmp/motty-bin}
server=/tmp/motty-e2esshd
go build -tags vivahead -o "$head" ./cmd/motty
go build -o "$bin" ./cmd/motty
(cd tools/e2esshd && go build -o "$server" .)

work=$(mktemp -d)
cleanup() {
	[ -n "${fixture_pid:-}" ] && kill "$fixture_pid" 2>/dev/null
	rm -rf "$work"
}
trap cleanup EXIT
mkdir -p "$work/home/.ssh"
ssh-keygen -q -t ed25519 -N "" -C agent-e2e -f "$work/home/.ssh/id_ed25519"

E2E_EXTRA_PUBKEY="$work/home/.ssh/id_ed25519.pub" E2E_ALLOW_EXEC=1 "$server" 2248 >"$work/fixture.log" 2>&1 &
fixture_pid=$!
i=0
while ! grep -q "^listening on" "$work/fixture.log" 2>/dev/null; do
	i=$((i + 1))
	if [ "$i" -gt 100 ]; then cat "$work/fixture.log"; exit 1; fi
	sleep 0.05
done

drive() { # script
	MOTTY_HEAD="$head" MOTTY_LANG=en python3 tools/headless_drive.py "$work/home" "$1"
}

echo "== window: hosts, trust, mode =="
drive tools/e2e/agent_mcp_setup_e2e.txt >"$work/setup.log" 2>&1 || { cat "$work/setup.log"; exit 1; }
[ "$(sqlite3 "$work/home/motty-data/ai.db" "select value from settings where key='ai.mcp.stdio.mode'")" = ask ] || { echo "the mode was not saved"; exit 1; }

echo "== mcp-serve over stdio =="
python3 tools/e2e/agent_mcp_e2e.py "$bin" "$work/home" "$work/home/motty-data"

echo "== window: what the agent did =="
drive tools/e2e/agent_mcp_activity_e2e.txt >"$work/activity.log" 2>&1 || { cat "$work/activity.log"; exit 1; }
grep -q "agent-host  terminal_exec rm -rf /tmp/motty-agent-e2e — refused" "$work/activity.log" || { echo "the activity list lacks the refusal"; cat "$work/activity.log"; exit 1; }
grep -q "closed-host  terminal_exec echo nope — failed" "$work/activity.log" || { echo "the activity list lacks the call on the closed host"; exit 1; }
grep -q "terminal_exec echo [$][(][(]6[*]7[)][)]marker — ran" "$work/activity.log" || { echo "the activity list lacks the last command"; exit 1; }
if grep -q "42marker" "$work/activity.log"; then echo "output leaked into the log"; exit 1; fi
[ "$(sqlite3 "$work/home/motty-data/ai.db" "select count(*) from agent_audit")" = 0 ] || { echo "Clear did not empty the log"; exit 1; }
echo "agent MCP end to end verified"
