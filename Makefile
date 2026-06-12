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
        build-prod prod-up prod-down prod-logs prod-shell test test-cover lint scan clean help

# Default target
.DEFAULT_GOAL := help

# --- Local Environment (Host) ---
## local-setup: Install required tools via Mise
local-setup:
	@echo "🔍 Checking for Mise..."
	@command -v mise >/dev/null 2>&1 || { echo "Error: 'mise' is not installed."; exit 1; }
	@echo "Installing tools via Mise..."
	mise install
	@echo "Local environment is ready!"

# --- Development ---
## dev-up: Start the development environment with Docker Compose
dev-up:
	$(COMPOSE_DEV) up --build -d

## dev-down: Stop the development environment
dev-down:
	$(COMPOSE_DEV) down

## dev-logs: Follow logs for the development environment
dev-logs:
	$(COMPOSE_DEV) logs -f telegram-bot

## dev-shell: Open a shell in the development container
dev-shell:
	$(COMPOSE_DEV) exec -it telegram-bot sh

## dev-rebuild: Rebuild and restart the development container
dev-rebuild:
	$(COMPOSE_DEV) build --no-cache
	$(COMPOSE_DEV) up -d

# --- Production & Build ---
## build-prod: Build the production Docker image locally
build-prod:
	@echo "Building production image..."
	docker build \
		--build-arg GO_VERSION=${GO_IMAGE_VERSION} \
		--build-arg ALPINE_VERSION=${ALPINE_IMAGE_VERSION} \
		-t $(PROJECT_NAME)-prod:local \
		-f docker/Dockerfile.prod .

## prod-up: Start the production environment using the local image
prod-up: build-prod
	DOCKER_IMAGE=$(PROJECT_NAME)-prod:local $(COMPOSE_PROD) up -d

## prod-latest: Start the production environment using the latest published image
prod-latest:
	$(COMPOSE_PROD) up -d

## prod-down: Stop the production environment
prod-down:
	$(COMPOSE_PROD) down

## prod-logs: Follow logs for the production environment
prod-logs:
	$(COMPOSE_PROD) logs -f telegram-bot

## prod-shell: Open a shell in the production container
prod-shell:
	$(COMPOSE_PROD) exec telegram-bot sh

# --- Testing & Quality ---
## test: Run all Go tests with race detection
test:
	mise x -- go test -v -race ./...

## test-cover: Run tests and generate HTML coverage report
test-cover:
	mise x -- go test -coverprofile=coverage.out ./...
	mise x -- go tool cover -html=coverage.out -o coverage.html

## fmt: Format all Go code
fmt:
	mise x -- go fmt ./...
	
## lint: Run golangci-lint
lint:
	mise x -- golangci-lint run

## scan: Run Trivy vulnerability scanner on the production image
scan: build-prod
	@echo "Running Trivy vulnerability scanner on production image..."
	docker run --rm --name $(PROJECT_NAME)-scan \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(PWD)/trivy.yaml:/trivy.yaml \
		aquasec/trivy:latest image \
		--config /trivy.yaml \
		$(PROJECT_NAME)-prod:local

## clean: Stop containers and remove temporary files
clean:
	$(COMPOSE_DEV) down -v --rmi local
	$(COMPOSE_PROD) down -v --rmi local || true
	rm -f coverage.out coverage.html bot

# --- Help ---
## help: Show this help message
help:
	@echo "Available targets:"
	@awk '/^## / { 		sub(/^## /, ""); 		split($$0, p, ": "); 		printf "  %-15s %s\n", p[1], p[2] 	}' Makefile