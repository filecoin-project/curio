# Integration tests (`itest/`)

- **`itest/helpers/`** — shared utilities (DB env, CLI test contexts). Import as
  `github.com/filecoin-project/curio/itest/helpers`.
- **`itest/ittestgroup1/` … `ittestgroup5/`** — parallel test shards. Each folder is
  one Go package named like the directory (`package ittestgroup1`, …).

CI discovers every `itest/ittestgroup*` directory and runs `go test` on each in
parallel (see `.github/scripts/emit-test-matrix.sh`). The main unit-test job runs
`go list ./... | grep -v '/itest/'` so packages here are not double-run there.

**Add a test:** put it in the group that fits runtime (keep shards roughly balanced).
**Add a shard:** create `itest/ittestgroup6/` with `package ittestgroup6` — no
workflow change needed.
