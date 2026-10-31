#!/bin/bash
# Every `khub …` a skill names must be a call the binary accepts.
#
#   bash plugin/tools/skill-flags.sh <khub> <workspace>... -- <file>...
#
# Each backticked `khub <verb> … --flag` span in the given files is checked:
# the verb (and a schema subcommand) must print a Usage, and every --flag must
# appear in that verb's --help. add, edit and query take field flags the
# schema declares, so for those a flag is also accepted when it names a field
# in `khub schema --format json` of any workspace given — read live, with no
# type or field spelled here. `--<field>` placeholders are skipped.
#
# Deterministic and offline. The skills are prose about the CLI; this is the
# one check that the prose and the binary agree, which no fixture performs.
set -euo pipefail

BIN="$1"; shift
WS=()
while [ "$1" != "--" ]; do WS+=("$1"); shift; done
shift

fields="$(for w in "${WS[@]}"; do "$BIN" -C "$w" schema --format json; done \
  | node -e '
let s = "";
process.stdin.on("data", (d) => (s += d)).on("end", () => {
  for (const l of s.split("\n")) {
    if (!l.trim()) continue;
    for (const t of JSON.parse(l).types) for (const f of t.fields || []) console.log(f.name);
  }
});' | sort -u)"

fail=0
for file in "$@"; do
  while read -r line; do
    # shellcheck disable=SC2086  # the span is split into words on purpose
    set -- $line
    shift                                   # drop "khub"
    verb="${1:-}"
    [ -z "$verb" ] && continue
    case "$verb" in -*) continue ;; esac    # a bare global flag, no verb
    sub=""
    if [ "$verb" = schema ]; then
      case "${2:-}" in show|diff|snapshot|types|edges|base) sub="$2" ;; esac
    fi
    # shellcheck disable=SC2086  # $sub is one word or empty
    help="$("$BIN" "$verb" $sub --help 2>&1 || true)"
    if ! grep -q '^Usage:' <<<"$help"; then
      echo "$file: unknown verb: $line"; fail=1; continue
    fi
    for tok in "$@"; do
      case "$tok" in --\<*) continue ;; --*) ;; *) continue ;; esac
      flag="${tok%%=*}"
      flag="$(printf '%s' "$flag" | sed 's/[^-a-z_].*$//')"
      grep -qE "^[[:space:]]+(-[a-zA-Z], )?$flag( |$|,)" <<<"$help" && continue
      case "$verb" in
        add|edit|query) grep -qx -- "${flag#--}" <<<"$fields" && continue ;;
      esac
      echo "$file: $verb${sub:+ $sub}: flag not in --help: $flag   (in: $line)"; fail=1
    done
  # shellcheck disable=SC2016  # the backticks are the Markdown code spans being matched
  done < <(grep -o '`khub [^`]*`' "$file" | tr -d '`' | sort -u)
done
exit $fail
