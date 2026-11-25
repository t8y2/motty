# The README's light port-forwarding shot (assets/readme-forward-light.png):
# local and proxy rules through saved hosts, in the light appearance.
#
#   go build -tags vivahead -o /tmp/motty-head ./cmd/motty
#   MOTTY_LANG=en python3 tools/headless_drive.py "$(mktemp -d)" tools/e2e/readme_forward_light.txt
#
# The host catalog is the same fixture as readme_hosts.txt, so the rules point at
# services that picture already lists instead of invented addresses. Each rule
# names a service on the host it travels through: databases and caches on
# db-primary and cache-01, dashboards on edge-proxy, the registry and CI on
# build-runner, the site itself on the staging-web loopback.
#
# The table is filled to the foot of the card: a handful of rows leaves most of
# the page empty and the picture reads as a screenshot of a dialog rather than of
# the feature. Sixteen rules is what a machine lending out its services
# accumulates; the list has to look worked in, not staged.
#
# Two rule kinds besides local: a SOCKS5 and an HTTP CONNECT rule, whose
# destination cell names the kind instead of an address. No remote forward: its
# listener cell carries the "Remote · " prefix and is cut off at this width, and
# a clipped address in the README reads as a defect rather than as the feature.
#
# Light is chosen in Settings rather than with the appearance line: Motty opens
# pinned to dark and ignores the system appearance until the reader asks.
#
# Where a rule travels is chosen in the host picker, not left on the default:
# every rule in the picture names the host whose service it actually reaches.
# The picker's rows are clicked by identity because their order is the catalog's.
#
# The dialog's fields are clicked at their centres. The proxy kinds keep the
# dialog's height, so the field rows stay where the local form has them; the
# remote kind is shorter and moves every row, which is why a rule's mode is
# chosen before its fields are filled.
#
# Nothing is started: a started rule would bind these loopback ports on the
# machine taking the picture.
resize 1560 975
settle 600
click "Settings"
settle 900
click "Appearance"
settle 600
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
click 780 445
settle 400
click id:forwards/forward-host-item/#2
settle 400
click 780 387
type postgres
click 588 561
key cmd+a
type 15432
click 780 561
type 10.0.2.21
click 971 561
key cmd+a
type 5432
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#2
settle 400
click 780 387
type mysql
click 588 561
key cmd+a
type 13306
click 780 561
type 10.0.2.21
click 971 561
key cmd+a
type 3306
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#3
settle 400
click 780 387
type redis
click 588 561
key cmd+a
type 16379
click 780 561
type 10.0.2.31
click 971 561
key cmd+a
type 6379
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#3
settle 400
click 780 387
type minio
click 588 561
key cmd+a
type 19000
click 780 561
type 10.0.2.31
click 971 561
key cmd+a
type 9000
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#0
settle 400
click 780 387
type web
click 588 561
key cmd+a
type 8081
click 780 561
type 10.0.1.11
click 971 561
key cmd+a
type 80
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#1
settle 400
click 780 387
type mail
click 588 561
key cmd+a
type 12525
click 780 561
type 10.0.1.12
click 971 561
key cmd+a
type 25
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#1
settle 400
click 780 387
type metrics
click 588 561
key cmd+a
type 19100
click 780 561
type 10.0.1.12
click 971 561
key cmd+a
type 9100
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#5
settle 400
click 780 387
type grafana
click 588 561
key cmd+a
type 13000
click 780 561
type 10.0.0.5
click 971 561
key cmd+a
type 3000
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#5
settle 400
click 780 387
type kibana
click 588 561
key cmd+a
type 15601
click 780 561
type 10.0.0.5
click 971 561
key cmd+a
type 5601
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#4
settle 400
click 780 387
type registry
click 588 561
key cmd+a
type 15000
click 780 561
type 10.0.3.41
click 971 561
key cmd+a
type 5000
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#4
settle 400
click 780 387
type jenkins
click 588 561
key cmd+a
type 18080
click 780 561
type 10.0.3.41
click 971 561
key cmd+a
type 8080
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#4
settle 400
click 780 387
type loki
click 588 561
key cmd+a
type 13100
click 780 561
type 10.0.3.41
click 971 561
key cmd+a
type 3100
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#5
settle 400
click 843 503
settle 400
click 780 387
type browser
click 588 561
key cmd+a
type 1080
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#5
settle 400
click 969 503
settle 400
click 780 387
type https-proxy
click 588 561
key cmd+a
type 8888
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#6
settle 400
click 780 387
type docs
click 588 561
key cmd+a
type 14000
click 780 561
type 127.0.0.1
click 971 561
key cmd+a
type 8080
settle 300
click "Add Forward"
settle 900
click "New Forward"
settle 800
click 780 445
settle 400
click id:forwards/forward-host-item/#6
settle 400
click 780 387
type api
click 588 561
key cmd+a
type 18000
click 780 561
type 127.0.0.1
click 971 561
key cmd+a
type 8081
settle 300
click "Add Forward"
settle 1200
shot /tmp/readme-forward-light.png
layout
