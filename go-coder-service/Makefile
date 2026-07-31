.PHONY: all css build run clean

all: css build

css:
	npm install
	npm run build:css

build:
	go build -o bin/go-coder-service ./cmd/server

run: build
	./bin/go-coder-service

clean:
	rm -rf bin
