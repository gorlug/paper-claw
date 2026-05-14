.PHONY: setup format lint test check build deploy

GOPATH_BIN := $(shell go env GOPATH)/bin

setup:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/evilmartians/lefthook@latest
	ln -sf $(GOPATH_BIN)/golangci-lint ~/.local/bin/golangci-lint
	ln -sf $(GOPATH_BIN)/lefthook ~/.local/bin/lefthook
	lefthook install

format:
	gofmt -w .

lint:
	$(GOPATH_BIN)/golangci-lint run ./...

test:
	go test -race -count=1 ./...

build:
	go build -o bin/paperclaw ./cmd/paperclaw

deploy: build
	sudo install -m 755 bin/paperclaw /usr/local/bin/paperclaw

check: format lint test
