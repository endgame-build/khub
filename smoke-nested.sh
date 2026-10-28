#!/bin/bash
# Manual smoke for nested path templates. `bash smoke-nested.sh [workdir]`.
set -uo pipefail

REPO=$(cd "$(dirname "$0")" && pwd)
WORK=${1:-/tmp/khub-smoke-nested}
KHUB="${KHUB:-$REPO/khub}"
export KHUB_PARITY_NOW=2026-01-15
FAILED=0
CASES=0

rm -rf "$WORK"
WS=$WORK/ws
R=$WORK/results
mkdir -p "$WS" "$R"

run() {
  local want=$1 label=$2 out=$3
  shift 3
  CASES=$((CASES + 1))
  "$@" >"$out" 2>"$out.stderr"
  local got=$?
  if [ "$got" = "$want" ]; then
    printf '  ok   %-52s\n' "$label"
  else
    FAILED=$((FAILED + 1))
    printf '  FAIL %-52s want=%s got=%s\n' "$label" "$want" "$got"
  fi
}

json_is() {
  local label=$1 file=$2 path=$3 expected=$4
  CASES=$((CASES + 1))
  if python3 -I - "$file" "$path" "$expected" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as f:
    value = json.load(f)
if sys.argv[2]:
    for part in sys.argv[2].split("."):
        value = value[int(part)] if isinstance(value, list) else value[part]
expected = json.loads(sys.argv[3])
if value != expected:
    print(f"got {value!r}, want {expected!r}", file=sys.stderr)
    raise SystemExit(1)
PY
  then
    printf '  ok   %-52s\n' "$label"
  else
    FAILED=$((FAILED + 1))
    printf '  FAIL %-52s\n' "$label"
  fi
}

tree_is() {
  local label=$1 expected=$2 actual
  CASES=$((CASES + 1))
  actual=$(cd "$WS" && find work projects -type f | LC_ALL=C sort)
  if [ "$actual" = "$expected" ]; then
    printf '  ok   %-52s\n' "$label"
  else
    FAILED=$((FAILED + 1))
    printf '  FAIL %-52s\nactual:\n%s\nwant:\n%s\n' "$label" "$actual" "$expected"
  fi
}

has() {
  CASES=$((CASES + 1))
  if grep -qF "$3" "$2"; then
    printf '  ok   %-52s\n' "$1"
  else
    FAILED=$((FAILED + 1))
    printf '  FAIL %-52s missing %s\n' "$1" "$3"
  fi
}

hasnt() {
  CASES=$((CASES + 1))
  if grep -qF "$3" "$2"; then
    FAILED=$((FAILED + 1))
    printf '  FAIL %-52s unexpectedly has %s\n' "$1" "$3"
  else
    printf '  ok   %-52s\n' "$1"
  fi
}

run 0 "init nested workspace" "$R/init.json" \
  "$KHUB" init build-hub "$WS" --no-wire --format json
rm -rf "$WS/.khub/templates" "$WS/knowledge"
cat >"$WS/.khub/ontology.yaml" <<'YAML'
ontology:
  entities:
    epic: {}
    story:
      relations:
        epic: {to: epic}
        supersedes: {to: story}
YAML
cat >"$WS/.khub/policy.yaml" <<'YAML'
policy:
  story: {path_refs: true}
YAML
cat >"$WS/.khub/storage.yaml" <<'YAML'
storage:
  epic: {layout: file, path: ["work/{slug}/{slug}.md", "projects/{project}/work/{slug}/{slug}.md"]}
  story: {layout: file, path: ["work/{epic}/{slug}.md", "projects/{project}/work/{epic}/{slug}.md"]}
YAML

run 0 "add epic to first template" "$R/epic-1.json" \
  "$KHUB" -C "$WS" add epic --title Hub --id ep-1 --format json
run 0 "add epic to nested project template" "$R/epic-2.json" \
  "$KHUB" -C "$WS" add epic --title Project --id ep-2 --project p1 --format json
run 0 "add story under first epic" "$R/story-1.json" \
  "$KHUB" -C "$WS" add story --title First --epic ep-1 --format json
run 0 "add story under project epic" "$R/story-2.json" \
  "$KHUB" -C "$WS" add story --title Second --epic ep-2 --project p1 --format json
run 0 "add story with qualified relation" "$R/story-qualified.json" \
  "$KHUB" -C "$WS" add story --title Qualified --epic epic/ep-1 --format json
