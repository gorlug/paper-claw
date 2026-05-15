.PHONY: setup format fmt-check lint test build deadcode smoke vuln check docker-build

SHELL          := /bin/bash
GOPATH_BIN     := $(shell go env GOPATH)/bin
GOLANGCI_VERSION     := v2.12.2
LEFTHOOK_VERSION     := v2.1.6
GITLEAKS_VERSION     := v8.30.1
GOVULNCHECK_VERSION  := v1.3.0
DEADCODE_VERSION     := v0.31.0

GOLANGCI    := $(shell command -v golangci-lint 2>/dev/null || echo $(GOPATH_BIN)/golangci-lint)
GOVULNCHECK := $(shell command -v govulncheck 2>/dev/null || echo $(GOPATH_BIN)/govulncheck)

setup:
	mkdir -p ~/.local/bin
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	go install github.com/evilmartians/lefthook/v2@$(LEFTHOOK_VERSION)
	go install github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	go install golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION)
	ln -sf $(GOPATH_BIN)/golangci-lint ~/.local/bin/golangci-lint
	ln -sf $(GOPATH_BIN)/lefthook ~/.local/bin/lefthook
	ln -sf $(GOPATH_BIN)/gitleaks ~/.local/bin/gitleaks
	ln -sf $(GOPATH_BIN)/govulncheck ~/.local/bin/govulncheck
	ln -sf $(GOPATH_BIN)/deadcode ~/.local/bin/deadcode
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
	go run golang.org/x/tools/cmd/deadcode@$(DEADCODE_VERSION) -test ./...

smoke:
	@bash scripts/smoke-test.sh

vuln:
	@test -x "$(GOVULNCHECK)" || { echo "govulncheck not found — run: make setup" >&2; exit 1; }
	$(GOVULNCHECK) ./...

check: format lint test vuln

docker-build:
	docker build -t paperclaw .

-include Makefile.local
