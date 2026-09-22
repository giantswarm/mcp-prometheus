##@ Development

.PHONY: lint-yaml
lint-yaml: ## Run YAML linter
	@echo "Running YAML linter..."
	@# Exclude zz_generated files
	@yamllint .github/workflows/ci.yaml

.PHONY: check
check: lint-yaml ## Run YAML linter

##@ Testing

.PHONY: test-vet
test-vet: ## Run go test and go vet
	@echo "Running Go tests (with NO_COLOR=true)..."
	@NO_COLOR=true go test -cover ./...
	@echo "Running go vet..."
	@go vet ./...

# Note: These targets require Docker and 'act' to be installed.
# See: https://github.com/nektos/act#installation

.PHONY: test-ci-pr
test-ci-pr: ## Run 'act' to simulate CI checks for a pull request
	@echo "Simulating CI workflow (pull_request event)..."
	@act pull_request --job check

.PHONY: test-ci-push
test-ci-push: ## Run 'act' to simulate CI checks for a push to main
	@echo "Simulating CI workflow (push event)..."
	@act push --job check

##@ Helm

HELM_UNITTEST_VERSION := 1.0.3

.PHONY: helm-lint
helm-lint: ## Lint the chart.
	helm lint helm/mcp-prometheus

.PHONY: helm-test
helm-test: helm-lint helm-unittest ## Run every chart check (what the chart-test CI job runs).

.PHONY: helm-unittest
helm-unittest: helm-plugin-unittest ## Run the helm-unittest suites in helm/mcp-prometheus/tests/.
	helm unittest helm/mcp-prometheus

.PHONY: helm-plugin-unittest
helm-plugin-unittest:
	@helm plugin list | grep -q '^unittest' || helm plugin install https://github.com/helm-unittest/helm-unittest --version $(HELM_UNITTEST_VERSION)
