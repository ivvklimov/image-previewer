.PHONY: build run run-local tidy test lint docker-build docker-run clean-cache logs

# Команда docker compose (для старых версий docker-compose)
DC ?= docker compose

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

run: docker-run

run-local: build
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

clean-cache:
	rm -rf $(BINARY) cache/

logs: docker-logs


# =========================================
# Integration Tests
# =========================================
TEST_PROJECT_NAME := image-previewer-test
COMPOSE_FILE := deployments/docker/docker-compose.integration.yml
COMPOSE_CMD := PROJECT_ROOT=$(PWD) COMPOSE_PROJECT_NAME=$(TEST_PROJECT_NAME) $(DC) -f $(COMPOSE_FILE)

# Полный цикл (как в CI): Сборка -> Поднятие -> Тесты -> Гарантированная очистка
.PHONY: integration-test
integration-test:
	@echo "[1/4] Building test images..."
	@$(COMPOSE_CMD) build
	@echo "[2/4] Starting test environment..."
	@$(COMPOSE_CMD) up -d
	@echo "Waiting for services to be ready..."
	@sleep 3
	@echo "[3/4] Running integration tests..."
	@PROJECT_ROOT=$(PWD) go test -v -tags=integration -timeout=5m -count=1 ./tests/integration/... ; \
	EXIT_CODE=$$? ; \
	echo "[4/4] Tearing down test environment..."; \
	$(COMPOSE_CMD) down -v ; \
	exit $$EXIT_CODE

# Только поднять окружение (без форсированной пересборки)
.PHONY: integration-test-up
integration-test-up:
	@echo "Starting integration test environment..."
	@$(COMPOSE_CMD) up -d
	@echo "Waiting for services..."
	@sleep 3
	@echo "Environment ready. You can now run 'make integration-run' or debug manually."

# Только пересобрать образы (полезно после изменений в коде)
.PHONY: integration-test-build
integration-test-build:
	@echo "Building test images..."
	@$(COMPOSE_CMD) build

# Запустить тесты в уже работающем окружении
.PHONY: integration-test-run
integration-test-run:
	@echo "Running integration tests against running environment..."
	@PROJECT_ROOT=$(PWD) go test -v -tags=integration -timeout=5m -count=1 ./tests/integration/...

# Полная очистка тестового окружения и томов
.PHONY: integration-test-down
integration-test-down:
	@echo "Cleaning integration test environment..."
	@$(COMPOSE_CMD) down -v
	@echo "Cleaned."


# =========================================
# Dockerized Application
# =========================================
DOCKER_PROJECT := image-previewer-app
DOCKER_COMPOSE_FILE := deployments/docker/docker-compose.yml
DOCKER_COMPOSE := PROJECT_ROOT=$(PWD) COMPOSE_PROJECT_NAME=$(DOCKER_PROJECT) $(DC) -f $(DOCKER_COMPOSE_FILE)

# Сборка Docker-образа приложения
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	@$(DOCKER_COMPOSE) build

# Запуск приложения в контейнере
.PHONY: docker-run
docker-run:
	@echo "Starting application in Docker..."
	@$(DOCKER_COMPOSE) up -d
	@echo "Application started."

# Остановка контейнеров (тома сохраняются), кеш сохранится
.PHONY: docker-stop
docker-stop:
	@echo "Stopping application..."
	@$(DOCKER_COMPOSE) down

# Перезапуск контейнеров
.PHONY: docker-restart
docker-restart: docker-stop docker-run

# Пересборка образа и перезапуск (после изменений в коде)
.PHONY: docker-rebuild
docker-rebuild: docker-build docker-restart

# Полная очистка: контейнеры и персистентные тома, кеш очистится
.PHONY: docker-clean
docker-clean:
	@echo "Cleaning Docker environment and persistent volumes..."
	@$(DOCKER_COMPOSE) down -v
	@echo "Cleaned."

# Просмотр логов приложения в реальном времени
.PHONY: docker-logs
docker-logs:
	@$(DOCKER_COMPOSE) logs -f --tail=100 app
