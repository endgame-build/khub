# Releasing khub

A release is a version bump, a green suite, and a tag on `main`. Pushing the tag
builds four binaries and publishes them; nothing is uploaded by hand.

1. Bump `Version` in `internal/version/version.go` and add a section to
   `CHANGELOG.md` (Keep a Changelog format). That constant is the single source
   of truth — the release build injects the tag over it with `-ldflags`, and CI
   fails the release if the two disagree.
2. Re-record the two fixtures that pin the version string. `khub --version` is
   part of the CLI contract, so bumping the constant in step 1 turns them red
   by construction — this is the one re-record a release always needs, and it
   is deliberate:
   ```bash
   go build -o khub ./cmd/khub && go build -o parity-run ./parity/runner
   ./parity-run -bin "$PWD/khub" -record -only cli-contract/version
   ./parity-run -bin "$PWD/khub" -record -only cli-contract/version-eager
   ```
   Three lines should move, each the version string. Anything else means the
   bump touched something it should not have.
3. Run the gates. All of them must pass before you tag:
   ```bash
   go test ./...
   ./parity-run -bin "$PWD/khub" -cases parity/cases
   ./parity-run -coverage parity/coverage.yaml -cases parity/cases -subset-of "$PWD/khub"
   bash smoke.sh
   npm/smoke.sh
   ```
4. Commit on a branch, then merge to `main` (fast-forward or PR).
5. Tag the release commit on `main`: `git tag -a vX.Y.Z -m "khub vX.Y.Z: <summary>"`.
6. Push both: `git push origin main && git push origin vX.Y.Z`.

The tag triggers `.github/workflows/release.yml`, which re-runs the gates,
builds darwin and linux on amd64 and arm64 with goreleaser, attaches the
archives and `checksums.txt` to the GitHub release, then assembles and
publishes the npm package (`@endgame-build/khub`, carrying all four binaries)
to npmjs.org with provenance — a stable tag under the `latest` dist-tag, a
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

## The channel

The package is public on npmjs.org while the repository is still private —
the published artifact is the binary, not the source. Releases publish through
[npm trusted publishing](https://docs.npmjs.com/trusted-publishers): the job
proves its identity with a GitHub OIDC token (`id-token: write`), so no
publish token is stored anywhere.

The `endgame-build` organization owns the scope. One thing is still set up by
hand, once: **a trusted publisher on the `@endgame-build/khub` package**
(Settings → Trusted Publisher → GitHub Actions, owner `endgame-build`,
repository `khub`, workflow `release.yml`). npm only offers that setting on a
package that already exists, so the **first** publish comes from a
maintainer's machine — `npm publish --access public ./npm/dist/khub` after the
dry run below — and every later release goes through CI.

**`--provenance` is deliberately absent** from the publish step. npm will not
attest a private source repository, and the attestation lands in a public
transparency log. Add the flag to `release.yml` when the repository goes
public; nothing else about the channel changes.

Existing consumers that mapped the `@endgame-build` scope to GitHub Packages
in `~/.npmrc` remove that mapping; the bare `npm install -D @endgame-build/khub`
is the whole install.

`go install` and a plain `curl` of a release archive both need the repository
to be public, so they are not documented as install paths yet.

Archive names still carry no version (`khub_darwin_arm64.tar.gz`):
`npm/build-packages.sh` derives them from the same template as
`.goreleaser.yml`, so the two have to move together.

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
npx --no-install @endgame-build/khub --version  # package.json's `files` allowlist entirely
```

`npm/smoke.sh` does all of that against fixture tarballs and needs no
goreleaser run — CI runs it on every PR, and it is the faster check when you
have only changed the packaging. Release CI exercises the real artifacts.

