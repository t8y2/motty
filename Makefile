.PHONY: dev run head gtk check test-dev race race-workbench race-heavy race-rest

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

# The tests only the dev build defines. Running the whole package again under
# -tags dev to reach them repeated about 3200 release tests for 37 dev ones, so
# the names are read from the files the tag compiles: a dev test added there
# joins the run without a list to maintain.
DEV_TEST_FILES = $(shell grep -rlE '^//go:build.*[[:space:]]dev([[:space:]]|$$)' --include='*_test.go' cmd/motty internal/workbench)
DEV_TEST_NAMES = $(shell grep -h '^func Test' $(DEV_TEST_FILES) | sed -E 's/^func (Test[A-Za-z0-9_]+).*/\1/')
DEV_TEST_PATTERN = ^($(shell echo '$(DEV_TEST_NAMES)' | tr ' ' '|'))$$

dev:
	tools/dev.sh

run:
	tools/dev.sh --once

# The headless build used to iterate the interface includes the dev-tagged
# component gallery; the release build does not.
head:
	go build -tags vivahead,dev -o /tmp/motty-head ./cmd/motty

# Viva's only GTK shell is GTK 3, selected by the upstream tag vivagtk; there
# is no GTK 1 or 2 backend and the tag carries no version suffix. That shell is
# what gives Linux the GtkIMContext path, so GTK immodules such as fcitx 4 can
# compose; the default build stays on XCB + Vulkan. GTK 3 must be installed at
# run time.
gtk:
	@test "$$(go env GOOS)" = linux || { echo "make gtk: the GTK shell is Linux only"; exit 2; }
	go build -tags vivagtk -o /tmp/motty-gtk ./cmd/motty

# The plain build is the one that ships; the dev build adds the component gallery
# (see internal/workbench/debug.go), so both are vetted and tested.
check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go vet -tags dev ./internal/workbench
	go test ./... -count=1
	$(MAKE) test-dev
	bash -n tools/dev.sh tools/dev_launch.sh tools/macos_dev_signing.sh \
		tools/macos_dev_signing_integration_test.sh
	bash tools/dev_launch_test.sh
	bash tools/macos_dev_signing_test.sh
	bash tools/dev_script_test.sh
	$(MAKE) race

# The dev build's own tests, without repeating the release suite: everything
# else in these packages already ran above, and a second pass under -tags dev
# buys nothing but build-tag compilation that vet has done.
test-dev:
	@test -n '$(DEV_TEST_NAMES)' || { echo "test-dev: no //go:build dev test files found"; exit 2; }
	go test -tags dev -count=1 -run '$(DEV_TEST_PATTERN)' ./cmd/motty ./internal/workbench

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
	@go run ./tools/testslice -race -n $(SHARDS) -i $(SHARD) -max-pattern 120000 $(RACE_WORKBENCH) | while IFS= read -r pattern; do \
		go test -race -p 1 -count=1 $(RACE_WORKBENCH) -run "$$pattern" || exit 1; \
	done

race-heavy:
	go test -race -p 1 -count=1 $(RACE_HEAVY)

race-rest:
	go test -race -p 1 -count=1 $(RACE_REST)
