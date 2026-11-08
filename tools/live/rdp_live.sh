#!/bin/bash
# Live tests of internal/desktop/rdp against real RDP servers (see
# docs/remote-desktop.md). The tests skip without MOTTY_RDP_ADDR.
#
#   tools/live/rdp_live.sh docker    xrdp in a throwaway container (motty-e2e-rdp, port
#                               3389 on loopback, user motty, password motty-pw);
#                               the container and its image are removed afterwards
#   tools/live/rdp_live.sh windows USER PASSWORD
#                               a Windows test machine (../viva/tools/testenv): the
#                               test binary runs ON it against localhost:3389, since
#                               the port is not reachable from outside. The account
#                               must be allowed to sign in over Remote Desktop; a
#                               picture of each stage is left on its Desktop as
#                               live-*.png. A first sign-in builds the profile and
#                               takes a minute; a user already signed in on the
#                               console is asked first, MOTTY_RDP_CLICK answers it.
#                               MOTTY_RDP_PASTE_DIR (a folder on the machine the
#                               account may write to) also runs the file test:
#                               files copied by the test are pasted in Explorer
#                               and compared by SHA-256.
#
# DOCKER_BUILD_PROXY sets the proxy apt uses inside the Docker build.
set -eu
cd "$(dirname "$0")/../.."
case "${1:-}" in
docker)
	trap 'docker rm -f motty-e2e-rdp >/dev/null 2>&1; docker rmi motty-e2e-rdp:latest >/dev/null 2>&1' EXIT
	docker build --platform linux/amd64 --build-arg APT_PROXY="${DOCKER_BUILD_PROXY:-http://host.docker.internal:7897}" -t motty-e2e-rdp:latest tools/live/rdp
	docker run -d --name motty-e2e-rdp --platform linux/amd64 -p 127.0.0.1:3389:3389 motty-e2e-rdp:latest
	sleep 6
	MOTTY_RDP_ADDR=127.0.0.1:3389 MOTTY_RDP_USER=motty MOTTY_RDP_PASSWORD=motty-pw MOTTY_RDP_PNG="${MOTTY_RDP_PNG:-}" \
		go test ./internal/desktop/rdp -run Live -v -count=1
	;;
windows)
	env=../viva/tools/testenv
	user=${2:?user} password=${3:?password}
	GOOS=windows GOARCH=amd64 go test -c -o /tmp/rdp-live.exe ./internal/desktop/rdp
	$env/upload.sh windows /tmp/rdp-live.exe 'C:\Users\Docker\Desktop\rdplive.exe'
	$env/exec.sh windows "\$env:MOTTY_RDP_ADDR='127.0.0.1:3389'; \$env:MOTTY_RDP_USER='$user'; \$env:MOTTY_RDP_PASSWORD='$password'; \$env:MOTTY_RDP_PNG='C:\\Users\\Docker\\Desktop\\live'; \$env:MOTTY_RDP_CLICK='${MOTTY_RDP_CLICK:-}'; \$env:MOTTY_RDP_PASTE_DIR='${MOTTY_RDP_PASTE_DIR:-}'; Start-Process -FilePath cmd.exe -ArgumentList '/c','cd /d C:\\Users\\Docker\\Desktop && rdplive.exe -test.v -test.run Live -test.count=1 > live.log 2>&1' -WindowStyle Hidden"
	echo "started; read C:\\Users\\Docker\\Desktop\\live.log on the machine"
	;;
*)
	echo "usage: $0 docker | windows USER PASSWORD" >&2
	exit 2
	;;
esac
