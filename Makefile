.PHONY: dev head check

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
	go test -tags dev ./internal/workbench -count=1
	go test -race -p 1 ./...
