#!/usr/bin/env bash
# Export a firm-ops and a build-hub workspace, each with a few linked and
# reviewed entities, and check both bundles with the OKF reference parser
# (okf_validate.py). CI runs it informationally against a pinned document.py.
#
#   parity/tools/okf_reference.sh <khub binary> <dir holding document.py>
set -euo pipefail

khub=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
ref=$(cd "$2" && pwd)
tools=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export KHUB_ACTOR=process:okf-reference

mkdir "$work/firm-ops" "$work/build-hub"
(
  cd "$work/firm-ops"
  "$khub" init firm-ops .
  "$khub" add client --name "Acme Corp" --industry manufacturing
  "$khub" add person --name "Dana Lee" --role partner
  "$khub" add project --title "Acme Diagnostic" --client acme-corp --owner dana-lee
  "$khub" verify acme-diagnostic --by human:reviewer
) >/dev/null
(
  cd "$work/build-hub"
  "$khub" init build-hub .
  "$khub" add component --title Search --kind service --description "Finds documents"
  "$khub" add component --title Storage --kind service
  "$khub" link cmp-search depends_on cmp-storage
  "$khub" add requirement --title "Answers fast" --kind functional --realized_in cmp-search
  "$khub" verify req-answers-fast --by process:ci
) >/dev/null

status=0
for ws in firm-ops build-hub; do
  echo "== $ws"
  "$khub" -C "$work/$ws" export --okf "$work/$ws-bundle" >/dev/null
  uv run --no-project --with pyyaml==6.0.3 python "$tools/okf_validate.py" "$work/$ws-bundle" "$ref" || status=1
done
exit "$status"
