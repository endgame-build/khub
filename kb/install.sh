#!/bin/sh
# Wire a dropped-in kb/ into the agent that will use it.
#
# None of this is required if your opencode.json can name the skill directory
# in place — that is the zero-copy path, and it installs nothing:
#
#     { "skills": { "paths": ["kb/skills"] } }
#
# Otherwise opencode only looks in .opencode/skills, .claude/skills and
# .agents/skills, so the skill has to be copied into one of them. That is what
# this does. Idempotent: re-run it to upgrade.
#
#   kb/install.sh                    -> ./.opencode/skills/kb
#   kb/install.sh --claude --agents     also the other two locations
#   kb/install.sh --global           -> $XDG_CONFIG_HOME/opencode/skills
#   kb/install.sh --bin ~/.local/bin    symlink `kb` onto PATH
#
# Only the skill is copied. scripts/ stays where you dropped it: kb.py resolves
# build.schema.yaml and templates/ relative to itself, so it runs from anywhere.
set -eu

home_dir="$(cd "$(dirname "$0")" && pwd)"
src="$home_dir/skills/kb"
name=kb
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
  echo "installed $dir/$name"
done

if [ -n "$bin" ]; then
  mkdir -p "$bin"
  ln -sf "$home_dir/scripts/kb.py" "$bin/kb"
  echo "linked $bin/kb -> $home_dir/scripts/kb.py"
fi

echo
echo "next: kb init && kb check   (or python3 $home_dir/scripts/kb.py init)"
echo "then allow it in opencode.json — catch-all FIRST, last match wins:"
echo '  { "permission": { "bash": { "*": "ask", "kb *": "allow" } } }'
