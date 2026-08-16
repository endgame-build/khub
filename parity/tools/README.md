# parity/tools

What survived the Go cutover, and why.

khub is a Go binary; **nothing in it needs Python or uv.** The scripts here are
standalone dev tools that regenerate corpora, and they run only when you extend
one — not on any build, test, or release path.

Run them with whatever Python you have. Two need `ruamel.yaml`; a throwaway
environment is the easiest way to get it, e.g.
`uv run --with ruamel.yaml python parity/tools/gen_emit_cases.py`, but
`pip install ruamel.yaml` in any venv works identically.

## Still live

| Tool | What it does | How to run |
|---|---|---|
| `gen_emit_cases.py` | Regenerates `parity/corpus/emit-cases.jsonl`, the T3 emitter differential | needs `ruamel.yaml` |
| `gen_load_cases.py` | Regenerates the YAML 1.1-vs-1.2 scalar resolution corpus | needs `ruamel.yaml` |
| `gen_json_cases.py` | Regenerates the JSON-dialect corpus | stdlib only |
| `gen_mdparse_cases.py` | Regenerates the frontmatter-split corpus | stdlib only |
| `concurrency.py` | N processes writing one collection at once — exercises the flock path a byte-diff cannot see | takes the binary path as an argument |
| `seed_adversarial.py` | Plants hostile fixtures (exotic scalars, CRLF, comments) into a workspace for fuzz runs | takes a workspace path |

The four generators keep earning their place because **khub's on-disk format is
still ruamel-shaped**. That is a property of the file format, not a leftover of
the Python implementation: entities written by khub must stay readable and
diff-stable for anything else that touches them, so `internal/canon` reproduces
ruamel's emitter byte-for-byte and these corpora are what pin it.

## Retired at cutover

`pykhub.sh`, `record.sh`, `smoke-diff.sh`, `pyshim/`, `bench.py`,
`gen_corpus.py`, `gen_coverage.py`, `gen_values_cases.py` — every one of them
either drove the Python CLI or imported `khub.*`, so none can run now. They are
recoverable from git history at the cutover commit if a question about how a
corpus was originally derived ever comes up.

The recorded fixtures under `parity/cases/` do **not** depend on any of this.
They are bytes on disk, and `parity/runner` verifies the Go binary against them
with nothing else installed.
