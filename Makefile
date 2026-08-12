.PHONY: build test install clean

build:
	go build -o bin/smate ./cmd/smate

test:
	go vet ./...
	go test ./...

# Installs smate into $(go env GOBIN), ~/go/bin by default. That directory
# has to be on your PATH.
install:
	make test
	make build
	go install ./cmd/smate

clean:
	rm -rf bin
