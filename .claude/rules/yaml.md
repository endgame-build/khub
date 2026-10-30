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

## Where the emitter departs from ruamel

Byte-identity holds wherever ruamel's bytes read back as the value written.
Two of ruamel's folds do not, and there khub's emitter writes a different,
spec-correct shape (issue #80, `TestIssue80Bytes` pins both):

- **A double-quoted fold right after an escape.** ruamel's lookahead drops the
  `\` continuation when a lone space lies ahead, even when the fold lands just
  after an escaped character and the next line opens with a non-space, so the
  break reads as an invented space. khub keeps the `\` unless the next line
  would open with the one space the fold stands for.
- **A plain fold inside a run of two or more spaces.** ruamel writes the run,
  then breaks before the next word; the trailing white space is discarded on
  read. khub treats a space run at a would-be fold the way `analyze_scalar`
  treats a space before a break: plain is ruled out and the style falls through
  to ruamel's next choice (single, or double when a quote or a break needs it).

Both decisions are made where the fold would land, so a short value, or a long
one that folds elsewhere, keeps ruamel's bytes; the T3 differential stays
1089/1089. A trailing space before a plain fold (a line ending `word ` before the
break) is ruamel's correct shape and stays: the spec strips it on read.

On the read side, `yamlload.go` refolds every multi-line plain scalar from its
token origin per the spec (trailing white space excluded, one break folds to a
space, each empty line to a line feed), because goccy keeps a continuation
line's trailing white space in the value.

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
