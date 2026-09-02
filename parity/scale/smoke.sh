#!/bin/bash
# Smoke over khub at scale: a few hundred entity files, every command timed,
# every exit code and every answer asserted.
#
# `smoke.sh` at the repo root walks the whole verb surface on a corpus of a
# dozen files — it answers "does the CLI work". This answers a different
# question: does it still work, and still tell the truth, when the corpus is
# the size of a real project's. The failures that only show up at n are the
# ones it is here for — a walk that does not terminate, an index that quietly
# drops entities, a scan whose cost turns quadratic, a `check` that gets
# slower than the edit loop it gates.
#
# The answers are not compared against khub's own output. The generator
# writes `smoke-manifest.json` beside the corpus — orphans, closures, degrees,
# counts and dates computed from its own model of the graph — and every
# assertion below is khub against that. Nothing here spells an id, a title or
# an id scheme: they all come out of the manifest, so this file survives a
# change to either. The one exception is the concurrent-add case at the end,
# which spells a title on purpose — the race it proves is over one minted id.
#
# Needs the Go toolchain and nothing else: the manifest is read through
# parity/scale/manifest, which also tells the time (macOS bash 3.2 has no
# EPOCHREALTIME).
#
#   bash parity/scale/smoke.sh                # generate 550 entities, then assert
#   bash parity/scale/smoke.sh --scale 300    # a smaller corpus
#   bash parity/scale/smoke.sh --keep         # assert the corpus already on disk
#   bash parity/scale/smoke.sh --fast         # skip the regenerate-and-diff pass
#   bash parity/scale/smoke.sh --work /tmp/x  # somewhere other than $TMPDIR/khub-scale/smoke
#
# KHUB=<binary> overrides the khub under test (default: the repo's ./khub).
set -uo pipefail

REPO=$(cd "$(dirname "$0")/../.." && pwd)
KHUB="${KHUB:-$REPO/khub}"
WORK=${TMPDIR:-/tmp}/khub-scale/smoke
SCALE=550
SEED=1
TODAY=2026-01-15
KEEP=0
FAST=0

while [ $# -gt 0 ]; do
  case $1 in
    --keep)  KEEP=1; shift ;;
    --fast)  FAST=1; shift ;;
    --scale) SCALE=$2; shift 2 ;;
    --seed)  SEED=$2; shift 2 ;;
    --today) TODAY=$2; shift 2 ;;
    --work)  WORK=$2; shift 2 ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [ ! -x "$KHUB" ]; then
  echo "no khub binary at $KHUB — go build -o khub ./cmd/khub, or set KHUB" >&2
  exit 2
fi

# The two Go helpers, built once. `go run` would pay a link per call, and the
# clock helper is called twice per case.
TOOLS=$(mktemp -d "${TMPDIR:-/tmp}/khub-scale-tools.XXXXXX")
trap 'rm -rf "$TOOLS"' EXIT
(cd "$REPO" && go build -o "$TOOLS/gen" ./parity/scale/gen && go build -o "$TOOLS/manifest" ./parity/scale/manifest) || {
  echo "building parity/scale helpers failed" >&2; exit 2; }
GEN=$TOOLS/gen
M=$TOOLS/manifest

FAILED=0
CASES=0
SLOWEST=""

say() { printf '\n\033[1m== %s\033[0m\n' "$*"; }

now_ms() {
  if [ -n "${EPOCHREALTIME:-}" ]; then
    local t=${EPOCHREALTIME/[.,]/}
    echo $(( t / 1000 ))
  else
    "$M" -now
  fi
}

# ok <expected-exit> <label> <command...> — asserts the exit code, reports the
# wall clock. The timing is the point of this file as much as the exit code:
# a command that answers correctly in nine seconds has still regressed.
ok() {
  local want=$1 label=$2; shift 2
  CASES=$((CASES + 1))
  local start; start=$(now_ms)
  local out; out=$("$@" 2>&1); local got=$?
  local ms=$(( $(now_ms) - start ))
  if [ "$got" != "$want" ]; then
    FAILED=$((FAILED + 1))
    printf '  \033[31mFAIL\033[0m %-46s want=%s got=%s %6sms\n' "$label" "$want" "$got" "$ms"
    printf '       %s\n' "$(echo "$out" | tail -3)"
  else
    printf '  ok   %-46s %6sms\n' "$label" "$ms"
  fi
  [ "$ms" -gt 2000 ] && SLOWEST="$SLOWEST\n       ${ms}ms  $label"
  return 0
}

