#!/bin/bash
# Extensive smoke over khub's CLI surface: build-lite and build-hub, every command,
# every exit code asserted. The unit suite proves the core; this proves the binary.
#
# Lived in kb/smoke.sh until kb moved to its own repo and took it along; khub needs
# its own. `bash smoke.sh [workdir]`.
set -uo pipefail

REPO=$(cd "$(dirname "$0")" && pwd)
WORK=${1:-/tmp/khub-smoke}
KHUB="uv run --project $REPO khub"
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
    printf '  \033[31mFAIL\033[0m %-50s want=%s got=%s\n' "$label" "$want" "$got"
    printf '       %s\n' "$(echo "$out" | tail -3)"
  else
    printf '  ok   %-50s\n' "$label"
  fi
}
# has <label> <file> <literal string>
has() {
  CASES=$((CASES + 1))
  if grep -qF "$3" "$2"; then printf '  ok   %-50s\n' "$1"
  else
    FAILED=$((FAILED + 1))
    printf '  \033[31mFAIL\033[0m %-50s (missing from %s)\n       %s\n' "$1" "$2" "$3"
  fi
}
# hasnt <label> <file> <literal string>
hasnt() {
  CASES=$((CASES + 1))
  if grep -qF "$3" "$2"; then
    FAILED=$((FAILED + 1))
    printf '  \033[31mFAIL\033[0m %-50s (unexpectedly in %s)\n' "$1" "$2"
  else printf '  ok   %-50s\n' "$1"; fi
}

rm -rf "$WORK"; mkdir -p "$WORK"

# ------------------------------------------------------------------- build-lite
say "khub · build-lite"
L=$WORK/lite; mkdir -p "$L"; cd "$L"
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
ok 0 "link realized_in (fr)"       $KHUB -C "$L" link fr-001-pay-by-card realized_in cmp-001-public-api
ok 0 "link realized_in (cst)"      $KHUB -C "$L" link cst-001-settle-in-2s realized_in cmp-001-public-api
ok 0 "link depends_on"             $KHUB -C "$L" link cmp-001-public-api depends_on cmp-002-stripe
ok 0 "link requirements"           $KHUB -C "$L" link fs-001-checkout requirements fr-001-pay-by-card
ok 0 "unlink is idempotent"        $KHUB -C "$L" unlink fs-001-checkout requirements fr-001-pay-by-card
ok 0 "relink"                      $KHUB -C "$L" link fs-001-checkout requirements fr-001-pay-by-card
ok 0 "edit status"                 $KHUB -C "$L" edit adr/ad-001-use-stripe status proposed
ok 0 "get --edges"                 $KHUB -C "$L" get cmp-001-public-api --edges --format json
ok 0 "query --type"                $KHUB -C "$L" query --type requirement --format json
ok 0 "query --missing"             $KHUB -C "$L" query --missing realized_in --format json
ok 0 "neighbors"                   $KHUB -C "$L" neighbors cmp-001-public-api --format json
ok 0 "impact --reverse"            $KHUB -C "$L" impact cmp-002-stripe --reverse --format json
ok 0 "history"                     $KHUB -C "$L" history ad-001-use-stripe --format json
ok 0 "search"                      $KHUB -C "$L" search stripe --format json
ok 0 "status"                      $KHUB -C "$L" status --format json
ok 0 "schema types"                $KHUB -C "$L" schema types --format json
ok 0 "schema show"                 $KHUB -C "$L" schema show adr --format json
ok 0 "stale"                       $KHUB -C "$L" stale --format json
ok 0 "validate"                    $KHUB -C "$L" validate --format json
ok 0 "check"                       $KHUB -C "$L" check --format json
ok 0 "check --strict (all wired)"  $KHUB -C "$L" check --strict --format json
ok 0 "reindex"                     $KHUB -C "$L" reindex
ok 0 "viz"                         $KHUB -C "$L" viz --out "$L/viz.html"
ok 0 "backfill --dry-run"          $KHUB -C "$L" backfill --dry-run
ok 0 "wire"                        $KHUB -C "$L" wire
ok 1 "remove refuses on inbound"   $KHUB -C "$L" remove cmp-001-public-api
ok 0 "remove --force"              $KHUB -C "$L" remove cmp-002-stripe --force
ok 1 "check now dangles"           $KHUB -C "$L" check --format json

