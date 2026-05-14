# PaperClaw Skill

You have access to the `paperclaw` CLI, which manages a personal document library. Use it to answer questions about the user's PDFs — invoices, utility bills, bank statements, contracts, and more.

## When to use this skill

Use `paperclaw` whenever the user asks about their documents:
- "Which bills are overdue?"
- "Find the invoice for that gadget I bought."
- "Show me the latest electricity bill."
- "What did I pay Stadtwerke last month?"
- "List all contracts from 2025."
- "Search for IBAN in my documents."

## Commands

All commands print JSON when stdout is not a TTY. Run them without a TTY (piped or captured) so you receive structured output.

### `paperclaw list`

List documents in the library. Filters are combinable.

```
paperclaw list [--library PATH] [--type TYPE] [--since DATE] [--vendor VENDOR] [--overdue]
```

| Flag | Description |
|---|---|
| `--type` | Exact document type: `invoice`, `utility_bill`, `bank_statement`, `insurance_letter`, `contract`, `government_letter`, `other` |
| `--since` | Documents with `document_date >= DATE` (YYYY-MM-DD) |
| `--vendor` | Substring match on vendor name (case-insensitive) |
| `--overdue` | Only documents with a `due_date` in the past |

**Output** — JSON array of metadata objects:

```json
[
  {
    "id": "<sha256-hex>",
    "type": "utility_bill",
    "document_date": "2026-04-01",
    "vendor": "Stadtwerke",
    "summary": "Stadtwerke electricity bill for April 2026, due 2026-04-30.",
    "source_filename": "stadtwerke-stromrechnung.pdf",
    "processed_at": "2026-05-14T10:00:00Z",
    "content_hash": "<sha256-hex>",
    "amount": 89.50,
    "currency": "EUR",
    "due_date": "2026-04-30",
    "language": "de"
  }
]
```

Optional fields (`amount`, `currency`, `due_date`, `tags`, `language`) are present only when the classifier extracted them.

### `paperclaw show <id-prefix>`

Show full metadata and transcript for one document. The id-prefix can be a short prefix of the SHA-256 `id` field (8+ characters is typically unambiguous).

```
paperclaw show [--library PATH] <id-prefix>
```

**Output** — JSON object:

```json
{
  "metadata": { ... },
  "transcript": "Full OCR text of the document..."
}
```

### `paperclaw search <query>`

Full-text search across all document transcripts.

```
paperclaw search [--library PATH] <query>
```

**Output** — JSON array of hits:

```json
[
  { "entry": "2026-04-01_stadtwerke_strom-rechnung", "id": "<sha256-hex>" }
]
```

Use `paperclaw show` on a hit's `id` to retrieve full details.

### `paperclaw process`

Process PDFs from inbox into the library. Run this when the user says they've added new documents or when the library appears incomplete.

```
paperclaw process [--inbox PATH] [--library PATH] [--processed PATH]
```

After successful processing, each original PDF is **moved** from the inbox to the `--processed` directory (`~/paperclaw/processed` by default). The inbox is left empty; processed files are safely archived.

**Output** — JSON summary:

```json
{ "processed": 3, "skipped": 1, "quarantine": 0 }
```

A non-zero `quarantine` count means some documents failed; they are in `library/_quarantine/` with a `processing_error.json` explaining why.

## Document types (closed enum)

| Value | Meaning |
|---|---|
| `invoice` | Commercial invoice for goods or services |
| `utility_bill` | Electricity, gas, water, internet |
| `bank_statement` | Monthly account statement |
| `insurance_letter` | Policy, premium notice, claim |
| `contract` | Signed agreement |
| `government_letter` | Tax notice, official correspondence |
| `other` | Anything that doesn't fit above |

## Workflow guidance

1. **Answer filtering questions** with `paperclaw list` and the appropriate flags. Never guess — let the CLI filter.
2. **Fetch full text** with `paperclaw show` only when the user needs the document content (amounts, account numbers, specific clauses).
3. **Search by keyword** with `paperclaw search` when the user refers to something specific they remember reading (a product name, an IBAN, a reference number).
4. **Report quarantine** — if `paperclaw list` returns fewer results than expected, or after running `process`, check whether `quarantine > 0` and tell the user which files failed and why (`processing_error.json` → `error` and `retry_hint` fields).
5. **Default library path** is `~/paperclaw/library`. Do not override unless the user specifies a different path.
