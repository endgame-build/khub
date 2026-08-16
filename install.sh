#!/bin/sh
# khub installer.
#
#   curl -fsSL https://khub.end.game/install.sh | sh
#
# Options, all via environment:
#   KHUB_VERSION      pin a release (default: latest)
#   KHUB_INSTALL_DIR  where to put the binary (default: ~/.local/bin)
#   KHUB_TOKEN        GitHub token; see "Auth" below for what else is tried
#   KHUB_REPO         source repo (default: endgame-build/khub)
#
# This script is public. The binaries are not — endgame-build/khub is a private
# repo, so its release assets 404 to anyone without a token. Nothing secret
# lives here: it detects your platform, fetches an archive, checks it against
# the release's own checksums.txt, and moves one file into place.
#
# Auth. The install command carries no credential; this reads whatever the
# machine already has, in order:
#
#   1. KHUB_TOKEN / GITHUB_TOKEN / GH_TOKEN
#   2. `gh auth token`, if the GitHub CLI is logged in
#   3. `git credential fill`, which reads an HTTPS token from the OS keychain
#   4. nothing — then the anonymous path is used, which is what will work once
#      the repo is public, and which explains itself until then
#
# An SSH key does NOT help and is deliberately not consulted: it authenticates
# git-over-SSH, and the GitHub REST API ignores it.
#
# When khub goes open source this script keeps working with no change at all —
# rung 4 stops being an error and becomes the normal path.
set -eu

REPO="${KHUB_REPO:-endgame-build/khub}"
INSTALL_DIR="${KHUB_INSTALL_DIR:-$HOME/.local/bin}"
DL_HOST="${KHUB_BASE_URL:-https://github.com}"   # test seam
API_HOST="${KHUB_API_URL:-https://api.github.com}"
VERSION="${KHUB_VERSION:-}"
VERSION="${VERSION#v}"   # accept v0.19.0 or 0.19.0

die() { printf 'khub install: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

have curl || die "need curl"
have tar || die "need tar"

# --- target -----------------------------------------------------------------
os=$(uname -s)
case "$os" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) die "unsupported OS '$os' (khub ships darwin and linux; Windows is not supported)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture '$arch'" ;;
esac

# The archive name carries no version, which is what makes the anonymous
# /releases/latest/download/ URL usable without asking the API anything.
archive="khub_${os}_${arch}.tar.gz"

# --- auth -------------------------------------------------------------------
token=""
for candidate in "${KHUB_TOKEN:-}" "${GITHUB_TOKEN:-}" "${GH_TOKEN:-}"; do
  if [ -n "$candidate" ]; then token="$candidate"; break; fi
done
if [ -z "$token" ] && have gh; then
  token=$(gh auth token 2>/dev/null | tr -d '\r\n') || token=""
fi
if [ -z "$token" ] && have git; then
  # Reads the HTTPS credential the OS keychain already holds. Prints nothing
  # (and cannot prompt) when there is none, because GIT_TERMINAL_PROMPT=0.
  token=$(printf 'protocol=https\nhost=github.com\n\n' \
    | GIT_TERMINAL_PROMPT=0 git credential fill 2>/dev/null \
    | sed -n 's/^password=//p' | head -1) || token=""
fi

tmp=$(mktemp -d) || die "cannot create a temp directory"
# Any exit from here removes the staging directory, including a failed checksum
# — a rejected archive must never be left behind for someone to run.
trap 'rm -rf "$tmp"' EXIT INT TERM

# --- fetch ------------------------------------------------------------------
# Two paths. Anonymous is the simple one and the one that survives going public;
# the token path exists only because the repo is private today.
if [ -z "$token" ]; then
  if [ -n "$VERSION" ]; then
    base="$DL_HOST/$REPO/releases/download/v$VERSION"
  else
    base="$DL_HOST/$REPO/releases/latest/download"
  fi
  curl -fsSL "$base/$archive" -o "$tmp/$archive" 2>/dev/null \
    || die "cannot download $archive anonymously.

$REPO is private, so its releases need a token. Any one of these is enough:
  - export KHUB_TOKEN=<a token with 'repo' scope>
  - gh auth login          (the GitHub CLI, then re-run this)
  - a GitHub HTTPS credential in your keychain

An SSH key does not work here — the GitHub API does not accept one."
  curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" \
    || die "cannot download checksums.txt"
