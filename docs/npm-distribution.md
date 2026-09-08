# Feature spec: npm distribution

Structure follows the four-section feature-spec shape (Summary, Scope, Behaviour,
Acceptance criteria) the build presets shipped as a template until build-hub
0.5.0 cut the delivery layer; this repo is not a khub workspace either, so the
spec lives as a plain doc.

## Summary

khub's distribution channel becomes npm. Each release publishes one package,
`@endgame-build/khub`, carrying every prebuilt static binary goreleaser already
produces (`binaries/{darwin,linux}-{x64,arm64}/khub`) behind a launcher that
picks the matching one at run time. A workspace repo pins its
khub with one exact `devDependencies` entry, so **one repo runs one khub
version** — the byte-level output contract (`internal/canon`) stops depending
on what each teammate happens to have installed. npm is the only way in: the
curl-based downloader and the hosted bootstrap that briefly replaced it are
both gone.

## Scope

**In:**

- The npm package and its launcher shim (`npm/` tree at the repo root),
  assembled from goreleaser's `dist/` by `npm/build-packages.sh`.
- Publishing to GitHub Packages (`npm.pkg.github.com`) from the release
  workflow on every `v*` tag, with an end-to-end install verify.
- Deleting `install.sh`, its hosting, and the `publish-install` workflow. A
  machine-global install is `npm install -g`.
- Consumer flows: first install, per-repo pin, upgrade, CI, agents,
  side-by-side versions.
- Documentation repositioning: README, getting-started, RELEASING, the design
  memo's Distribution section, and the `setup` skill.

**Out:**

- An in-binary version check against a workspace pin. Considered and cut:
  pinning rides `package.json` alone, so a stray pre-npm global binary is
  unprotected. Revisit if that failure mode shows up in practice.
- Windows. The binary matrix is darwin/linux × x64/arm64, unchanged. The
  launcher has no hardcoded platform list to refuse from — it reports what the
  package actually ships (`no binary for win32-x64. This build ships: …`), so
  the message stays true if the matrix ever changes.
- `khub init` writing a `package.json` into a workspace. Adding the dependency
  is a documented manual step; scaffolding it is a possible follow-up.
- Publishing to public npmjs.com. The design stays compatible (see
  *Going public* below) but the move happens when the repo does.
- Deleting the GitHub release assets. goreleaser keeps attaching archives and
  `checksums.txt` to the release; they feed the package build and remain a
  manual escape hatch, but no user-facing doc points at them.

## Behaviour

### Package layout

One package, one name. `package.json`'s `bin` is a single path, so a
dependency-free Node shim sits there: it maps `process.platform`/`process.arch`
to `binaries/<platform>-<arch>/khub` inside its own package and hands over with
`stdio: "inherit"`, forwarding the exit code and terminating signal. There are
**no lifecycle scripts**: installs run clean under `--ignore-scripts`, and
there is no postinstall to supply-chain-harden.

Platform selection happens at run time rather than install time. An earlier
draft split the binaries into four `optionalDependencies` with `os`/`cpu`
fields — npm's own mechanism, and what esbuild and swc do — so a machine
downloaded only its own 5.2 MB. Collapsed deliberately: the shim is required
either way (`bin` is one path), so the split bought a 4x bandwidth saving in
exchange for four extra published packages, a publish-ordering constraint, an
`--omit=optional` failure mode, and the platform set spelled in six places.
One package is 21 MB gzipped and none of that. The package version is —
the khub version, exactly, which is the git tag, which release CI already
asserts equals `internal/version.Version`.

Naming is constrained by the registry: GitHub Packages requires the scope to
equal the repo owner, hence `@endgame-build/khub`. Archive names inside
goreleaser keep khub's `amd64`/`arm64` spelling; npm package names use npm's
`x64`/`arm64`. `npm/build-packages.sh` holds that mapping as a table and
becomes the third consumer of the `khub_<os>_<arch>` naming contract alongside
goreleaser and the release verify. Adding or dropping a platform touches two
shipped files — that table and `.goreleaser.yml`'s build matrix — plus the CI
smoke, which lists the platforms as test fixtures. The launcher reads its
supported set off the `binaries/` directory, so it has no list to drift.

### Consumer flows

```bash
# Per-repo pin — the primary form. One khub version per repo, reviewed in git.
npm install -D @endgame-build/khub
npx khub --version

# One-off / machine-global
npx @endgame-build/khub@latest --version
npm install -g @endgame-build/khub                  # or @X.Y.Z to pin

# Upgrade a repo: bump one line in package.json, in a PR.
npm install -D @endgame-build/khub@X.Y.Z
```

The upgrade PR is the point of the design: if a release changes any on-disk
bytes, the churn lands in that one reviewed commit instead of leaking into
everyone's unrelated commits. Side-by-side versions need no machinery — each
repo's `node_modules` holds its own khub, and npm's cache dedupes downloads.

While the registry is private, a machine needs npm told where the scope lives
and how to authenticate — one config line plus a token with `read:packages`:

```
@endgame-build:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=<token>
```

Two commands write both, once per machine:

```bash
npm config set @endgame-build:registry https://npm.pkg.github.com
npm config set //npm.pkg.github.com/:_authToken "$(gh auth token)"
```

