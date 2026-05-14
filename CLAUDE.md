# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test        # run all tests (with -race -count=1)
make lint        # run golangci-lint
make format      # gofmt -w .
make check       # format + lint + test

go test -run TestFormatDirName ./internal/document/  # run a single test
```

Pre-commit hooks (via lefthook) enforce formatting, linting, and tests on every commit. If `gofmt` reformats files, the commit is blocked — re-stage and commit again.

## Architecture

paper-claw is a CLI tool for managing PDF documents. PDFs land in an **inbox** directory; each run processes all files there in three steps: transcribe content, derive a sensible name, move to the **library**.

Library layout follows the sidecar pattern:

```
library/
  2026-05-13_Finanzamt_Letter/
    document.pdf
    transcript.md
    metadata.json
```

Source is organised under `internal/`:

- `internal/document/` — core domain logic (naming, metadata)

Secrets are injected at runtime via **Infisical** (see `.infisical.json`).

## Linters

golangci-lint runs `gocritic`, `goimports`, `gosec`, and `revive`. The `goimports` local prefix is `papwer-claw` (note the typo — matches the module name in `go.mod`).

# Test first

Always create tests that cover the code you're adding. The tests should cover multiple possible inputs.

# Test first, then refactor

Refactor code only after it has been thoroughly tested. Avoid premature optimization and unnecessary complexity.

After you think you're finished, run linter, formatter, and tests before committing.
