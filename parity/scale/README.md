# parity/scale — a khub corpus at scale, and the smoke test that reads it

The parity fixtures pin khub's bytes on corpora of a dozen files. `smoke.sh` at
the repo root walks the whole verb surface on about the same. Neither says
anything about how khub behaves on a corpus the size of a real project's, and
that is where a different class of bug lives: a walk that does not terminate,
an index that silently drops entities, a scan whose cost turns quadratic, a
`check` that gets slower than the edit loop it gates.

Four pieces, all Go, all stdlib:

| | |
|---|---|
| `corpus/` | the generator — writes a build-hub workspace of 300–2000 entities, every one authored by `khub add`, and its manifest |
| `gen/` | the generator's command line |
| `smoke.sh` | 80 assertions over that workspace, every command timed |
| `bench/` | the read surface over several corpus sizes, reporting how cost grows |
| `manifest/` | the one helper `smoke.sh` needs: a manifest field, a JSON reduction, the clock |

```bash
go build -o khub ./cmd/khub                                   # the binary under test
bash parity/scale/smoke.sh                                    # generate 550 entities, then assert — ~30s
bash parity/scale/smoke.sh --scale 300                        # a smaller corpus
bash parity/scale/smoke.sh --keep                             # assert the corpus already on disk — ~5s
bash parity/scale/smoke.sh --fast                             # skip the regenerate-and-diff pass
bash parity/scale/smoke.sh --work /tmp/x                      # somewhere other than $TMPDIR/khub-scale/smoke
KHUB=/path/to/other/khub bash parity/scale/smoke.sh           # smoke another build

go run ./parity/scale/gen -bin ./khub -work /tmp/x -scale 550 -seed 1 -today 2026-01-15
go run ./parity/scale/bench -bin ./khub                       # 100/550/2000, corpora cached between runs
go run ./parity/scale/bench -bin ./khub -scales 100,550       # nearer in
go run ./parity/scale/bench -bin ./khub -format json
```

Nothing here needs Python. The smoke script builds the two helpers with
`go build` on the way in, so the Go toolchain is the only thing it asks of the
machine — the same claim the parity suite carries.

## The corpus

A fake payments platform: 1 prd, 1 arc42, ~45 repos, ~110 components
(services, our libraries, and the vendors we sit on), ~260 requirements across
all four kinds, and ~130 decisions at `-scale 550`. It is **generated, never
committed** — the default lives under `$TMPDIR/khub-scale/`, outside the
checkout, and the generator puts it back in about ten seconds.

Every file is written by `khub add`, driven out of process with relations
passed as `--<predicate> <id>` and ids taken from `add`'s JSON output, never
predicted. Nothing hand-writes frontmatter, so the generator cannot produce
something the ontology forbids: `add` refuses and the run fails. The closing
`khub check` is the proof that what came out is a legal corpus.

The shape is deliberate, not random:

- **`depends_on` is a DAG by construction** — a component may only depend on
  one declared before it, which is also the truth (a service depends on a
  library depends on a vendor, not the other way round). The cycle case in the
  smoke test then closes a loop on purpose and asserts `check` catches it.
- **One requirement in eight is left unlinked**, an orphan gap by design:
  `realized_in` is the only edge tying a rule to the code satisfying it. That
  is what makes `check` exit 0 and `check --strict` exit 1 on the same corpus.