else
  # Private assets are fetched by ID through the API with
  # `Accept: application/octet-stream`. Note plain -L, never
  # --location-trusted: the redirect crosses to a CDN host and the token has no
  # business being sent there.
  rel="repos/$REPO/releases/latest"
  [ -n "$VERSION" ] && rel="repos/$REPO/releases/tags/v$VERSION"

  # Mapping asset name -> id is the one step that needs to read JSON, and the
  # release listing is fetched ONCE for it rather than per asset.
  #
  # No gh branch here. gh earns its place as a TOKEN SOURCE above, but as a
  # reader it is curl with extra steps: by this point a token is in hand, so
  # `gh api` and `curl` do exactly the same work. Keeping it meant a third code
  # path, and that path is the one that shipped a bug — gh authenticates from
  # its own config or GH_TOKEN and cannot see a shell variable, so on any
  # machine with gh installed but not logged in (every GitHub Actions runner)
  # it failed and reported it as a missing asset.
  #
  # Checked HERE rather than inside asset_id: that runs in a command
  # substitution, so a `die` in it cannot exit the script — it would print the
  # real reason and then fall through to a misleading "release has no asset
  # named ..." from the caller.
  have jq || have python3 \
    || die "reading a private release needs jq or python3
(neither is needed once the repo is public — this whole path goes away then)"

  curl -fsSL -H "Authorization: Bearer $token" \
    -H "Accept: application/vnd.github+json" "$API_HOST/$rel" -o "$tmp/release.json" \
    || die "cannot read the release listing${VERSION:+ for v$VERSION}.
The token was rejected or the release does not exist. A token needs 'repo' scope
to read a private release."

  asset_id() { # asset_id <name>
    if have jq; then
      jq -r ".assets[] | select(.name==\"$1\") | .id" "$tmp/release.json"
    else
      python3 -c 'import json,sys
want = sys.argv[1]
with open(sys.argv[2]) as fh:
    for a in json.load(fh).get("assets", []):
        if a["name"] == want:
            print(a["id"]); break' "$1" "$tmp/release.json"
    fi
  }

  fetch_asset() { # fetch_asset <name> <dest>
    id=$(asset_id "$1" | tr -d '\r\n')
    [ -n "$id" ] || die "release has no asset named $1${VERSION:+ (v$VERSION)}"
    curl -fsSL -H "Authorization: Bearer $token" \
      -H "Accept: application/octet-stream" \
      "$API_HOST/repos/$REPO/releases/assets/$id" -o "$2" \
      || die "cannot download $1"
  }

  fetch_asset "$archive" "$tmp/$archive"
  fetch_asset "checksums.txt" "$tmp/checksums.txt"
fi

# --- verify -----------------------------------------------------------------
want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$archive is not listed in checksums.txt"

if have sha256sum; then
  got=$(sha256sum "$tmp/$archive" | awk '{print $1}')
elif have shasum; then
  got=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
else
  die "need sha256sum or shasum to verify the download"
fi

[ "$got" = "$want" ] || die "checksum mismatch for $archive
  expected $want
  got      $got
Refusing to install. Retry, and if it persists report it — the download may be
corrupted or tampered with."

# --- install ----------------------------------------------------------------
tar xzf "$tmp/$archive" -C "$tmp" khub || die "cannot unpack $archive"
mkdir -p "$INSTALL_DIR" || die "cannot create $INSTALL_DIR"
# Same-filesystem move where possible; cp covers /tmp being a different mount.
mv "$tmp/khub" "$INSTALL_DIR/khub" 2>/dev/null || {
  cp "$tmp/khub" "$INSTALL_DIR/khub" || die "cannot write $INSTALL_DIR/khub"
}
chmod +x "$INSTALL_DIR/khub"

printf 'installed khub %s to %s\n' "$("$INSTALL_DIR/khub" --version)" "$INSTALL_DIR"
# The literal $PATH below is the point: it is a line for the user to paste into
# their shell rc, not something to expand here.
# shellcheck disable=SC2016
case ":${PATH}:" in
  *":$INSTALL_DIR:"*) printf 'run: khub --help\n' ;;
  *) printf '\n%s is not on your PATH. Add it:\n  export PATH="%s:$PATH"\n' "$INSTALL_DIR" "$INSTALL_DIR" ;;
esac
