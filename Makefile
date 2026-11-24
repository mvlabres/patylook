.PHONY: run test vet fmt build build-linux hash-password tidy

BIN := bin/patylook

run:
	go run ./cmd/patylook

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

build:
	go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/patylook

# Static Linux binary for the hosting server (templates and assets are embedded).
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/patylook ./cmd/patylook

hash-password:
	go run ./cmd/patylook hash-password
