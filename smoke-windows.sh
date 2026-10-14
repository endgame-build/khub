#!/bin/bash
# Windows smoke, under Git Bash. `bash smoke-windows.sh [workdir]`.
#
# Two parts. First the whole of smoke.sh, unchanged, against the Windows
# binary: every command over both build presets, every exit code asserted.
# Then what smoke.sh does not reach and Windows is most likely to break: a
# workspace named by a Windows path, git-derived dates (stale, backfill), the
# workspace lock under concurrent writers, line endings, and serve's TTY
# refusal.
#
# It runs on darwin and linux too (KHUB defaults to ./khub there), so it can
# be checked without a Windows machine; only the cygpath legs are skipped.
set -uo pipefail

REPO=$(cd "$(dirname "$0")" && pwd)
WORK=${1:-${TMPDIR:-/tmp}/khub-smoke-windows}
if [ -z "${KHUB:-}" ]; then
  if [ -x "$REPO/khub.exe" ]; then KHUB="$REPO/khub.exe"; else KHUB="$REPO/khub"; fi
fi
export KHUB KHUB_PARITY_NOW=2026-01-15
# The gate decides on stdout alone here: no override may make serve start.
unset TTY_COMPATIBLE FORCE_COLOR
# A pinned git identity, and no user or system config (Git for Windows ships
# core.autocrlf=true in its system config).
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=Smoke GIT_AUTHOR_EMAIL=smoke@example.com
export GIT_COMMITTER_NAME=Smoke GIT_COMMITTER_EMAIL=smoke@example.com
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
  if grep -qF -- "$3" "$2"; then printf '  ok   %-50s\n' "$1"
  else
    FAILED=$((FAILED + 1))
    printf '  \033[31mFAIL\033[0m %-50s (missing from %s)\n       %s\n' "$1" "$2" "$3"
  fi
}
# no_cr <label> <file>: khub writes LF, on every OS.
no_cr() {
  CASES=$((CASES + 1))
  if grep -q $'\r' "$2"; then
    FAILED=$((FAILED + 1))
    printf '  \033[31mFAIL\033[0m %-50s (CR in %s)\n' "$1" "$2"
  else printf '  ok   %-50s\n' "$1"; fi
}
# native <path>: the spelling a Windows user types (C:\…); the path itself
# elsewhere.
native() {
  if command -v cygpath >/dev/null 2>&1; then cygpath -w "$1"; else printf '%s' "$1"; fi
}

rm -rf "$WORK"; mkdir -p "$WORK"

# --------------------------------------------------------------- all of smoke.sh
say "smoke.sh against $KHUB"
# smoke.sh calls python3 once. On a Windows runner python3 can be the Store
# alias, which prints an install hint and exits non-zero; hand it python.
if ! python3 -c 'import sys' >/dev/null 2>&1 && python -c 'import sys' >/dev/null 2>&1; then
  python3() { python "$@"; }
  export -f python3
fi
CASES=$((CASES + 1))
if bash "$REPO/smoke.sh" "$WORK/smoke"; then printf '  ok   %-50s\n' "smoke.sh"
else FAILED=$((FAILED + 1)); printf '  \033[31mFAIL\033[0m %-50s\n' "smoke.sh"; fi

# ------------------------------------------------------- a workspace by native path
say "a workspace named by a native path"
W=$WORK/native; mkdir -p "$W"
WN=$(native "$W")
ok 0 "init at a native path"           "$KHUB" init build-hub "$WN" --no-wire
ok 0 "add via -C <native path>"        "$KHUB" -C "$WN" add requirement --title "Pay by card" --kind functional
ok 0 "add a component"                 "$KHUB" -C "$WN" add component --title "API" --kind service
ok 0 "edit"                            "$KHUB" -C "$WN" edit req-pay-by-card kind constraint
ok 0 "link"                            "$KHUB" -C "$WN" link req-pay-by-card realized_in cmp-api
"$KHUB" -C "$WN" query --type requirement --format json > "$W/q.json" 2>&1
has  "query: the edited entity"        "$W/q.json" '"id": "requirement/req-pay-by-card"'
no_cr "query: LF only on stdout"       "$W/q.json"
"$KHUB" -C "$WN" get req-pay-by-card --format json > "$W/g.json" 2>&1
has  "get: a slash-separated path"     "$W/g.json" '"path": "knowledge/requirements/req-pay-by-card.md"'
has  "get: the edit landed"            "$W/g.json" '"kind": "constraint"'
no_cr "entity file: LF only"           "$W/knowledge/requirements/req-pay-by-card.md"
no_cr "config: LF only"                "$W/.khub/config.yaml"
"$KHUB" -C "$WN" search card --format json > "$W/s.json" 2>&1
has  "search: FTS5 finds it"           "$W/s.json" '"requirement/req-pay-by-card"'
ok 0 "check"                           "$KHUB" -C "$WN" check --format json
ok 0 "viz"                             "$KHUB" -C "$WN" viz --out "$(native "$W/viz.html")"
has  "viz: wrote the page"             "$W/viz.html" "<html"

