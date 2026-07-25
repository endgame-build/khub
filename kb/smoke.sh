#!/bin/bash
# Extensive smoke: khub and kb, each over build-hub and build-lite.
# Every command's exit code is asserted; any unexpected one fails the run loudly.
set -uo pipefail

REPO=/path/to/khub
WORK=${1:-/tmp/khub-smoke}
KHUB="uv run --project $REPO khub"
KB="python3 $REPO/kb/scripts/kb.py"
FAILED=0
CASES=0

say() { printf '\n\033[1m== %s\033[0m\n' "$*"; }
# ok <expected-exit> <label> <command...>
ok() {
  local want=$1 label=$2; shift 2
  CASES=$((CASES + 1))
  local out; out=$("$@" 2>&1); local got=$?
  if [ "$got" != "$want" ]; then
    FAILED=$((FAILED + 1))
    printf '  \033[31mFAIL\033[0m %-52s want=%s got=%s\n' "$label" "$want" "$got"
    printf '       %s\n' "$(echo "$out" | tail -3)"
  else
    printf '  ok   %-52s\n' "$label"
  fi
}
# same <label> <a> <b> — two payloads must match, JSON compared as data not text
norm() { python3 -c 'import json,sys
raw=sys.stdin.read()
try: print(json.dumps(json.loads(raw), sort_keys=True))
except Exception: print(raw.strip())'; }
same() {
  CASES=$((CASES + 1))
  local a b; a=$(printf '%s' "$2" | norm); b=$(printf '%s' "$3" | norm)
  if [ "$a" == "$b" ]; then printf '  ok   %-52s\n' "$1"
  else
    FAILED=$((FAILED + 1))
    printf '  \033[31mFAIL\033[0m %-52s\n       a=%s\n       b=%s\n' "$1" "$a" "$b"
  fi
}

rm -rf "$WORK"; mkdir -p "$WORK"

# ----------------------------------------------------------------- khub / build-lite
say "khub · build-lite"
L=$WORK/khub-lite; mkdir -p "$L"; cd "$L"
ok 0 "init"                        $KHUB init build-lite .
ok 0 "add component (service)"     $KHUB -C "$L" add component --title "Public API" --kind service --repo acme/api
ok 0 "add component (external)"    $KHUB -C "$L" add component --title "Stripe" --kind external --stack Stripe
ok 0 "add requirement functional"  $KHUB -C "$L" add requirement --title "Pay by card" --kind functional
ok 0 "add requirement constraint"  $KHUB -C "$L" add requirement --title "Settle in 2s" --kind constraint
ok 0 "add adr"                     $KHUB -C "$L" add adr --title "Use Stripe" --status accepted
ok 0 "add feature-spec"            $KHUB -C "$L" add feature-spec --title "Checkout" --status active
ok 1 "add: bad enum refused"       $KHUB -C "$L" add adr --title "Bad" --status bogus
ok 1 "add: dangling target refused" $KHUB -C "$L" add adr --title "Ghost" --status proposed --affects nope
ok 0 "link affects"                $KHUB -C "$L" link ad-001-use-stripe affects cmp-001-public-api
ok 0 "link realized_in"            $KHUB -C "$L" link fr-001-pay-by-card realized_in cmp-001-public-api
ok 0 "link depends_on"             $KHUB -C "$L" link cmp-001-public-api depends_on cmp-002-stripe
ok 0 "link requirements"           $KHUB -C "$L" link fs-001-checkout requirements fr-001-pay-by-card
ok 0 "unlink is idempotent"        $KHUB -C "$L" unlink fs-001-checkout requirements fr-001-pay-by-card
ok 0 "relink"                      $KHUB -C "$L" link fs-001-checkout requirements fr-001-pay-by-card
ok 0 "edit status"                 $KHUB -C "$L" edit adr/ad-001-use-stripe status proposed
ok 0 "get --edges"                 $KHUB -C "$L" get cmp-001-public-api --edges --format json
ok 0 "query --type"                $KHUB -C "$L" query --type requirement --format json
ok 0 "query --missing"             $KHUB -C "$L" query --missing realized_in --format json
ok 0 "neighbors"                   $KHUB -C "$L" neighbors cmp-001-public-api --format json
ok 0 "impact"                      $KHUB -C "$L" impact cmp-002-stripe --reverse --format json
ok 0 "history"                     $KHUB -C "$L" history ad-001-use-stripe --format json
ok 0 "search"                      $KHUB -C "$L" search stripe --format json
ok 0 "status"                      $KHUB -C "$L" status --format json
ok 0 "schema types"                $KHUB -C "$L" schema types --format json
ok 0 "stale"                       $KHUB -C "$L" stale --format json
ok 0 "validate"                    $KHUB -C "$L" validate --format json
ok 0 "check"                       $KHUB -C "$L" check --format json
ok 1 "check --strict fails on orphan" $KHUB -C "$L" check --strict --format json
ok 0 "reindex"                     $KHUB -C "$L" reindex
ok 0 "viz"                         $KHUB -C "$L" viz --out "$L/viz.html"
ok 0 "backfill --dry-run"          $KHUB -C "$L" backfill --dry-run
ok 0 "wire"                        $KHUB -C "$L" wire
ok 1 "remove refuses on inbound"   $KHUB -C "$L" remove cmp-001-public-api
ok 0 "remove --force"              $KHUB -C "$L" remove cmp-002-stripe --force
ok 1 "check now dangles"           $KHUB -C "$L" check --format json

# ----------------------------------------------------------------- khub / build-hub
say "khub · build-hub"
H=$WORK/khub-hub; mkdir -p "$H"; cd "$H"
ok 0 "init"                        $KHUB init build-hub .
ok 0 "add capability"              $KHUB -C "$H" add capability --title "Payments"
ok 0 "add requirement"             $KHUB -C "$H" add requirement --title "Pay by card" --kind functional
ok 0 "add boundary"                $KHUB -C "$H" add boundary --title "Payment domain" --scope domain
ok 0 "add quality-attribute"       $KHUB -C "$H" add quality-attribute --title "p99 under 300ms"
ok 0 "add component"               $KHUB -C "$H" add component --title "Checkout API" --kind service
ok 0 "add adr"                     $KHUB -C "$H" add adr --title "Use Stripe" --status accepted
ok 0 "add pdr"                     $KHUB -C "$H" add pdr --title "Charge model" --status accepted
ok 0 "add feature-spec"            $KHUB -C "$H" add feature-spec --title "Checkout" --status active
ok 0 "add test-spec"               $KHUB -C "$H" add test-spec --title "Checkout suite"
ok 0 "add work-package"            $KHUB -C "$H" add work-package --title "Ship checkout" --status planned
ok 0 "validate (add seeds bodies)"    $KHUB -C "$H" validate --format json
ok 1 "check: required edges unset"  $KHUB -C "$H" check --format json
ok 0 "search"                      $KHUB -C "$H" search checkout --format json
ok 0 "status"                      $KHUB -C "$H" status --format json
ok 0 "reindex"                     $KHUB -C "$H" reindex
ok 0 "viz"                         $KHUB -C "$H" viz --out "$H/viz.html"

# ----------------------------------------------------------------- kb / build-lite
say "kb · build-lite"
KL=$WORK/kb-lite; mkdir -p "$KL"; cd "$KL"
ok 0 "init"                        $KB -C "$KL" init
ok 0 "add component (service)"     $KB -C "$KL" add component --title "Public API" --kind service --repo acme/api
ok 0 "add component (external)"    $KB -C "$KL" add component --title "Stripe" --kind external --stack Stripe
ok 0 "add requirement functional"  $KB -C "$KL" add requirement --title "Pay by card" --kind functional
ok 0 "add requirement constraint"  $KB -C "$KL" add requirement --title "Settle in 2s" --kind constraint
ok 0 "add adr"                     $KB -C "$KL" add adr --title "Use Stripe" --status accepted
ok 0 "add feature-spec"            $KB -C "$KL" add feature-spec --title "Checkout" --status active
ok 2 "add: bad enum refused"       $KB -C "$KL" add adr --title "Bad" --status bogus
ok 2 "add: dangling target refused" $KB -C "$KL" add adr --title "Ghost" --status proposed --affects nope
ok 0 "add with tags (list coerce)" $KB -C "$KL" add adr --title "Tagged" --status proposed --tags a,b
ok 0 "link affects"                $KB -C "$KL" link ad-001-use-stripe affects cmp-001-public-api
ok 0 "link realized_in"            $KB -C "$KL" link fr-001-pay-by-card realized_in cmp-001-public-api
ok 0 "link depends_on"             $KB -C "$KL" link cmp-001-public-api depends_on cmp-002-stripe
ok 0 "link requirements"           $KB -C "$KL" link fs-001-checkout requirements fr-001-pay-by-card
ok 0 "unlink"                      $KB -C "$KL" unlink fs-001-checkout requirements fr-001-pay-by-card
ok 0 "relink"                      $KB -C "$KL" link fs-001-checkout requirements fr-001-pay-by-card
ok 0 "edit status"                 $KB -C "$KL" edit ad-001-use-stripe status proposed
ok 2 "edit: bad enum refused"      $KB -C "$KL" edit ad-001-use-stripe status bogus
ok 0 "get --edges"                 $KB -C "$KL" get cmp-001-public-api --edges --format json
ok 0 "query --type"                $KB -C "$KL" query --type requirement --format json
ok 0 "query --missing"             $KB -C "$KL" query --missing realized_in --format json
ok 0 "neighbors"                   $KB -C "$KL" neighbors cmp-001-public-api --format json
ok 0 "impact"                      $KB -C "$KL" impact cmp-002-stripe --reverse --format json
ok 0 "history"                     $KB -C "$KL" history ad-001-use-stripe --format json
ok 0 "search"                      $KB -C "$KL" search stripe --format json
ok 0 "status"                      $KB -C "$KL" status --format json
ok 0 "schema types"                $KB -C "$KL" schema types --format json
ok 0 "stale"                       $KB -C "$KL" stale --format json
ok 0 "validate"                    $KB -C "$KL" validate --format json
ok 0 "check"                       $KB -C "$KL" check --format json
ok 0 "reindex"                     $KB -C "$KL" reindex
ok 0 "wire --target AGENTS"        $KB -C "$KL" wire --target AGENTS
ok 0 "install-skills --dry-run"    $KB -C "$KL" install-skills --dry-run
ok 2 "remove refuses on inbound"   $KB -C "$KL" remove cmp-001-public-api
ok 0 "remove --force"              $KB -C "$KL" remove cmp-002-stripe --force
ok 1 "check now dangles"           $KB -C "$KL" check --format json

# ----------------------------------------------------------------- kb / build-hub
# kb hardcodes no type: point it at a build-hub schema and the whole surface works.
say "kb · build-hub (schema override)"
KH=$WORK/kb-hub; mkdir -p "$KH"; cd "$KH"
$KHUB init build-hub . >/dev/null 2>&1
mkdir -p "$KH/.kb"
python3 - "$REPO" "$KH" <<'PY'
import sys, pathlib
repo, work = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
core = (repo / "src/khub/presets/core.yaml").read_text()
hub = (repo / "src/khub/presets/build-hub/schema.yaml").read_text()
(work / ".kb/build.schema.yaml").write_text(core + "\n" + hub)
PY
ok 0 "reads a build-hub schema"    $KB -C "$KH" schema types
ok 0 "add capability"              $KB -C "$KH" add capability --title "Payments"
ok 0 "add requirement"             $KB -C "$KH" add requirement --title "Pay by card" --kind functional
ok 0 "add component"               $KB -C "$KH" add component --title "Checkout API" --kind service
ok 0 "add adr"                     $KB -C "$KH" add adr --title "Use Stripe" --status accepted
ok 0 "add work-package"            $KB -C "$KH" add work-package --title "Ship it" --status planned
ok 0 "validate (add seeds bodies)"    $KB -C "$KH" validate --format json
same "check matches khub"          "$($KHUB -C "$KH" check --format json)" "$($KB -C "$KH" check --format json)"
same "validate matches khub"       "$($KHUB -C "$KH" validate --format json)" "$($KB -C "$KH" validate --format json)"
ok 0 "search"                      $KB -C "$KH" search checkout --format json

# ----------------------------------------------------------------- cross-tool parity
say "parity · the same corpus read by both tools"
P=$WORK/parity; mkdir -p "$P"; cd "$P"
$KHUB init build-lite . >/dev/null 2>&1
$KHUB -C "$P" add component --title "Public API" --kind service >/dev/null 2>&1
$KHUB -C "$P" add requirement --title "Settle in 2s" --kind constraint >/dev/null 2>&1
$KHUB -C "$P" link cst-001-settle-in-2s realized_in cmp-001-public-api >/dev/null 2>&1
same "validate payload"  "$($KHUB -C "$P" validate --format json)" "$($KB -C "$P" validate --format json)"
same "check payload"     "$($KHUB -C "$P" check --format json)"    "$($KB -C "$P" check --format json)"
same "search ranking"    "$($KHUB -C "$P" search api --format ids)" "$($KB -C "$P" search api --format ids)"
same "query ids"         "$($KHUB -C "$P" query --format json | python3 -c 'import json,sys;print([r["id"] for r in json.load(sys.stdin)])')" \
                         "$($KB -C "$P" query --format json | python3 -c 'import json,sys;print([r["id"] for r in json.load(sys.stdin)])')"
$KHUB -C "$P" reindex >/dev/null 2>&1; A=$(cat "$P/index.md"); rm "$P/index.md"
$KB -C "$P" reindex >/dev/null 2>&1;   B=$(cat "$P/index.md")
same "index.md bytes"    "$A" "$B"

# kb authored, khub graduated
G=$WORK/graduate; mkdir -p "$G"; cd "$G"
$KB -C "$G" init >/dev/null 2>&1
$KB -C "$G" add component --title "Ledger" --kind service >/dev/null 2>&1
$KB -C "$G" add requirement --title "Balances reconcile" --kind constraint >/dev/null 2>&1
$KB -C "$G" link cst-001-balances-reconcile realized_in cmp-001-ledger >/dev/null 2>&1
entities() { find "$G/knowledge" "$G/specs" -name '*.md' -exec shasum {} \; | sort | awk '{print $1}'; }
BEFORE=$(entities)
ok 0 "khub init over a kb corpus"  $KHUB init build-lite . --force
AFTER=$(entities)
same "no entity file rewritten"  "$BEFORE" "$AFTER"
ok 0 "khub validate"               $KHUB -C "$G" validate --format json
ok 0 "khub check"                  $KHUB -C "$G" check --format json

printf '\n\033[1m%s cases, %s failed\033[0m\n' "$CASES" "$FAILED"
exit $((FAILED > 0))
