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
| `bench/` | the read surface and one write cycle over several corpus sizes, both clocks, reporting how cost grows |
| `gate/` | the performance rule: bench's JSON against `baseline.json`, pass / fail / invalid |
| `footprint/` | the bytes every read puts in front of an agent, pinned exactly in `footprint.json` |
| `manifest/` | the one helper `smoke.sh` needs: a manifest field, a JSON reduction, the clock |
| `corpus/cases.go` | the one list of read and write cases bench and footprint share, filled from the manifest |

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
go run ./parity/scale/bench -bin ./khub -profile gate -format json > bench.json   # what the gate reads
go run ./parity/scale/gate -baseline parity/scale/baseline.json bench.json        # the rule; -format md for a summary
go run ./parity/scale/footprint -bin ./khub -work <corpus> -check parity/scale/footprint.json
go run ./parity/scale/footprint -bin ./khub -work <corpus> -record > parity/scale/footprint.json   # re-record, deliberately
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
`bench` runs the read surface and one write cycle over several sizes and fits
an exponent between the smallest and the largest — `n^1.0` is linear, `n^2.0`
is the wall — so the answer is a column rather than a hunch.

It measures out of process, on purpose: khub is one static binary and the
number a user feels at the shell is the only one there is. Two rows are the
controls: `--version` is the process floor, and `schema show <type>` reads
the ontology and nothing else, so it is the one command whose cost should not
move with the corpus at all. Everything else should sit above them by about
what its scan costs.

Two clocks per sample. Wall time is what the user feels. The child's CPU time
(user + sys) ignores the scheduler contention of a shared runner: measured on
a loaded machine, wall-time medians swung up to 3.5× between runs of
unchanged code and missed an injected +80% scan, while the min of eleven CPU
samples varied 2–8% and caught it. So the table shows wall medians and CPU
mins, the growth is fitted on CPU min, and the runs are interleaved — every
repeat visits every scale and every case before the next begins — so a slow
stretch hits all rows alike instead of one scale.

Every read is run once as a warm-up with its exit code asserted — a command
that is fast because it errored out is the one measurement mistake this file
cannot afford — then `-repeat` times (default 5; the gate profile is 11).

The write cycle runs on a copy of each corpus: `add` a probe, `link` it,
`link` again (which must report `changed: false`), `edit`, `unlink`,
`remove`. Afterwards the copy must be byte-identical to the cached corpus,
which doubles as a write-path oracle at n that no fixture can be: remove
really removes, nothing is left behind. Every write costs one full scan,
about what `status` costs, and grows at the same rate as the reads. Write
wall time is fsync-bound (0.7–8.8 s outliers were seen for a 30–100 ms
operation on a busy disk) and is never judged.

Corpora are cached under `-cache` (default `$TMPDIR/khub-scale/<scale>`)
because they are deterministic and generating one is slower than measuring
it. A cached corpus generated with another scale, seed or day than asked is
rebuilt; one an older khub wrote is kept and said so (`-regen` rebuilds).

Nothing in `bench` fails on a number. The milliseconds depend on the machine,
the filesystem and what else is running; the exponent is the part that
travels, and the rule lives in `gate`.

### The gate

`gate` reads bench's JSON (the `-profile gate` shape: scales 100 and 2000,
eleven repeats, both clocks) and compares three statistics per command with
`baseline.json`, every one taken over the min of the samples:

| statistic | what | rule |
|---|---|---|
| `cpu_e` | growth exponent of CPU time between the smallest and largest scale | FAIL above 1.15, or above the baseline + 0.10 |
| `cpu_r` | CPU time at the largest scale over `schema show`'s | FAIL above 1.35× the baseline |
| `wall_r` | wall time at the largest scale over `--version`'s | FAIL above 2× the baseline; reads only, a backstop for lost parallelism |

The exponent is the cliff detector and the per-command delta beside it sees
a growth drift; the CPU ratio sees a constant-factor regression the exponent
never can (an extra full scan leaves the exponent alone). A run whose control
grew with the corpus, or whose process floor is three times the recorded
one, is INVALID (exit 2): it measured the machine, not khub. Stated limit: a
quadratic term still small at n=2000 is invisible at n≤2000.

