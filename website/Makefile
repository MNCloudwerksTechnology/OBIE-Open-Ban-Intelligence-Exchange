# OBIE website build tooling. Run `make -C website help` for an overview;
# `make -C website ci` must pass before every commit that touches website/.
# Independent of the Go node's root Makefile.

NPM  ?= npm
JAVA ?= java
GO   ?= go

FRONTEND := frontend
BACKEND  := backend
MVNW     := ./mvnw -B

BIN_DIR   := $(CURDIR)/bin
TOOLS_DIR := $(BIN_DIR)/tools

# Pinned tool version, installed into a versioned directory under ./bin/tools.
OSV_SCANNER_VERSION := v2.6.0
OSV_SCANNER := $(TOOLS_DIR)/osv-scanner-$(OSV_SCANNER_VERSION)/osv-scanner
# Lighthouse CI, run with npx at this pinned version (not a front-end dependency).
LHCI_VERSION := 0.15.1

# Stamp written by `npm ci`; reinstall whenever the lockfile changes.
NODE_MODULES := $(FRONTEND)/node_modules/.package-lock.json
# Runtime dependency SBOM written by `mvnw verify` (Spring Boot's CycloneDX setup).
SBOM := $(BACKEND)/target/classes/META-INF/sbom/application.cdx.json
JAR  := $(BACKEND)/target/obie-website.jar

.DEFAULT_GOAL := build

# `ci` relies on its prerequisites running in order (vuln scans the SBOM that
# backend writes), so never run them in parallel, even under `make -j`.
.NOTPARALLEL:

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  %-16s %s\n", $$1, $$2}'

$(NODE_MODULES): $(FRONTEND)/package-lock.json
	cd $(FRONTEND) && $(NPM) ci

.PHONY: frontend-lint
frontend-lint: $(NODE_MODULES) ## Lint the front end (ESLint + Prettier)
	cd $(FRONTEND) && $(NPM) run lint

.PHONY: frontend-test
frontend-test: $(NODE_MODULES) ## Run the front-end unit tests
	cd $(FRONTEND) && $(NPM) test

.PHONY: frontend
frontend: $(NODE_MODULES) ## Production build of the front end, prerendered to static HTML
	cd $(FRONTEND) && $(NPM) run build

.PHONY: backend
backend: frontend ## Build and verify the jar (tests, Spotless, SpotBugs) with the front end inside
	cd $(BACKEND) && $(MVNW) verify

.PHONY: build
build: backend ## Build the single deployable artefact: backend/target/obie-website.jar

.PHONY: run
run: ## Run the built jar on http://localhost:8080
	$(JAVA) -jar $(JAR)

.PHONY: vuln
vuln: $(NODE_MODULES) $(OSV_SCANNER) ## Scan runtime dependencies for known vulnerabilities (needs `make backend`)
	cd $(FRONTEND) && $(NPM) audit --omit=dev --audit-level=high
	$(OSV_SCANNER) scan source --lockfile $(SBOM)

.PHONY: lighthouse
lighthouse: build ## Lighthouse CI (mobile) against the locally started jar; fails below 95 in any category
	JAR=$(JAR) LHCI_VERSION=$(LHCI_VERSION) ./scripts/lighthouse.sh

.PHONY: ci
ci: frontend-lint frontend-test backend vuln ## Run every check the CI website job runs

.PHONY: clean
clean: ## Remove build output and installed tools
	rm -rf $(FRONTEND)/dist $(FRONTEND)/.angular $(BACKEND)/target $(BIN_DIR) .lighthouseci

$(OSV_SCANNER):
	GOBIN=$(dir $@) $(GO) install github.com/google/osv-scanner/v2/cmd/osv-scanner@$(OSV_SCANNER_VERSION)
