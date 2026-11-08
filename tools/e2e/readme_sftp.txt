# The README's file-panel shot (assets/readme-sftp.png): the SFTP panel beside
# the shell on one connection, both showing the same deploy tree.
#
#   (cd tools/e2esshd && go build -o /tmp/e2esshd .) && /tmp/e2esshd 2224 &
#   export SFTP_ROOT=<the "sftp root" path e2esshd prints>
#   python3 tools/e2e/hero_fixture.py "$SFTP_ROOT"
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   sed "s|\$SFTP_ROOT|$SFTP_ROOT|g" tools/e2e/readme_sftp.txt > /tmp/readme-sftp.txt
#   MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" /tmp/readme-sftp.txt
#
# The substitution is done with sed because the driver sends each line to the
# app as typed: $SFTP_ROOT would reach zsh unexpanded.
#
# The shell is moved into the fixture root and cleared before the listing. A
# bare `ls` in the login directory would print this computer's own home -- temp
# files, editor sockets, the works -- which is both unrelated to the picture and
# someone's private state.
#
# The panel is opened before the prompt is set, and the terminal is then
# clicked: opening the panel takes the focus away from it, and typing with no
# focused text field is an error, not a keystroke.
#
# The prompt is set so the terminal half shows the same build@ci-runner account
# the monitor shot uses, not this computer's own.
resize 1560 975
appearance dark
settle 600
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
shot /tmp/readme-sftp.png
layout