# same <label> <got> <want> — two strings must match. Callers reduce JSON
# through the helper first, so payloads compare as data.
same() {
  CASES=$((CASES + 1))
  if [ "$2" == "$3" ]; then printf '  ok   %-46s\n' "$1"
  else
    FAILED=$((FAILED + 1))
    printf '  \033[31mFAIL\033[0m %-46s\n       khub= %s\n       want= %s\n' "$1" "$2" "$3"
  fi
}

# json <op> [arg] — reduce khub's JSON on stdin (see parity/scale/manifest).
json() { "$M" -json "$@"; }
# reach <slug> — distinct ids a walk reached, minus the entity it started from.
reach() { json reach "$1"; }
# field <path> — one value out of the manifest.
field() { "$M" "$MANIFEST" "$1"; }

# ------------------------------------------------------------------- the corpus
say "corpus"
if [ "$KEEP" = 1 ]; then
  printf '  ..   reusing %s\n' "$WORK"
else
  ok 0 "generate $SCALE entities" "$GEN" -bin "$KHUB" -work "$WORK" -scale "$SCALE" \
       -seed "$SEED" -today "$TODAY" -quiet
fi
MANIFEST=$WORK/smoke-manifest.json
if [ ! -f "$MANIFEST" ]; then
  echo "no manifest at $MANIFEST — run without --keep" >&2
  exit 2
fi

# Every expected value, read once.
M_TOTAL=$(field total)
M_TYPES=$(field per_type)
M_ORPHANS=$(field orphans)
M_ORPHAN_N=$(echo "$M_ORPHANS" | wc -w | tr -d ' ')
M_DRAFTS=$(field drafts)
M_UNLINKED=$(field missing_realized_in)
M_STALE90=$(field stale.90)
M_STALE365=$(field stale.365)
M_TAG=ledger
M_TAG_N=$(field tags.ledger)
M_HUB=$(field closures.0.id);        M_HUB_REV=$(field closures.0.reverse);   M_HUB_FWD=$(field closures.0.forward)
M_DEEP=$(field closures.1.id);       M_DEEP_FWD=$(field closures.1.forward);  M_DEEP_REV=$(field closures.1.reverse)
M_MID=$(field closures.2.id);        M_MID_FWD=$(field closures.2.forward);   M_MID_REV=$(field closures.2.reverse)
M_BUSY=$(field degrees.0.id);        M_BUSY_HOP=$(field degrees.0.one_hop)
M_BUSY_IN=$(field degrees.0.in);     M_BUSY_OUT=$(field degrees.0.out)
M_ADR=$(field degrees.2.id);         M_ADR_HOP=$(field degrees.2.one_hop)
M_NEWEST=$(field history.newest);    M_CHAIN=$(field history.chain)
M_MIDDLE=$(field history.middle);    M_MIDDLE_N=$(field history.middle_reaches)
M_NEEDLE=$(field search.needle);     M_NEEDLE_ID=$(field search.id)
M_CYCLE_FROM=$(field probes.cycle_from); M_CYCLE_TO=$(field probes.cycle_to)
M_HELD=$(field probes.held_component)
M_TAKEN=$(field probes.taken_title); M_TAKEN_TYPE=$(field probes.taken_type)
M_LINK_TARGET=$(field probes.link_target)
M_SEED=$(field generated.seed); M_SCALE=$(field generated.scale); M_TODAY=$(field generated.today)
# The clock the corpus was written against, so every date-relative answer
# (stale, status) is judged on the same day the manifest was.
export KHUB_PARITY_NOW=$M_TODAY
printf '  ..   %s entities, %s orphan gaps, %s stale past 90 days\n' \
  "$M_TOTAL" "$M_ORPHAN_N" "$M_STALE90"

