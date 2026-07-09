## What & why

<!-- One or two sentences. Link the issue if there is one. -->

## Checklist

- [ ] `uv run pytest` passes
- [ ] `uv run ruff check src tests` is clean
- [ ] `uv run mypy` is clean
- [ ] `CHANGELOG.md` updated (if user-facing)
- [ ] Docs updated (if the command surface or schema changed)
- [ ] Surfaces stay schema-generic — no per-type branching in `cli/`; logic lives in `core/`
