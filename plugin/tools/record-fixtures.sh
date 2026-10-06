#!/bin/bash
# Records real khub output into tests/fixtures.ts, so the plugin's tests fail when the
# CLI's JSON contract moves.
#
#   plugin/tools/record-fixtures.sh
#
# Builds khub from this checkout, scaffolds a build-hub workspace with the clock pinned,
# and replays one session: seed, record a decision and a requirement, break a relation
# by hand, fix it.

set -euo pipefail

PLUGIN="$(cd "$(dirname "$0")/.." && pwd)"
REPO="$(cd "$PLUGIN/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

(cd "$REPO" && go build -o "$TMP/khub" ./cmd/khub)

export KHUB_PARITY_NOW=2026-01-15
WS="$TMP/ws"
BODY="$TMP/fixtures.body"
ADR=ad-2026-01-15-use-re2-patterns
REQ=req-search-answers-within-300-ms

k() { "$TMP/khub" -C "$WS" "$@"; }

# rec <name> <khub args...>: runs the call and appends one fixture entry.
rec() {
  local name="$1" exit=0
  shift
  k "$@" >"$TMP/out" 2>"$TMP/err" || exit=$?
  python3 - "$WS" "$name" "$exit" "$TMP/out" "$TMP/err" "$@" >>"$BODY" <<'PY'
import json, os, re, sys

ws, name, code, out, err, *args = sys.argv[1:]

# khub prints the workspace path in a few documents. It reads `<ws>` in a fixture,
# however khub spelled it, so no temporary or home path is recorded.
def portable(text):
    for root in sorted({ws, os.path.realpath(ws)}, key=len, reverse=True):
        text = re.sub(r"(?:\.\./)*/?" + re.escape(root.lstrip("/")), "<ws>", text)
    return text

entry = {
    "args": args,
    "exit": int(code),
    "stdout": portable(open(out).read().rstrip("\n")),
    "stderr": portable(open(err).read().rstrip("\n")),
}
print(f"  {name}: {json.dumps(entry, ensure_ascii=False)},")
PY
}

"$TMP/khub" init build-hub "$WS" --no-wire >/dev/null
: >"$BODY"

k add repo --title "API" --repo endgame/api --status active >/dev/null
k add component --title "Storage" --kind service --owner team-platform >/dev/null
k add component --title "Search" --kind service --owner team-platform --lifecycle production \
  --tier tier-1 --repo rp-api --depends_on cmp-storage >/dev/null
k add actor --title "Analyst" >/dev/null
k add capability --title "Find documents" >/dev/null
k add use-case --title "Find a document" --trigger human --actor act-analyst \
  --capability cap-find-documents --served_by cmp-search >/dev/null

# The seeded workspace.
rec version --version
rec schema schema --format json
rec status_base status --format json
rec check_base check --format json
rec query_base query --format json
rec query_components query --type component --format json
rec query_draft query --draft --format json
rec get_search get cmp-search --edges --format json
rec neighbors_search neighbors cmp-search --format json
rec impact_storage impact cmp-storage --reverse --format json
rec search_hits search --plain search --format json
rec search_empty search --plain zzzz --format json
rec get_missing get nope --format json
rec remove_refused remove cmp-search --format json
rec reindex_dry reindex --dry-run

# A decision, linked.
rec add_adr add adr --title "Use RE2 patterns" --status accepted --format json
rec query_added query --format json
rec validate_adr validate "adr/$ADR" --format json
rec link_adr link "$ADR" affects cmp-search --format json
rec link_adr_again link "$ADR" affects cmp-search --format json

# A requirement, then a relation typed by hand to a slug that does not exist.
k add requirement --title "Search answers within 300 ms" --kind non-functional >/dev/null
rec add_refused add requirement --title "Other" --kind non-functional --realized_in cmp-serach --format json
python3 - "$WS/knowledge/requirements/$REQ.md" <<'PY'
import pathlib, sys

path = pathlib.Path(sys.argv[1])
head, rest = path.read_text().split("\n---", 1)
path.write_text(head + "\nrealized_in:\n- cmp-serach\n---" + rest)
PY
rec validate_requirement_dangling validate "requirement/$REQ" --format json
rec check_dangling check --format json
rec status_dangling status --format json
rec query_dangling query --format json

# The fix, and the remaining write verbs.
rec edit_fix edit "$REQ" realized_in cmp-search --format json
rec edit_component edit cmp-search lifecycle deprecated --format json
rec unlink_adr unlink "$ADR" affects cmp-search --format json
k add actor --title "Temp" >/dev/null
rec remove_actor remove act-temp --format json

{
  echo "// Real khub output, recorded by tools/record-fixtures.sh. Do not edit."
  echo
  echo "export type Fixture = { args: string[]; exit: number; stdout: string; stderr: string }"
  echo
  echo "export const F = {"
  cat "$BODY"
  echo "} satisfies Record<string, Fixture>"
} >"$PLUGIN/tests/fixtures.ts"

echo "recorded $(grep -c '^  [a-z_]*: ' "$PLUGIN/tests/fixtures.ts") fixtures into tests/fixtures.ts"
