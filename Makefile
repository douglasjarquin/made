.PHONY: build test lint skill release-validation

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run ./...

skill:
	go run ./cmd/genskill

release-validation:
	GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test -race -shuffle=on -count=1 ./...
	@if test -n "$$MADE_GITHUB_SMOKE_REPO" && test -n "$$MADE_GITHUB_SMOKE_PR_URL"; then \
		GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=commit.gpgsign GIT_CONFIG_VALUE_0=false go test -race -shuffle=on -count=1 ./internal/github -run '^TestLive_CIWorkflowOwnershipSmoke$$' -v; \
	else \
		echo 'release-validation: GitHub smoke skipped; set MADE_GITHUB_SMOKE_REPO and MADE_GITHUB_SMOKE_PR_URL'; \
	fi
