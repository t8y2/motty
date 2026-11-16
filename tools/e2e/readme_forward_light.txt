# The README's light port-forwarding shot (assets/readme-forward-light.png):
# local rules through a saved host, in the light appearance.
#
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" tools/e2e/readme_forward_light.txt
#
# The host catalog is the same fixture as readme_hosts.txt, so the rules point at
# services that picture already lists instead of invented addresses.
#
# Light is chosen in Settings rather than with the appearance line: Motty opens
# pinned to dark and ignores the system appearance until the reader asks.
#
# Three rules rather than one: a single row leaves most of the window empty and
# the card reads as a screenshot of a dialog rather than of the feature.
#
# The dialog's inputs are clicked at their centres, not at the left edge of the
# label text that the texts command reports: a click on the boundary misses the
# input and the next type lands in whichever field still has focus.
#
# Nothing is started: a started rule would bind these loopback ports on the
# machine taking the picture.
resize 1560 975
settle 600
click "Settings"
settle 900
click "Light"
settle 900
click "Hosts"
settle 900
click id:shell/import-menu
settle 600
openfiles tools/e2e/fixtures/readme_hosts_catalog.json
click "Import Catalog"
settle 1500
click "Port Forwarding"
settle 900
click "New Forward"
settle 800
click 780 402
type postgres
click 588 576
key cmd+a
type 15432
click 780 576
type 10.0.2.21
click 971 576
key cmd+a
type 5432
settle 400
click "Add Forward"
settle 1000
click "New Forward"
settle 800
click 780 402
type redis
click 588 576
key cmd+a
type 16379
click 780 576
type 10.0.2.31
click 971 576
key cmd+a
type 6379
settle 400
click "Add Forward"
settle 1000
click "New Forward"
settle 800
click 780 402
type web
click 588 576
key cmd+a
type 8081
click 780 576
type 10.0.1.11
click 971 576
key cmd+a
type 80
settle 400
click "Add Forward"
settle 1200
shot /tmp/readme-forward-light.png
layout
