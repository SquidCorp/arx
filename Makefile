.PHONY: quality-gate fmt lint test coverage vuln dev dev-down migrate seed-test e2e e2e-go e2e-bruno

# Main quality gate — run this!
quality-gate: fmt lint test coverage vuln
	@echo "✅ Quality gate PASSED — code is clean, tested, and documented!"

fmt:
	@echo "🔍 Checking formatting (gofumpt + goimports)..."
	@test -z "$$(gofumpt -l .)" || (echo "❌ Formatting issues — run: gofumpt -w ."; exit 1)
	@test -z "$$(goimports -l .)" || (echo "❌ Import issues — run: goimports -w ."; exit 1)

lint:
	@echo "🔍 Running linter (includes documentation checks)..."
	golangci-lint run --timeout=5m

test:
	@echo "🔍 Running tests with race detector..."
	go test ./... -race -count=1

coverage:
	@echo "🔍 Checking test coverage (excluding generated & mocks)..."
	# Run tests and generate coverage
	go test ./... -race -coverprofile=coverage.tmp -covermode=atomic >/dev/null 2>&1
	
	# Filter out unwanted files (add more patterns if needed)
	cat coverage.tmp | \
	  grep -v -E '(_mock\.go|_generated\.go|\.pb\.go|main\.go)$$' > coverage.out || true
	
	# Calculate and check total coverage
	@TOTAL=$$(go tool cover -func=coverage.out 2>/dev/null | grep total: | awk '{print $$3}' | sed 's/%//'); \
	if [ -z "$$TOTAL" ]; then TOTAL=0; fi; \
	echo "📊 Coverage: $$TOTAL% (excluding generated/mocks)"; \
	if awk -v cov=$$TOTAL -v min=70 'BEGIN { if (cov < min) exit 1; else exit 0 }'; then \
		echo "✅ Coverage OK (>=70%)"; \
	else \
		echo "❌ Coverage below 70% — add more tests!"; \
		rm -f coverage.tmp coverage.out; exit 1; \
	fi; \
	rm -f coverage.tmp

vuln:
	@echo "🔍 Checking for vulnerabilities..."
	govulncheck ./...

dev:
	@echo "🚀 Starting infrastructure..."
	docker compose up -d db redis
	@echo "⏳ Waiting for services to be healthy..."
	@docker compose exec db sh -c 'until pg_isready -U postgres; do sleep 1; done' >/dev/null 2>&1
	@$(MAKE) migrate
	@echo "🔥 Starting app with hot-reload..."
	@if [ -z "$$ARX_MASTER_KEY" ] && [ -f e2e/bruno/arx-webhooks/.env ]; then \
		echo "📎 Loading ARX_MASTER_KEY from e2e .env..."; \
		export $$(grep '^ARX_MASTER_KEY=' e2e/bruno/arx-webhooks/.env); \
	fi && \
	go run github.com/air-verse/air@latest -c .air.toml

dev-down:
	@echo "🛑 Stopping infrastructure..."
	docker compose down

migrate:
	@echo "📦 Running migrations..."
	@for f in migrations/*.sql; do \
		echo "  → $$f"; \
		sed -n '/^-- +goose Up$$/,/^-- +goose Down$$/{ /^-- +goose/d; p; }' $$f | \
			docker compose exec -T db psql -U postgres -d arx; \
	done
	@echo "✅ Migrations applied"

seed-test:
	@echo "🌱 Seeding test tenant..."
	@ARX_MASTER_KEY=$${ARX_MASTER_KEY:-$$(openssl rand -hex 32)} go run ./cmd/seedtest > e2e/bruno/arx-webhooks/.env.tmp && mv e2e/bruno/arx-webhooks/.env.tmp e2e/bruno/arx-webhooks/.env
	@echo "✅ Test tenant seeded — .env written to e2e/bruno/arx-webhooks/.env"

# Run all e2e test suites (Go + Bruno)
e2e: e2e-go e2e-bruno
	@echo "✅ All e2e tests passed!"

# Run Go e2e tests (tests matching Test.*E2E)
e2e-go:
	@echo "🧪 Running Go e2e tests..."
	go test ./... -race -count=1 -run 'Test.*E2E' -v

# Run Bruno API e2e tests
e2e-bruno:
	@echo "🔧 Checking Bruno e2e prerequisites..."
	@command -v bru >/dev/null 2>&1 || { echo "❌ bru is required. Install with: npm install -g @usebruno/cli"; exit 1; }
	@test -f e2e/bruno/arx-webhooks/.env || { echo "❌ No .env file found. Run: make seed-test"; exit 1; }
	@curl -sf http://localhost:8080/health >/dev/null 2>&1 || { echo "⚠️  Warning: App does not appear to be running on http://localhost:8080. Start it with: make dev"; }
	@echo "🧪 Running Bruno e2e tests..."
	@mkdir -p e2e/bruno/arx-webhooks/reports
	bash -c 'set -a && source e2e/bruno/arx-webhooks/.env && set +a && cd e2e/bruno/arx-webhooks && bru run --env-file environments/local.json --sandbox=developer --reporter-json ./reports/results.json'
	@echo "✅ Bruno e2e tests completed — report: e2e/bruno/arx-webhooks/reports/results.json"

docs:
	@echo "📖 Starting local documentation server (pkgsite)..."
	@echo "   → Your docs will open automatically in the browser"
	@echo "   → Press Ctrl+C to stop the server"
	pkgsite -open
