# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test           # run all tests (with -race -count=1)
make lint           # run golangci-lint
make format         # gofmt -w .
make check          # format + lint + test
make deadcode       # find unreachable code (golang.org/x/tools/cmd/deadcode)
make fmt-check      # non-mutating format check for CI
make help-snapshot  # regenerate docs/cli-help.txt after CLI changes
make smoke          # end-to-end smoke test (requires ANTHROPIC_API_KEY for live steps)

go test -run TestFormatDirName ./internal/document/  # run a single test
```

Pre-commit hooks (via lefthook) enforce formatting, linting, secret scanning (gitleaks), and tests on every commit. If `gofmt` reformats files, the commit is blocked — re-stage and commit again. Run `make setup` once on a fresh clone to install all tools and register the hooks.

## End-to-end smoke test

After making changes, verify the golden path works:

```bash
make smoke
```

The script builds the binary, runs `process` / `list` / `show` / `search` against `testdata/` PDFs, and asserts JSON output is valid. The `process` step is skipped if `ANTHROPIC_API_KEY` is not set (all other assertions still run).

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

golangci-lint (v2.x) runs `errcheck`, `errorlint`, `gocritic`, `goimports`, `gosec`, `govet`, `revive`, `staticcheck`, `unparam`, `unused`. The `goimports` local prefix is `paper-claw`.

## What NOT to do

- **Do not re-introduce inbox deletion.** The `process` command moves processed files to the `--processed` directory (`~/paperclaw/processed` by default). Never delete them with `os.Remove`.
- **The OCR tool is `pdftotext`**, not `ocrmypdf` or `tesseract`. Some older docs mention ocrmypdf — it was replaced. Don't revert.
- **`PAPERCLAW_INBOX` and `PAPERCLAW_LIBRARY` env vars are not implemented.** The README and `docs/plan.md` document them as a goal, but `os.Getenv` is never called. Don't assume they work; don't add silent env-var reads without wiring them fully.
- **Do not loosen the document-type enum** in `internal/document/schema.json` without updating all classifier prompts and tests.
- **If CLI flags or commands change**, regenerate the help snapshot: `make help-snapshot && git add docs/cli-help.txt`.

# Test first

Always create tests that cover the code you're adding. The tests should cover multiple possible inputs.

# Test first, then refactor

Refactor code only after it has been thoroughly tested. Avoid premature optimization and unnecessary complexity.

After you think you're finished, run linter, formatter, and tests before committing.

Always commit with a meaningful message.
