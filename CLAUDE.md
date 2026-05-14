# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make test           # run all tests (with -race -count=1)
make lint           # run golangci-lint
make format         # gofmt -w .
make vuln           # scan dependencies for known CVEs (govulncheck)
make check          # format + lint + test + vuln
make deadcode       # find unreachable code (golang.org/x/tools/cmd/deadcode)
make fmt-check      # non-mutating format check for CI
make help-snapshot  # regenerate docs/cli-help.txt after CLI changes
make smoke          # end-to-end smoke test (requires ANTHROPIC_API_KEY for live steps)

go test -run TestFormatDirName ./internal/document/  # run a single test
```

Pre-commit hooks (via lefthook) enforce formatting, linting, secret scanning (gitleaks), dependency vulnerability scanning (govulncheck), and tests on every commit. If `gofmt` reformats files, the commit is blocked — re-stage and commit again. Run `make setup` once on a fresh clone to install all tools and register the hooks.

## End-to-end smoke test

After making changes, verify the golden path works:

```bash
make smoke
```

The script builds the binary, runs `process` / `list` / `show` / `search` against `testdata/` PDFs, and asserts JSON output is valid. The `process` step is skipped if `ANTHROPIC_API_KEY` is not set (all other assertions still run).

## Architecture

paper-claw runs in two modes:

**CLI** (`process` / `list` / `show` / `search`) — processes a local inbox directory once and exits. File I/O uses `os.*` directly via `internal/storage/local`.

**Daemon** (`serve`) — long-lived server that polls a Google Drive inbox and also accepts Drive push notifications (Phase 3). All Drive I/O goes through `internal/storage/drive`. State (dedup index, OAuth token, sync state, counters) lives in a SQLite database at `<state.dir>/paperclaw.db`.

Library layout follows the sidecar pattern:

```
library/
  2026-05-13_Finanzamt_Letter/
    document.pdf
    transcript.md
    metadata.json
```

The storage-agnostic pipeline (`internal/document.ProcessOne`) is shared by both modes. Source packages:

- `internal/document/`     — core domain types, pipeline, OCR, classification
- `internal/storage/local` — os.* Storage impl (CLI only)
- `internal/storage/drive` — Google Drive Storage impl (daemon only)
- `internal/storage/fake`  — in-memory Storage impl (tests only)
- `internal/config/`       — YAML config + env secrets loader
- `internal/store/`        — SQLite state store (modernc.org/sqlite, pure Go)
- `internal/oauth/`        — Google OAuth2 flow + /oauth/start + /oauth/callback
- `internal/serverhttp/`   — HTTP server, /healthz, /readyz, Anthropic prober
- `internal/serve/`        — serialised scan worker

Secrets are injected at runtime via **Infisical** (see `.infisical.json`).

## Linters

golangci-lint (v2.x) runs `errcheck`, `errorlint`, `gocritic`, `goimports`, `gosec`, `govet`, `revive`, `staticcheck`, `unparam`, `unused`. The `goimports` local prefix is `paper-claw`.

## What NOT to do

- **Do not re-introduce inbox deletion.** The `process` command moves processed files to the `--processed` directory (`~/paperclaw/processed` by default). Never delete them with `os.Remove`.
- **The OCR tool is `pdftotext`**, not `ocrmypdf` or `tesseract`. Some older docs mention ocrmypdf — it was replaced. Don't revert.
- **`PAPERCLAW_INBOX` and `PAPERCLAW_LIBRARY` env vars are not implemented.** The README and `docs/plan.md` document them as a goal, but `os.Getenv` is never called. Don't assume they work; don't add silent env-var reads without wiring them fully.
- **Do not loosen the document-type enum** in `internal/document/schema.json` without updating all classifier prompts and tests.
- **If CLI flags or commands change**, regenerate the help snapshot: `make help-snapshot && git add docs/cli-help.txt`.
- **The dependency vulnerability scanner is `govulncheck`**, not `trivy`, `snyk`, or `nancy`. It uses the Go vulnerability database (vuln.go.dev) and is the official Go team tool. Don't replace it.
- **The daemon does NOT write `process.log`**. The `process` CLI command writes `process.log`; the daemon emits structured `slog` JSON events to stderr only.
- **CLI commands (`process`/`list`/`show`/`search`) are local-only** and use `os.*` directly. Only the `serve` daemon talks to Google Drive.
- **SQLite driver is `modernc.org/sqlite`** (pure Go, no cgo). Do not switch to `mattn/go-sqlite3` — the Docker image builds with `CGO_ENABLED=0`.
- **Telemetry goes through a local OpenTelemetry Collector**, not directly to dash0. OTLP config comes from standard `OTEL_*` env vars; the Go SDK honours them natively.
- **The Docker image is built with `CGO_ENABLED=0`** — the SQLite driver (`modernc.org/sqlite`) is pure Go. Do not add any cgo dependencies or the build will break.
- **The `internal/document` package must stay OTEL-free.** The `Observer` interface lives there; the OTEL-backed `SpanObserver` lives in `internal/telemetry`. Never import `go.opentelemetry.io/otel` from `internal/document`.
- **Webhook handlers must return immediately** and only enqueue a scan. Never process a Drive push notification inline.
- **Drive push notification channels expire** (~7 days); the renewal timer in `ChannelManager` is mandatory. Both poll ticker and webhook share the same coalescing worker channel — never add a second worker.
- **Both poll ticker and webhook use the same coalescing `scanRequests` channel** (capacity 1). Do not add a second worker or a second channel.
- **The webhook domain must be verified** in Google Cloud Console (APIs & Services → Domain verification) before Drive will POST to it — this is an operational prerequisite.
- **OAuth scope is `https://www.googleapis.com/auth/drive`** (broad). Do not narrow to `drive.file` — moving user-dropped inbox files requires the full scope.

# Test first

Always create tests that cover the code you're adding. The tests should cover multiple possible inputs.

# Test first, then refactor

Refactor code only after it has been thoroughly tested. Avoid premature optimization and unnecessary complexity.

After you think you're finished, run linter, formatter, and tests before committing.

Always commit with a meaningful message.