The baseline is a deliberate commit, like a parity re-record, and it must be
**recorded on the runner class that gates**: ratios depend on the
microarchitecture, and even the exponents shift with it: the first run on a
GitHub runner against a container-recorded baseline put every exponent a
uniform 0.06–0.12 higher, because the runner's process floor dilutes the small
scale differently. So a baseline from another machine (the file records goos,
core count and CPU model) keeps only the absolute exponent ceiling blocking
and downgrades the per-command delta and both ratios to notes. The nightly's `rebaseline`
dispatch input runs the bench three times on the runner and uploads a
baseline recorded from their medians; `gate -record` prints the per-command
deltas against the previous baseline for the commit message. The committed
`baseline.json` was recorded in a development container, so on the GitHub
runner only the ceiling judges until that dispatch has run once.

The gate runs nightly, report-only, until unchanged `main` has shown zero
FAIL and zero INVALID for three to four weeks; then the `|| true` in
`nightly.yml` goes and a red gate is routed to the tracking issue. Tolerances
live in the baseline file, so a change to them is reviewed like any other
number.

### The footprint

An agent reads khub's JSON into its context, so the size of every answer is a
cost khub charges on every call. `footprint` runs the read surface against a
corpus on a pipe with the clock pinned and records, per command, the exit
code, stdout and stderr bytes, the row count of an array answer and a hash of
stdout. The bytes are deterministic — byte-identical across runs, workspace
paths and a fresh regeneration — so `footprint.json` is an exact pin, like a
parity fixture, recorded at the per-PR smoke scale (300): a tolerance would
only let ten PRs at +4.9% each add 60% with no review moment. The scale smoke
checks it between its read and mutation sections; an answer that moved is
exit 1 with a delta table (bytes, per cent, rows), and re-recording it in the
commit that grows the answer is the review. The corpus itself is
fingerprinted, so a generator or preset change reads as "the corpus moved,
re-record" (exit 2) and never as khub growing.

What it showed when first recorded, at n=550 (bytes/4 is a rough token
count; this JSON is punctuation-dense, so real tokens run 15–35% higher):
a bare `query` is 125 KB, about 228 bytes a row, and 460 KB at n=2000; a
third of each row is the `type` and `slug` keys repeating what `id` carries;
`schema` is 26 KB at every scale; `check` is 2.4 KB; `get` under 1 KB.

### What it says today

Measured 100 → 2000 entities on a 4-vCPU container, khub 0.30.0, CPU min of
11 interleaved runs, out of process:

| | growth (CPU) |
|---|---|
| `--version` (process floor), `schema show <type>` | flat |
| `get`, `query`, `neighbors`, `history`, `stale`, `status` | `n^0.7`–`n^0.85` |
| `check`, `validate`, `impact`, `reindex --dry-run` | `n^0.75`–`n^0.85` |
| `search`, one hit or a hundred | about `n^0.75` — no longer the steepest row, as it was in 0.22 |
| `add`, `link`, `edit`, `unlink`, `remove` | about `n^0.8`, each one full scan |

Everything that touches the corpus is sub-linear in n (the process floor and
the schema load are a fixed few milliseconds that the small corpus pays too),
and the graph walks cost about what the scan costs, because khub's projection
carries inverse edges. Run the bench for the current numbers; `baseline.json`
holds the recorded exponents and ratios, and the table here is a snapshot,
not a contract.

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

Not a gate on time by itself. The timings `smoke.sh` prints are there so a
regression is visible to a human reading the output; `gate` holds the rule,
against a recorded baseline, nightly.

Per commit, only the small end runs: `smoke.sh --scale 300` with the
footprint check (`go-extras` in `ci.yml`, about sixteen seconds). The 550
smoke on two OSes, the full bench, the gate and the in-process comparison
against the last release run in `nightly.yml`. Run the bench by hand after
touching `index`, `graph`, `integrity`, `query`, `search` or `entity`.

Not a second fixture suite. The parity cases stay the contract for bytes;
this asserts answers, counts, exit codes and sizes, and says nothing about
formatting.
