.PHONY: build run test test-race cover vet fmt fmt-check lint clean

BINARY := bin/chokepoint

build:
	go build -o $(BINARY) ./cmd/chokepoint

run: build
	./$(BINARY)

test:
	go test ./...

test-race:
	go test -race ./...

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt found unformatted files:"; gofmt -l .; exit 1)

lint: fmt-check vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed; ran gofmt+vet only"

clean:
	rm -rf bin coverage.out
