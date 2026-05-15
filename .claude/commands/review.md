Review the staged or unpushed changes in this repo. Spawn four parallel specialist agents and synthesise their findings.

## Specialists

Run all four agents in parallel over the output of `git diff HEAD` (or `git diff --cached` if reviewing staged changes):

1. **Go idioms** — error handling, defer/close patterns, context propagation, interface misuse, nil safety.
2. **Security** — path traversal, command injection, insecure file modes, secrets in output, gosec findings.
3. **Correctness & tests** — logic bugs, missing test coverage for new code, flaky test patterns, edge cases.
4. **Maintainability** — naming, dead code, doc drift between docs and the changed code, linter suppressions without justification.

## Rules

- See `REVIEW.md` for what to flag and what to ignore.
- Each specialist reports only findings with a concrete impact — no hypotheticals.
- Map every finding to a file and line number.
- Synthesise into a single ranked list: must-fix, should-fix, nice-to-have.