files() { find "$WORK/knowledge" "$WORK/specs" -name '*.md' 2>/dev/null; }
same "one file per entity on disk" "$(files | wc -l | tr -d ' ')" "$M_TOTAL"

# ---------------------------------------------------------------------- the gate
say "gates"
ok 0 "check"                       "$KHUB" -C "$WORK" check
ok 0 "check --format json"         "$KHUB" -C "$WORK" check --format json
# Orphans are the one finding that is legal until asked about. This corpus
# carries them on purpose — unlinked requirements — so the strict gate must
# fail and the ordinary one must not.
ok 1 "check --strict fails on orphans" "$KHUB" -C "$WORK" check --strict
same "check: nothing is thin" \
  "$("$KHUB" -C "$WORK" check --format json | json count thin)" "0"
same "check: the orphan list is the manifest's" \
  "$("$KHUB" -C "$WORK" check --format json | json field orphans | tr ' ' '\n' | sed 's#^[^/]*/##' | sort | tr '\n' ' ')" \
  "$(echo "$M_ORPHANS" | tr ' ' '\n' | sort | tr '\n' ' ')"
ok 0 "validate (whole workspace)"  "$KHUB" -C "$WORK" validate
ok 0 "validate requirement"        "$KHUB" -C "$WORK" validate requirement
ok 0 "validate one entity"         "$KHUB" -C "$WORK" validate "requirement/$M_NEEDLE_ID"
ok 0 "status"                      "$KHUB" -C "$WORK" status

# ------------------------------------------------------------------- the answers
say "answers vs. the manifest"
STATUS=$("$KHUB" -C "$WORK" status --format json)
same "status: counts per type" "$(printf '%s' "$STATUS" | json field counts)" "$M_TYPES"
same "status: total"           "$(printf '%s' "$STATUS" | json field total)"  "$M_TOTAL"
same "status: drafts"          "$(printf '%s' "$STATUS" | json field draft)"  "$M_DRAFTS"
same "status: orphans"         "$(printf '%s' "$STATUS" | json field orphan)" "$M_ORPHAN_N"
# 90 is build-lite's stale_days: workspace.DefaultStaleDays, no policy override.
same "status: stale (90-day default)" "$(printf '%s' "$STATUS" | json field stale)" "$M_STALE90"
same "status: no strays, nothing malformed" \
  "$(printf '%s' "$STATUS" | json field stray)/$(printf '%s' "$STATUS" | json field malformed)" "0/0"

# The orphan set, not just its size: a checker that reports the right number
# of the wrong entities is worse than one that reports none.
same "query --orphan is exactly the unlinked set" \
  "$("$KHUB" -C "$WORK" query --orphan --format ids | sort | tr '\n' ' ')" \
  "$(echo "$M_ORPHANS" | tr ' ' '\n' | sort | tr '\n' ' ')"
same "query --tag $M_TAG" \
  "$("$KHUB" -C "$WORK" query --tag "$M_TAG" --format ids | wc -l | tr -d ' ')" "$M_TAG_N"
same "query --draft" \
  "$("$KHUB" -C "$WORK" query --draft --format ids | wc -l | tr -d ' ')" "$M_DRAFTS"
same "query --active is the complement" \
  "$("$KHUB" -C "$WORK" query --active --format ids | wc -l | tr -d ' ')" "$((M_TOTAL - M_DRAFTS))"
same "query --missing realized_in" \
  "$("$KHUB" -C "$WORK" query --type requirement --missing realized_in --format ids | wc -l | tr -d ' ')" "$M_UNLINKED"
same "stale --days 90"  "$("$KHUB" -C "$WORK" stale --days 90 --format json | json len)"  "$M_STALE90"
same "stale --days 365" "$("$KHUB" -C "$WORK" stale --days 365 --format json | json len)" "$M_STALE365"

