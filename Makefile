# OBIE build tooling. Run `make help` for an overview; `make ci` must pass
# before every commit.

GO      ?= go
MODULE  := github.com/MNCloudwerksTechnology/obie
CMDS    := obied obiectl
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(MODULE)/internal/version.Version=$(VERSION)

BIN_DIR   := $(CURDIR)/bin
TOOLS_DIR := $(BIN_DIR)/tools

# Pinned tool versions. Tools are installed into a versioned directory under
# ./bin/tools, so bumping a version here reinstalls it automatically.
GOLANGCI_LINT_VERSION := v2.14.0
GOVULNCHECK_VERSION   := v1.8.0
ACTIONLINT_VERSION    := v1.7.12

GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)/golangci-lint
GOVULNCHECK   := $(TOOLS_DIR)/govulncheck-$(GOVULNCHECK_VERSION)/govulncheck
ACTIONLINT    := $(TOOLS_DIR)/actionlint-$(ACTIONLINT_VERSION)/actionlint

# The Gitea workflow is the source; the GitHub mirror must be byte-identical.
WORKFLOWS := .gitea/workflows/ci.yml .github/workflows/ci.yml

.DEFAULT_GOAL := build

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  %-10s %s\n", $$1, $$2}'

.PHONY: build
build: ## Build static binaries into ./bin/
	@mkdir -p $(BIN_DIR)
	@for cmd in $(CMDS); do \
		echo "go build $$cmd ($(VERSION))"; \
		CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' \
			-o $(BIN_DIR)/$$cmd ./cmd/$$cmd || exit 1; \
	done

.PHONY: test
test: ## Run all tests with the race detector
	$(GO) test -race -count=1 ./...

.PHONY: test-privileged
test-privileged: ## Run all tests including the `privileged` build tag (needs root)
	$(GO) test -race -count=1 -tags privileged ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is not gofmt-formatted
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed for:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint (pinned version, .golangci.yml)
	$(GOLANGCI_LINT) run ./...

.PHONY: vuln
vuln: $(GOVULNCHECK) ## Scan for known vulnerabilities with govulncheck
	$(GOVULNCHECK) ./...

.PHONY: lint-workflows
lint-workflows: $(ACTIONLINT) ## Validate the CI workflows with actionlint and check they are identical
	$(ACTIONLINT) $(WORKFLOWS)
	@cmp -s $(WORKFLOWS) || { \
		echo "$(word 1,$(WORKFLOWS)) and $(word 2,$(WORKFLOWS)) differ; keep them identical"; \
		exit 1; \
	}

# FUZZTIME is how long `make fuzz` runs each fuzz target.
FUZZTIME ?= 30s

.PHONY: fuzz
fuzz: ## Run every fuzz target (func Fuzz* in *_test.go) for FUZZTIME each (default 30s)
	@set -e; \
	for file in $$(grep -rl --include='*_test.go' --exclude-dir=website '^func Fuzz' . | sort); do \
		for target in $$(sed -n 's/^func \(Fuzz[A-Za-z0-9_]*\)(.*/\1/p' "$$file"); do \
			echo "fuzz $$target in $$(dirname "$$file") for $(FUZZTIME)"; \
			$(GO) test -run='^$$' -fuzz="^$$target\$$" -fuzztime=$(FUZZTIME) "$$(dirname "$$file")"; \
		done; \
	done

# SOAKTIME is how long `make soak` sends events; SOAKRATE how many per second.
SOAKTIME ?= 30m
SOAKRATE ?= 50

.PHONY: soak
soak: ## Run the soak test (3 nodes, SOAKRATE events/s for SOAKTIME, default 50/s for 30m); not part of `make ci`
	$(GO) test -tags soak -run '^TestSoak$$' -count=1 -v -timeout 0 ./test/e2e \
		-soak.duration=$(SOAKTIME) -soak.rate=$(SOAKRATE)

.PHONY: ci
ci: fmt-check vet lint lint-workflows test vuln ## Run every check the CI gate runs

.PHONY: clean
clean: ## Remove build output and installed tools
	rm -rf $(BIN_DIR)

$(GOLANGCI_LINT):
	GOBIN=$(dir $@) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(GOVULNCHECK):
	GOBIN=$(dir $@) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

$(ACTIONLINT):
	GOBIN=$(dir $@) $(GO) install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
