.PHONY: dev head check race

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
	./internal/rfb \
	./internal/s3 \
	./internal/scpclient \
	./internal/selfupdate \
	./internal/serial \
	./internal/sftpclient \
	./internal/sshconn \
	./internal/telnet \
	./internal/term \
	./internal/vault \
	./internal/webdav \
	./internal/workbench \
	./internal/x11fwd

dev:
	tools/dev.sh

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
	go test -tags dev ./internal/workbench -run '^TestDebug' -count=1
	$(MAKE) race

race:
	go test -race -p 1 $(RACE_PACKAGES) -count=1
