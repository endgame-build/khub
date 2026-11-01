#!/bin/bash
# `test && pass "…" || fail "…"` throughout: pass is a printf and never fails,
# so fail runs exactly when the test is false — the if-then-else it reads as.
# shellcheck disable=SC2015
# Cross-version compatibility smoke: a workspace OLD wrote must survive NEW.
#
#   parity/compat/smoke.sh <old-khub> <new-khub> [workdir] [--forward]
#
# For each preset (build-hub, firm-ops, and the parity collections preset
# preset-cells through --preset-source) OLD scaffolds and authors a small fixed
# set of entities and edges using only verbs and flags every version since
# 0.24.0 has carried, then edits a layer file and deletes a template and a
# singleton where the preset has them, so the upgrade has backups and
# re-provisioning to do. The tree is committed. NEW then previews, upgrades,
# upgrades again, reads the graph, and writes one edit.
#
# Assertions are exit codes, the upgrade JSON against the tree (backups named
# == backups present; git status scoped to what the report names; nothing
# modified under a storage root; the journal gone), idempotence (the second
# upgrade reports nothing and moves nothing), and the read surface (entity and
# edge counts the authoring set fixed). With --forward, NEW authors and OLD
# reads; that half only reports.
#
# Before the upgrade, NEW reads the raw OLD tree as it is — the common case,
# a user who bumped the npm pin and never ran `upgrade`. When OLD knows
# `schema snapshot` (0.27 and later) it takes the snapshot, and NEW's
# `schema diff` must then report nothing pending.
#
# Env pinned exactly as parity/runner does: KHUB_PARITY_NOW, the git identity,
# GIT_CONFIG_GLOBAL/SYSTEM, TTY_COMPATIBLE=0 (every call is a pipe). Needs
# bash, git, jq and GNU find; it runs on the ubuntu leg only, by design.
# With no registry at hand, an OLD binary also comes from a release asset:
#   curl -sSL -o khub.tar.gz https://github.com/endgame-build/khub/releases/download/v0.29.0/khub_linux_amd64.tar.gz
#   (verify against checksums.txt beside it, then tar xzf khub.tar.gz khub)
set -uo pipefail

OLD=${1:?old khub binary}
NEW=${2:?new khub binary}
WORK=${3:-/tmp/khub-compat}
FORWARD=0
case "${4:-}" in --forward) FORWARD=1 ;; esac
REPO=$(cd "$(dirname "$0")/../.." && pwd)
PRESETSRC="$REPO/parity/corpus"

export KHUB_PARITY_NOW=2026-01-15
export GIT_AUTHOR_NAME=Parity GIT_AUTHOR_EMAIL=parity@example.com
export GIT_COMMITTER_NAME=Parity GIT_COMMITTER_EMAIL=parity@example.com
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export TTY_COMPATIBLE=0 NO_COLOR=1

OLDV=$("$OLD" --version) NEWV=$("$NEW" --version)
FAILED=0 CASES=0
say() { printf '\n== %s\n' "$*"; }
pass() { CASES=$((CASES + 1)); printf '  ok   %s\n' "$1"; }
fail() { CASES=$((CASES + 1)); FAILED=$((FAILED + 1)); printf '  FAIL %s\n       %s\n' "$1" "${2:-}"; }
# ok <want-exit> <label> <cmd...>  — runs, captures stdout to $OUT, asserts exit
OUT=
ok() {
  local want=$1 label=$2; shift 2
  OUT=$("$@" 2>"$WORK/stderr"); local got=$?
  if [ "$got" = "$want" ]; then pass "$label"; else fail "$label" "want exit $want got $got: $(head -c 300 "$WORK/stderr" "$WORK/stderr" 2>/dev/null | tail -c 300; echo "$OUT" | head -c 300)"; fi
}
# jq_eq <label> <filter> <expected>  — over $OUT
jq_eq() {
  local got; got=$(printf '%s' "$OUT" | jq -c "$2" 2>&1)
  if [ "$got" = "$3" ]; then pass "$1"; else fail "$1" "want $3 got $got"; fi
}
# status_within <label> <regex-of-allowed-paths>  — every porcelain line's path matches
status_within() {
  local bad; bad=$(git status --porcelain | awk '{print $2}' | grep -Ev "$2" || true)
  if [ -z "$bad" ]; then pass "$1"; else fail "$1" "unexpected: $(echo "$bad" | tr '\n' ' ')"; fi
}

