# PaperClaw — M1 Design

PaperClaw is a Go CLI that turns an inbox folder of PDFs into an organised library an agent can search. PDFs are OCR'd, classified, given a deterministic filename, and filed into a flat, sidecar-pattern library. The agent reaches the library through a Claude Skill calling the same CLI.

## Optimization lens

This project is built for the **best agentic engineering setup** dimension: investment goes into JSON Schema validation, golden fixtures, an eval harness, drift checks, and an agentic review loop. Architecture and agent-native polish are second-order. The 2-hour build target is a thin slice through M3 (one PDF end-to-end answerable by an agent) so that feedback loops cover the whole pipeline from day one.

## Pipeline

```
inbox/foo.pdf
   │
   ▼  hash (SHA-256) → check library for duplicate; skip if seen
   │
   ▼  OCR (deterministic) → transcript.md
   │
   ▼  Classify + extract metadata (Claude Sonnet 4.6, JSON-schema-constrained)
   │
   ▼  Validate against metadata schema
   │
   ├──► library/YYYY-MM-DD_<vendor>_<desc>/{document.pdf, transcript.md, metadata.json}
   │
   └──► library/_quarantine/<filename>/{document.pdf, processing_error.json}  (on any failure)
```

The transcription stage uses `ocrmypdf` / `tesseract` only — no LLM. Transcripts are byte-stable across runs, which makes golden-file tests trivial. The only stochastic stage is the classifier call, isolated behind a JSON Schema.

## Data model

### Classification taxonomy

`type` is a closed enum, schema-validated, eval-comparable by exact match:

```
invoice · utility_bill · bank_statement · insurance_letter · contract · government_letter · other
```

Plus an open `tags: []` for everything that doesn't fit the enum. `other` is the escape hatch when no enum value applies.

### `metadata.json` schema

Required fields:

| Field | Type | Notes |
|---|---|---|
| `id` | string | SHA-256 of the source PDF — also the dedupe key |
| `type` | enum (above) | |
| `document_date` | ISO 8601 date | Extracted from the document content, not file mtime |
| `vendor` | string | Issuer of the document |
| `summary` | string | One-sentence agent-readable summary |
| `source_filename` | string | Original name in the inbox |
| `processed_at` | ISO 8601 datetime | When the CLI ran |
| `content_hash` | string | SHA-256, same as `id` |

Optional fields (extracted when present):

`amount` (number), `currency` (ISO 4217), `due_date` (ISO 8601), `tags` (string[]), `language` (BCP-47).

A JSON Schema for this lives at `internal/document/schema.json` and is validated in CI on every produced `metadata.json` plus on all golden fixtures.

## Library layout

Flat, doc-per-directory, slug-named — the agent walks `metadata.json` files, not the tree:

```
library/
  2026-05-13_stadtwerke_strom-rechnung/
    document.pdf
    transcript.md
    metadata.json
  2026-04-02_finanzamt_steuerbescheid-2024/
    ...
  _quarantine/
    weird-scan.pdf/
      document.pdf
      processing_error.json
```

**Slug rules:** lowercase ASCII, hyphenated, vendor ≤30 chars, description ≤40 chars, German umlauts transliterated (`ü`→`ue`). Collisions resolved with `-2`, `-3` suffix on the directory name.

**Identity & idempotency:** before processing an inbox file, hash it and check whether a library entry already has that `content_hash`. If yes → skip and log. Catches duplicate scans without surprising the user.

## Failure handling

Any failure — OCR returns empty text, classifier returns invalid JSON, schema validation rejects the output, low-confidence classification — moves the source PDF to `library/_quarantine/<original-filename>/` and writes `processing_error.json`:

```json
{
  "stage": "classify" | "ocr" | "schema_validate" | "library_write",
  "error": "<message>",
  "last_llm_response": "<raw, when applicable>",
  "retry_hint": "<actionable suggestion>",
  "occurred_at": "<iso8601>"
}
```

The CLI exits zero with a summary; one bad PDF never blocks a batch. CI and the agent skill both surface "N docs in quarantine" so failures are loud but recoverable.

## CLI surface