# --------------------------------------------------------------------- the walks
say "walks"
ok 0 "impact --reverse (the hub)"      "$KHUB" -C "$WORK" impact "$M_HUB" --reverse --format json
same "impact --reverse: the hub's whole ancestry" \
  "$("$KHUB" -C "$WORK" impact "$M_HUB" --reverse --format json | reach "$M_HUB")" "$M_HUB_REV"
same "impact: the hub depends on nothing further" \
  "$("$KHUB" -C "$WORK" impact "$M_HUB" --format json | reach "$M_HUB")" "$M_HUB_FWD"
same "impact: the deepest service's blast radius" \
  "$("$KHUB" -C "$WORK" impact "$M_DEEP" --format json | reach "$M_DEEP")" "$M_DEEP_FWD"
same "impact --reverse: the deepest has no dependants" \
  "$("$KHUB" -C "$WORK" impact "$M_DEEP" --reverse --format json | reach "$M_DEEP")" "$M_DEEP_REV"
same "impact: a middle node, forward" \
  "$("$KHUB" -C "$WORK" impact "$M_MID" --format json | reach "$M_MID")" "$M_MID_FWD"
same "impact: a middle node, reverse" \
  "$("$KHUB" -C "$WORK" impact "$M_MID" --reverse --format json | reach "$M_MID")" "$M_MID_REV"
ok 0 "neighbors (busiest entity)"      "$KHUB" -C "$WORK" neighbors "$M_BUSY" --format json
same "neighbors: one hop, both directions" \
  "$("$KHUB" -C "$WORK" neighbors "$M_BUSY" --format json | reach "$M_BUSY")" "$M_BUSY_HOP"
same "neighbors --in: the inbound half" \
  "$("$KHUB" -C "$WORK" neighbors "$M_BUSY" --in --format json | reach "$M_BUSY")" "$M_BUSY_IN"
same "neighbors --out: the outbound half" \
  "$("$KHUB" -C "$WORK" neighbors "$M_BUSY" --out --format json | reach "$M_BUSY")" "$M_BUSY_OUT"
same "neighbors: an adr's one hop" \
  "$("$KHUB" -C "$WORK" neighbors "$M_ADR" --format json | reach "$M_ADR")" "$M_ADR_HOP"
# A depth greater than the graph's diameter must terminate rather than walk
# the cycle it does not have.
ok 0 "neighbors --depth past the diameter" "$KHUB" -C "$WORK" neighbors "$M_BUSY" --depth 12 --format json
ok 0 "history (supersession chain)"    "$KHUB" -C "$WORK" history "$M_NEWEST" --format json
same "history: the whole chain from the newest" \
  "$("$KHUB" -C "$WORK" history "$M_NEWEST" --format json | reach "$M_NEWEST")" "$((M_CHAIN - 1))"
same "history: the older half from the middle" \
  "$("$KHUB" -C "$WORK" history "$M_MIDDLE" --format json | reach "$M_MIDDLE")" "$M_MIDDLE_N"
ok 0 "get --edges (inverses computed)" "$KHUB" -C "$WORK" get "$M_HELD" --edges --format json
same "get returns the entity asked for" \
  "$("$KHUB" -C "$WORK" get "$M_NEEDLE_ID" --format json | json field slug)" "$M_NEEDLE_ID"

# ---------------------------------------------------------------------- full text
say "search"
ok 0 "search"                      "$KHUB" -C "$WORK" search "$M_NEEDLE" --format json
same "search finds the one planted body" \
  "$("$KHUB" -C "$WORK" search "$M_NEEDLE" --format ids | tr '\n' ' ' | tr -d ' ')" "$M_NEEDLE_ID"
same "search misses what is not there" \
  "$("$KHUB" -C "$WORK" search "quinceberry" --format ids | wc -l | tr -d ' ')" "0"
ok 0 "search --type narrows"       "$KHUB" -C "$WORK" search "$M_TAG" --type adr --format json

