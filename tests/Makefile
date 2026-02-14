# EdgeComet Acceptance Tests Makefile

.PHONY: help setup unit unit-verbose basic basic-watch basic-single clean install-deps basic-verbose basic-ci sharding sharding-verbose sharding-ci recache recache-verbose recache-ci cleanup cleanup-verbose cleanup-ci test

GINKGO = go run github.com/onsi/ginkgo/v2/ginkgo

# Default target
help:
	@echo "EdgeComet Acceptance Tests"
	@echo "=========================="
	@echo ""
	@echo "Available commands:"
	@echo "  help             Show this help message"
	@echo "  install-deps     Install test dependencies (Ginkgo, etc.)"
	@echo "  setup            Set up test environment"
	@echo "  unit             Run all unit tests (internal/, cmd/, pkg/)"
	@echo "  unit-verbose     Run unit tests with verbose output and coverage"
	@echo "  basic            Run basic acceptance tests (single EG-RS)"
	@echo "  basic-watch      Run basic tests in watch mode"
	@echo "  basic-verbose    Run basic tests with verbose output"
	@echo "  basic-single     Run a single basic test file (use FILE=test_file.go)"
	@echo "  basic-ci         Run basic tests for CI/CD (with XML output)"
	@echo "  sharding         Run sharding acceptance tests"
	@echo "  sharding-verbose Run sharding tests with verbose output"
	@echo "  sharding-ci      Run sharding tests for CI/CD (with XML output)"
	@echo "  recache          Run recache acceptance tests"
	@echo "  recache-verbose  Run recache tests with verbose output"
	@echo "  recache-ci       Run recache tests for CI/CD (with XML output)"
	@echo "  cleanup          Run filesystem cleanup acceptance tests"
	@echo "  cleanup-verbose  Run cleanup tests with verbose output"
	@echo "  cleanup-ci       Run cleanup tests for CI/CD (with XML output)"
	@echo "  test             Alias for 'basic' (run basic acceptance tests)"
	@echo "  clean            Clean up test environment and artifacts"
	@echo ""
	@echo "Examples:"
	@echo "  make basic                    # Run all basic tests"
	@echo "  make basic-single FILE=static_content_test.go"
	@echo "  make basic FOCUS='Static Content'"
	@echo "  make basic SKIP='JavaScript'"
	@echo ""
	@echo "Note: All tests now run in local mode with embedded miniredis."
	@echo "      No Docker required!"

# Install test dependencies
install-deps:
	@echo "Installing test dependencies..."
	go mod tidy
	@echo "Dependencies installed successfully"

# Set up test environment (local mode only)
setup: install-deps
	@echo "Setting up local test environment..."
	@mkdir -p reports temp/cache
	@echo "Test environment ready"
	@echo ""
	@echo "Tests will automatically start miniredis and test server"
	@echo "To run tests: make test"

# Run all unit tests
unit:
	@echo "Running unit tests..."
	@cd .. && go test ./internal/... ./cmd/... ./pkg/... \
		--race \
		--timeout=2m \
		-cover \
		-short \
		-count=1
	@echo "Unit tests completed"

# Run unit tests with verbose output and coverage report
unit-verbose:
	@echo "Running unit tests with verbose output..."
	@mkdir -p reports
	@cd .. && go test ./internal/... ./cmd/... ./pkg/... \
		--race \
		--timeout=2m \
		-cover \
		-coverprofile=tests/reports/coverage.out \
		-v
	@echo "Unit tests completed"
	@echo "Coverage report saved to tests/reports/coverage.out"
	@echo "View coverage: go tool cover -html=tests/reports/coverage.out"

# Run basic acceptance tests (local mode with miniredis)
basic: setup
	@echo "Running EdgeComet basic acceptance tests (local mode)..."
	cd acceptance/basic && TEST_MODE=local $(GINKGO) run \
		--timeout=5m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/results.json \
		--junit-report=../../reports/junit.xml \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		.
	@echo "Tests completed. Check reports/ for detailed results"

# Alias: 'test' now points to 'basic'
test: basic

# Run basic tests with verbose output
basic-verbose: setup
	@echo "Running basic acceptance tests with verbose output (local mode)..."
	cd acceptance/basic && TEST_MODE=local $(GINKGO) run \
		--timeout=5m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/results.json \
		--junit-report=../../reports/junit.xml \
		-v \
		--show-node-events \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		.

# Run basic tests in watch mode (for development)
basic-watch: setup
	@echo "Running basic tests in watch mode (local mode)..."
	@echo "Press Ctrl+C to stop watching"
	cd acceptance/basic && TEST_MODE=local $(GINKGO) watch \
		--timeout=5m \
		--race \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		.

# Run a single basic test file
basic-single: setup
	@if [ -z "$(FILE)" ]; then \
		echo "Error: Please specify FILE=<test_file.go>"; \
		echo "Example: make basic-single FILE=static_content_test.go"; \
		exit 1; \
	fi
	@echo "Running single test file: $(FILE)"
	cd acceptance/basic && TEST_MODE=local $(GINKGO) run \
		--timeout=5m \
		--race \
		--json-report=../../reports/results.json \
		-v \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		--focus-file="$(FILE)" \
		.

# Run basic tests for CI/CD (local mode)
basic-ci:
	@echo "Running basic tests for CI/CD (local mode)..."
	@mkdir -p reports
	# Run tests - services start automatically via BeforeSuite
	cd acceptance/basic && TEST_MODE=local $(GINKGO) run \
		--timeout=10m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/results.json \
		--junit-report=../../reports/junit.xml \
		--output-dir=../../reports \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		. || true
	@if [ -f reports/junit.xml ]; then \
		echo "Test results saved to reports/"; \
	else \
		echo "Warning: Test results not generated"; \
	fi