khub used to do this for you: `install.sh` sourced a token from
`KHUB_TOKEN`/`GITHUB_TOKEN`/`GH_TOKEN`, then `gh auth token`, then
`git credential fill`, and passed it through a temporary userconfig it deleted
on exit — deliberately never writing to `~/.npmrc`, because a one-liner piped
from the network must not mutate a file that holds credentials. Deleting the
script removes that constraint along with the convenience: a user typing
`npm config set` is consenting, which a remote script cannot do on their
behalf. An SSH key does not work here — the npm registry API does not accept
one. It *does* work for `npx skills add git@github.com:endgame-build/khub.git
-s setup`, which is how an agent bootstraps a machine that has no khub yet.

### Publishing (release workflow)

On a `v*` tag, after the existing gates and goreleaser:

1. `npm/build-packages.sh <version> dist/` unpacks the four archives,
   **verifies each against `checksums.txt`**, and assembles the package tree.
   It refuses `-snapshot` versions.
2. The workflow publishes that one package to
   `npm.pkg.github.com` using the built-in `GITHUB_TOKEN` (job permission
   `packages: write` — no new secret). Stable tags publish under the `latest`
   dist-tag; `-rc` tags under `next`, so a prerelease never becomes what a
   bare install resolves — the same concern the release verify's explicit
   `@<tag>` pin encodes for `prerelease: auto`.
3. End-to-end verify, through both real consumer paths: a global
   `npm install -g @endgame-build/khub@<version>` asserts `khub --version` and
   `khub --help`; a scratch per-repo `npm install` asserts `npx khub --version`
   too. The global step retries briefly — it is the first read after publish,
   and registry propagation is not instant.
4. Missing credentials or a failed verify fail the job. Never warn-and-pass.

CI (on PRs and main) never publishes; it lints the launcher and the assembly
script and smoke-runs the assembly against fixture tarballs.

### Positioning: why this is not "a second channel"

The design memo records removing the Claude Code plugin marketplace because it
was *a second, Claude-only distribution channel for the same files*. npm does
not reintroduce that shape: it **replaces** the curl downloader as the single
channel. An earlier draft kept the hosted script alive as a bootstrap over npm
for the machine that has nothing yet; that was deleted for the same reason the
marketplace was. A hosted script is a second thing to maintain, to secure, and
to keep truthful, and it earned none of that — it wrapped one command. The
GitHub release assets are build inputs, not a channel.

Deleting it also removed khub's largest stated attack surface. The script was
*designed* to locate a repo-scoped GitHub token from gh or the keychain, so a
replaced copy needed no malware to be catastrophic; that is why the security
boundary used to include whatever host served it. It no longer does.

What is genuinely given up: the old installer's zero-prerequisite property.
The primary consumers — agents in Node-bearing sandboxes and developer
machines — already have npm; a machine without Node gets pointed at Node
first. That trade buys exact per-repo pinning with standard, reviewable
mechanics (`package.json` + lockfile) that no bespoke wrapper matched.

### Going public

When the repo goes public, publishing moves (or dual-publishes) to public
npmjs.com and the `.npmrc`/token requirement disappears; `@endgame/khub` is
available as a rename at that point if wanted. One thing must be settled
first, as with the old channel: the package carries the binaries, and each
binary embeds the presets and skills — publishing publicly ships those
bytes. Same question, same moment, new spelling.

## Acceptance criteria

- `npm ci && npx khub --version` in a repo pinning version X prints X, on all
  four platforms; `npx khub schema | head` exits 0 (EPIPE passthrough).
- Two repos on one machine pinning different versions each resolve their own,
  with no interference.
- The published package contains no `preinstall`/`install`/`postinstall`
  scripts; install succeeds with `--ignore-scripts`, and `npx khub` runs the
  binary that landed.
- `npm/build-packages.sh` fails on a checksum mismatch and on `-snapshot`
  versions, and produces byte-identical binaries to the archives.
- The release job fails (not warns) when publishing or the install verify
  fails; an `-rc` tag never moves the `latest` dist-tag.
- `npm install -g @endgame-build/khub@X.Y.Z` on a machine with npm and a
  configured `~/.npmrc` puts `khub` on the PATH reporting version X.Y.Z.
- No shipped string, skill, or doc still instructs `uv tool install`, the curl
  download path, or a hosted script as an install method.

## Dependencies

- GitHub Packages enabled for the `endgame-build` org; consumers hold a token
  with `read:packages` (the classic-PAT `repo`-scoped tokens users already
  have for the old installer qualify).
- Node/npm present on consumer machines — the new baseline assumption.
- `internal/wire/wire.go`'s wired-block install line changes → a deliberate
  parity re-record of the affected wire fixtures (`.claude/rules/testing.md`
  record discipline applies).
- `skills/setup/SKILL.md` rewrite (also removes its drifted hardcoded version
  pin — RELEASING.md's "bump the skill on release" step dies with it).
- `.goreleaser.yml` archive naming stays load-bearing; `npm/build-packages.sh`
  joins the comment's list of consumers that must move with it, and its build
  matrix is one of the two shipped places the platform set is spelled (the
  other is the assembler's table; the CI smoke lists it too, as test data).
