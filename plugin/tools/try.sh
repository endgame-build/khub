#!/bin/bash
# Builds khub and a throwaway build-hub workspace, then prints the command that
# opens a Claude Code session there with the plugin loaded.
#
#   plugin/tools/try.sh [dir]     a fresh temporary directory when none is given

set -euo pipefail

PLUGIN="$(cd "$(dirname "$0")/.." && pwd)"
REPO="$(cd "$PLUGIN/.." && pwd)"
DIR="${1:-$(mktemp -d)}"

mkdir -p "$DIR/bin"
(cd "$REPO" && go build -o "$DIR/bin/khub" ./cmd/khub)

k() { "$DIR/bin/khub" -C "$DIR/ws" "$@"; }

if [[ ! -d "$DIR/ws/.khub" ]]; then
  "$DIR/bin/khub" init build-hub "$DIR/ws" >/dev/null
  k add repo --title "API" --repo endgame/api --status active >/dev/null
  k add component --title "Storage" --kind service --owner team-platform >/dev/null
  k add component --title "Search" --kind service --owner team-platform --lifecycle production \
    --tier tier-1 --repo rp-api --depends_on cmp-storage >/dev/null
  k add actor --title "Analyst" >/dev/null
  k add capability --title "Find documents" >/dev/null
  k add use-case --title "Find a document" --trigger human --actor act-analyst \
    --capability cap-find-documents --served_by cmp-search >/dev/null
fi

echo "Workspace: $DIR/ws ($(k status --format json | grep -o '"total": [0-9]*' | cut -d' ' -f2) entities)"
echo
echo "cd \"$DIR/ws\" && PATH=\"$DIR/bin:\$PATH\" claude --plugin-dir \"$PLUGIN\""
