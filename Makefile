.PHONY: build install test check example race fmt

build:
	go build -o bin/go-git-wip ./cmd/go-git-wip

install:
	go install ./cmd/go-git-wip

test:
	go test ./...

check:
	go vet ./...
	go test ./...
	cd examples/workflow && go vet ./...
	$(MAKE) example
	@test -z "$$(gofmt -l .)"

# Separate module verifies that embedding uses only the public library contract.
example:
	cd examples/workflow && go run .

# Optional: requires cgo and a C compiler.
race:
	CGO_ENABLED=1 go test -race ./...

fmt:
	gofmt -w .
