.PHONY: build test check race fmt

build:
	go build -o bin/go-git-wip ./cmd/go-git-wip

test:
	go test ./...

check:
	go vet ./...
	go test ./...
	@test -z "$$(gofmt -l .)"

# Optional: requires cgo and a C compiler.
race:
	CGO_ENABLED=1 go test -race ./...

fmt:
	gofmt -w .