```
paperclaw process [--inbox PATH] [--library PATH]
paperclaw list    [--type T] [--since DATE] [--vendor V] [--overdue]
paperclaw show    <id-prefix>
paperclaw search  <text-query>
```

- `process` runs the pipeline over every file in the inbox.
- `list` scans `library/**/metadata.json` and filters structurally.
- `show` prints metadata + transcript for one entry.
- `search` greps `transcript.md` across the library.

All commands print JSON when stdout is not a TTY (so the agent can parse cleanly).

## Configuration

Resolution order: CLI flag > env var > default.

| Setting | Flag | Env | Default |
|---|---|---|---|
| Inbox | `--inbox` | `PAPERCLAW_INBOX` | `~/paperclaw/inbox` |
| Library | `--library` | `PAPERCLAW_LIBRARY` | `~/paperclaw/library` |

`ANTHROPIC_API_KEY` is injected by Infisical at runtime — not read from a config file, not committed.

No TOML/YAML config in v1. If a third path setting appears, revisit.

## Feedback loops

Existing (from initial commits): `gofmt`, `golangci-lint` (gocritic, goimports, gosec, revive), `lefthook` pre-commit running format/lint/test, `make check`.

New for M1:

1. **Golden fixtures.** 3–5 synthetic PDFs in `testdata/inbox/`, one per `type` enum value, covering multi-page + missing-date + German + English edge cases. Generated from HTML/text (stable rendering), committed to the repo. Each paired with `testdata/expected/<name>.json` — the frozen expected metadata.
2. **Schema validation test.** Unit test loads `schema.json`, validates every `testdata/expected/*.json`. Catches schema drift the moment it's introduced.
3. **Drift / eval harness.** `make eval` runs `paperclaw process testdata/inbox` against a temp library, diffs each produced `metadata.json` against `testdata/expected/`. Exit non-zero on drift. Wired into lefthook (pre-push if pre-commit is too slow).
4. **Agentic review skill.** `skills/paperclaw-review/SKILL.md` documents the PR-review checklist for an agent: schema valid · golden diffs clean · quarantine empty for fixtures · new test added for any new behaviour. The agent reads its own checklist before approving a change.

## M3 agent interface

Project-local Claude Skill at `skills/paperclaw/SKILL.md`. The skill is the contract: it lists the CLI subcommands, gives example queries from the workshop spec ("which bills are overdue?", "find the invoice for that gadget", "show me the latest electricity bill"), and describes the JSON output shape. The agent drives the CLI; the CLI stays testable on its own.

No MCP server — the CLI is the single source of truth and a Skill is a thinner wrapper than an MCP transport.

## Implementation notes

- **Language:** Go (module currently `papwer-claw` — typo to fix in a follow-up commit alongside the `.golangci.yml` `local-prefixes` entry).
- **LLM:** `claude-sonnet-4-6` via `anthropics/anthropic-sdk-go`, using `tools` / `response_format` to force JSON output that matches `schema.json`. No shell-out to the `claude` CLI.
- **Secrets:** `ANTHROPIC_API_KEY` from Infisical.
- **Reuse:** `internal/document/FormatDirName` already exists with a passing test — extend in place with slug rules and collision suffix logic.

## Out of scope (v1)

- Multi-document PDFs (multiple bills in one scan).
- Live inbox watch / daemon mode.
- Embedding-based semantic search.
- Real-PDF fixtures (privacy — synthetic only).
- MCP server.
- Web UI.

## Verification checklist

When M1 and M2 are wired:

1. `make check` green (lint + test).
2. Schema test green for every fixture.
3. `make eval` green against frozen expected metadata.
4. Drop a synthetic PDF into a temp inbox, run `paperclaw process`, verify library entry layout and that `document.pdf` is byte-identical to input.
5. Re-run `process` on the same inbox — second run is a no-op (idempotency).
6. Feed a corrupt PDF — lands in `_quarantine/` with populated `processing_error.json`.
7. From Claude Code, invoke the `paperclaw` skill: "list all utility bills" → agent calls `paperclaw list --type=utility_bill` and returns matches.
8. Invoke the `paperclaw-review` skill against a diff that breaks a fixture — agent names the failing checklist item.
