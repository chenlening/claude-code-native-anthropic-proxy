# Makefile
.PHONY: build test run clean

build:
	go build -o bin/proxy ./cmd/proxy

test:
	go test -v ./...

run:
	go run ./cmd/proxy --config configs/proxy.yaml

clean:
	rm -rf bin/