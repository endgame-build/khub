# CI, release, supply chain

khub ships a static binary from a **private** repo via `curl … | sh`. That
shape drives everything; much mainstream supply-chain advice does not apply.

## Odd-looking decisions that are correct — do not "fix"

Each is commented where it lives:

- **Version-free asset names** — what makes `/releases/latest/download/<asset>`
  work with plain curl the day the repo goes public.
- **`prerelease: auto` + `KHUB_VERSION` pin in release verify** — an `-rc` tag
  leaves `latest` at the previous stable; the pin keeps the assert honest.
- **Plain `-L`, never `--location-trusted`** — strips `Authorization` on
  cross-host redirect (CVE-2022-27776).
- **Release re-runs the full gate suite on the tag** — a tag is not proof CI
  ran on that commit.
- **Release installs through `install.sh` itself**, not a reimplementation.
- **publish-install hard-fails on missing Cloudflare creds**; warn-and-pass is
  how stale live scripts happen.
- **`cancel-in-progress: false` on publish**; CI never diffs install.sh
  against the live copy (a PR is *supposed* to differ until merge).
- **No Windows.** `gofrs/flock` does support LockFileEx — the library is not
  the blocker. The blockers: mandatory (not advisory) locks, rename-over-open-
  file breaking `fsio`'s atomic replace, and 117 byte-exact tree fixtures.

## Gaps worth closing (risk per effort, descending)

1. **Pin `wrangler` exactly + `--ignore-scripts`** — `npx --yes wrangler@4`
   resolves a whole npm tree with lifecycle scripts in the job holding
   `CLOUDFLARE_API_TOKEN`. The largest unpinned dependency in the repo.
2. **GitHub immutable releases** (repo setting) — the tj-actions tag-repoint
   class, applied to khub's own artifacts.
3. **`permissions: {}` root + per-job grants** in ci.yml/publish-install.yml.
4. **`main() { … }; main "$@"` wrapper in install.sh** — truncated download
   cannot half-execute (rustup/ollama pattern).
5. **Run the release install check on macos-latest too** — today the primary
   platform's artifact is never executed before shipping; darwin/arm64 needs a
   valid (ad-hoc) signature or the kernel SIGKILLs.
6. Pin `golangci-lint-action` `version:` (breaks on Go-minor skew).
7. `govulncheck`, `go mod tidy -diff`, `actionlint` + `zizmor` in CI.
8. `mod_timestamp: "{{ .CommitTimestamp }}"` — deterministic archives make an
   unexpected checksum change signal, not noise.

## Rejected while private

SLSA/attestations (needs GHEC for private repos) · cosign keyless (public
Rekor entry names the private repo) · SBOM (govulncheck + go.sum is better,
with a consumer) · Scorecard/CodeQL/dependency-review (public-repo or GHAS-
gated) · `universal_binaries` (breaks asset naming) · Homebrew tap (can't
fetch private assets) · macOS notarization (curl sets no quarantine xattr and
tar doesn't propagate it — Gatekeeper never fires on this channel; revisit for
.pkg/DMG/browser download) · per-OS release fan-out (exists for signing
toolchains khub doesn't use).

## Threat model, plainly

install.sh is *designed* to locate a repo-scoped GitHub token (gh/keychain).
A replaced script needs no malware — it exfiltrates the token; blast radius is
every private repo the developer can read. So the security boundary is the
Cloudflare Pages deployment and push access to `main`, not the binary.
`checksums.txt` from the same channel detects corruption, **not** compromise —
never describe it as integrity; only a signature with a key baked into the
installer closes that.
