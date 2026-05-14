# Plan — Rewrite `docs/plan.md` for PaperClaw M1

## Context

PaperClaw is the BetterVibe Agentic Engineering Workshop project. The repo currently has a 30-line `docs/plan.md` that sketches the inbox→library pipeline but leaves every load-bearing decision unspecified. The workshop hard-caps M1 at 30 minutes and an instructor reviews the plan before M2 build begins. The plan needs to be defensible cold-read and aligned to one of three judging dimensions.

Why this change: turn `docs/plan.md` into a real M1 design doc that locks the decisions an instructor (and the agent) will need during M2/M3, organised around the optimization lens the user picked.

**Optimization lens (chosen):** _Best agentic engineering setup_ — investment goes into golden fixtures, evals, schema validation, drift checks, agentic review loops. Architecture and agent-native polish are second-order.

**Scope target (chosen):** _Thin slice through M3_ — one PDF end-to-end, one query answered via an agent skill. Cut richness, keep all three milestones wired so feedback loops cover the whole pipeline.

## Locked decisions (the substance of the rewritten plan)

| # | Area | Decision |
|---|------|----------|
| 1 | Transcription | OCR-only, deterministic (ocrmypdf → tesseract). No LLM in transcription. Byte-stable transcripts → golden-file diff tests trivial. |
| 2 | Classification taxonomy | Closed enum `type` ∈ {invoice, utility_bill, bank_statement, insurance_letter, contract, government_letter, other} + open `tags: []`. |
| 3 | Metadata schema | Required: `id`, `type`, `document_date`, `vendor`, `summary`, `source_filename`, `processed_at`, `content_hash`. Optional: `amount`, `currency`, `due_date`, `tags`, `language`. JSON Schema in repo, validated in CI. |
| 4 | Library layout | Flat doc-per-dir, slug-named: `library/YYYY-MM-DD_<vendor-slug>_<description-slug>/` containing `document.pdf`, `transcript.md`, `metadata.json`. Lowercase ASCII slugs, ≤40 chars, `-2`/`-3` for collisions. |
| 5 | Identity & idempotency | `id` = SHA-256 of file content. Ingest checks `content_hash` against library; skip + log duplicates. |
| 6 | Failure handling | `library/_quarantine/<original-filename>/` with `processing_error.json` (stage, error, last LLM response, retry hint). |
| 7 | M3 agent interface | `skills/paperclaw/SKILL.md` (project-local Claude Skill) calling CLI subcommands. No MCP. |
| 8 | Search surface | `paperclaw list [--type --since --vendor --overdue]` over metadata.json; `paperclaw search <query>` greps transcript.md. |
| 9 | Golden fixtures | 3–5 synthetic PDFs in `testdata/inbox/`, one per `type`, covering multi-page + missing date + German/English. Paired with `testdata/expected/<name>.json`. |
| 10 | Drift check | Pre-commit/CI runs `paperclaw process testdata/inbox` against a temp library, diffs metadata.json against `testdata/expected/`. Plus `skills/paperclaw-review/SKILL.md` documenting the agent's PR-review checklist. |
| 11 | Configuration | CLI flags > env (`PAPERCLAW_INBOX`/`PAPERCLAW_LIBRARY`) > defaults (`~/paperclaw/inbox`, `~/paperclaw/library`). No config file. |
| 12 | LLM model | `claude-sonnet-4-6` for classification + metadata extraction. |
| 13 | LLM invocation | `anthropics/anthropic-sdk-go` directly from Go, with `tools`/`response_format` forcing JSON-schema-conformant output. `ANTHROPIC_API_KEY` injected via Infisical. |

## Files to create / modify

**Workshop deliverable (the M1 artifact):**
- `docs/plan.md` — rewrite end-to-end. Sections: Goal · Pipeline · Data model (taxonomy + JSON Schema reference) · Library layout · Identity & idempotency · Failure handling · CLI surface · Configuration · Feedback loops (existing lint/test + new eval + drift + agentic review) · M3 agent interface · Out of scope.

