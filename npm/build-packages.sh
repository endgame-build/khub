#!/bin/sh
# Assemble khub's npm package from a goreleaser dist/ directory.
#
#   npm/build-packages.sh <version> <dist-dir> [<out-dir>]
#
# Produces one publishable package tree at <out-dir>/khub (default npm/dist),
# carrying every platform binary under binaries/<platform>-<arch>/khub (khub.exe
# for win32) plus the launcher shim that picks one at run time. Every archive,
# zip or tarball, is verified against dist's checksums.txt before it is unpacked
# — the same posture the retired downloader took at install time, moved to
# package-build time.
#
# npm names platforms x64/arm64 where goreleaser (and the archive naming
# contract in .goreleaser.yml) says amd64/arm64; the table at the bottom of
# this script is where that mapping lives. Windows archives are zips holding
# khub.exe, every other platform a tar.gz holding khub — the same split as
# .goreleaser.yml's format_overrides. Adding or dropping a platform is a
# two-file edit: that table and .goreleaser.yml's build matrix. The launcher
# reads its supported set off the binaries/ directory, so it needs no edit.
set -eu

die() { printf 'build-packages: %s\n' "$*" >&2; exit 1; }

[ $# -ge 2 ] || die "usage: build-packages.sh <version> <dist-dir> [<out-dir>]"
version="${1#v}"
dist="$2"
out="${3:-npm/dist}"

case "$version" in
  *-snapshot*) die "refusing to package snapshot version $version" ;;
  '') die "empty version" ;;
esac

# Resolve the template locations relative to this script, so the script works
# from any cwd (CI runs it from the repo root; the fixture smoke does not).
src=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(dirname -- "$src")

checksums="$dist/checksums.txt"
[ -f "$checksums" ] || die "no checksums.txt in $dist"

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  die "need sha256sum or shasum"
fi
command -v unzip >/dev/null 2>&1 || die "need unzip for the windows archives"

rm -rf "$out"
pkg="$out/khub"
mkdir -p "$pkg/bin" "$pkg/binaries"
tmp=$(mktemp -d) || die "cannot create a temp directory"
trap 'rm -rf "$tmp"' EXIT INT TERM

# --- binaries ----------------------------------------------------------------
# Columns: npm platform, npm arch, goreleaser GOOS, goreleaser GOARCH.
while read -r nplat narch gos garch; do
  case "$gos" in
    windows) archive="khub_${gos}_${garch}.zip" exe=khub.exe ;;
    *) archive="khub_${gos}_${garch}.tar.gz" exe=khub ;;
  esac
  [ -f "$dist/$archive" ] || die "missing $archive in $dist"

  want=$(awk -v f="$archive" '$2 == f || $2 == "*" f { print $1 }' "$checksums")
  [ -n "$want" ] || die "$archive is not listed in checksums.txt"
  got=$(sha256 "$dist/$archive")
  [ "$got" = "$want" ] || die "checksum mismatch for $archive
  expected $want
  got      $got"

  slot="$pkg/binaries/${nplat}-${narch}"
  mkdir -p "$slot"
  rm -rf "$tmp/x" && mkdir "$tmp/x"
  case "$archive" in
    *.zip) unzip -q "$dist/$archive" "$exe" -d "$tmp/x" || die "cannot unpack $archive" ;;
    *) tar xzf "$dist/$archive" -C "$tmp/x" "$exe" || die "cannot unpack $archive" ;;
  esac
  [ -f "$tmp/x/$exe" ] || die "$archive holds no $exe"
  mv "$tmp/x/$exe" "$slot/$exe"
  chmod +x "$slot/$exe"
done <<TABLE
darwin x64 darwin amd64
darwin arm64 darwin arm64
linux x64 linux amd64
linux arm64 linux arm64
win32 x64 windows amd64
win32 arm64 windows arm64
TABLE

# --- package -----------------------------------------------------------------
cp "$src/khub/bin/khub.js" "$pkg/bin/khub.js"
chmod +x "$pkg/bin/khub.js"
sed -e "s/__VERSION__/$version/g" "$src/khub/package.json.tmpl" >"$pkg/package.json"
cp "$repo_root/LICENSE" "$pkg/LICENSE"
cp "$repo_root/README.md" "$pkg/README.md"

count=$(find "$pkg/binaries" -type f \( -name khub -o -name khub.exe \) | wc -l | tr -d ' ')
printf 'assembled 1 package (%s binaries) for %s in %s\n' "$count" "$version" "$out"
