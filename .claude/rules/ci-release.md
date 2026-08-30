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
  one carries the propagation retry because it reads first. Both run on the
  linux runner; `verify-macos` is a third verification, after publish rather
  than before it — see gap 3.
- **No hosted install script.** There was one, at `khub.end.game`, wrapping
  `npm install -g`. It was deleted rather than maintained: a script designed
  to locate a repo-scoped GitHub token is the highest-value thing an attacker
  could replace, and it bought one command's worth of convenience. Do not
  reintroduce a curl channel — document the command instead.
- **No Windows.** `gofrs/flock` does support LockFileEx — the library is not
  the blocker. The blockers: mandatory (not advisory) locks, rename-over-open-
  file breaking `fsio`'s atomic replace, and 117 byte-exact tree fixtures.

## Gaps worth closing (risk per effort, descending)

Closed items keep their reasoning rather than being deleted: the reason is what
stops one being reopened, or quietly undone.

1. **GitHub immutable releases** (repo setting) — the tj-actions tag-repoint
   class, applied to khub's own artifacts. **Still open, and not closable from
   a PR**: no such field is exposed on `gh api repos/endgame-build/khub` and
   repository properties are empty, so it is a toggle under Settings → General
   → Releases and nothing else.
2. ~~**`permissions: {}` root + per-job grants** in ci.yml.~~ **Closed.** Root
   grants nothing; `go` and `npm-package` each take `contents: read`, which is
   all either needs. release.yml keeps its root `contents: write` +
   `packages: write` — it is one job, so root already is per-job there, and
   re-scoping the release path breaks a release rather than a PR.
3. ~~**Run the release install check on macos-latest too**~~ **Closed, as a
   detector.** `verify-macos` in release.yml installs the published package on
   macos-latest and runs the binary. Two things to keep straight if it is ever
   edited: it must NOT sign before running (signing changes the artifact under
   test, so a green check would say nothing about the bytes that shipped), and
   `needs: release` means it cannot un-publish — a failure reports, it does not
   gate. Gating means reordering the workflow.

   Note what it does and does not prove. khub's darwin binaries are NOT
   unsigned: `.goreleaser.yml` has no `signs:` block, but Go's linker ad-hoc
   signs darwin/arm64 itself, cross-compiled included — `codesign -dv` on a
   `-trimpath -s -w` build reports `flags=0x20002(adhoc,linker-signed)`. So the
   kernel was never going to refuse it, and the job is not rescuing an unsigned
   artifact. What went unchecked until now is the rest of the chain on darwin:
   that the launcher resolves the right binary out of the package, and that the
   install works at all on the platform khub is actually used on.
4. ~~Pin `golangci-lint-action` `version:`~~ **Closed** — `v2.13.2` (#63).
   `version: latest` let a linter release turn the tree red with no commit of
   ours, and the choke-point rules are a gate.
5. ~~`govulncheck`, `go mod tidy -diff`, `actionlint`~~ **Closed**, ubuntu leg
   only — none is platform-dependent and the matrix exists to test locking,
   paths and terminal detection. Tool versions pinned per `dependencies.md`.
   **`zizmor` deliberately left out**, not forgotten: it overlaps actionlint
   across two workflow files, and it is not a Go tool — adding it puts a second
   toolchain in CI for a repo whose whole posture is one static Go binary.
   Revisit if the workflows grow.
6. `mod_timestamp: "{{ .CommitTimestamp }}"` — deterministic archives make an
   unexpected checksum change signal, not noise. **Still open.**

Note `go-version: "1.25"` floats across 1.25 PATCH releases on purpose. A stdlib
advisory is fixed by picking up the patch, so pinning it exactly would opt out
of exactly what govulncheck is there to notice — the opposite of the tool pins
above, and the distinction is deliberate.

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
