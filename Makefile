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
GOLANGCI_LINT_VERSION   := v2.14.0
GOVULNCHECK_VERSION     := v1.8.0
ACTIONLINT_VERSION      := v1.7.12
CYCLONEDX_GOMOD_VERSION := v1.12.0
# markdownlint-cli2 is an npm package; it needs Node.js and npm.
MARKDOWNLINT_VERSION    := 0.23.3

GOLANGCI_LINT   := $(TOOLS_DIR)/golangci-lint-$(GOLANGCI_LINT_VERSION)/golangci-lint
GOVULNCHECK     := $(TOOLS_DIR)/govulncheck-$(GOVULNCHECK_VERSION)/govulncheck
ACTIONLINT      := $(TOOLS_DIR)/actionlint-$(ACTIONLINT_VERSION)/actionlint
CYCLONEDX_GOMOD := $(TOOLS_DIR)/cyclonedx-gomod-$(CYCLONEDX_GOMOD_VERSION)/cyclonedx-gomod
MARKDOWNLINT    := $(TOOLS_DIR)/markdownlint-cli2-$(MARKDOWNLINT_VERSION)/node_modules/.bin/markdownlint-cli2

# Release artefacts (make release VERSION=x.y.z).
RELEASE_DIR := $(CURDIR)/dist/release

# The Gitea workflows are the source; the GitHub mirrors must be byte-identical.
WORKFLOWS := ci.yml release.yml website-release.yml

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
	$(ACTIONLINT) $(foreach w,$(WORKFLOWS),.gitea/workflows/$(w) .github/workflows/$(w))
	@for w in $(WORKFLOWS); do \
		cmp -s .gitea/workflows/$$w .github/workflows/$$w || { \
			echo ".gitea/workflows/$$w and .github/workflows/$$w differ; keep them identical"; \
			exit 1; \
		}; \
	done

.PHONY: lint-md
lint-md: $(MARKDOWNLINT) ## Lint the Markdown files with markdownlint-cli2 (.markdownlint-cli2.yaml)
	$(MARKDOWNLINT) '**/*.md'

.PHONY: release
release: $(CYCLONEDX_GOMOD) ## Build reproducible release tarballs, SBOMs and SHA256SUMS (VERSION=x.y.z)
	VERSION='$(VERSION)' GO='$(GO)' CYCLONEDX_GOMOD='$(CYCLONEDX_GOMOD)' packaging/release.sh $(RELEASE_DIR)

.PHONY: image
image: ## Build the container image obie:$(VERSION) (docker)
	docker build --build-arg VERSION='$(VERSION)' -t obie:$(VERSION) .

.PHONY: lab-smoke
lab-smoke: ## Start the 3-node compose lab, check the nodes see each other and block, remove it (docker)
	packaging/compose/smoke-test.sh

.PHONY: sandbox-check
sandbox-check: ## Run every step of documentation/sandbox.md against a sandbox of its own and compare the output (docker)
	$(GO) test -tags sandbox -run '^TestWalkthrough$$' -count=1 -v -timeout 20m ./test/sandbox

.PHONY: tutorial-check
tutorial-check: $(CYCLONEDX_GOMOD) ## Run every command of documentation/getting-started.md on a systemd host in a container and compare the output (docker, privileged)
	$(GO) test -tags tutorial -run '^TestTutorial$$' -count=1 -v -timeout 30m ./test/tutorial -tutorial.cyclonedx=$(CYCLONEDX_GOMOD)

.PHONY: guides-check
guides-check: $(CYCLONEDX_GOMOD) ## Run every command of the how-to guides in documentation/guides on a systemd host in a container and compare the output (docker, privileged)
	$(GO) test -tags tutorial -run '^TestGuides$$' -count=1 -v -timeout 60m ./test/tutorial -tutorial.cyclonedx=$(CYCLONEDX_GOMOD)

.PHONY: fail2ban-versions
fail2ban-versions: ## Ban and unban through the Fail2Ban action with the Fail2Ban of current distributions (docker); not part of `make ci`
	contrib/fail2ban/check-versions.sh

.PHONY: check-unit
check-unit: build ## Check the systemd unit with systemd-analyze (verify, exposure <= 3.0)
	packaging/systemd/check-unit.sh $(BIN_DIR)

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

# RESOURCESVERDICTS are the verdicts the measured node holds at each
# measurement of `make resources`; RESOURCESRATE how many reports per second
# each of the two publishing nodes sends.
RESOURCESVERDICTS ?= 10000,100000
RESOURCESRATE     ?= 100

.PHONY: resources
resources: build ## Measure one node's memory, CPU and disk in a 3-node mesh: idle, receiving verdicts, at rest; not part of `make ci`
	$(GO) test -tags resources -run '^TestResources$$' -count=1 -v -timeout 0 ./test/resources \
		-resources.bin=$(BIN_DIR) -resources.verdicts=$(RESOURCESVERDICTS) -resources.rate=$(RESOURCESRATE)

# The trust simulation (ADR 0034): SCENARIO is baseline, reduced (the one CI
# runs) or one behavior model; SEEDS the number of seeds; TRACE a trace file
# to replay instead of the synthetic world; OUT the report's directory.
sim-trust: SCENARIO ?= reduced
sim-trust: SEEDS ?= 20
sim-trust: TRACE ?=
sim-trust: OUT ?= $(BIN_DIR)/sim-trust/$(SCENARIO)

.PHONY: sim-trust
sim-trust: ## Run the trust simulation SCENARIO (default reduced) over SEEDS seeds, report to OUT; not part of `make ci`
	$(GO) test -tags simtrust -run '^TestSimTrust$$' -count=1 -v -timeout 0 ./test/simtrust \
		-simtrust.scenario='$(SCENARIO)' -simtrust.seeds='$(SEEDS)' -simtrust.trace='$(TRACE)' \
		-simtrust.out='$(OUT)' -simtrust.version='$(VERSION)'

.PHONY: ci
ci: fmt-check vet lint lint-workflows lint-md test vuln ## Run every check the CI gate runs

.PHONY: clean
clean: ## Remove build output, release artefacts and installed tools
	rm -rf $(BIN_DIR) $(RELEASE_DIR)

$(GOLANGCI_LINT):
	GOBIN=$(dir $@) $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

$(GOVULNCHECK):
	GOBIN=$(dir $@) $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

$(ACTIONLINT):
	GOBIN=$(dir $@) $(GO) install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

$(MARKDOWNLINT):
	npm install --prefix $(TOOLS_DIR)/markdownlint-cli2-$(MARKDOWNLINT_VERSION) --no-audit --no-fund \
		markdownlint-cli2@$(MARKDOWNLINT_VERSION)

$(CYCLONEDX_GOMOD):
	GOBIN=$(dir $@) $(GO) install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@$(CYCLONEDX_GOMOD_VERSION)
