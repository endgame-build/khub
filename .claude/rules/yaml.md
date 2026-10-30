# YAML in khub — everything goes through internal/canon

Only `internal/canon` may import a YAML library. This is enforced by the
choke-point lint in `.golangci.yml`, not by convention.

khub's on-disk bytes are a contract, and a second library reaching the write
path would quietly reshape them.

## Why the format is still ruamel-shaped

`internal/canon` reproduces ruamel.yaml's emitter byte-for-byte. That is a
property of **khub's file format**, not nostalgia for the retired
implementation — entities live in git, so they must stay diff-stable, and a
reformat on every write would make every khub commit unreadable.

Two profiles, both pinned:

| Profile | Width | Used for |
|---|---|---|
| round-trip | 80 | entity frontmatter, collections — the human-edited surface |
| safe | 4096 | `.khub/{ontology,policy,storage}.yaml` and friends, where long scalars stay on one line |

## Where khub departs from ruamel

Byte-for-byte holds except where ruamel's own output does not read back as
the value it was given (issue #80). There the value wins, and each departure
is marked `khub, not ruamel` in `ryaml.go`:

- a double-quoted fold that is not at a space keeps its `\` escape (ruamel
  drops it after an escape such as `\\`, and the value gains a space);
- a plain scalar never breaks after two or more spaces (the reader would fold
  them to one);
- a plain simple key never breaks before a long word (the key stops parsing).

None of these shapes is in the T3 corpus, so T3 stays 1089/1089. ruamel's
single trailing space before a plain fold is kept: it reads back exactly.

The reader departs from goccy the same way: `foldPlain` in `yamlload.go`
folds a multi-line plain scalar per the spec, because goccy keeps the white
space before each break once a continuation line starts with `-`.
`FuzzStringRoundTrip` holds the property: any string outside the documented
loader gaps reads back equal once emitted. The gaps it skips are U+2028,
U+2029 and U+0085, `<<`, values that resolve as another type under YAML 1.1,
non-printable characters and invalid UTF-8.

## Why goccy is parser-only

`goccy/go-yaml` is the parser and lexer. It is **never** the writer: its encoder
re-renders comment spacing and unfolds folded scalars. The writer is khub's own, over the lexer's token
origins — `concat(token.Origin)` reconstructs the source byte-exactly, so an edit rewrites only the tokens whose values moved and every other
byte survives by construction.

That is what lets `khub edit` keep a hand-written comment, a deliberately quoted
scalar, or an author's sequence indentation across a write.

## Two resolvers, deliberately

khub resolves scalars under **YAML 1.1** at the scan altitude (markdown
frontmatter, matching python-frontmatter) and **YAML 1.2** at the edit altitude.
`yes`/`no`/`on`/`off`, octals and sexagesimals resolve differently between them,
and the split is load-bearing: it decides what `fts_body` indexes. See
`canon.Mode11` / `canon.Mode12`.

## The standing regression

```bash
go run ./parity/yamlgate parity/corpus     # T1c round-trip + T2 surgical edit
go run ./parity/yamlgate/emit              # T3 emitter differential, both profiles
```

CI fails on any diff, on a corpus that shrinks below 130 files, or on a T3 count
below 1000. `parity/tools/gen_emit_cases.py` and `gen_load_cases.py` regenerate
the corpora (they need `ruamel.yaml`; see `parity/tools/README.md`).

## Reference implementations

- `internal/canon/yamlload.go` — the dual-mode loader.
- `internal/canon/splice.go` — the token-splice writer.
- `internal/canon/ryaml.go` — the ruamel-shaped emitter, both profiles.
