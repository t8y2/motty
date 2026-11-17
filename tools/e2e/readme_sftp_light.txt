# The README's light file-panel shot (assets/readme-sftp-light.png): the SFTP
# panel beside its shell, in the light appearance.
#
#   (cd tools/e2esshd && go build -o /tmp/e2esshd .) && /tmp/e2esshd 2224 &
#   export SFTP_ROOT=<the "sftp root" path e2esshd prints>
#   python3 tools/e2e/hero_fixture.py "$SFTP_ROOT"
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   sed "s|\$SFTP_ROOT|$SFTP_ROOT|g" tools/e2e/readme_sftp_light.txt > /tmp/readme-sftp-light.txt
#   MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" /tmp/readme-sftp-light.txt
#
# Light is chosen in Settings before connecting, not with the appearance line:
# Motty opens pinned to dark and ignores the system appearance until asked.
#
# The shell is moved into the fixture root and cleared before the listing, so
# neither this computer's home directory nor the command echo ends up in the
# picture. The prompt names the same build@ci-runner account the other shots use.
#
# The panel is opened before the prompt is set, and the terminal is then clicked,
# because opening the panel takes the focus away from it.
resize 1560 975
settle 600
click "Settings"
settle 900
click "Light"
settle 900
click "Hosts"
settle 900
click "Search"
type tester@127.0.0.1:2224
settle 400
click "Connect"
waitfor "Trust" 5000
click "Trust and Save"
waitfor "Password" 5000
type secret
key enter
settle 2500
click id:sftp/sftp-open
settle 1800
click 700 400
settle 400
type export PS1='build@ci-runner:~/app\$ '
key enter
settle 300
type cd "$SFTP_ROOT"
key enter
settle 400
type clear
key enter
settle 600
type ls --color=always
key enter
settle 900
shot /tmp/readme-sftp-light.png
layout
