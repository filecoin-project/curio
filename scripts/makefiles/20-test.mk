# Test and coverage targets.

# Container names and images for local test databases.
TEST_PG_NAME := curio-test-pg
TEST_PG_IMAGE ?= postgres:15
TEST_SCYLLA_NAME := curio-test-scylla
TEST_SCYLLA_IMAGE ?= scylladb/scylla:latest

# Environment variables for running tests against local containers.
TEST_DB_ENV := CURIO_HARMONYDB_HOSTS=127.0.0.1 CURIO_HARMONYDB_PORT=5432 CURIO_DB_HOST_CQL=127.0.0.1

test-dbs-up:
	@docker start $(TEST_PG_NAME) 2>/dev/null \
		|| docker run --name $(TEST_PG_NAME) -d -p 5432:5432 \
			-e POSTGRES_HOST_AUTH_METHOD=trust $(TEST_PG_IMAGE)
	@docker start $(TEST_SCYLLA_NAME) 2>/dev/null \
		|| docker run --name $(TEST_SCYLLA_NAME) -d -p 9042:9042 \
			$(TEST_SCYLLA_IMAGE) --smp 1 --memory 512M --overprovisioned 1
	@printf "Waiting for Postgres..."
	@until docker exec $(TEST_PG_NAME) pg_isready -U postgres >/dev/null 2>&1; do sleep 1; printf "."; done
	@echo " ready"
	@docker exec $(TEST_PG_NAME) psql -U postgres -tAc \
		"SELECT 1 FROM pg_roles WHERE rolname = 'yugabyte'" 2>/dev/null | grep -q 1 \
		|| docker exec $(TEST_PG_NAME) psql -U postgres -c "CREATE ROLE yugabyte WITH LOGIN PASSWORD 'yugabyte'"
	@docker exec $(TEST_PG_NAME) psql -U postgres -tAc \
		"SELECT 1 FROM pg_database WHERE datname = 'yugabyte'" 2>/dev/null | grep -q 1 \
		|| docker exec $(TEST_PG_NAME) psql -U postgres -c "CREATE DATABASE yugabyte OWNER yugabyte"
	@printf "Waiting for ScyllaDB..."
	@until docker exec $(TEST_SCYLLA_NAME) cqlsh -e "DESCRIBE KEYSPACES" >/dev/null 2>&1; do sleep 2; printf "."; done
	@echo " ready"
	@echo ""
	@echo "Test databases running. To run tests:"
	@echo ""
	@echo "  $(TEST_DB_ENV) \\"
	@echo "    go test -v -tags='fvm,nosupraseal' -timeout 30m ./itests/ -run TestName"
	@echo ""
	@echo "Containers persist across runs. Use 'make test-dbs-down' to remove them."
.PHONY: test-dbs-up

test-dbs-down:
	@docker rm -f $(TEST_PG_NAME) $(TEST_SCYLLA_NAME) 2>/dev/null || true
	@echo "Test database containers removed"
.PHONY: test-dbs-down

test-deps: $(BUILD_DEPS)
	@echo "Built dependencies with FVM support for testing"
.PHONY: test-deps

test: test-deps
	$(TEST_ENV_VARS) go test -v -tags="cgo,fvm" -timeout 30m ./itests/...
.PHONY: test

coverage: cov
.PHONY: coverage

cov:
	@mkdir -p $(COVERAGE_DIR)
	go test -coverprofile=$(COVERAGE_PROFILE) -covermode=atomic ./...
	go tool cover -html=$(COVERAGE_PROFILE) -o $(COVERAGE_HTML)
	@echo ""
	@echo "Coverage report generated:"
	@echo "  Profile: $(COVERAGE_PROFILE)"
	@echo "  HTML:    $(COVERAGE_HTML)"
	@echo ""
	@echo "Opening coverage report..."
	@which xdg-open > /dev/null 2>&1 && xdg-open $(COVERAGE_HTML) || open $(COVERAGE_HTML) || echo "Open $(COVERAGE_HTML) in your browser"
.PHONY: cov
