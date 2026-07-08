# Releasing khub

khub ships from git tags (no PyPI while the repo is private). A release is a version bump, a green test run, and a tag on `main`.

1. Bump `version` in `pyproject.toml` and add a section to `CHANGELOG.md` (Keep a Changelog format).
2. Run the full suite: `uv run pytest`. It must pass before you tag.
3. Commit on a branch, then merge to `main` (fast-forward or PR).
4. Tag the release commit on `main`: `git tag -a vX.Y.Z -m "khub vX.Y.Z: <summary>"`.
5. Push both: `git push origin main && git push origin vX.Y.Z`.

Consumers install a pinned tag over git (SSH key, or HTTPS with a token):

```bash
uv tool install git+ssh://git@github.com/endgame-build/knowledge-hub@vX.Y.Z
uvx --from git+ssh://git@github.com/endgame-build/knowledge-hub@vX.Y.Z khub init firm-ops ./my-hub
```

The khub Claude Code plugin pins the same tag in `plugin/skills/setup/SKILL.md`; bump it there when you cut a release so `/khub:setup` installs the matching version.
