# The README's light-appearance shot (assets/readme-hosts-light.png): the same
# host catalog as readme_hosts.txt, drawn with the light palette.
#
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" tools/e2e/readme_hosts_light.txt
#
# The appearance is chosen in Settings, not by the `appearance` line alone: Motty
# opens pinned to dark and ignores the system appearance until the reader asks
# for another look, so `appearance light` on its own changes nothing visible.
# The light palette comes from Settings > General > Appearance > Light.
#
# The host catalog is the same fixture as the dark shot, so the pair reads as one
# interface in two looks rather than two different products.
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
click "List view"
settle 900
shot /tmp/readme-hosts-light.png
layout
