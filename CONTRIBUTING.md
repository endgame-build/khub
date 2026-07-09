# Contributing to khub

Thanks for helping improve khub. The bar is simple: keep the engine schema-generic and the tests green.

## Dev setup

khub uses [uv](https://docs.astral.sh/uv/) and targets Python 3.11+.

```bash
uv sync            # install the package and dev dependencies
uv run khub --help
```

## The gates

Run all three before you open a PR — CI runs the same on every push and pull request:

```bash
uv run pytest              # full suite (markers: unit | integration | e2e)
uv run ruff check src tests
uv run mypy                # strict; checks src/khub
```

Run one test with `uv run pytest tests/test_query.py::test_name`.

## What review looks for

- **Schema-generic surfaces.** All logic lives in `core/`; the CLI and every other surface are thin, schema-introspecting adapters with zero per-type code. Branching on a type name inside a surface is the bug — push it into the schema or the generic core path.
- **YAML is always `ruamel.yaml`,** never PyYAML. See [`.claude/rules/yaml.md`](.claude/rules/yaml.md).
- **`validate` and `check` stay distinct** — per-entity well-formedness versus the graph-wide gate. Capture is never blocked on write.
- Read [`docs/design-memo.md`](docs/design-memo.md) for the invariants before non-trivial work.

## Commits and releases

Keep commits scoped and their messages descriptive. Update [`CHANGELOG.md`](CHANGELOG.md) (Keep a Changelog format) for anything user-facing. Releases are tag-based — see [`RELEASING.md`](RELEASING.md).
