# Architecture audit release notes

This release intentionally tightens filesystem, graph, upgrade, and CLI behavior. Golden changes should be limited to the new multi-ID `get` surface, plain piped help, additive upgrade JSON fields, bounded representative cycles, and errors that now expose unsafe paths or incomplete scans.

- Existing-workspace mutations share one stable `.khub/generated/locks/workspace.lock` from scan through commit. Lock files must remain while a process may hold them. Concurrent first writers race to create the lock directory; on darwin the open that follows has been seen to report a transient `ENOENT` for a directory that exists, so the open is retried up to three times and no first write fails on it.
- Item and collection writes use exclusive sibling temporaries, preserve permissions, honor umask for creates, sync publication, and refuse workspace escape, ownership overlap, wrong file shape, reserved paths, and symlink traversal. Scan I/O failures fail the command.
- Schema patterns use one cached regexp2 full-match gate with a one-second timeout for writes and integrity checks.
- Relation comparison uses resolved `(type, slug)` identity. Derived inverses keep source type plus predicate identity. HTTP entity edge records add sorted `resolved_targets`; CLI records stay unchanged.
- `check` emits one deterministic real cycle witness per cyclic component and acyclic predicate, including self-loops. A self-loop that shares a component with a longer cycle is not listed separately; it surfaces once the longer cycle is broken. `misplaced` now also catches a typed `.json`/`.yaml` at the wrong depth inside a declared storage tree; `.json`/`.yaml` outside every storage tree are data files and are neither read nor reported.
- `upgrade --dry-run` validates a copied candidate without locks or workspace files. The copy excludes `.git/`, `.khub/generated/`, vendored directories (`node_modules`, `.venv`, `venv`, `__pycache__`, `.kb`) and non-regular files. Real upgrades recompute under lock, preserve modes and backups, publish the version last, and roll back failures. A failed recovery retains a named `.khub/generated/upgrade-*` journal; no automatic crash recovery is promised. JSON appends `dry_run` and `removed_types`.
- `get ID...` returns the existing object for one ID and an argument-ordered array for multiple IDs. Resolution is all-or-nothing; duplicates remain; raw mode refuses batches.
- Help uses the existing TTY gate: rich terminal help remains, while piped help is plain and keeps section order and EPIPE behavior.

Validation before release:

```bash
gofmt -l ./cmd ./internal ./parity
go test ./...
go vet ./...
golangci-lint run
go run ./parity/yamlgate parity/corpus
go build -o khub ./cmd/khub
go build -o parity-run ./parity/runner
./parity-run -bin "$PWD/khub"
./parity-run -bin "$PWD/khub" -coverage parity/coverage.yaml -subset-of "$PWD/khub"
bash smoke.sh
bash parity/tools/gen_cli_reference.sh
git diff --exit-code -- docs/cli-reference.md
```

Reviewed fixture changes: plain help and batch-get arity; additive upgrade fields;
`internal-path-error` now identifies the failing directory scan (`readdir`, normalized across platforms) before
any mkdir; installed skill manifests change because shipped guidance now documents
batch reads and upgrade previews. Only these cases are re-recorded.

Normal upgrade can repair an invalid old schema; removed-type comparison is empty
when the old schema cannot resolve. Fresh initialization has no cross-process
serialization guarantee; cleanup removes only its own files and empty directories.

Local scale check (three-run medians, milliseconds; advisory, no CI threshold):

| Entities | Query before / after | Search before / after |
|---|---|---|
| 100 | 13.95 / 14.74 | 26.23 / 24.38 |
| 550 | 36.91 / 45.97 | 81.00 / 82.00 |
| 2000 | 89.27 / 138.09 | 265.72 / 204.53 |

Search at 2000 entities improved about 23%; root-confined reads added query cost.
These measurements include process startup and vary with filesystem cache and load.

The query cost was the per-file read through the workspace `os.Root`, which
re-opened every path component on each entity (three `openat` calls for
`knowledge/requirements/req-x.md`; 71% of an in-process `query` at 2000 entities
in a CPU profile, 48% of that in directory components). The scan now opens one
`os.Root` per type directory and reads by basename; the leaf still opens
`O_NOFOLLOW` and the directory descriptor is pinned. Same machine, same corpora,
before and after that change, five-run medians in milliseconds:

| Entities | Query before / after | Search before / after | Check before / after |
|---|---|---|---|
| 100 | 12.6 / 11.2 | 19.2 / 16.5 | 25.0 / 17.9 |
| 550 | 44.3 / 33.6 | 83.2 / 57.9 | 58.7 / 50.6 |
| 2000 | 157.1 / 95.8 | 273.6 / 187.1 | 248.9 / 165.4 |

In-process (`BenchmarkQueryLargeCorpus`, eight runs each): 115–125 ms before,
87–96 ms after. Every command that scans the corpus moved by a similar share.
