# PaperClaw

A CLI tool that turns an inbox of PDFs into an organised, agent-searchable document library. PDFs are OCR'd, classified by Claude, given a deterministic filename, and filed into a flat sidecar library.

## Requirements

- Go 1.21+
- [`pdftotext`](https://poppler.freedesktop.org/) (part of the `poppler-utils` / `poppler` package)
- An Anthropic API key (for document classification)

## Installation

```bash
make deploy
```

This builds the binary and installs it to `/usr/local/bin/paperclaw` (requires sudo). To build without installing:

```bash
make build   # output: bin/paperclaw
```

## Configuration

| Setting | Flag | Default |
|---|---|---|
| Inbox | `--inbox` | `~/paperclaw/inbox` |
| Library | `--library` | `~/paperclaw/library` |
| Processed | `--processed` | `~/paperclaw/processed` |

`ANTHROPIC_API_KEY` must be set in the environment for the `process` command.

## Usage

### Process inbox PDFs

```bash
paperclaw process [--inbox PATH] [--library PATH] [--processed PATH]
```

Walks the inbox, OCRs each PDF, classifies it with Claude, and files it into the library. After successful processing, the original PDF is moved from the inbox to the `--processed` directory (default `~/paperclaw/processed`). Duplicates are skipped (detected by SHA-256 hash). Failed documents land in `library/_quarantine/` with a `processing_error.json` explaining the failure.

```
3 documents processed, 1 skipped (duplicate), 0 quarantined
```

### List documents

```bash
paperclaw list [--type TYPE] [--since DATE] [--vendor VENDOR] [--overdue]
```

Filters are combinable:

| Flag | Description |
|---|---|
| `--type` | `invoice`, `utility_bill`, `bank_statement`, `insurance_letter`, `contract`, `government_letter`, `other` |
| `--since` | Documents dated on or after `YYYY-MM-DD` |
| `--vendor` | Substring match on vendor name (case-insensitive) |
| `--overdue` | Only documents with a past `due_date` |

```bash
paperclaw list --type utility_bill --overdue
paperclaw list --vendor stadtwerke --since 2026-01-01
```

### Show a document

```bash
paperclaw show <id-prefix>
```

Prints full metadata and OCR transcript for one document. The `id-prefix` is a short prefix of the SHA-256 document ID (8+ characters is typically unambiguous).

### Search transcripts

```bash
paperclaw search <query>
```

Full-text search across all OCR transcripts. Returns matching document entries and their IDs; use `paperclaw show` to fetch the full content.

```bash
paperclaw search IBAN
paperclaw search "Rechnungsnummer 2024"
```

### JSON output

All commands print JSON when stdout is not a TTY, so they compose cleanly with `jq` and agent tooling:

```bash
paperclaw list --type invoice | jq '.[].summary'
```

## Library layout

```
~/paperclaw/
  inbox/            ← drop PDFs here
  processed/        ← originals land here after successful processing
  library/
    process.log
    2026-04-01_stadtwerke_strom-rechnung/
      document.pdf
      transcript.md
      metadata.json
    _quarantine/
      bad-scan.pdf/
        document.pdf
        processing_error.json
```

Each `metadata.json` contains the document type, date, vendor, summary, and optional fields (amount, currency, due date, tags, language).

## Server mode (daemon)

`paperclaw serve` runs as a long-lived daemon. The inbox and library live in Google Drive. Telemetry (logs, metrics, traces) is exported via OpenTelemetry to a local Collector that forwards to dash0.

### Quick start

```bash
cp deploy/paperclaw.example.yaml deploy/paperclaw.yaml
# Fill in drive folder IDs, OAuth client ID, bind_addr, public_base_url.

# Set secrets (or use Infisical):
export ANTHROPIC_API_KEY=sk-ant-...
export OAUTH_CLIENT_SECRET=GOCSPX-...
export DASH0_AUTH_TOKEN=...

docker compose -f deploy/docker-compose.yml up -d
```

Then visit `http://localhost:8080/oauth/start` to authorise Google Drive access. Once the OAuth flow completes, `/readyz` returns 200 and the daemon begins polling the Drive inbox.

### Endpoints

| Path | Description |
|---|---|
| `GET /healthz` | Always 200 — liveness probe |
| `GET /readyz` | 200 when Drive is authenticated and Anthropic is reachable; 503 otherwise |
| `GET /oauth/start` | Starts the Google OAuth2 consent flow |
| `GET /oauth/callback` | Redirect target registered in Google Cloud Console |

### Telemetry

Traces, metrics, and structured logs are exported via OTLP/gRPC to the local OpenTelemetry Collector (second service in `docker-compose.yml`), which forwards them to dash0. Configure the Collector endpoint with the standard `OTEL_EXPORTER_OTLP_ENDPOINT` env var. When that variable is unset (e.g., local development), paper-claw falls back to structured JSON logging on stderr and no-op metric/trace providers.

Metrics exported:
- `documents.processed` — count + processing duration histogram (`document.processing.duration`)
- `documents.skipped` — duplicates detected by SHA-256 hash
- `documents.quarantined` — pipeline failures
- `poll.count` — Drive inbox poll cycles

Traces follow each document through its pipeline stages: `process_document` → `ocr` → `classify` → `library_write`.

### State

All daemon state lives in a SQLite database at `<state.dir>/paperclaw.db` (default `/data/paperclaw.db`). Mount a Docker volume at `/data` to persist state across container restarts.

## Development

```bash
make check   # format + lint + test
make test    # tests only
make lint    # lint only
```

Pre-commit hooks (via lefthook) run format, lint, secret scanning (gitleaks), and tests automatically. Run `make setup` once to install all tools and register the hooks.

## Claude Code skill

`skills/paperclaw/SKILL.md` is a Claude Code project skill that lets an agent drive PaperClaw on your behalf. It is installed as a slash command via `.claude/commands/paperclaw.md`.

To use it, open this project in Claude Code and type `/paperclaw` followed by a natural-language question:

```
/paperclaw Which utility bills are overdue?
/paperclaw Find the invoice for the gadget I bought in March.
/paperclaw Show me the latest electricity bill from Stadtwerke.
/paperclaw Search for my IBAN across all documents.
```

The agent maps your question to the right `paperclaw` subcommand, runs it, and returns a plain-language answer. It uses `list` for filtering, `search` for keyword lookup, and `show` when you need the full document content.
