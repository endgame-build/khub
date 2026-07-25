#!/bin/sh
# Install the build-lite skill.
#
#   lite/install.sh                  -> ./.opencode/skills/build-lite
#   lite/install.sh --global         -> $XDG_CONFIG_HOME/opencode/skills (or ~/.config)
#   lite/install.sh --claude --agents  also .claude/skills and .agents/skills
#   lite/install.sh --bin ~/.local/bin symlink `bl` onto PATH
#
# Idempotent: re-run it to upgrade. The copies are managed — edit the source.
set -eu

src="$(cd "$(dirname "$0")/skill" && pwd)"
name=build-lite
global=0 claude=0 agents=0 bin=""

while [ $# -gt 0 ]; do
  case "$1" in
    --global) global=1 ;;
    --claude) claude=1 ;;
    --agents) agents=1 ;;
    --bin) bin="${2:?--bin needs a directory}"; shift ;;
    *) echo "install.sh: unknown option $1" >&2; exit 2 ;;
  esac
  shift
done

if [ "$global" -eq 1 ]; then
  # opencode reads its global skills under the XDG config root, not ~/.opencode.
  targets="${XDG_CONFIG_HOME:-$HOME/.config}/opencode/skills"
  [ "$claude" -eq 1 ] && targets="$targets $HOME/.claude/skills"
  [ "$agents" -eq 1 ] && targets="$targets $HOME/.agents/skills"
else
  targets=".opencode/skills"
  [ "$claude" -eq 1 ] && targets="$targets .claude/skills"
  [ "$agents" -eq 1 ] && targets="$targets .agents/skills"
fi

for dir in $targets; do
  mkdir -p "$dir/$name"
  cp -R "$src/." "$dir/$name/"
  # opencode samples at most 10 files from a skill directory; don't spend a slot
  # on bytecode.
  find "$dir/$name" -name '__pycache__' -type d -prune -exec rm -rf {} + 2>/dev/null || true
  echo "installed $dir/$name"
  last="$dir/$name"
done

if [ -n "$bin" ]; then
  mkdir -p "$bin"
  ln -sf "$(cd "$(dirname "$last")" && pwd)/$name/bl.py" "$bin/bl"
  echo "linked $bin/bl"
fi

echo
echo "next: bl init && bl check   (or python3 $last/bl.py init)"
echo "then allow it in opencode.json — catch-all FIRST, last match wins:"
echo '  { "permission": { "bash": { "*": "ask", "bl *": "allow" } } }'
