# The README's host-list shot (assets/readme-hosts.png). The catalog it imports
# is a fixture of demo hosts, not anyone's real network: the file is a normal
# Motty host catalog, so the same path a reader uses to move a machine between
# computers is the one that builds this picture.
#
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" tools/e2e/readme_hosts.txt
#
# The file picker is answered by the openfiles line, which must come before the
# click that opens it: the driver queues the answer for the next dialog.
#
# The list view is chosen because it shows every field at once -- group heading,
# label, account, address, tags and connection state -- where the card view
# drops the tags into the card body.
resize 1560 975
appearance dark
settle 600
click id:shell/import-menu
settle 600
openfiles tools/e2e/fixtures/readme_hosts_catalog.json
click "Import Catalog"
settle 1500
click "List view"
settle 900
shot /tmp/readme-hosts.png
layout