- **Dates are spread over three years**, skewed towards the recent, so `stale`
  and `status` answer something. `add` and `edit` both honour an explicit
  `--created`/`--updated`, so every date is written on the authoring path —
  no post-hoc splice. The two singletons `init` scaffolds keep their scaffolded
  bodies (a template's own scaffold satisfies every rule it declares) and are
  backdated through `edit`.
- **One deliberate supersession chain** of known length, kept clear of the
  scattered pairs, so `history` has an expected answer from either end.
- **A needle** (`xylotrope`) planted in exactly one body, so `search` does too.

The adr is the dated type: `--created` is passed so the date in the minted id
is the day the decision was taken, not the day the generator ran — which is
what keeps the corpus reproducible on any day. Minting under the live clock is
exercised by the smoke test's authoring pass instead.

## The manifest

The generator writes `smoke-manifest.json` at the workspace root: counts per
type, the orphan set, transitive `depends_on` closures for three chosen roots
(the hub with the most ancestors, the deepest service, one in the middle),
one-hop in/out degree for three chosen nodes, the supersession chain, the
needle's id, stale counts at four thresholds, tag counts, and handles for the
mutation cases. It is computed from the generator's **own** model of the graph
— an independent implementation of "orphan", "transitive closure", "one hop",
"stale" — never read back out of khub. The only things it takes from khub are
declared configuration: the type list and each type's `orphan` exemption,
which are authored input, not computed answers.

That is the point. A smoke test that compares khub's output to khub's output
only proves khub is consistent with itself. Every assertion in `smoke.sh` is
khub against the manifest, so a disagreement is a real finding in one of the
two.

It also means nothing in `smoke.sh` spells an id, a title or an id scheme —
they all come out of the manifest through `parity/scale/manifest`. The change
that dropped the `-NNN-` ordinal from minted ids would not touch a line of it.

## The benchmark

`smoke.sh` prints a wall clock beside each case, which is enough to notice a
regression and not enough to answer the question underneath it: is a
command's cost linear in the corpus, or quadratic and merely still small.
`bench` runs the read surface over several sizes and fits an exponent between
the smallest and the largest — `n^1.0` is linear, `n^2.0` is the wall — so the
answer is a column rather than a hunch.

It measures out of process, on purpose: khub is one static binary and the
number a user feels at the shell is the only one there is. Two rows are the
controls: `--version` is the process floor, and `schema show <type>` reads
the ontology and nothing else, so it is the one command whose cost should not
move with the corpus at all. Everything else should sit above them by about
what its scan costs.

Corpora are cached under `-cache` (default `$TMPDIR/khub-scale/<scale>`)
because they are deterministic and generating one is slower than measuring it.
Every read is run once as a warm-up with its exit code asserted — a command
that is fast because it errored out is the one measurement mistake this file
cannot afford — then `-repeat` times (default 5), reporting the median.

Nothing here fails on a threshold. The milliseconds depend on the machine, the
filesystem and what else is running; the exponent is the part that travels.

### What it says today

Measured 100 → 2000 entities on one Apple Silicon laptop, khub 0.22.1 on the
kb-port branch, median of 5, out of process:

| | n=100 | n=550 | n=2000 | growth |
|---|---|---|---|---|
| `--version` (process floor) | 2.8 ms | 3.4 ms | 3.0 ms | flat |
| `schema show <type>` | 3.5 ms | 4.2 ms | 3.6 ms | flat |
| `get`, `query --tag`, `query --orphan`, `neighbors`, `history`, `stale` | ~8 ms | ~25 ms | ~78 ms | `n^0.72`–`n^0.79` |
| `impact`, `impact --reverse`, `neighbors --depth 12`, `reindex --dry-run` | ~8 ms | ~28 ms | ~88 ms | `n^0.81`–`n^0.82` |
| `status` | 8.2 ms | 22.8 ms | 88.5 ms | `n^0.79` |
| `validate` (whole corpus) | 12.5 ms | 37.7 ms | 126.9 ms | `n^0.77` |
| `check`, `check --strict` | 13.4 ms | 43.9 ms | 164.6 ms | `n^0.84` |
| `search` | 15 ms | 65 ms | 260 ms | `n^0.95` |

Everything that reads the corpus is sub-linear in n (the process floor and the
schema load are a fixed few milliseconds that the small corpus pays too), and
the graph walks cost about what the scan costs — there is no `in_edges` cliff
of the kind kb measured at `n^1.65`, because khub's projection carries inverse
edges. `search` is the steepest row: it builds the FTS5 index in memory per
call, so it grows with the bytes indexed rather than the entity count.

Run `go run ./parity/scale/bench -bin ./khub` for the current numbers; the
table in this file is a snapshot, not a contract.

## Determinism

Same `-seed`, `-scale` and `-today` produce byte-identical files, and the
smoke test asserts it by regenerating into a second directory and diffing
`knowledge/`, `index.md` and the manifest. The corpus is a file format, and
this is where a map iteration order leaking into a written file shows up —
every map in the generator is walked through its creation-order slice, and
the manifest's maps are emitted with sorted keys.

Dates default to the real today in `gen`; `smoke.sh` and `bench` pin
`-today 2026-01-15` so the fixture never drifts, and export `KHUB_PARITY_NOW`
to the same day so every date-relative answer is judged on the day the
manifest was.

## What it does not do

Not a gate on time. The timings printed next to each case are there so a
regression is visible to a human reading the output; nothing fails on a
threshold, because the number depends on the machine. If you want the trend,
run the bench.

Not part of CI per commit, and not run by `go test` or the root `smoke.sh`.
It takes half a minute and writes a couple of megabytes, which is the wrong
trade for a per-commit gate and the right one before a release or after
touching `internal/index`, `internal/graph`, `internal/integrity`,
`internal/query` or `internal/search`.

Not a second fixture suite. The parity cases stay the contract for bytes;
this asserts answers, counts and exit codes, and says nothing about
formatting.