**Code scaffolding to land alongside M1 (so M2 starts from a runnable skeleton):**
- `cmd/paperclaw/main.go` — CLI entry, subcommands `process`, `list`, `show`, `search`.
- `internal/document/document.go` — already exists with `FormatDirName`; extend with slug rules + collision suffix logic. Reuse, don't rewrite.
- `internal/document/document_test.go` — already exists; add cases for German umlauts, length cap, collision.
- `internal/document/schema.json` — JSON Schema for `metadata.json`.
- `internal/document/metadata.go` — Go struct mirroring schema + load/save/validate helpers.
- `internal/ocr/ocr.go` — wrapper around `ocrmypdf` / `tesseract` shell-out; produces `transcript.md`.
- `internal/classifier/classifier.go` — Anthropic SDK call, prompt + JSON schema, returns typed `Metadata` struct.
- `internal/library/library.go` — write entry (atomic via `os.Rename` from temp dir), dedupe by `content_hash`, list/search.
- `internal/quarantine/quarantine.go` — quarantine writer.
- `internal/inbox/inbox.go` — orchestrates the per-file pipeline (hash → OCR → classify → library or quarantine).
- `testdata/inbox/*.pdf` — synthetic fixtures (generated, not real).
- `testdata/expected/*.json` — frozen metadata.
- `skills/paperclaw/SKILL.md` — agent contract: lists subcommands, query examples, output shape.
- `skills/paperclaw-review/SKILL.md` — PR-review checklist (schema valid · golden diffs clean · quarantine empty · new fixture for new behaviour).
- `Makefile` — add `eval` target (runs pipeline against `testdata/inbox/`, diffs vs `testdata/expected/`).
- `lefthook.yml` — add `eval` to pre-commit (or pre-push if too slow).
- `go.mod` — fix module name typo `papwer-claw` → `paperclaw`; add `github.com/anthropics/anthropic-sdk-go`. _Note: this is a separate concern from the M1 plan and should ride in its own commit; the typo currently bleeds into the `goimports` `local-prefixes` setting in `.golangci.yml`._

## Reuse / what's already in place

- Feedback-loop substrate from the existing commits: `gofmt`, `golangci-lint` (gocritic, goimports, gosec, revive), `lefthook` pre-commit running format/lint/test, `make check`. The new `eval` target hooks into the same lefthook flow — no new infra.
- `internal/document/FormatDirName(date, vendor, description)` exists and tests pass; extend in place rather than introduce a parallel naming module.
- `.infisical.json` already configured; Infisical injects `ANTHROPIC_API_KEY` into the shell — no new secrets system.

## Out of scope (state this explicitly in `docs/plan.md`)

- Multi-document PDFs (e.g. multiple invoices in one scan).
- Live inbox watch / daemon mode.
- Embedding-based semantic search.
- Real-PDF fixtures (privacy: workshop uses synthetic only).
- MCP server (Claude Skill is the M3 interface).

## Verification

End-to-end checks once M1 ships and M2 lands:

1. **Lint+test still green:** `make check`.
2. **Schema validation:** unit test loads `internal/document/schema.json`, validates every file in `testdata/expected/*.json` — all pass.
3. **Golden-fixture drift:** `make eval` processes `testdata/inbox/` into a temp library, diffs against `testdata/expected/`. Exit 0 on clean, non-zero on drift. Wired into lefthook.
4. **Round-trip:** drop a synthetic PDF into a temp inbox, run `paperclaw process --inbox=/tmp/in --library=/tmp/lib`, assert resulting `metadata.json` matches expected and `document.pdf` is byte-identical to input.
5. **Idempotency:** running `process` twice over the same inbox produces the same library (second run skips and logs).
6. **Quarantine path:** feed an empty/corrupt PDF, assert it lands in `_quarantine/` with a populated `processing_error.json`.
7. **Agent skill smoke test:** from Claude Code, invoke `paperclaw` skill, ask "list all utility bills" → agent calls `paperclaw list --type=utility_bill`, returns matching entries from `testdata/expected/`.
8. **Review skill smoke test:** invoke `paperclaw-review` skill against the current diff, agent walks the checklist and either approves or names a specific failing item.
