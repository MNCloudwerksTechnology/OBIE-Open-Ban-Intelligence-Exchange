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

GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)/golangci-lint
GOVULNCHECK   := $(TOOLS_DIR)/govulncheck-$(GOVULNCHECK_VERSION)/govulncheck

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

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is not gofmt-formatted
	@unformatted="$$(gofmt -l $$(git ls-files --cached --others --exclude-standard '*.go'))"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed for:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint (pinned version, .golangci.yml)
	$(GOLANGCI_LINT) run ./...

.PHONY: vuln
vuln: $(GOVULNCHECK) ## Scan for known vulnerabilities with govulncheck
	$(GOVULNCHECK) ./...

.PHONY: ci
ci: fmt-check vet lint test vuln ## Run every check the CI gate runs

.PHONY: clean
clean: ## Remove build output and installed tools
	rm -rf $(BIN_DIR)

$(GOLANGCI_LINT):
	GOBIN=$(dir $@) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(GOVULNCHECK):
	GOBIN=$(dir $@) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
