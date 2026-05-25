# Makefile
.PHONY: build test test-integration run clean

build:
	go build -o bin/proxy ./cmd/proxy

test:
	go test ./internal/...

test-integration:
	@echo "Running integration tests..."
	@echo "Make sure proxy is running (sudo systemctl start proxy-anthropic)"
	go test ./tests/...

run:
	go run ./cmd/proxy --config configs/proxy.yaml

clean:
	rm -rf bin/