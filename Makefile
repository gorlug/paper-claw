.PHONY: setup format fmt-check lint test check build deploy deadcode help-snapshot help-check smoke vuln

SHELL          := /bin/bash
GOPATH_BIN     := $(shell go env GOPATH)/bin
GOLANGCI_VERSION     := v2.12.2
LEFTHOOK_VERSION     := v2.1.6
GITLEAKS_VERSION     := v8.30.1
GOVULNCHECK_VERSION  := v1.3.0

GOLANGCI    := $(shell command -v golangci-lint 2>/dev/null || echo $(GOPATH_BIN)/golangci-lint)
GOVULNCHECK := $(shell command -v govulncheck 2>/dev/null || echo $(GOPATH_BIN)/govulncheck)

setup:
	mkdir -p ~/.local/bin
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_VERSION)
	go install github.com/evilmartians/lefthook@$(LEFTHOOK_VERSION)
	go install github.com/gitleaks/gitleaks/v8@$(GITLEAKS_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	ln -sf $(GOPATH_BIN)/golangci-lint ~/.local/bin/golangci-lint
	ln -sf $(GOPATH_BIN)/lefthook ~/.local/bin/lefthook
	ln -sf $(GOPATH_BIN)/gitleaks ~/.local/bin/gitleaks
	ln -sf $(GOPATH_BIN)/govulncheck ~/.local/bin/govulncheck
	lefthook install

format:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt: files need formatting — run: make format"; gofmt -l .; exit 1; }

lint:
	@test -x "$(GOLANGCI)" || { echo "golangci-lint not found — run: make setup" >&2; exit 1; }
	$(GOLANGCI) run ./...

test:
	go test -race -count=1 ./...

build:
	go build -o bin/paperclaw ./cmd/paperclaw

deadcode:
	go run golang.org/x/tools/cmd/deadcode@latest ./...

help-snapshot: build
	./bin/paperclaw -help >docs/cli-help.txt 2>&1; true

help-check: build
	@diff docs/cli-help.txt <(./bin/paperclaw -help 2>&1) || { echo "CLI help changed — run: make help-snapshot && git add docs/cli-help.txt"; exit 1; }

smoke:
	@bash scripts/smoke-test.sh

vuln:
	@test -x "$(GOVULNCHECK)" || { echo "govulncheck not found — run: make setup" >&2; exit 1; }
	$(GOVULNCHECK) ./...

check: format lint test vuln

deploy: build
	sudo install -m 755 bin/paperclaw /usr/local/bin/paperclaw
