#!/bin/bash
# The plugin's static context footprint, against plugin/budget.json.
#
#   bash plugin/tools/budget.sh            # measure and compare; exit 1 on any exceed
#   bash plugin/tools/budget.sh --write    # re-record the budget at today's numbers
#
# What is measured, all exact and offline:
#   skill.<name>.bytes              wc -c of the whole SKILL.md (what `claude plugin
#                                   details` projects into tokens at about bytes/3)
#   skill.<name>.description_chars  the `description:` line; the skill listing
#                                   truncates it at 1,536 characters
#   wire.<preset>.<file>.bytes      the block `khub wire` writes between
#                                   <!-- khub:begin --> and <!-- khub:end -->,
#                                   per preset and agent file
#
# The rule: every measured number is at most its budget, every budgeted key is
# measured, and a description is under 1,536 whatever the budget says. A
# budget is a ceiling only: shrinking is always fine. To grow one, run
# --write in the same commit as the text change and say why in the message —
# the parity re-record discipline applied to a number. The parity fixtures pin
# the wire block's bytes by hash; this is the number beside the hash, and the
# headroom, in a diff a reviewer can read.
#
# Node is the only interpreter here: it is the plugin's own toolchain.
set -euo pipefail

PLUGIN="$(cd "$(dirname "$0")/.." && pwd)"
REPO="$(cd "$PLUGIN/.." && pwd)"
BUDGET="$PLUGIN/budget.json"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

(cd "$REPO" && go build -o "$TMP/khub" ./cmd/khub)
export KHUB_PARITY_NOW=2026-01-15

{
  for s in "$PLUGIN"/skills/*/SKILL.md; do
    n="$(basename "$(dirname "$s")")"
    printf 'skill.%s.bytes %s\n' "$n" "$(wc -c <"$s" | tr -d ' ')"
    printf 'skill.%s.description_chars %s\n' "$n" \
      "$(awk '/^description:/{sub(/^description: */,""); print length($0); exit}' "$s")"
  done
  for p in build-hub firm-ops; do
    "$TMP/khub" init "$p" "$TMP/$p" >/dev/null
    for f in CLAUDE.md AGENTS.md; do
      printf 'wire.%s.%s.bytes %s\n' "$p" "$f" \
        "$(awk '/<!-- khub:begin -->/{p=1} p{print} /<!-- khub:end -->/{p=0}' "$TMP/$p/$f" | wc -c | tr -d ' ')"
    done
  done
} >"$TMP/measured"

if [ "${1:-}" = "--write" ]; then
  node -e '
const fs = require("fs");
const o = {};
for (const l of fs.readFileSync(process.argv[1], "utf8").trim().split("\n")) {
  const [k, v] = l.split(" ");
  o[k] = Number(v);
}
fs.writeFileSync(process.argv[2], JSON.stringify(o, null, 2) + "\n");
' "$TMP/measured" "$BUDGET"
  cat "$BUDGET"
  exit 0
fi

node -e '
const fs = require("fs");
const LISTING_CAP = 1536;
const measured = fs.readFileSync(process.argv[1], "utf8").trim().split("\n");
const budget = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const seen = new Set();
let bad = false;
console.log("metric".padEnd(36) + "measured".padStart(9) + "budget".padStart(8) + "headroom".padStart(10));
for (const l of measured) {
  const [k, v] = l.split(" ");
  const m = Number(v);
  let lim = budget[k];
  seen.add(k);
  if (k.endsWith(".description_chars")) lim = Math.min(lim ?? LISTING_CAP, LISTING_CAP);
  const over = lim === undefined || m > lim;
  if (over) bad = true;
  console.log(k.padEnd(36) + String(m).padStart(9) + String(lim ?? "-").padStart(8)
    + String(lim === undefined ? "-" : lim - m).padStart(10) + (lim === undefined ? "  no budget" : m > lim ? "  EXCEEDED" : ""));
}
for (const k of Object.keys(budget)) if (!seen.has(k)) { console.log(k.padEnd(36) + "  budgeted, not measured"); bad = true; }
process.exit(bad ? 1 : 0);
' "$TMP/measured" "$BUDGET"
