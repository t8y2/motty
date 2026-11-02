.PHONY: dev run head check race race-workbench race-heavy race-rest

RACE_PACKAGES = \
	./cmd/motty \
	./internal/ai/agent \
	./internal/ai/cli \
	./internal/ai/gate \
	./internal/ai/llm \
	./internal/ai/mcp \
	./internal/ai/store \
	./internal/backup \
	./internal/cmdhistory \
	./internal/configstore \
	./internal/desktop \
	./internal/desktop/rdp \
	./internal/desktop/vnc \
	./internal/diag \
	./internal/gist \
	./internal/knownhosts \
	./internal/localshell \
	./internal/portforward \
	./internal/rdpcore \
	./internal/remotestats \
	./internal/monitoring \
	./internal/rfb \
	./internal/s3 \
	./internal/scpclient \
	./internal/selfupdate \
	./internal/serial \
	./internal/sessionlog \
	./internal/sftpclient \
	./internal/sshconn \
	./internal/telnet \
	./internal/term \
	./internal/vault \
	./internal/webdav \
	./internal/workbench \
	./internal/x11fwd

# How CI divides the list above. The workbench suite is half of the runtime and
# is sliced by test name into four shards; the heaviest remaining packages get a
# job of their own.
RACE_WORKBENCH = ./internal/workbench
RACE_HEAVY = \
	./internal/ai/agent \
	./internal/ai/cli \
	./internal/ai/gate \
	./internal/ai/llm \
	./internal/ai/mcp \
	./internal/ai/store \
	./internal/cmdhistory \
	./internal/configstore \
	./internal/rdpcore \
	./internal/sshconn \
	./internal/term
RACE_REST = $(filter-out $(RACE_WORKBENCH) $(RACE_HEAVY),$(RACE_PACKAGES))

dev:
	tools/dev.sh

run:
	tools/dev.sh --once

# The headless build used to iterate the interface includes the dev-tagged
# component gallery; the release build does not.
head:
	go build -tags vivahead,dev -o /tmp/motty-head ./cmd/motty

# The plain build is the one that ships; the dev build adds the component gallery
# (see internal/workbench/debug.go), so both are vetted and tested.
check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go vet -tags dev ./internal/workbench
	go test ./... -count=1
	go test -tags dev ./cmd/motty ./internal/workbench -count=1
	bash -n tools/dev.sh tools/dev_launch.sh tools/macos_dev_signing.sh \
		tools/macos_dev_signing_integration_test.sh
	bash tools/dev_launch_test.sh
	bash tools/macos_dev_signing_test.sh
	bash tools/dev_script_test.sh
	$(MAKE) race

# The detector is slow and multiplies the memory the tests hold, so one package
# runs at a time (-p 1) and CI spreads the list over parallel jobs instead. A
# package added to RACE_PACKAGES joins the "rest" job by itself; only a package
# that becomes slow enough to need a job of its own is listed above.
# `make race` still runs everything, for the gate before a merge.
race:
	go test -race -p 1 $(RACE_PACKAGES) -count=1

# One shard of the workbench suite (SHARD of SHARDS, picked by test-name hash):
# the package is about half of the whole race run and its tests cannot overlap
# each other, so it gets the parallel jobs.
race-workbench:
	@test -n "$(SHARD)" -a -n "$(SHARDS)" || { echo "usage: make race-workbench SHARD=<i> SHARDS=<n>"; exit 2; }
	@pattern=$$(go run ./tools/testslice -race -n $(SHARDS) -i $(SHARD) $(RACE_WORKBENCH)) && \
		go test -race -p 1 -count=1 $(RACE_WORKBENCH) -run "$$pattern"

race-heavy:
	go test -race -p 1 -count=1 $(RACE_HEAVY)

race-rest:
	go test -race -p 1 -count=1 $(RACE_REST)
