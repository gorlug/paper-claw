# Review rubric

When reviewing changes to this repo, focus on the diff only.

## What to flag

- Bugs, panics, nil dereferences (e.g. `h.ID[:12]` with an empty ID in `runSearch`).
- Security issues: path traversal, command injection, overly permissive file modes.
- Go idiom violations: ignored errors, incorrect error wrapping (`%w` vs `%v`), missing `defer f.Close()`, naked returns.
- Tests missing for new logic; test coverage regressions.
- Inbox deletion re-introduced — files must be moved to the processed dir, never deleted.
- Linter suppression (`//nolint`) without a specific reason comment.

## What NOT to flag

- Style preferences not enforced by `golangci-lint`.
- Hypothetical or theoretical risks with no concrete attack vector.
- Untouched code outside the diff.
- The `paper-claw` module name — it is intentional (no `/v2` suffix needed).
- The `pdftotext` OCR tool — it replaces the originally planned `ocrmypdf`; this is intentional.
- Unimplemented `PAPERCLAW_INBOX` / `PAPERCLAW_LIBRARY` env vars — known gap, documented in `CLAUDE.md`.
