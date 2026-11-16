#!/bin/bash
# Convert a point in a screenshot saved by `realwin.sh shot` into the coordinates a scenario uses.
#   locate.sh SHOT_X SHOT_Y [WINDOW_X WINDOW_Y [SHOT_WIDTH]]
# Screenshots are scaled to SHOT_WIDTH (default 1440) from the physical desktop (2880 wide here).
# The window sits at WINDOW_X,WINDOW_Y after `place` (default 200,100). Prints the `clickw` line.
set -euo pipefail
sx=${1:?shot x}; sy=${2:?shot y}; wx=${3:-200}; wy=${4:-100}; sw=${5:-1440}
python3 -I - "$sx" "$sy" "$wx" "$wy" "$sw" <<'PY'
import sys
sx, sy, wx, wy, sw = map(float, sys.argv[1:])
physical_width, scale = 2880.0, 2.0  # this machine: 2880x1800 at 200%
k = physical_width / sw
px, py = sx * k, sy * k
print(f"physical {px:.0f},{py:.0f}   click {px:.0f} {py:.0f}   clickw {(px - wx) / scale:.0f} {(py - wy) / scale:.0f}")
PY