# Clean up test environment
clean:
	@echo "Cleaning up test environment..."
	rm -rf reports/
	rm -rf temp/cache/*
	@echo "Cleanup completed"

# Generate test report summary
report-summary:
	@if [ -f reports/results.json ]; then \
		echo "Test Results Summary:"; \
		echo "==================="; \
		jq -r '"Total: " + (.SuiteSucceeded | length + .SuiteFailed | length | tostring) + " | Passed: " + (.SuiteSucceeded | length | tostring) + " | Failed: " + (.SuiteFailed | length | tostring)' reports/results.json 2>/dev/null || echo "JSON parsing not available"; \
	else \
		echo "No test results found. Run 'make test' first."; \
	fi

# Validate test configuration
validate:
	@echo "Validating test configuration..."
	@test -d acceptance/basic || (echo "Error: Basic acceptance tests not found" && exit 1)
	@test -f acceptance/basic/go.mod || (echo "Error: Basic module go.mod not found" && exit 1)
	@test -d acceptance/basic/fixtures || (echo "Error: Basic fixtures not found" && exit 1)
	@echo "Configuration validation passed"

# Run sharding acceptance tests (local mode with miniredis)
sharding: setup
	@echo "Running EdgeComet sharding acceptance tests (local mode)..."
	cd acceptance/sharding && TEST_MODE=local $(GINKGO) run \
		--timeout=5m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/sharding-results.json \
		--junit-report=../../reports/sharding-junit.xml \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		.
	@echo "Sharding tests completed. Check reports/ for detailed results"

# Run sharding tests with verbose output
sharding-verbose: setup
	@echo "Running sharding acceptance tests with verbose output (local mode)..."
	cd acceptance/sharding && TEST_MODE=local $(GINKGO) run \
		--timeout=5m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/sharding-results.json \
		--junit-report=../../reports/sharding-junit.xml \
		-v \
		--show-node-events \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		.

# Run sharding tests for CI/CD (local mode)
sharding-ci:
	@echo "Running sharding tests for CI/CD (local mode)..."
	@mkdir -p reports
	cd acceptance/sharding && TEST_MODE=local $(GINKGO) run \
		--timeout=10m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/sharding-results.json \
		--junit-report=../../reports/sharding-junit.xml \
		--output-dir=../../reports \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		. || true
	@if [ -f reports/sharding-junit.xml ]; then \
		echo "Sharding test results saved to reports/"; \
	else \
		echo "Warning: Sharding test results not generated"; \
	fi

# Run recache acceptance tests (local mode with miniredis)
recache: setup
	@echo "Running EdgeComet recache acceptance tests (local mode)..."
	cd acceptance/recache && TEST_MODE=local $(GINKGO) run \
		--timeout=5m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/recache-results.json \
		--junit-report=../../reports/recache-junit.xml \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		.
	@echo "Recache tests completed. Check reports/ for detailed results"

# Run recache tests with verbose output
recache-verbose: setup
	@echo "Running recache acceptance tests with verbose output (local mode)..."
	cd acceptance/recache && TEST_MODE=local $(GINKGO) run \
		--timeout=5m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/recache-results.json \
		--junit-report=../../reports/recache-junit.xml \
		-v \
		--show-node-events \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		.

# Run recache tests for CI/CD (local mode)
recache-ci:
	@echo "Running recache tests for CI/CD (local mode)..."
	@mkdir -p reports
	cd acceptance/recache && TEST_MODE=local $(GINKGO) run \
		--timeout=10m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/recache-results.json \
		--junit-report=../../reports/recache-junit.xml \
		--output-dir=../../reports \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		. || true
	@if [ -f reports/recache-junit.xml ]; then \
		echo "Recache test results saved to reports/"; \
	else \
		echo "Warning: Recache test results not generated"; \
	fi

# Run filesystem cleanup acceptance tests (local mode with miniredis)
cleanup: setup
	@echo "Running EdgeComet filesystem cleanup acceptance tests (local mode)..."
	cd acceptance/cleanup && TEST_MODE=local $(GINKGO) run \
		--timeout=5m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/cleanup-results.json \
		--junit-report=../../reports/cleanup-junit.xml \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		.
	@echo "Cleanup tests completed. Check reports/ for detailed results"

# Run cleanup tests with verbose output
cleanup-verbose: setup
	@echo "Running cleanup acceptance tests with verbose output (local mode)..."
	cd acceptance/cleanup && TEST_MODE=local $(GINKGO) run \
		--timeout=5m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/cleanup-results.json \
		--junit-report=../../reports/cleanup-junit.xml \
		-v \
		--show-node-events \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		.

# Run cleanup tests for CI/CD (local mode)
cleanup-ci:
	@echo "Running cleanup tests for CI/CD (local mode)..."
	@mkdir -p reports
	cd acceptance/cleanup && TEST_MODE=local $(GINKGO) run \
		--timeout=10m \
		--race \
		--randomize-suites \
		--randomize-all \
		--json-report=../../reports/cleanup-results.json \
		--junit-report=../../reports/cleanup-junit.xml \
		--output-dir=../../reports \
		$(if $(FOCUS),--focus="$(FOCUS)") \
		$(if $(SKIP),--skip="$(SKIP)") \
		. || true
	@if [ -f reports/cleanup-junit.xml ]; then \
		echo "Cleanup test results saved to reports/"; \
	else \
		echo "Warning: Cleanup test results not generated"; \
	fi

# Full test cycle (for CI/CD)
full-test: clean validate basic-ci report-summary

.DEFAULT_GOAL := help