rm -rf "$WORK"; mkdir -p "$WORK"
printf 'compat: OLD=%s (%s) NEW=%s (%s)\n' "$OLDV" "$OLD" "$NEWV" "$NEW"

# ---------------------------------------------------------------------------
# author <preset> <dir> <binary> [init-args...]: scaffold and write the fixed set
# with the given binary. Prints nothing; the caller asserts.
author() {
  local preset=$1 dir=$2 bin=$3; shift 3
  mkdir -p "$dir"; cd "$dir" || exit 1
  ok 0 "init $preset ($($bin --version))" "$bin" init "$preset" . "$@"
  case $preset in
    build-hub)
      ok 0 "add repo"        "$bin" add repo --title "API" --repo acme/api --status active
      ok 0 "add component"   "$bin" add component --title "Public API" --kind service --repo rp-api
      ok 0 "add component"   "$bin" add component --title "Stripe" --kind external --stack Stripe
      ok 0 "add requirement" "$bin" add requirement --title "Pay by card" --kind functional
      ok 0 "add adr"         "$bin" add adr --title "Use Stripe" --status accepted
      ok 0 "add adr --draft" "$bin" add adr --title "Drop Stripe" --status proposed --draft
      ok 0 "link realized_in" "$bin" link req-pay-by-card realized_in cmp-public-api
      ok 0 "link affects"     "$bin" link ad-2026-01-15-use-stripe affects cmp-public-api
      ok 0 "link depends_on"  "$bin" link cmp-public-api depends_on cmp-stripe
      ok 0 "link supersedes"  "$bin" link ad-2026-01-15-drop-stripe supersedes ad-2026-01-15-use-stripe
      AUTHORED=6 ANCHOR=cmp-public-api ANCHOR_EDGES=2 LINKED=req-pay-by-card STORAGE='^knowledge/' ;;
    firm-ops)
      ok 0 "add person"      "$bin" add person --name "Ada Lovelace" --role consultant
      ok 0 "add client"      "$bin" add client --name "Initech" --updated 2026-01-15
      ok 0 "add opportunity" "$bin" add opportunity --name "Initech platform" --stage prospect --client initech --owner ada-lovelace --updated 2026-01-15
      ok 0 "add project"     "$bin" add project --title "Initech build" --client initech --owner ada-lovelace --updated 2026-01-15
      ok 0 "add fragment --draft" "$bin" add fragment --title "Initech wants SSO" --stage raw --owner ada-lovelace --draft --body "Heard on the call."
      ok 0 "link origin_opportunity" "$bin" link initech-build origin_opportunity initech-platform
      ok 0 "link team"       "$bin" link initech-build team ada-lovelace
      ok 0 "link related"    "$bin" link initech-wants-sso related initech-build
      AUTHORED=5 ANCHOR=initech-build ANCHOR_EDGES=4 LINKED=initech-build STORAGE='^(opportunities|projects|meetings|transcripts|fragments|case-studies|partnerships|identity|clients)/' ;;
    preset-cells)
      ok 0 "add note"        "$bin" add note --title "Plain note"
      ok 0 "add card (json)" "$bin" add card --title "Json card"
      ok 0 "add sheet (yaml)" "$bin" add sheet --title "Yaml sheet"
      ok 0 "add dossier (folder)" "$bin" add dossier --title "Folder dossier"
      ok 0 "add repo (jsonl row)" "$bin" add repo --title "khub" --stack go
      ok 0 "add vendor (yaml row)" "$bin" add vendor --title "Neon"
      ok 0 "add account (json row)" "$bin" add account --title "Acme" --amount 12.5 --due 2026-02-01
      ok 0 "link related (row -> file)" "$bin" link repo/khub related plain-note
      ok 0 "link related (file -> row)" "$bin" link json-card related vendor/neon
      AUTHORED=7 ANCHOR=repo/khub ANCHOR_EDGES=1 LINKED=json-card STORAGE='^(notes|cards|sheets|dossiers|charter\.md|repos\.jsonl|vendors\.yaml|accounts\.json)' ;;
  esac
}

# disturb: edits the OLD side makes so the upgrade has work to do.
disturb() {
  printf '\n# mine\n' >> .khub/storage.yaml
  sed -i.orig 's/^\(  stale_days: [0-9]*\).*/\1   # tuned/' .khub/config.yaml && rm -f .khub/config.yaml.orig
  [ -f .khub/templates/adr.yaml ] && rm .khub/templates/adr.yaml
  [ -f knowledge/arc42.md ] && rm knowledge/arc42.md
  return 0
}

