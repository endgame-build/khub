#!/bin/sh
# Assembly smoke for khub's npm package. Runs in CI on every PR, and locally.
#
#   npm/smoke.sh
#
# The real package is assembled from goreleaser artifacts on a tag
# (release.yml). This proves the assembler itself without a release: fake
# per-platform binaries in the same archive + checksums.txt shape goreleaser
# emits (a tar.gz holding khub, or for windows a zip holding khub.exe), then
# assert the package tree is sound, that a PACKED TARBALL carries what it
# should, and that the launcher runs what landed.
set -eu

die() { printf 'npm smoke: %s\n' "$*" >&2; exit 1; }
here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

if command -v sha256sum >/dev/null 2>&1; then
  sums() { sha256sum "$@"; }
elif command -v shasum >/dev/null 2>&1; then
  sums() { shasum -a 256 "$@"; }
else
  die "need sha256sum or shasum"
fi
command -v zip >/dev/null 2>&1 || die "need zip to build the windows fixture archives"

dist=$(mktemp -d)
work=$(mktemp -d)
trap 'rm -rf "$dist" "$work"' EXIT INT TERM

for a in darwin_amd64 darwin_arm64 linux_amd64 linux_arm64; do
  d="$work/src-$a"
  mkdir -p "$d"
  printf '#!/bin/sh\necho 9.9.9-fixture\n' >"$d/khub"
  chmod +x "$d/khub"
  tar czf "$dist/khub_$a.tar.gz" -C "$d" khub
done
# The windows binaries never run here; the fixture only has to be a khub.exe
# inside a zip, which is the layout the assembler and the shim depend on.
for a in windows_amd64 windows_arm64; do
  d="$work/src-$a"
  mkdir -p "$d"
  printf 'MZ fixture\n' >"$d/khub.exe"
  (cd "$d" && zip -q "$dist/khub_$a.zip" khub.exe)
done
(cd "$dist" && sums ./*.tar.gz ./*.zip | sed 's|\./||' >checksums.txt)

out="$work/pkgs"
"$here/build-packages.sh" 9.9.9 "$dist" "$out"

[ -f "$out/khub/package.json" ] || die "no package assembled"
node -e "JSON.parse(require('fs').readFileSync('$out/khub/package.json'))" \
  || die "package.json is not valid JSON"
grep -q '"version": "9.9.9"' "$out/khub/package.json" || die "version not substituted"

# Every platform's binary must be present and executable — npm ships them all
# and the launcher picks one at run time.
for p in darwin-x64 darwin-arm64 linux-x64 linux-arm64; do
  [ -x "$out/khub/binaries/$p/khub" ] || die "no binary for $p"
done
for p in win32-x64 win32-arm64; do
  [ -f "$out/khub/binaries/$p/khub.exe" ] || die "no binary for $p"
done

# No lifecycle scripts. Load-bearing supply-chain property
# a template edit that reintroduces one must fail
# here rather than ship.
node -e '
  const f = process.argv[1];
  const s = (JSON.parse(require("fs").readFileSync(f)).scripts) || {};
  for (const k of ["preinstall", "install", "postinstall"]) {
    if (s[k]) { console.error(f + " declares a " + k + " script"); process.exit(1); }
  }
' "$out/khub/package.json" || die "the package declares a lifecycle script"

# Pack, then install THE TARBALL — never the directory. `npm install <dir>`
# creates a symlinked file: dep, which bypasses package.json's `files` allowlist
# completely: drop binaries/ from `files` and a directory install still passes
# while the published package ships no binaries at all. The tarball is what a
# user actually receives.
proj="$work/proj"
mkdir -p "$proj"
(cd "$out/khub" && npm pack --pack-destination "$proj" >/dev/null) \
  || die "npm pack failed"
listing=$(tar tzf "$proj"/*.tgz)
shipped=$(printf '%s\n' "$listing" | grep -cE '^package/binaries/[^/]+/khub(\.exe)?$')
[ "$shipped" = 6 ] || die "tarball carries $shipped binaries, expected 6 — check \`files\`"
for p in win32-x64 win32-arm64; do
  printf '%s\n' "$listing" | grep -qx "package/binaries/$p/khub.exe" \
    || die "tarball carries no binaries/$p/khub.exe"
done

# --no-install so a missing local bin fails here instead of reaching the registry.
(cd "$proj" \
  && npm init -y >/dev/null \
  && npm install --ignore-scripts --no-audit --no-fund ./*.tgz >/dev/null \
  && [ "$(npx --no-install @endgame-build/khub)" = "9.9.9-fixture" ]) \
  || die "the packed package did not install and run under --ignore-scripts"

# A tampered archive must be refused, zip or tarball. Each is tampered alone,
# so neither refusal can be the other's.
cp "$dist/khub_windows_arm64.zip" "$work/zip.orig"
printf 'tamper' >>"$dist/khub_windows_arm64.zip"
if "$here/build-packages.sh" 9.9.9 "$dist" "$work/x" 2>/dev/null; then
  die "checksum mismatch on a zip was not detected"
fi
cp "$work/zip.orig" "$dist/khub_windows_arm64.zip"
printf 'tamper' >>"$dist/khub_linux_amd64.tar.gz"
if "$here/build-packages.sh" 9.9.9 "$dist" "$work/x" 2>/dev/null; then
  die "checksum mismatch was not detected"
fi

# So must a snapshot version.
if "$here/build-packages.sh" 9.9.9-snapshot "$dist" "$work/y" 2>/dev/null; then
  die "snapshot version was not refused"
fi

printf 'npm smoke: ok — packed tarball carries 6 binaries, installs, and runs\n'
