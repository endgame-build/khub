#!/bin/bash
# In-process benchmarks, HEAD against the last release tag, on one machine.
#
#   bash parity/scale/benchcmp.sh [workdir]
#
# A nightly runs on main, so "main vs nightly" would compare a commit with
# itself, and a result cached from another night ran on other hardware and
# does not compare in ns/op. This builds the last release tag into a worktree,
# generates one corpus with the BASE binary (HEAD reading an older-written
# corpus is the upgrade direction khub promises; the reverse may not hold),
# then runs the large-corpus benchmarks of both trees interleaved, several
# times each, so a slow drift on the runner hits both sides alike. benchstat
# reports the delta; allocs/op is the column that travels between machines
# (±0.02% here), ns/op the one that does not.
#
# Report only. Nothing here fails on a number.
set -euo pipefail

REPO=$(cd "$(dirname "$0")/../.." && pwd)
WORK=${1:-${TMPDIR:-/tmp}/khub-benchcmp}
SCALE=${BENCHCMP_SCALE:-2000}
COUNT=${BENCHCMP_COUNT:-6}
BENCHSTAT=golang.org/x/perf/cmd/benchstat@v0.0.0-20261009192801-be2c69fb417e
export KHUB_PARITY_NOW=2026-01-15 CGO_ENABLED=0

base=$(git -C "$REPO" describe --tags --abbrev=0 --match 'v*' 2>/dev/null) || {
  echo "no release tag reachable from HEAD (fetch tags first)" >&2; exit 2; }
head=$(git -C "$REPO" rev-parse --short HEAD)
echo "base $base  head $head  scale $SCALE  count $COUNT"

rm -rf "$WORK"
mkdir -p "$WORK"
git -C "$REPO" worktree add -q --detach "$WORK/base" "$base"
trap 'git -C "$REPO" worktree remove --force "$WORK/base" >/dev/null 2>&1 || true' EXIT

(cd "$WORK/base" && go build -o "$WORK/khub-base" ./cmd/khub)
# The benchmarks are HEAD's on both sides, so only the packages under them
# differ (a benchmark that reads PROF_QUERY must read it on both). A new
# benchmark that names a symbol the base lacks fails the base build here,
# which is the honest outcome: there is nothing to compare it with yet.
for f in $(cd "$REPO" && git ls-files 'internal/*/profile_test.go'); do
  cp "$REPO/$f" "$WORK/base/$f"
done
(cd "$REPO" && go build -o "$WORK/khub-head" ./cmd/khub \
  && go build -o "$WORK/gen" ./parity/scale/gen && go build -o "$WORK/manifest" ./parity/scale/manifest)

"$WORK/gen" -bin "$WORK/khub-base" -work "$WORK/corpus" -scale "$SCALE" -seed 1 -today 2026-01-15 -quiet
needle=$("$WORK/manifest" "$WORK/corpus/smoke-manifest.json" search.needle)
export PROF_WS=$WORK/corpus PROF_QUERY=$needle

pkgs="./internal/integrity ./internal/query ./internal/search"
bench='LargeCorpus'
: >"$WORK/base.txt"; : >"$WORK/head.txt"
for i in $(seq "$COUNT"); do
  echo "round $i of $COUNT"
  # shellcheck disable=SC2086  # the package list is split on purpose
  (cd "$WORK/base" && go test -run '^$' -bench "$bench" -benchmem -count=1 $pkgs) >>"$WORK/base.txt"
  # shellcheck disable=SC2086
  (cd "$REPO" && go test -run '^$' -bench "$bench" -benchmem -count=1 $pkgs) >>"$WORK/head.txt"
done

echo
echo "benchstat $base (old) vs $head (new), $COUNT runs each, corpus $SCALE written by $base:"
(cd "$WORK" && go run "$BENCHSTAT" base.txt head.txt)