# ---------------------------------------------------------------------- the index
say "index"
ok 0 "reindex"                     "$KHUB" -C "$WORK" reindex
ok 0 "reindex --dry-run"           "$KHUB" -C "$WORK" reindex --dry-run
BEFORE=$(cksum < "$WORK/index.md")
"$KHUB" -C "$WORK" reindex >/dev/null
same "reindex is idempotent" "$(cksum < "$WORK/index.md")" "$BEFORE"
# The index is the whole corpus in one file, and the failure mode at this
# size is silent truncation — every entity has to be linked from it.
same "index.md links every entity" \
  "$(comm -23 <(files | sed -e 's#.*/##' -e 's/\.md$//' | sort -u) \
              <(grep -o '([^)]*\.md)' "$WORK/index.md" | sed -e 's#.*/##' -e 's/\.md)$//' | sort -u) | wc -l | tr -d ' ')" "0"

# ----------------------------------------------------------------- the byte format
say "file format"
same "no CRLF anywhere in the corpus" \
  "$(grep -rl $'\r' "$WORK/knowledge" "$WORK/index.md" | wc -l | tr -d ' ')" "0"
same "every file decodes as UTF-8" "$("$M" -utf8 "$WORK")" "0"
same "index paths are POSIX" "$(grep -c '[\]' "$WORK/index.md" | tr -d ' ')" "0"

# ------------------------------------------------------------------- determinism
# The corpus is a file format, so the same inputs have to produce the same
# bytes — on one machine at minimum, and this is where a map iteration order
# leaking into a written file shows up.
if [ "$FAST" = 1 ]; then
  say "determinism (skipped: --fast)"
else
  say "determinism"
  REPRO=${TMPDIR:-/tmp}/khub-scale/smoke-repro
  rm -rf "$REPRO"
  ok 0 "regenerate with the same seed" "$GEN" -bin "$KHUB" -work "$REPRO" -scale "$M_SCALE" \
       -seed "$M_SEED" -today "$M_TODAY" -quiet
  ok 0 "byte-identical entities"       diff -r "$WORK/knowledge" "$REPRO/knowledge"
  ok 0 "byte-identical index"          diff "$WORK/index.md" "$REPRO/index.md"
  ok 0 "byte-identical manifest"       diff "$MANIFEST" "$REPRO/smoke-manifest.json"
  rm -rf "$REPRO"
fi

# ---------------------------------------------------------------------- mutation
# Everything above is read-only. These write, and they run last so nothing
# before them sees a corpus that has drifted from the manifest. Refusals exit
# 2 and write nothing; a gate that ran and failed exits 1.
say "authoring into a full corpus"
PROBE=$("$KHUB" -C "$WORK" add requirement --title "Smoke probe at scale" --kind constraint \
        --format json | json field slug)
same "add minted an id"            "$([ -n "$PROBE" ] && echo minted)" "minted"
ok 2 "add refuses a taken title"   "$KHUB" -C "$WORK" add "$M_TAKEN_TYPE" --title "$M_TAKEN" --kind functional
ok 2 "add refuses a dangling edge" "$KHUB" -C "$WORK" add requirement --title "Ghost rule" \
     --kind functional --realized_in nope
ok 0 "link into the graph"         "$KHUB" -C "$WORK" link "$PROBE" realized_in "$M_LINK_TARGET"
same "link again is a reported no-op" \
  "$("$KHUB" -C "$WORK" link "$PROBE" realized_in "$M_LINK_TARGET" --format json | json field changed)" "false"
ok 0 "edit an attribute"           "$KHUB" -C "$WORK" edit "$PROBE" kind non-functional
ok 0 "validate the one entity"     "$KHUB" -C "$WORK" validate "requirement/$PROBE"
ok 0 "check still passes"          "$KHUB" -C "$WORK" check
ok 2 "remove refuses while held"   "$KHUB" -C "$WORK" remove "$M_HELD"
ok 0 "unlink"                      "$KHUB" -C "$WORK" unlink "$PROBE" realized_in "$M_LINK_TARGET"
ok 0 "remove the probe"            "$KHUB" -C "$WORK" remove "$PROBE"

