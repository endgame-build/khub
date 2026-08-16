# M0 YAML round-trip gate — REPORT

Decision: **GO.** Date: 2026-08-15. goccy/go-yaml v1.19.2, ruamel.yaml per
`uv.lock`, corpus at `parity/corpus/` (canonical files from khub 0.18.0
across all four presets and every layout×format cell; 6 hand-annotated
comment-bearing files; 1083 emit-differential cases).

## Results

| Test | Result | Meaning |
|---|---|---|
| T1a convenience (`MapSlice` decode→encode) | 7/133 | **Not viable** — quote churn, comment loss, anchor expansion. Rejected as anticipated. |
| T1b AST `String()` round-trip | 91/133 | **Not viable as writer** — re-renders comment spacing and unfolds folded scalars. goccy stays parser-only. |
| T1c lexer token-origin reconstruction | **133/133** | `concat(token.Origin) + final newline == source`, byte-exact on every file including comment-bearing and parser-rejected ones. This is the CST substrate: edits are token splices, untouched bytes survive **by construction**. |
| T2 surgical edit (`updated:` splice) | **99/99** applicable (34 files have no such key) | Exactly one line changes; comments, key order, folds, sibling rows byte-preserved. |
| T3 emit-from-scratch differential | **1083/1083 RT · 1083/1083 SAFE** | `internal/canon` reproduces both ruamel profiles byte-exactly: style choice (plain/single/double incl. the 1.2 resolver set), width-80 folding with all three writers' fold rules, exotic breaks, complex keys, `{}`/`[]`, Python float `repr`, big ints. Ported line-for-line from ruamel 0.18 emitter source; fuzz-converged. |

## Resulting write architecture (locks go-port-plan §M2)

- **Read**: goccy `parser.ParseBytes(…, ParseComments)` for structure/values.
- **Edit existing scalars**: token-origin splice over `lexer.Tokenize` — fidelity by construction (T1c+T2), stronger than any emitter guarantee.
- **New keys / new files**: `internal/canon` emitter (`DumpRT` width-80, `DumpWide` width-4096), T3-pinned. Key insertion into an existing file = splice of a rendered line at the token boundary.

## Accepted divergence (recorded; carries to parity/DECISIONS.md)

- goccy's **parser** rejects `keep: |+` with empty content (`could not find
  multi-line content`) where ruamel parses it; the **lexer** handles it
  (T1c green on that file). A workspace file using an empty keep-chomped
  block literal reads as malformed under Go, valid under Python. Judged
  acceptable: khub never writes the shape, the malformed-file contract
  contains it, and the M7 differential run will surface any real-world hit.
  Upstream: goccy/go-yaml parser issue to file at dual-ship time.

## Reproduce

```
uv run python parity/tools/gen_corpus.py           # canonical corpus (frozen clock)
uv run python parity/tools/gen_emit_cases.py       # T3 expectations from ruamel
go build -o yamlgate ./parity/yamlgate && ./yamlgate parity/corpus   # T1/T2
go run ./parity/yamlgate/emit                      # T3
```
