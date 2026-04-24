.PHONY: quality-gate fmt lint test coverage vuln dev dev-down migrate seed-test e2e

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
	ARX_MASTER_KEY=$${ARX_MASTER_KEY:-$$(openssl rand -hex 32)} go run ./cmd/seedtest | tee e2e/bruno/arx-webhooks/.env
	@echo "✅ Test tenant seeded — .env written to e2e/bruno/arx-webhooks/.env"

e2e:
	@echo "🧪 Running Bruno e2e tests..."
	cd e2e/bruno/arx-webhooks && bru run --env local --sandbox=developer --reporter-json ./reports/results.json

docs:
	@echo "📖 Starting local documentation server (pkgsite)..."
	@echo "   → Your docs will open automatically in the browser"
	@echo "   → Press Ctrl+C to stop the server"
	pkgsite -open
