# parity/tools

Standalone dev tools that regenerate corpora. They run only when you extend a
corpus — never on a build, test, or release path. khub itself needs no Python.

Run them with whatever Python you have. Two need `ruamel.yaml`; a throwaway
environment is the easiest way to get it, e.g.
`uv run --with ruamel.yaml python parity/tools/gen_emit_cases.py`, but
`pip install ruamel.yaml` in any venv works identically.

| Tool | What it does | How to run |
|---|---|---|
| `gen_emit_cases.py` | Regenerates `parity/corpus/emit-cases.jsonl`, the T3 emitter differential | needs `ruamel.yaml` |
| `gen_load_cases.py` | Regenerates the YAML 1.1-vs-1.2 scalar resolution corpus | needs `ruamel.yaml` |
| `gen_json_cases.py` | Regenerates the JSON-dialect corpus | stdlib only |
| `gen_mdparse_cases.py` | Regenerates the frontmatter-split corpus | stdlib only |
| `concurrency.py` | N processes writing one collection at once — exercises the flock path a byte-diff cannot see | takes the binary path as an argument |
| `seed_adversarial.py` | Plants hostile fixtures (exotic scalars, CRLF, comments) into a workspace for fuzz runs | takes a workspace path |
| `gen_cli_reference.sh` | Regenerates `docs/cli-reference.md` from the built binary | `KHUB=./khub` |

The scale fixture is not here: `parity/scale/` is Go (generator, timed smoke,
growth bench over a ~550-entity corpus) and needs only the toolchain — see its
README.

The generators exist because **khub's on-disk format is ruamel-shaped**: a
property of the file format, so that entities written by khub stay readable
and diff-stable for anything else that touches them. `internal/canon`
reproduces ruamel's emitter byte-for-byte and these corpora are what pin it.

The recorded fixtures under `parity/cases/` do **not** depend on any of this.
They are bytes on disk, and `parity/runner` verifies the Go binary against them
with nothing else installed.
