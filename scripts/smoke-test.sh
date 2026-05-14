#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$REPO_ROOT/bin/paperclaw"
TESTDATA="$REPO_ROOT/testdata"
WORK="$(mktemp -d)"

cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "  ok  $*"; }

echo "=== smoke test ==="
echo "binary:   $BIN"
echo "testdata: $TESTDATA"
echo "workdir:  $WORK"
echo ""

# Build if binary is missing or stale.
if [ ! -x "$BIN" ] || [ "$REPO_ROOT/cmd/paperclaw/main.go" -nt "$BIN" ]; then
  echo "building..."
  (cd "$REPO_ROOT" && go build -o bin/paperclaw ./cmd/paperclaw)
fi

INBOX="$WORK/inbox"
LIBRARY="$WORK/library"
PROCESSED="$WORK/processed"
mkdir -p "$INBOX" "$LIBRARY" "$PROCESSED"

# --- help ---
"$BIN" -help >/dev/null 2>&1 || fail "paperclaw -help failed"
ok "-help exits 0"

# --- list on empty library (non-TTY → JSON; empty array is valid) ---
out=$("$BIN" list --library "$LIBRARY" 2>&1)
echo "$out" | grep -qE '^\[\]?$|no documents' || fail "list on empty library should return [] or 'no documents', got: $out"
ok "list empty library"

# --- copy test PDFs into inbox ---
cp "$TESTDATA/stadtwerke-stromrechnung.pdf" "$INBOX/"
cp "$TESTDATA/finanzamt-bescheid.pdf"       "$INBOX/"

# --- process (requires ANTHROPIC_API_KEY; skip with a clear message if absent) ---
if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
  echo ""
  echo "SKIP: ANTHROPIC_API_KEY not set — skipping live process/list/show/search tests"
  echo "      Set ANTHROPIC_API_KEY to run the full smoke suite."
  echo ""
  echo "=== smoke test passed (partial) ==="
  exit 0
fi

"$BIN" process --inbox "$INBOX" --library "$LIBRARY" --processed "$PROCESSED" \
  || fail "process command failed"
ok "process"

# Inbox files moved to processed dir.
[ -f "$INBOX/stadtwerke-stromrechnung.pdf" ] && fail "stadtwerke still in inbox after process"
[ -f "$INBOX/finanzamt-bescheid.pdf" ]       && fail "finanzamt still in inbox after process"
ok "inbox files moved to processed"

# Library has entries.
entry_count=$(find "$LIBRARY" -mindepth 1 -maxdepth 1 -type d ! -name '_quarantine' | wc -l | tr -d ' ')
[ "$entry_count" -ge 1 ] || fail "library has no entries after process (got $entry_count)"
ok "library has $entry_count entries"

# --- list ---
json_out=$("$BIN" list --library "$LIBRARY" 2>/dev/null | head -c 1)
[ "$json_out" = "[" ] || fail "list did not return JSON array (first char: $json_out)"
ok "list returns JSON"

# --- show (pick first ID) ---
first_id=$("$BIN" list --library "$LIBRARY" 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin)[0]['id'][:8])" 2>/dev/null || true)
if [ -n "$first_id" ]; then
  show_out=$("$BIN" show --library "$LIBRARY" "$first_id" 2>/dev/null | head -c 1)
  [ "$show_out" = "{" ] || fail "show did not return JSON object"
  ok "show returns JSON (id prefix: $first_id)"
fi

# --- search ---
search_out=$("$BIN" search --library "$LIBRARY" Stadtwerke 2>/dev/null)
echo "$search_out" | python3 -c "import sys,json; hits=json.load(sys.stdin); assert len(hits)>0" \
  || fail "search for 'Stadtwerke' returned no hits"
ok "search finds content"

echo ""
echo "=== smoke test passed ==="
