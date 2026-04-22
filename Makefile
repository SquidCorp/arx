.PHONY: quality-gate fmt lint test coverage vuln

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
	  grep -v -E '(_mock\.go|_generated\.go|\.pb\.go|main\.go)$' > coverage.out || true
	
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

docs:
	@echo "📖 Starting local documentation server (pkgsite)..."
	@echo "   → Your docs will open automatically in the browser"
	@echo "   → Press Ctrl+C to stop the server"
	pkgsite -open