# ----------------------------------------------------------------- the workspace lock
say "concurrent writers"
for i in 1 2 3 4 5 6; do
  "$KHUB" -C "$WN" add actor --title "Actor $i" >/dev/null 2>&1 &
done
wait
"$KHUB" -C "$WN" query --type actor --format ids > "$W/actors.txt" 2>&1
CASES=$((CASES + 1))
n=$(grep -c '^act-' "$W/actors.txt")
if [ "$n" = 6 ]; then printf '  ok   %-50s\n' "six concurrent adds, six actors"
else FAILED=$((FAILED + 1)); printf '  \033[31mFAIL\033[0m %-50s got %s\n' "six concurrent adds, six actors" "$n"; fi
ok 0 "check after the race"            "$KHUB" -C "$WN" check --format json

# --------------------------------------------------------------- git-derived dates
say "git: stale and backfill read git log"
G=$WORK/git; mkdir -p "$G"; cd "$G" || exit 1
ok 0 "init"                            "$KHUB" init build-hub . --no-wire
ok 0 "add"                             "$KHUB" add requirement --title "Old" --kind functional
commit_at() { # <message>: commit everything, dated 2025-06-01
  # Only ever into the repository `git init` made here. If init failed, git
  # would find whatever repository encloses $WORK (a checkout, when the
  # workdir is inside one) and commit the smoke's files into it.
  [ "$(git rev-parse --git-dir 2>/dev/null)" = ".git" ] || {
    echo "refusing to commit: $G is not the root of its own repository"; return 1; }
  git add -A && GIT_AUTHOR_DATE=2025-06-01T10:00:00Z GIT_COMMITTER_DATE=2025-06-01T10:00:00Z git commit -qm "$1"
}
# Drop `updated` so the date can only come from git.
strip_updated() {
  sed -i.orig '/^updated:/d' knowledge/requirements/req-old.md && rm -f knowledge/requirements/req-old.md.orig
}
ok 0 "git init"                        git init -q --template= .
ok 0 "commit the workspace"            commit_at seed
ok 0 "strip updated"                   strip_updated
ok 0 "commit the strip"                commit_at strip
"$KHUB" stale --format json > "$G/stale.json" 2>&1
has  "stale: dated by git log"         "$G/stale.json" '"source": "git log"'
has  "stale: the commit date"          "$G/stale.json" '"effective_date": "2025-06-01"'
ok 0 "backfill --dry-run"              "$KHUB" backfill --dry-run
ok 0 "history"                         "$KHUB" history req-old --format json

# ------------------------------------------------------------------ serve refuses
say "serve without a terminal"
# stdout is a pipe here, so serve must refuse at once with exit 2. `timeout`
# turns a regression into a failure instead of a hung job.
T=""; command -v timeout >/dev/null 2>&1 && T="timeout 20"
CASES=$((CASES + 1))
out=$($T "$KHUB" -C "$WN" serve 2>&1); got=$?
if [ "$got" = 2 ] && printf '%s' "$out" | grep -qF '"serve_needs_tty"'; then
  printf '  ok   %-50s\n' "serve: refuses a non-terminal stdout"
else
  FAILED=$((FAILED + 1))
  printf '  \033[31mFAIL\033[0m %-50s exit=%s %s\n' "serve: refuses a non-terminal stdout" "$got" "$out"
fi

printf '\n\033[1m%s cases, %s failed\033[0m\n' "$CASES" "$FAILED"
exit $((FAILED > 0))
