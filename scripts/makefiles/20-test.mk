# Test and coverage targets.

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
