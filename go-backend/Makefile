.PHONY: build run test cover vet fmt clean

BIN := bin/server

build:
	go build -o $(BIN) ./cmd/server

run: build
	./$(BIN)

test:
	go test ./...

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

vet:
	go vet ./...
	gofmt -l .

fmt:
	gofmt -w .

clean:
	rm -rf bin coverage.out