backward() {
  local preset=$1; shift
  local dir="$WORK/back-$preset"
  say "$preset: $OLDV writes, $NEWV upgrades and reads"
  author "$preset" "$dir" "$OLD" "$@"
  disturb
  # OLD records the schema it wrote under, where it can (0.27+).
  local old_snapshot=0
  if [ "$(printf '%s\n0.27.0\n' "$OLDV" | sort -V | head -1)" = "0.27.0" ]; then
    ok 0 "OLD schema snapshot" "$OLD" schema snapshot --format json
    old_snapshot=1
  fi
  git init -q . && git add -A && git commit -qm "authored by $OLDV"
  local before_cfg; before_cfg=$(cat .khub/config.yaml)
  local singletons_glob='knowledge/arc42.md'

  # ---- read without upgrading: the common case
  ok 0 "raw tree: validate" "$NEW" validate --format json
  jq_eq "raw tree: no errors" '.errors' '[]'
  ok 0 "raw tree: check" "$NEW" check --format json
  jq_eq "raw tree: check passed" '.passed' 'true'
  ok 0 "raw tree: query" "$NEW" query --format json
  jq_eq "raw tree: query sees the authored set" 'length' "$((AUTHORED + $(find . -path ./.git -prune -o -name '*.md' -print | grep -cE '^\./(knowledge/(prd|arc42)|charter)\.md$' || true)))"
  ok 0 "raw tree: get --edges" "$NEW" get "$ANCHOR" --edges --format json
  jq_eq "raw tree: anchor edges" '[.edges[]|select(.derived==false)]|length' "$ANCHOR_EDGES"
  ok 0 "raw tree: status" "$NEW" status --format json
  if [ "$old_snapshot" = 1 ]; then
    ok 0 "raw tree: schema diff against OLD's snapshot" "$NEW" schema diff --format json
    jq_eq "raw tree: nothing pending across versions" '.pending' 'false'
  fi
  [ -z "$(git status --porcelain)" ] && pass "reads moved nothing" || fail "a read moved files" "$(git status --porcelain)"

  # ---- preview is pure
  ok 0 "upgrade --dry-run" "$NEW" upgrade --dry-run --format json
  jq_eq "dry-run says dry_run" '.dry_run' 'true'
  jq_eq "dry-run names the edited layer backup" '[.config[]|select(.backup!=null)|.backup]|index(".khub/storage.yaml.bak")!=null' 'true'
  printf '       plan: %s -> %s config=%s singletons=%s wire=%s index=%s\n' "$(printf '%s' "$OUT" | jq -r .version_from)" "$(printf '%s' "$OUT" | jq -r .version_to)" "$(printf '%s' "$OUT" | jq -c '[.config[]|.action+" "+.name]')" "$(printf '%s' "$OUT" | jq -c .singletons_created)" "$(printf '%s' "$OUT" | jq -c '[.wire[].action]')" "$(printf '%s' "$OUT" | jq -r .index)"
  [ -z "$(git status --porcelain)" ] && pass "dry-run moved nothing (git status clean)" || fail "dry-run moved files" "$(git status --porcelain)"
  [ -z "$(ls -d .khub/generated/upgrade-* 2>/dev/null)" ] && pass "dry-run left no journal" || fail "dry-run left a journal"
  ok 0 "upgrade --no-schema --dry-run" "$NEW" upgrade --no-schema --dry-run --format json
  jq_eq "--no-schema reports no drift (same preset types)" '.schema_drift' '[]'
  jq_eq "--no-schema replaces no layer file" '.config' '[]'

  # ---- the upgrade
  ok 0 "upgrade" "$NEW" upgrade --format json
  local report=$OUT
  jq_eq "exit 0 and dry_run false" '.dry_run' 'false'
  jq_eq "removed_types empty" '.removed_types' '[]'
  jq_eq "no tail error" '[.wire_error?,.index_error?]|map(select(.!=null))' '[]'
  jq_eq "storage.yaml replaced with a backup" '[.config[]|select(.name==".khub/storage.yaml")|.action,.backup]' '["replaced",".khub/storage.yaml.bak"]'
  # backups named == backups present
  local named present
  named=$(printf '%s' "$report" | jq -r '.config[]|select(.backup!=null)|.backup' | sort)
  present=$(find .khub -name '*.bak' -printf '%P\n' | sed 's|^|.khub/|' | sort)
  [ "$named" = "$present" ] && pass ".bak set == config[].backup" || fail ".bak set != config[].backup" "named=[$named] present=[$present]"
  # the backup holds the OLD bytes
  tail -c 8 .khub/storage.yaml.bak | grep -q '# mine' && pass "backup carries the edit" || fail "backup lacks the edit"
  ! grep -q '# mine' .khub/storage.yaml && pass "replaced layer is the shipped one" || fail "layer still carries the edit"
  # git status within what the report names
  local allowed='^(\.khub/(ontology|policy|storage)\.yaml(\.bak)?|\.khub/templates/.*|\.khub/config\.yaml|index\.md|CLAUDE\.md|AGENTS\.md)$'
  local created; created=$(printf '%s' "$report" | jq -r '.singletons_created[]')
  [ -n "$created" ] && allowed="$allowed|^$singletons_glob$"
  status_within "git status within .khub/, index.md, agent files, named singletons" "$allowed"
  # nothing MODIFIED or DELETED under a storage root (creations are singletons, asserted above)
  local touched; touched=$(git status --porcelain | grep -E '^ ?[MD]' | awk '{print $2}' | grep -E "$STORAGE" || true)
  [ -z "$touched" ] && pass "no entity modified or deleted under storage roots" || fail "entities moved" "$touched"
  # untracked under storage == singletons_created
  local newfiles; newfiles=$(git status --porcelain | grep '^??' | awk '{print $2}' | grep -E "$STORAGE" || true)
  if [ -z "$created" ]; then [ -z "$newfiles" ] && pass "no new entity files" || fail "unreported new files" "$newfiles"
  else [ "$newfiles" = "$singletons_glob" ] && pass "only the named singleton was created ($created)" || fail "new files != singletons_created" "$newfiles"; fi
  # config.yaml: restamp touches version only; an unchanged version leaves the edited comment
  if [ "$(printf '%s' "$report" | jq -r '.version_from')" = "$(printf '%s' "$report" | jq -r '.version_to')" ]; then
    [ "$(cat .khub/config.yaml)" = "$before_cfg" ] && pass "config.yaml untouched (version unchanged, comment kept)" || fail "config.yaml rewritten" "$(diff <(echo "$before_cfg") .khub/config.yaml)"
  else
    grep -q '# tuned' .khub/config.yaml && pass "config.yaml restamped, stale_days comment kept" || fail "restamp lost the comment"
  fi
  [ "$(ls .khub/generated)" = "locks" ] && pass "journal removed; generated/ holds only locks/" || fail "generated/ holds more" "$(ls .khub/generated)"
  if git status --porcelain | grep -qF .gitignore; then fail ".gitignore moved"; else pass ".gitignore untouched"; fi

  # ---- idempotence
  local after1; after1=$(git status --porcelain)
  ok 0 "upgrade again" "$NEW" upgrade --format json
  jq_eq "second upgrade: nothing to do" '[.config,.singletons_created,.schema_drift,([.wire[].action]|unique),.index]' '[[],[],[],["unchanged"],"unchanged"]'
  [ "$(git status --porcelain)" = "$after1" ] && pass "second upgrade moved nothing" || fail "second upgrade moved files"
  ok 0 "dry-run after upgrade" "$NEW" upgrade --dry-run --format json
  jq_eq "dry-run after upgrade: nothing pending" '[.config,.singletons_created,([.wire[].action]|unique),.index]' '[[],[],["unchanged"],"unchanged"]'
  git add -A && git commit -qm "upgraded by $NEWV"

  # ---- read surface on the upgraded tree
  ok 0 "validate" "$NEW" validate --format json
  jq_eq "validate: no errors" '.errors' '[]'
  ok 0 "check" "$NEW" check --format json
  jq_eq "check passed" '.passed' 'true'
  ok 0 "query" "$NEW" query --format json
  local total; total=$(printf '%s' "$OUT" | jq 'length')
  local singles; singles=$(find . -path ./.git -prune -o -name '*.md' -print | grep -cE '^\./(knowledge/(prd|arc42)|charter)\.md$' || true)
  [ "$total" = "$((AUTHORED + singles))" ] && pass "query sees $AUTHORED authored + $singles singletons" || fail "query count" "got $total want $((AUTHORED + singles))"
  ok 0 "query --draft" "$NEW" query --draft --format json
  [ "$preset" = preset-cells ] && jq_eq "no drafts in cells" 'length' '0' || jq_eq "the one draft survives" 'length' '1'
  ok 0 "get --edges" "$NEW" get "$ANCHOR" --edges --format json
  jq_eq "anchor edges" '[.edges[]|select(.derived==false)]|length' "$ANCHOR_EDGES"
  ok 0 "neighbors" "$NEW" neighbors "$LINKED" --format json
  ok 0 "search" "$NEW" search --plain "api initech note" --format json
  ok 0 "status" "$NEW" status --format json
  jq_eq "status total" '.total' "$((AUTHORED + singles))"
  ok 0 "reindex --dry-run" "$NEW" reindex --dry-run
  [ "$OUT" = "index.md is up to date" ] && pass "index current after upgrade" || fail "index stale after upgrade" "$OUT"
  ok 0 "schema" "$NEW" schema --format json
  if [ "$old_snapshot" = 1 ]; then
    ok 0 "schema diff after upgrade" "$NEW" schema diff --format json
    [ "$(printf '%s' "$report" | jq -r '.version_from')" = "$(printf '%s' "$report" | jq -r '.version_to')" ] \
      && jq_eq "preset version unchanged: nothing pending" '.pending' 'false' \
      || pass "preset version moved: schema diff reports pending=$(printf '%s' "$OUT" | jq -r .pending)"
  fi
  ok 0 "schema snapshot" "$NEW" schema snapshot --format json
  ok 0 "schema diff" "$NEW" schema diff --format json
  jq_eq "snapshot by NEW: nothing pending" '.pending' 'false'

  # ---- one write on an OLD-authored entity is a minimal diff
  git add -A && git commit -qm snapshot
  ok 0 "edit OLD entity" "$NEW" edit "$LINKED" --tags compat --format json
  local files; files=$(git diff --name-only | wc -l)
  [ "$files" = 1 ] && pass "edit touched one file" || fail "edit touched $files files" "$(git diff --name-only)"
  ok 0 "validate after edit" "$NEW" validate --format json
  jq_eq "edit validates" '.errors' '[]'
  cd "$WORK" || exit 1
}

