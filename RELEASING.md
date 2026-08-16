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

The tag triggers `.github/workflows/release.yml`, which re-runs the gates, builds
darwin and linux on amd64 and arm64 with goreleaser, attaches the archives and
`checksums.txt` to the GitHub release, and then **installs through `install.sh`
itself** — the same script a user runs — asserting the installed binary reports
the tag.

Consumers install with:

```bash
curl -fsSL https://khub.end.game/install.sh | sh                     # latest
KHUB_VERSION=X.Y.Z curl -fsSL https://khub.end.game/install.sh | sh  # pinned
```

## How the private repo still serves a public one-liner

Only `install.sh` is public. The binaries stay in this repo's releases, which
404 to anyone without a token. The script supplies that token from whatever the
machine already has — `KHUB_TOKEN`/`GITHUB_TOKEN`/`GH_TOKEN`, then
`gh auth token`, then `git credential fill` against the OS keychain — so the
install command carries no credential. An SSH key does **not** work: it
authenticates git-over-SSH, and the REST API ignores it.

Archive names carry no version (`khub_darwin_arm64.tar.gz`). That is what makes
GitHub's anonymous `releases/latest/download/<asset>` URL usable the day this
repo goes public, with no API call and no JSON parser.

### Publishing install.sh

Automatic. `.github/workflows/publish-install.yml` deploys `install.sh` to
Cloudflare Pages on every merge to `main` that touches it, then fetches
`https://khub.end.game/install.sh` and diffs it against the commit to confirm
the deploy actually landed (retrying, since propagation is not instant). It also
publishes the file as the apex `index.html`, so `curl -fsSL https://khub.end.game | sh`
works as well.

This repo is the single source of truth and that workflow is the only writer, so
the live copy cannot drift. Nothing is uploaded by hand.

Needs two repository secrets — `CLOUDFLARE_API_TOKEN` and
`CLOUDFLARE_ACCOUNT_ID` — and optionally the `CLOUDFLARE_PAGES_PROJECT`
variable if the Pages project is not named `khub`. Without them the job warns
and skips rather than failing, so the repo works before Cloudflare is wired up.
Re-publish after a token rotation with **Run workflow** on that workflow; no
commit needed.

Note that CI does *not* diff a PR's `install.sh` against the live copy: a PR
that edits the file is supposed to differ until it merges. CI lints it;
publishing verifies it.

## Dry run

`goreleaser release --snapshot --clean --skip=publish` builds all four archives
into `dist/` without touching git or GitHub. To exercise the installer against
them, serve a directory that mirrors GitHub's URL shape and point the script at
it:

```bash
mkdir -p /tmp/m/endgame-build/khub/releases/latest/download
cp dist/*.tar.gz dist/checksums.txt /tmp/m/endgame-build/khub/releases/latest/download/
(cd /tmp/m && python3 -m http.server 8772 &)
KHUB_BASE_URL=http://localhost:8772 KHUB_INSTALL_DIR=/tmp/khub-test sh install.sh
```

`KHUB_API_URL` is the equivalent seam for the token path.

## When khub goes open source

This is what the installer's two-path design exists for:

1. Flip repository visibility to public.
2. `install.sh` — **no change.** With no token it already uses the anonymous
   path; it simply stops needing one.
3. Docs — drop the token fallbacks. `curl -fsSL https://khub.end.game/install.sh | sh`
   was already the primary and keeps working untouched.
4. Optional cleanup: delete the token branch from `install.sh` along with its
   `gh`/`jq`/`python3` requirement.

**The install command never changes.** Going public removes a dependency; it
does not migrate anyone.

Before flipping, settle one thing: a public release means the binaries are
publicly downloadable, and a binary carries the embedded `firm-ops` and
`build-hub` presets and the skill files. The source and those bytes become
public together.

The `setup` skill pins a version in `skills/setup/SKILL.md`; bump it there when
you cut a release so an agent following that skill installs the matching one.