say "khub · build-lite · the wired block"
has  "CLAUDE: capture cues"        "$L/CLAUDE.md" "Record as you go"
has  "CLAUDE: prd links its file"  "$L/CLAUDE.md" "[knowledge/prd.md](knowledge/prd.md)"
has  "CLAUDE: arc42 links its file" "$L/CLAUDE.md" "[knowledge/arc42.md](knowledge/arc42.md)"
has  "CLAUDE: schema import"       "$L/CLAUDE.md" "@.khub/schema.yaml"
has  "AGENTS: same links"          "$L/AGENTS.md" "[knowledge/prd.md](knowledge/prd.md)"
hasnt "AGENTS: no import directive" "$L/AGENTS.md" "@.khub/schema.yaml"
ok 0 "re-wire is a no-op"          $KHUB -C "$L" wire

# -------------------------------------------------------------------- build-hub
say "khub · build-hub"
H=$WORK/hub; mkdir -p "$H"; cd "$H"
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
ok 0 "validate (add seeds bodies)" $KHUB -C "$H" validate --format json
ok 1 "check: required edges unset" $KHUB -C "$H" check --format json
ok 0 "search"                      $KHUB -C "$H" search checkout --format json
ok 0 "status"                      $KHUB -C "$H" status --format json
ok 0 "reindex"                     $KHUB -C "$H" reindex
ok 0 "viz"                         $KHUB -C "$H" viz --out "$H/viz.html"

say "khub · build-hub · the wired block"
has "prd links its file"      "$H/CLAUDE.md" "[knowledge/product/prd.md](knowledge/product/prd.md)"
has "roadmap links its file"  "$H/CLAUDE.md" "[knowledge/product/roadmap.md](knowledge/product/roadmap.md)"
has "glossary links its file" "$H/CLAUDE.md" "[knowledge/product/glossary.md](knowledge/product/glossary.md)"
has "arc42 links its file"    "$H/CLAUDE.md" "[knowledge/architecture/arc42.md](knowledge/architecture/arc42.md)"
has "erd links its file"      "$H/CLAUDE.md" "[knowledge/architecture/erd.md](knowledge/architecture/erd.md)"

# --------------------------------------------------------------------- firm-ops
say "khub · firm-ops (no singletons)"
F=$WORK/firm; mkdir -p "$F"; cd "$F"
ok 0 "init"                        $KHUB init firm-ops .
ok 0 "add client"                  $KHUB -C "$F" add client --name "Acme Corp"
ok 0 "validate"                    $KHUB -C "$F" validate --format json
has   "capture cues still render"  "$F/AGENTS.md" "Record as you go"
hasnt "no singleton link"          "$F/AGENTS.md" "](knowledge/"

# ------------------------------------------------------- the misplaced-entity gate
say "khub · a file the scan cannot reach"
M=$WORK/misplaced; mkdir -p "$M"; cd "$M"
$KHUB init build-lite . >/dev/null 2>&1
$KHUB -C "$M" add component --title "API" --kind service >/dev/null 2>&1
python3 - "$M" <<'PY'
import pathlib, sys
p = pathlib.Path(sys.argv[1]) / ".khub" / "schema.yaml"
p.write_text(p.read_text().replace("path: knowledge/components",
                                   "path: knowledge/architecture/components"))
PY
ok 1 "check reports the misplaced entity" $KHUB -C "$M" check --format json

printf '\n\033[1m%s cases, %s failed\033[0m\n' "$CASES" "$FAILED"
exit $((FAILED > 0))
