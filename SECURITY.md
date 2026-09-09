# Security

## Reporting a vulnerability

Use GitHub's private vulnerability reporting on this repository (**Security →
Report a vulnerability**). If it is not enabled, open a draft security advisory
or contact the maintainer named in `.github/CODEOWNERS` directly. Do not open a
public issue for anything you believe is exploitable.

Include the khub version (`khub --version`), the platform, and the smallest
workspace or command sequence that shows the problem. A report is acknowledged
when it is read, and you hear back once it is triaged, with a fix and a
release when one is warranted.

## Supported versions

The latest minor release receives fixes. khub is pre-1.0; there are no
maintenance branches.

## What khub does and does not do

Knowing the shape of the binary makes reports faster to triage.

- **No outbound network.** khub never transmits anything. `khub serve` binds
  `127.0.0.1` only, answers `GET`/`HEAD` only, and validates the `Host` header
  (the guard that matters against DNS rebinding — the loopback bind alone would
  not be). There is no CORS header, on purpose.
- **Writes are confined and atomic.** Every mutation resolves paths under the
  workspace root with `os.Root`, refuses symlink components, workspace escape
  and reserved paths, and publishes through a unique sibling temporary under a
  single workspace lock.
- **Untrusted content drives writes.** Entity bodies and everything `get` and
  `search` return are attacker-influenceable text. The containment is
  provenance plus the `draft` flag, with `khub check` as the human review
  point; there is no execution of anything read from a workspace.
- **Schema patterns are author-supplied regular expressions** evaluated with a
  one-second match timeout, so a hostile pattern stalls one call, not the
  process.
- **The npm package has no lifecycle scripts** and installs with
  `--ignore-scripts`; the launcher picks a prebuilt binary out of the package
  itself.

Anything that breaks one of those statements is in scope.
