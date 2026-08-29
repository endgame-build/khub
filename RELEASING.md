# Releasing khub

A release is a version bump, a green suite, and a tag on `main`. Pushing the tag
builds four binaries and publishes them; nothing is uploaded by hand.

1. Bump `Version` in `internal/version/version.go` and add a section to
   `CHANGELOG.md` (Keep a Changelog format). That constant is the single source
   of truth — the release build injects the tag over it with `-ldflags`, and CI
   fails the release if the two disagree.
2. Run the gates. All of them must pass before you tag:
   ```bash
   go test ./...
   go build -o khub ./cmd/khub && go build -o parity-run ./parity/runner
   ./parity-run -bin "$PWD/khub" -cases parity/cases
   ./parity-run -coverage parity/coverage.yaml -cases parity/cases -subset-of "$PWD/khub"
   bash smoke.sh
   ```
3. Commit on a branch, then merge to `main` (fast-forward or PR).
4. Tag the release commit on `main`: `git tag -a vX.Y.Z -m "khub vX.Y.Z: <summary>"`.
5. Push both: `git push origin main && git push origin vX.Y.Z`.

The tag triggers `.github/workflows/release.yml`, which re-runs the gates,
builds darwin and linux on amd64 and arm64 with goreleaser, attaches the
archives and `checksums.txt` to the GitHub release, then assembles and
publishes the npm package (`@endgame-build/khub`, carrying all four binaries)
to GitHub Packages — a stable tag under the `latest` dist-tag, a
hyphenated tag (`v0.20.0-rc1`) under `next`, so a prerelease never becomes
what a bare install resolves. It then proves the channel through both real
consumer paths — a machine-global `npm install -g` and a scratch per-repo
`npm install`, each pinned to the tag and each asserting the installed khub
reports it. The global step also runs `khub --help`, and retries briefly:
it is the first read after publish, and registry propagation is not instant.

Consumers install with:

```bash
npm install -D @endgame-build/khub                # per-repo pin — the primary form
npm install -D @endgame-build/khub@X.Y.Z          # bump/downgrade a repo, in a PR
npm install -g @endgame-build/khub                # machine-global
```

## How the private repo serves the channel

Nothing about khub is public. The packages live on GitHub Packages, which
requires a token with `read:packages` while the repo is private, so every
consumer configures npm once:

```bash
npm config set @endgame-build:registry https://npm.pkg.github.com
npm config set //npm.pkg.github.com/:_authToken "$(gh auth token)"
```

There used to be a hosted `install.sh` that sourced that token from the
machine automatically. It was deleted: a script whose job is to find a
repo-scoped GitHub token is the most valuable thing an attacker could replace,
and it existed to wrap one command. An SSH key does **not** work here either:
it authenticates git-over-SSH, and the npm registry ignores it.

Archive names still carry no version (`khub_darwin_arm64.tar.gz`):
`npm/build-packages.sh` derives them the same way the retired downloader
did, so the template in `.goreleaser.yml` and that script have to move
together.

## Dry run

`goreleaser release --snapshot --clean --skip=publish` builds all four archives
into `dist/` without touching git or GitHub. To exercise the npm packaging
against them (the assembler refuses `-snapshot`, so name a real-looking
version), assemble, pack, and install locally:

```bash
npm/build-packages.sh X.Y.Z dist /tmp/khub-pkgs
cd "$(mktemp -d)" && npm init -y >/dev/null
npm pack /tmp/khub-pkgs/khub            # tarball, not the dir: `npm install <dir>`
npm install ./*.tgz                     # symlinks a file: dep and so bypasses
npx --no-install khub --version         # package.json's `files` allowlist entirely
```

`npm/smoke.sh` does all of that against fixture tarballs and needs no
goreleaser run — CI runs it on every PR, and it is the faster check when you
have only changed the packaging. Release CI exercises the real artifacts.

## When khub goes open source

1. Flip repository visibility to public. GitHub Packages under a public repo
   serves reads without a token, so existing `.npmrc` registry mappings keep
   working and the token line stops being needed.
2. Decide whether to also publish to public npmjs.com (where `@endgame/khub`
   is claimable as a rename) — a wider audience and no registry mapping at
   all, at the cost of a second publish target.
3. Docs — drop the `npm config set` / token setup from the install
   instructions; `npm install -D @endgame-build/khub` starts working bare.

Before flipping, settle one thing: public packages mean the binaries are
publicly downloadable, and a binary carries the embedded `firm-ops` and
`build-hub` presets and the skill files. The source and those bytes become
public together.
