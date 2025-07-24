APPLICATION := mcp-prometheus

##@ Testing

.PHONY: test-ci-pr
test-ci-pr: ## Run 'act' to simulate CI checks for a pull request
	@echo "Simulating CI workflow (pull_request event)..."
	@act pull_request --job check

.PHONY: test-ci-push
test-ci-push: ## Run 'act' to simulate CI checks for a push to main
	@echo "Simulating CI workflow (push event)..."
	@act push --job check

.PHONY: test-auto-release
test-auto-release: ## Run 'act' to simulate the auto-release workflow
	@echo "Simulating Auto-Release workflow (merged pull_request event)..."
	@echo "NOTE: Requires 'merged_pr_event.json' in the project root."
	@echo "NOTE: Git push steps within the workflow are expected to fail locally."
	@act pull_request --job auto_release --eventpath merged_pr_event.json 
