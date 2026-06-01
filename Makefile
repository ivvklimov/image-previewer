.PHONY: build run tidy test lint docker-build docker-run clean

# Переменные для версионирования
RELEASE ?= v0.0.1
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_HASH := $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")

# Путь к пакету version для ldflags
VERSION_PKG := github.com/ivvklimov/image-previewer/internal/version

# Флаги для встраивания версии в бинарник
LDFLAGS := -X $(VERSION_PKG).Release=$(RELEASE) \
           -X $(VERSION_PKG).BuildDate=$(BUILD_DATE) \
           -X $(VERSION_PKG).GitHash=$(GIT_HASH)

BINARY := bin/image-previewer

build:
	@echo "Building image-previewer $(RELEASE)..."
	go mod tidy
	mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) cmd/server/main.go
	@echo "Build complete: $(BINARY)"
	@$(BINARY) --version

run: build
	@echo "Starting image-previewer service..."
	./$(BINARY) --config=configs/config.yaml

tidy:
	go mod tidy

test:
	go test -v -count=1 -race ./...

install-lint-deps:
	(which golangci-lint > /dev/null) || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin v1.63.4

lint: install-lint-deps
	golangci-lint run ./...

clean:
	rm -rf $(BINARY) cache/