json_is "qualified relation fills only its slug" "$R/story-qualified.json" path '"work/ep-1/qualified.md"'
tree_is "both templates produce the exact tree" 'projects/p1/work/ep-2/ep-2.md
projects/p1/work/ep-2/second.md
work/ep-1/ep-1.md
work/ep-1/first.md
work/ep-1/qualified.md'
run 1 "no flat first-story fallback" "$R/no-flat-first" test -e "$WS/work/first.md"
run 1 "no flat project-story fallback" "$R/no-flat-second" test -e "$WS/work/second.md"
run 1 "no flat qualified-story fallback" "$R/no-flat-qualified" test -e "$WS/work/qualified.md"
run 2 "add refuses an unfillable path" "$R/add-missing.json" \
  "$KHUB" -C "$WS" add story --title Missing --format json
json_is "missing path field has the stable refusal" "$R/add-missing.json" error.code '"path_field_missing"'

run 0 "get reads the nested story" "$R/get.json" \
  "$KHUB" -C "$WS" get story/first --format json
json_is "get names the nested path" "$R/get.json" path '"work/ep-1/first.md"'
run 0 "edit updates the nested story" "$R/edit.json" \
  "$KHUB" -C "$WS" edit story/first --title "First edited" --format json
has "edit wrote the nested Markdown" "$WS/work/ep-1/first.md" "title: First edited"
run 0 "link updates the nested story" "$R/link.json" \
  "$KHUB" -C "$WS" link story/first depends_on story/second --format json
json_is "link reports a real change" "$R/link.json" changed true
has "link wrote the nested Markdown" "$WS/work/ep-1/first.md" "story/second"
run 0 "unlink updates the nested story" "$R/unlink.json" \
  "$KHUB" -C "$WS" unlink story/first depends_on story/second --format json
json_is "unlink reports a real change" "$R/unlink.json" changed true
hasnt "unlink removed only the stored edge" "$WS/work/ep-1/first.md" "depends_on"
run 0 "history reads the nested story" "$R/history.json" \
  "$KHUB" -C "$WS" history story/first --format json
json_is "history identifies the nested story" "$R/history.json" "" \
  '[{"id":"story/first","type":"story","slug":"first","superseded_by":null}]'
run 0 "search indexes the nested story" "$R/search.json" \
  "$KHUB" -C "$WS" search edited --format json
json_is "search finds the nested story" "$R/search.json" 0.id '"story/first"'
run 0 "stale reads nested entity dates" "$R/stale.json" \
  "$KHUB" -C "$WS" stale --format json
json_is "fresh nested entities are not stale" "$R/stale.json" "" '[]'
run 0 "validate reads the nested body" "$R/validate.json" \
  "$KHUB" -C "$WS" validate story/first --format json
json_is "nested validation has no errors" "$R/validate.json" errors '[]'
run 0 "reindex renders nested links" "$R/reindex.json" \
  "$KHUB" -C "$WS" reindex
has "index links the first-template story" "$WS/index.md" "work/ep-1/first.md"
has "index links the project-template story" "$WS/index.md" "projects/p1/work/ep-2/second.md"

run 0 "edit creates a scalar placement mismatch" "$R/mismatch-edit.json" \
  "$KHUB" -C "$WS" edit story/first --epic ep-2 --format json
cat >"$WS/work/ep-1/missing.md" <<'MD'
---
type: story
title: Missing field
created: 2026-01-15
---
MD
cat >"$WS/work/ep-1/blank.md" <<'MD'
---
type: story
title: Blank field
created: 2026-01-15
epic: "   "
---
MD
cat >"$WS/work/ep-1/list.md" <<'MD'
---
type: story
title: List field
created: 2026-01-15
epic: [ep-1]
---
MD
run 1 "check reports the scalar mismatch" "$R/check.json" \
  "$KHUB" -C "$WS" check --format json
json_is "missing blank and list fields are not misplaced" "$R/check.json" misplaced \
  '[{"path":"work/ep-1/first.md","type":"story","expected":"work/ep-2/first.md"}]'

run 0 "remove deletes the nested project story" "$R/remove.json" \
  "$KHUB" -C "$WS" remove story/second --format json
json_is "remove names the intended story" "$R/remove.json" id '"story/second"'
tree_is "mutations leave the exact final tree" 'projects/p1/work/ep-2/ep-2.md
work/ep-1/blank.md
work/ep-1/ep-1.md
work/ep-1/first.md
work/ep-1/list.md
work/ep-1/missing.md
work/ep-1/qualified.md'

printf '\n%s cases, %s failed\n' "$CASES" "$FAILED"
exit $((FAILED > 0))
