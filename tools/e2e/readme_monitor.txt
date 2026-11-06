# The README's split-terminal shot (assets/readme-monitor.png): two panes on one
# host with the resource monitor beside them. The readings are this computer's
# own, because the fixture runs the monitor's commands for real.
#
# The monitor needs the fixture to announce itself as macOS, which the stock
# binary does not; measuring a Linux box from the Overview tab works, but the
# per-core and disk sections only fill in for a Mac. See the recipe at the top of
# tools/e2e/monitor_macos_e2e.txt, which is the same preparation:
#
#   cp -R tools/e2esshd /tmp/e2esshd-darwin
#   sed -i '' 's|SSH-2.0-GopE2E|SSH-2.0-OpenSSH_9.9 Darwin|' /tmp/e2esshd-darwin/main.go
#   (cd /tmp/e2esshd-darwin && go build -o /tmp/e2esshd-darwin-bin .)
#   E2E_ALLOW_EXEC=1 /tmp/e2esshd-darwin-bin 2223 &
#   export SFTP_ROOT=<the "sftp root" path that fixture prints>
#   python3 tools/e2e/hero_fixture.py "$SFTP_ROOT"
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   sed "s|\$SFTP_ROOT|$SFTP_ROOT|g" tools/e2e/readme_monitor.txt > /tmp/readme-monitor.txt
#   MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" /tmp/readme-monitor.txt
#
# The substitution is done with sed because the driver sends each line to the
# app as typed: $SFTP_ROOT would reach zsh unexpanded.
#
# The prompt is set twice -- once per pane -- because a split opens a new shell
# with the login prompt, and the fixture's login prompt carries this computer's
# account name. The picture must not.
#
# clear is a command of its own, not part of the same line as the exports: run
# together, the shell prints the echoed line above the cleared screen.
#
# Disk volumes and Hardware are collapsed by the last two clicks. Expanded, Disk
# volumes lists this computer's real volume names.
resize 1560 975
appearance dark
settle 600
click "Search"
type tester@127.0.0.1:2223
settle 400
click "Connect"
waitfor "Trust" 5000
click "Trust and Save"
waitfor "Password" 5000
type secret
key enter
settle 2500
type export LANG=en_US.UTF-8 PROMPT_SP='' PROMPT='build@ci-runner:~/app\$ '
key enter
settle 300
type cd "$SFTP_ROOT"
key enter
settle 300
type clear
key enter
settle 500
type ./scripts/logs.sh 7 logs/app.log | sed 's/ status=[0-9]*//; s/ ms=[0-9]*//'
key enter
settle 900
click id:shell/pane-split-horizontal
settle 1500
type export LANG=en_US.UTF-8 PROMPT_SP='' PROMPT='build@ci-runner:~/app\$ '
key enter
settle 300
type cd "$SFTP_ROOT"
key enter
settle 300
type clear
key enter
settle 500
type wc -l logs/app.log
key enter
settle 600
click id:shell/remote-stats
settle 4500
click 1360 500
settle 700
click 1360 709
settle 700
shot /tmp/readme-monitor.png
