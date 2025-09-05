.PHONY: help install generate compile build clean test

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Install dependencies
	npm install

generate: ## Generate TypeScript client from swagger files
	npm run generate

compile: ## Compile TypeScript to JavaScript
	npm run compile

build: ## Build everything (generate + compile)
	npm run build

clean: ## Clean build artifacts
	npm run clean

test: ## Run tests (placeholder for future test setup)
	@echo "Tests not yet implemented"

dev: ## Development mode - watch for changes and rebuild
	@echo "Starting development mode..."
	@echo "Run 'make build' to rebuild after changes"

.PHONY: setup
setup: install generate compile ## Initial setup: install deps, generate client, and compile
	@echo "Setup complete! Run 'make build' to rebuild in the future."
