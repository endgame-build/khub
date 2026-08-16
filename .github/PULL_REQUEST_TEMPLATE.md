## What & why

<!-- One or two sentences. Link the issue if there is one. -->

## Checklist

- [ ] `go test ./...` passes
- [ ] `gofmt -l ./cmd ./internal ./parity` prints nothing; `go vet ./...` is clean
- [ ] `./parity-run -bin "$PWD/khub" -cases parity/cases` passes
- [ ] `./parity-run -coverage parity/coverage.yaml -cases parity/cases -subset-of "$PWD/khub"` reports no gaps
- [ ] `bash smoke.sh` passes
- [ ] Any re-recorded fixture bytes are **intended**, and the commit says why
- [ ] `CHANGELOG.md` updated (if user-facing)
- [ ] Docs updated (if the command surface or schema changed — regenerate with
      `bash parity/tools/gen_cli_reference.sh`)
- [ ] Surfaces stay schema-generic — no per-type branching in `internal/cli/`
