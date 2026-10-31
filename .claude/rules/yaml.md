# YAML in khub — everything goes through internal/canon

Only `internal/canon` may import a YAML library. This is enforced by the
choke-point lint in `.golangci.yml`, not by convention.

khub's on-disk bytes are a contract, and a second library reaching the write
path would quietly reshape them.

## Why the format is still ruamel-shaped

`internal/canon` reproduces ruamel.yaml's emitter byte-for-byte. That is a
property of **khub's file format**, not nostalgia for the retired
implementation — entities live in git, so they must stay diff-stable, and a
reformat on every write would make every khub commit unreadable. It deviates
from ruamel only where ruamel corrupts a value: `writePlain` never breaks a
plain scalar after a run of two or more spaces (the read folds them to one)
and never wraps a simple key (ruamel's two-line key does not parse), and
`chooseStyle` never single-quotes a value holding NEL, LS or PS (written raw,
no reader gets it back), so the T3 gate excludes the 58 rows ruamel writes
that way and counts the rest.

Two profiles, both pinned:

| Profile | Width | Used for |
|---|---|---|
| round-trip | 80 | entity frontmatter, collections — the human-edited surface |
| safe | 4096 | `.khub/{ontology,policy,storage}.yaml` and friends, where long scalars stay on one line |

## Why goccy is parser-only

`goccy/go-yaml` is the parser and lexer. It is **never** the writer: its encoder
re-renders comment spacing and unfolds folded scalars. The writer is khub's own, over the lexer's token
origins — `concat(token.Origin)` reconstructs the source byte-exactly (goccy drops the
space the emitter writes before each fold of a long plain scalar, and
`restoreFoldSpaces` in `splice.go` puts it back from the source), so an edit rewrites only the tokens whose values moved and every other
byte survives by construction.

That is what lets `khub edit` keep a hand-written comment, a deliberately quoted
scalar, or an author's sequence indentation across a write.

Structural edits follow line ownership. A pair owns its key's line through the
line holding its value's last token. A deleted pair takes exactly those lines;
a value the splice cannot change in place has those lines rewritten, keeping
only the comment on the key's line; a key appended to a nested block, or an
item appended to a block list, lands right after the owned lines; and a
standalone comment between pairs belongs to neither. The rule is written out
in `splice.go`'s fallback policy; change it there and in T4–T7 together.

## Two resolvers, deliberately

khub resolves scalars under **YAML 1.1** at the scan altitude (markdown
frontmatter, matching python-frontmatter) and **YAML 1.2** at the edit altitude.
`yes`/`no`/`on`/`off`, octals and sexagesimals resolve differently between them,
and the split is load-bearing: it decides what `fts_body` indexes. See
`canon.Mode11` / `canon.Mode12`.

## Merge keys

A plain `<<` key merges a mapping, or a list of them, into its own mapping,
and an explicit key wins wherever it sits. The key order follows the loader
each altitude mirrors: ruamel at 1.2 keeps explicit keys first, then each
merged mapping's new keys; PyYAML at 1.1 puts merged pairs first (a list from
its last mapping), then explicit ones. A quoted `"<<"` is an ordinary key.
One `<<` per mapping: goccy refuses a second, so a list merges several. An
edit leaves the `<<` line and its anchor as written. An edit of a key the
merge supplies lands as explicit keys in a block mapping (`mergedEdits` in
`splice.go`): the edited key, plus, under ruamel's order, every merged key
between the last written key and it, at their current values, so the loaded
order holds. A flow mapping takes no inserted key, so there the edit re-emits
the whole document, merge expanded.

## Limits on what a document may cost

`khub import` reads bundles an attacker can write, so loading is bounded.
Both limits live in `internal/canon/limits.go` and refuse as
`malformed_entity`, which a scan reports as a malformed file.

- **`MaxAliasNodes` (100,000).** The loader shares an anchored value
  instead of copying it, so a billion-laughs document loads in microseconds.
  Every render (`get`, `query`, a JSON or YAML write) expands each alias in
  full, so the resolver counts what aliases expand to and refuses the
  document past the budget. Flat documents never trip it.
- **`MaxTextBytes` (4 MiB).** `ReadText`, `ReadTextIn`, `Parse` and
  `LoadCollection` refuse a larger text document.

Neither limit moves a fixture or a corpus result. `TestAliasBombIsRefused`
reads `internal/canon/testdata/alias-bomb.yaml`, which sits outside
`parity/corpus` so yamlgate never loads it.

## The standing regression

```bash
go run ./parity/yamlgate parity/corpus     # T1c round-trip, T2 surgical edit, T4-T7 delete, rename, nested-append, list-append splices
go run ./parity/yamlgate/emit              # T3 emitter differential, both profiles
```

CI fails on any diff, on a corpus that shrinks below 130 files, or on a T3 count
below 1000. `parity/tools/gen_emit_cases.py` and `gen_load_cases.py` regenerate
the corpora (they need `ruamel.yaml`; see `parity/tools/README.md`).

## Reference implementations

- `internal/canon/yamlload.go` — the dual-mode loader.
- `internal/canon/splice.go` — the token-splice writer.
- `internal/canon/ryaml.go` — the ruamel-shaped emitter, both profiles.
