.PHONY: help test-unit test-python test-integration test-e2e test-browser test-containers test-monitoring baseline-today check

help:
	@echo "test-unit         Go tests without fixture databases, frontend unit tests, Python unit tests"
	@echo "test-python       Python unit tests for deployment and setup scripts"
	@echo "test-integration  One disposable PostgreSQL round with race detection"
	@echo "test-e2e          Build frontend and run browser -> API -> simulated Discord"
	@echo "test-browser      Standalone Playwright tests (requires free port 5173)"
	@echo "test-containers   Disposable Compose smoke and deployment rollback tests"
	@echo "test-monitoring   Disposable monitoring tests (requires monitoring images)"
	@echo "baseline-today    Uncached Today Service counts and memory benchmark (no Redis or database)"
	@echo "check             Formatting, Go vet, frontend lint, types and API contract"

test-unit: test-python
	go test ./cmd/... ./internal/... ./test/...
	npm --prefix frontend test

test-python:
	python3 -m unittest discover -s test/python -p 'test_*.py' -v

test-integration:
	python3 scripts/test_integration.py --repeat 1

test-e2e:
	npm --prefix frontend run build
	python3 scripts/test_integration.py --repeat 1 --browser-e2e

test-browser:
	npm --prefix frontend run test:e2e

test-containers:
	@if [ ! -d .local/stack ]; then python3 scripts/init_local_stack.py; fi
	docker compose -f compose.production.yaml build
	python3 -m test.python.test_local_stack

test-monitoring:
	python3 scripts/check_monitoring_rules.py
	python3 -m test.python.test_monitoring

baseline-today:
	go test ./test/load -run '^TestTodayUncached' -bench '^BenchmarkTodayUncached$$' -benchmem -benchtime=100x -count=1 -v

check:
	@test -z "$$(gofmt -l cmd internal test)"
	go vet ./cmd/... ./internal/... ./test/...
	npm --prefix frontend run lint
	npm --prefix frontend run typecheck
	npm --prefix frontend run api:check
