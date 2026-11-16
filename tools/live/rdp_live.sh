#!/bin/bash
# Live tests of internal/desktop/rdp against real RDP servers (see
# docs/remote-desktop.md). The tests skip without MOTTY_RDP_ADDR.
#
#   tools/live/rdp_live.sh docker    xrdp in a throwaway container (motty-e2e-rdp, port
#                               3389 on loopback, user motty, password motty-pw);
#                               the container and its image are removed afterwards.
#                               MOTTY_RDP_SECURITY_LAYER=rdp starts xrdp with
#                               standard RDP security only and runs
#                               TestLiveStandardSecurity instead of the TLS tests.
#   tools/live/rdp_live.sh windows USER PASSWORD
#                               the physical Windows machine (tools/realwin; run
#                               `realwin.sh setup` once first): the test binary
#                               runs ON it against localhost:3389, since the port is
#                               not reachable from outside. The account must be
#                               allowed to sign in over Remote Desktop; a picture
#                               of each stage is left in the machine's mt-probe
#                               directory as live-*.png. A first sign-in builds the
#                               profile and takes a minute; a user already signed
#                               in on the console is asked first, MOTTY_RDP_CLICK
#                               answers it. MOTTY_RDP_PASTE_DIR (a folder on the
#                               machine the account may write to) also runs the
#                               file test: files copied by the test are pasted in
#                               Explorer and compared by SHA-256.
#
# DOCKER_BUILD_PROXY sets the proxy apt uses inside the Docker build.
set -eu
cd "$(dirname "$0")/../.."
case "${1:-}" in
docker)
	trap 'docker rm -f motty-e2e-rdp >/dev/null 2>&1; docker rmi motty-e2e-rdp:latest >/dev/null 2>&1' EXIT
	docker build --platform linux/amd64 --build-arg APT_PROXY="${DOCKER_BUILD_PROXY:-http://host.docker.internal:7897}" -t motty-e2e-rdp:latest tools/live/rdp
	security_layer="${MOTTY_RDP_SECURITY_LAYER:-}"
	docker run -d --name motty-e2e-rdp --platform linux/amd64 -e MOTTY_RDP_SECURITY_LAYER="$security_layer" -p 127.0.0.1:3389:3389 motty-e2e-rdp:latest
	sleep 6
	# The standard-security server refuses TLS, so only the test that asks for
	# standard security applies to it; the others need the default TLS/NLA layer.
	run=Live
	if [ "$security_layer" = rdp ]; then run=LiveStandardSecurity; fi
	MOTTY_RDP_ADDR=127.0.0.1:3389 MOTTY_RDP_USER=motty MOTTY_RDP_PASSWORD=motty-pw MOTTY_RDP_PNG="${MOTTY_RDP_PNG:-}" \
		MOTTY_RDP_SECURITY="$security_layer" go test ./internal/desktop/rdp -run "$run" -v -count=1
	;;
windows)
	user=${2:?user} password=${3:?password}
	GOOS=windows GOARCH=amd64 go test -c -o /tmp/rdp-live.exe ./internal/desktop/rdp
	. tools/realwin/lib.sh
	b64() { printf %s "$1" | base64 | tr -d '\n'; }
	tmpdir=$(mktemp -d); script="$tmpdir/run-live.ps1"
	cat >"$script" <<'PS1'
param([string]$UserB64, [string]$PasswordB64, [string]$ClickB64, [string]$PasteDirB64)
function Dec([string]$s) { if (-not $s) { return "" }; [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($s)) }
$work = Join-Path $env:USERPROFILE "mt-probe"
$env:MOTTY_RDP_ADDR = "127.0.0.1:3389"
$env:MOTTY_RDP_USER = Dec $UserB64
$env:MOTTY_RDP_PASSWORD = Dec $PasswordB64
$env:MOTTY_RDP_PNG = Join-Path $work "live"
$env:MOTTY_RDP_CLICK = Dec $ClickB64
$env:MOTTY_RDP_PASTE_DIR = Dec $PasteDirB64
$p = Start-Process -FilePath (Join-Path $work "rdplive.exe") -WorkingDirectory $work `
	-ArgumentList '-test.v','-test.run','Live','-test.count=1' `
	-RedirectStandardOutput (Join-Path $work "rdplive.log") `
	-RedirectStandardError (Join-Path $work "rdplive.err") -WindowStyle Hidden -PassThru
"started pid=$($p.Id); log is $work\rdplive.log"
PS1
	rw_put /tmp/rdp-live.exe rdplive.exe
	rw_desktop "$script" "-UserB64 '$(b64 "$user")' -PasswordB64 '$(b64 "$password")' -ClickB64 '$(b64 "${MOTTY_RDP_CLICK:-}")' -PasteDirB64 '$(b64 "${MOTTY_RDP_PASTE_DIR:-}")'"
	rm -rf "$tmpdir"
	echo "fetch the log and pictures with: . tools/realwin/lib.sh && rw_get rdplive.log /tmp/rdplive.log"
	;;
*)
	echo "usage: $0 docker | windows USER PASSWORD" >&2
	exit 2
	;;
esac