forward() {
  local preset=$1; shift
  local dir="$WORK/fwd-$preset"
  say "$preset (forward, report-only): $NEWV writes, $OLDV reads"
  author "$preset" "$dir" "$NEW" "$@"
  ok 0 "reindex" "$NEW" reindex
  git init -q . && git add -A && git commit -qm "authored by $NEWV"
  ok 0 "OLD validate" "$OLD" validate --format json
  jq_eq "OLD validate: no errors" '.errors' '[]'
  ok 0 "OLD check" "$OLD" check --format json
  jq_eq "OLD check passed" '.passed' 'true'
  ok 0 "OLD query" "$OLD" query --format json
  ok 0 "OLD get --edges" "$OLD" get "$ANCHOR" --edges --format json
  jq_eq "OLD anchor edges" '[.edges[]|select(.derived==false)]|length' "$ANCHOR_EDGES"
  ok 0 "OLD status" "$OLD" status --format json
  ok 0 "OLD reindex --dry-run" "$OLD" reindex --dry-run
  [ "$OUT" = "index.md is up to date" ] && pass "OLD agrees NEW's index is current" || fail "OLD would rewrite NEW's index" "$(echo "$OUT" | head -5)"
  ok 0 "OLD upgrade --dry-run" "$OLD" upgrade --dry-run --format json
  printf '       OLD upgrade would: config=%s wire=%s index=%s extra=%s\n' \
    "$(printf '%s' "$OUT" | jq -c .config)" "$(printf '%s' "$OUT" | jq -c '[.wire[].action]')" "$(printf '%s' "$OUT" | jq -r .index)" \
    "$(printf '%s' "$OUT" | jq -c 'del(.path,.preset,.version_from,.version_to,.config,.singletons_created,.schema_drift,.wire,.index,.dry_run,.removed_types)')"
  [ -z "$(git status --porcelain)" ] && pass "OLD dry-run moved nothing" || fail "OLD dry-run moved files" "$(git status --porcelain)"
  cd "$WORK" || exit 1
}

backward build-hub
backward firm-ops
backward preset-cells --no-wire --preset-source "$PRESETSRC"
if [ "$FORWARD" = 1 ]; then
  forward build-hub
  forward firm-ops
fi

printf '\n%s -> %s: %d checks, %d failed\n' "$OLDV" "$NEWV" "$CASES" "$FAILED"
[ "$FAILED" = 0 ]
