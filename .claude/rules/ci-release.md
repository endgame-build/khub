# CI, release, supply chain

khub ships a static binary from a **private** repo through npm: one package
on GitHub Packages carrying every platform's binary, assembled from
goreleaser's artifacts by `npm/build-packages.sh` and published by release CI
(`docs/npm-distribution.md` is the spec). npm is the **only** channel — there
is no hosted install script and no Cloudflare. That shape drives everything;
much mainstream supply-chain advice does not apply.

## Odd-looking decisions that are correct — do not "fix"

Each is commented where it lives:

- **Version-free asset names** — `npm/build-packages.sh` derives them from the
  goreleaser template exactly as the retired downloader did; the two move
  together, and the shape still makes `/releases/latest/download/<asset>` work
  with plain curl the day the repo goes public.
- **`prerelease: auto` + dist-tag split in release** — an `-rc` tag leaves the
  GitHub release's `latest` at the previous stable, and publishes to npm under
  the `next` dist-tag for the same reason; the verify installs `@<tag>`
  explicitly so the assert stays honest on prereleases.
- **No lifecycle scripts in the npm package** — installs must survive
  `--ignore-scripts`, and a postinstall is exactly the surface npm
  supply-chain attacks use. The launcher resolves the platform binary at run
  time instead, out of the package's own `binaries/` directory.
- **One package carrying all four binaries, not four `optionalDependencies`** —
  the split is npm's own mechanism and saves 4x on bandwidth (5.2 MB vs 21 MB
  gzipped), but a launcher shim is needed either way because `bin` is a single
  path. It was collapsed for the four fewer published packages, no
  publish-ordering constraint, no `--omit=optional` failure mode, and the
  platform set spelled in two shipped files instead of six. Re-split only if
  the cold fetch cost is ever measured to matter.
- **The assembly smoke packs a tarball and installs THAT, never the directory** —
  `npm install <dir>` symlinks a `file:` dep and bypasses `files` entirely, so a
  directory install stays green while the published package ships no binaries.
  The smoke also asserts the tarball's binary count.
- **Release re-runs the full gate suite on the tag** — a tag is not proof CI
  ran on that commit.
- **Release verifies both consumer paths** — a global `npm install -g` pinned
  to the tag (which also runs `--help`, the only time a shipped binary is
  executed before release) and a scratch per-repo `npm install`. The global
  one carries the propagation retry because it reads first.
- **No hosted install script.** There was one, at `khub.end.game`, wrapping
  `npm install -g`. It was deleted rather than maintained: a script designed
  to locate a repo-scoped GitHub token is the highest-value thing an attacker
  could replace, and it bought one command's worth of convenience. Do not
  reintroduce a curl channel — document the command instead.
- **No Windows.** `gofrs/flock` does support LockFileEx — the library is not
  the blocker. The blockers: mandatory (not advisory) locks, rename-over-open-
  file breaking `fsio`'s atomic replace, and 117 byte-exact tree fixtures.

## Gaps worth closing (risk per effort, descending)

1. **GitHub immutable releases** (repo setting) — the tj-actions tag-repoint
   class, applied to khub's own artifacts.
2. **`permissions: {}` root + per-job grants** in ci.yml.
3. **Run the release install check on macos-latest too** — today the primary
   platform's artifact is never executed before shipping (the npm verify runs
   on the linux runner); darwin/arm64 needs a valid (ad-hoc) signature or the
   kernel SIGKILLs.
4. Pin `golangci-lint-action` `version:` (breaks on Go-minor skew).
5. `govulncheck`, `go mod tidy -diff`, `actionlint` + `zizmor` in CI.
6. `mod_timestamp: "{{ .CommitTimestamp }}"` — deterministic archives make an
   unexpected checksum change signal, not noise.

## Rejected while private

SLSA/attestations (needs GHEC for private repos) · cosign keyless (public
Rekor entry names the private repo) · SBOM (govulncheck + go.sum is better,
with a consumer) · Scorecard/CodeQL/dependency-review (public-repo or GHAS-
gated) · `universal_binaries` (breaks asset naming) · Homebrew tap (can't
fetch private assets) · macOS notarization (npm extracts the tarball itself
and sets no quarantine xattr, so Gatekeeper never fires on this channel;
revisit for .pkg/DMG/browser download) · per-OS release fan-out (exists for signing
toolchains khub doesn't use).

## Threat model, plainly

The security boundary is push access to `main` and `packages: write` on this
repo (what can publish to the npm scope) — not the binary. It used to also
include a Cloudflare Pages deployment, because `install.sh` was *designed* to
locate a repo-scoped GitHub token from gh or the keychain: a replaced script
needed no malware, it just exfiltrated the token, and the blast radius was
every private repo the developer could read. Deleting the script removed that
boundary outright, which is the main reason it went.
`checksums.txt` gates `npm/build-packages.sh` in the same workflow run that
built the archives, so it detects corruption in that hop, **not** compromise —
never describe it as integrity. npm's own tarball hashes pin what a lockfile
saw first, which is real but different: it detects a package changing after
first install, not a malicious publish. Only a signature with an
out-of-channel key closes that.
