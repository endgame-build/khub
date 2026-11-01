#!/bin/bash
# Binary coverage floor: runs the parity suite with a cover-instrumented khub
# and fails if internal/cli drops below parity/coverage-floor.txt.
#
#   go build -cover -o khub-cover ./cmd/khub
#   bash parity/tools/coverage-floor.sh "$PWD/khub-cover" /tmp/khub-cov
#
# The parity suite is deterministic, so its coverage is too: two runs give a
# byte-identical merged profile. What moves the number is the machine, not
# the run — err-os-error is skipped under root, viz --open and the OS-error
# paths differ by platform — so the floor carries a little slack and is
# pinned on ubuntu only. A drop below it means a CLI path with no fixture,
# which in this layer usually means per-type branching crept in (CLAUDE.md).
#
# The runner builds each child's environment from scratch (baseEnv), so
# GOCOVERDIR cannot be exported to it; it is handed in through -bin, which
# the runner splits on spaces. The counter directory must sit outside any
# workspace the suite creates, or the tree manifests pick up counter files.
set -euo pipefail

bin=${1:?cover-built khub}
dir=${2:?counter directory, outside any workspace}
floor=$(tr -d '[:space:]' < parity/coverage-floor.txt)
case "$bin$dir" in
  *' '*) echo "paths must not contain spaces: -bin is split on them" >&2; exit 2 ;;
esac
rm -rf "$dir"
mkdir -p "$dir"
go build -o parity-run ./parity/runner
./parity-run -bin "env GOCOVERDIR=$dir $bin" -cases parity/cases
got=$(go tool covdata percent -i="$dir" -pkg=github.com/endgame-build/khub/internal/cli \
  | sed -n 's/.*coverage: \([0-9.]*\)% of statements.*/\1/p')
case "$got" in
  ''|*[!0-9.]*) echo "coverage unparsed: '$got'" >&2; exit 1 ;;
esac
echo "internal/cli: $got% of statements from parity (floor $floor%)" \
  | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
if ! awk -v g="$got" -v f="$floor" 'BEGIN { exit !(g + 0 >= f + 0) }'; then
  echo "internal/cli coverage $got% is below the floor $floor%: a CLI path has no fixture" >&2
  exit 1
fi
