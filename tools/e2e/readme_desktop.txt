# The README's remote-desktop shot (assets/readme-desktop.png): a VNC host open
# in its own tab. The picture comes from tools/e2e/vncfixture, a loopback RFB
# server that draws a fixed desktop, so the shot needs no container runtime, no
# real VNC server and no fonts on the machine running it.
#
#   go build -o /tmp/vncfixture ./tools/e2e/vncfixture && /tmp/vncfixture 5901 &
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" tools/e2e/readme_desktop.txt
#
# The fixture speaks RFB 3.8 with no authentication and answers every update
# request with the whole desktop as a Raw rectangle. That is the smallest
# picture a reader can recognise as a remote screen; the encodings Motty
# negotiates -- Hextile, ZRLE and the rest -- are covered by the protocol tests,
# not by this shot.
#
# The desktop is 1024x640 inside a much larger window, so Fit is chosen to scale
# it up; at 100% it sits in the corner of an empty pane and reads as a failure.
resize 1560 975
appearance dark
settle 600
click "New Host"
settle 700
click "VNC"
settle 600
click 600 269
type build-runner
click 600 339
type 127.0.0.1
click 1050 339
key cmd+a
type 5901
click "Add Host"
settle 1000
dblclick 600 160
settle 5000
click "Fit"
settle 1500
shot /tmp/readme-desktop.png
layout
