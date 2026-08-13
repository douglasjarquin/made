.PHONY: build test lint skill

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run ./...

skill:
	go run ./cmd/genskill
