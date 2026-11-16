# The README's hero shot (assets/readme-hero.png, below the readme-hero.svg
# banner): the file panel, the shell and
# the assistant on one connection, the way the front page describes it. Real
# classes over the loopback fixture; only the assistant's conversation is
# authored, because a real one would need a model and a key.
#
#   (cd tools/e2esshd && go build -o /tmp/e2esshd .) && /tmp/e2esshd 2222 &
#   export SFTP_ROOT=<the "sftp root" path e2esshd prints>
#   python3 tools/e2e/hero_fixture.py "$SFTP_ROOT"
#   home=$(mktemp -d); mkdir -p "$home/motty-data"
#   go run ./tools/e2e/heroseed "$home/motty-data/ai.db" "$SFTP_ROOT/logs/app.log"
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   sed "s|\$SFTP_ROOT|$SFTP_ROOT|g" tools/e2e/readme_hero.txt > /tmp/readme-hero.txt
#   MOTTY_LANG=en python3 tools/headless_drive.py "$home" /tmp/readme-hero.txt
#
# The substitution is done with sed rather than left to the shell: the driver
# sends each line to the app as typed, so $SFTP_ROOT would reach zsh unexpanded
# and the shell would cd to a directory named after the variable.
#
# The shot is 1560x975 points (2x = 3120x1950). The width is not decoration: the
# fixture's log lines run to 80 characters, and a narrower terminal wraps them,
# which reads as a broken log rather than a busy one.
#
# The seed has to land in <home>/motty-data/ai.db, not in an MOTTY_DATA_DIR of
# its own: headless_drive.py sets MOTTY_DATA_DIR to <home>/motty-data and the
# app reads the conversation from there.
#
# The host is saved rather than typed into the search box because the tab title
# is the host's label; connecting ad hoc would title the tab with the address.
# The host dialog is filled by geometry because its fields carry no distinct
# labels, so those coordinates belong to the 1560x975 window and move with it.
# The assistant's target picker leaves its dismiss overlay up, so it is closed
# by clicking that overlay before the conversation history is opened; restoring
# a conversation resets the target, so the target is chosen after the restore.
#
# `layout` reports the two panel dividers as crossings: they are the hit areas
# that resize the panels, not text over text.
resize 1560 975
appearance dark
settle 600
click "New Host"
settle 700
click 600 307
type staging-web
click 600 377
type 127.0.0.1
click 560 447
type deploy
click 1050 377
select 0 2
type 2222
click 624 587
settle 500
click "Ubuntu"
settle 400
click "Add Host"
settle 1000
dblclick 600 160
waitfor "Trust" 5000
click "Trust and Save"
waitfor "Password" 5000
type secret
key enter
settle 2500
click id:sftp/sftp-open
settle 1500
click id:ai-panel/ai-toggle
settle 1200
click id:ai-panel/ai-history
settle 900
click 1300 118
settle 1500
click id:ai-panel/ai-target
settle 600
click id:ai-panel/ai-targets/#1
settle 500
click id:ai-panel/ai-target-dismiss
settle 500
click 700 400
settle 400
type cd "$SFTP_ROOT" && export PS1='\[\e[1;32m\]deploy@staging-web\[\e[0m\]:\[\e[1;34m\]~/app\[\e[0m\]$ ' && clear
key enter
settle 900
type ls --color=always
key enter
settle 700
type ./scripts/logs.sh 38
key enter
settle 1200
shot /tmp/readme-hero.png
layout
