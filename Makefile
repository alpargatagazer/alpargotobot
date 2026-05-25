# Makefile for Alpargotobot
# Professional orchestration for local, test, and production environments.

PROJECT_NAME = alpargotobot

# Load versions and local env if available (minus prefix ignores missing files)
-include .env
-include .env.versions
export

# Local Development setup
export UID ?= $(shell id -u)
export GID ?= $(shell id -g)

# Compose Commands
COMPOSE_DEV  = docker compose --env-file .env.versions -p $(PROJECT_NAME)-dev -f docker/docker-compose.dev.yml
COMPOSE_PROD = docker compose --env-file .env.versions -p $(PROJECT_NAME)-prod -f docker/docker-compose.prod.yml

.PHONY: local-setup dev-up dev-down dev-logs dev-shell dev-rebuild \
        build-prod prod-up prod-down prod-logs prod-shell test test-cover lint scan clean

# --- Local Environment (Host) ---
local-setup:
	@echo "🔍 Checking for Mise..."
	@command -v mise >/dev/null 2>&1 || { echo "Error: 'mise' is not installed."; exit 1; }
	@echo "Installing tools via Mise..."
	mise install
	@echo "Local environment is ready!"

# --- Development ---
dev-up:
	$(COMPOSE_DEV) up --build -d

dev-down:
	$(COMPOSE_DEV) down

dev-logs:
	$(COMPOSE_DEV) logs -f telegram-bot

dev-shell:
	$(COMPOSE_DEV) exec -it telegram-bot sh

dev-rebuild:
	$(COMPOSE_DEV) build --no-cache
	$(COMPOSE_DEV) up -d

# --- Production & Build ---
build-prod:
	@echo "Building production image..."
	docker build \
		--build-arg GO_VERSION=${GO_IMAGE_VERSION} \
		--build-arg ALPINE_VERSION=${ALPINE_IMAGE_VERSION} \
		-t $(PROJECT_NAME)-prod:local \
		-f docker/Dockerfile.prod .

prod-up: build-prod
	DOCKER_IMAGE=$(PROJECT_NAME)-prod:local $(COMPOSE_PROD) up -d

prod-latest:
	$(COMPOSE_PROD) up -d

prod-down:
	$(COMPOSE_PROD) down

prod-logs:
	$(COMPOSE_PROD) logs -f telegram-bot

prod-shell:
	$(COMPOSE_PROD) exec telegram-bot sh

# --- Testing & Quality ---
test:
	mise x -- go test -v -race ./...

test-cover:
	mise x -- go test -coverprofile=coverage.out ./...
	mise x -- go tool cover -html=coverage.out -o coverage.html

fmt:
	mise x -- go fmt ./...
	
lint:
	mise x -- golangci-lint run

scan: build-prod
	@echo "Running Trivy vulnerability scanner on production image..."
	docker run --rm --name $(PROJECT_NAME)-scan \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(PWD)/trivy.yaml:/trivy.yaml \
		aquasec/trivy:latest image \
		--config /trivy.yaml \
		$(PROJECT_NAME)-prod:local

clean:
	$(COMPOSE_DEV) down -v --rmi local
	$(COMPOSE_PROD) down -v --rmi local || true
	rm -f coverage.out coverage.html bot