# A target removed under force leaves its inbound edge dangling, which `check`
# has to find among a few hundred edges that still resolve.
say "dangling edge at scale"
TARGET=$("$KHUB" -C "$WORK" add component --title "Smoke target at scale" --kind library \
         --format json | json field slug)
HOLDER=$("$KHUB" -C "$WORK" add requirement --title "Smoke holder at scale" --kind functional \
         --realized_in "$TARGET" --format json | json field slug)
ok 2 "remove refuses the held target" "$KHUB" -C "$WORK" remove "$TARGET"
ok 0 "remove --force it anyway"       "$KHUB" -C "$WORK" remove "$TARGET" --force
ok 1 "check fails on the dangle"      "$KHUB" -C "$WORK" check
same "check names the dangling edge" \
  "$("$KHUB" -C "$WORK" check --format json | json count dangling)" "1"
ok 0 "remove the holder"              "$KHUB" -C "$WORK" remove "$HOLDER"
ok 0 "check clean again"              "$KHUB" -C "$WORK" check

# A cycle over an acyclic predicate, closed at the far end of a real
# dependency graph.
say "cycle detection at depth"
ok 0 "close a depends_on loop"     "$KHUB" -C "$WORK" link "$M_CYCLE_TO" depends_on "$M_CYCLE_FROM"
ok 1 "check catches the cycle"     "$KHUB" -C "$WORK" check
same "check names it a cycle" \
  "$("$KHUB" -C "$WORK" check --format json | json count cycles | sed 's/^0$/none/; s/^[1-9][0-9]*$/some/')" "some"
ok 0 "unlink it"                   "$KHUB" -C "$WORK" unlink "$M_CYCLE_TO" depends_on "$M_CYCLE_FROM"
ok 0 "check clean again"           "$KHUB" -C "$WORK" check
same "the corpus is back where it started" \
  "$("$KHUB" -C "$WORK" status --format json | json field total)" "$M_TOTAL"

# An id is a pure function of schema, type, frontmatter and title — no
# directory scan, no `-N` retry — so two writers minting one title race for
# one path and O_EXCL is the only gate. Eight at once: one lands, seven refuse
# `slug_taken`, and the file that landed is whole enough to read back.
say "concurrent adds of one title"
RACE=$(mktemp -d "${TMPDIR:-/tmp}/khub-scale-race.XXXXXX")
for i in 1 2 3 4 5 6 7 8; do
  ( "$KHUB" -C "$WORK" add adr --title "Race" --format json > "$RACE/$i.out" 2>&1
    echo $? > "$RACE/$i.exit" ) &
done
wait
same "one add landed (exit 0)"      "$(cat "$RACE"/*.exit | grep -c '^0$')" "1"
same "seven refused (exit 2)"       "$(cat "$RACE"/*.exit | grep -c '^2$')" "7"
same "every refusal is slug_taken" \
  "$(grep -l slug_taken "$RACE"/*.out | wc -l | tr -d ' ')" "7"
same "exactly one file on disk" \
  "$(find "$WORK/knowledge/decisions" -name 'ad-*-race.md' | wc -l | tr -d ' ')" "1"
RACE_ID=$(find "$WORK/knowledge/decisions" -name 'ad-*-race.md' | sed -e 's#.*/##' -e 's/\.md$//')
ok 0 "the file that landed parses"  "$KHUB" -C "$WORK" get "$RACE_ID" --format json
ok 0 "remove it"                    "$KHUB" -C "$WORK" remove "$RACE_ID"
rm -rf "$RACE"
same "the corpus is back where it started, again" \
  "$("$KHUB" -C "$WORK" status --format json | json field total)" "$M_TOTAL"

# ------------------------------------------------------------------------ summary
if [ -n "$SLOWEST" ]; then
  printf '\n\033[1mover 2s:\033[0m'
  printf '%b\n' "$SLOWEST"
fi
printf '\n\033[1m%s cases, %s failed\033[0m  (%s entities)\n' "$CASES" "$FAILED" "$M_TOTAL"
exit $((FAILED > 0))
