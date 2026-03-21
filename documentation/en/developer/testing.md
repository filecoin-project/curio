# Local Integration Testing

Curio integration tests need a SQL database (harmonydb) and a CQL database
(indexstore). Production uses YugabyteDB for both, but for local development
**PostgreSQL** and **ScyllaDB** are a fast, lightweight alternative that can be
run as simple Docker containers.

## Quick start

```bash
# 1. Start test databases (idempotent, containers persist across runs)
make test-dbs-up

# 2. Build FFI dependencies (needed once, or after submodule updates)
FFI_USE_OPENCL=1 make deps

# 3. Run tests (make test-dbs-up prints this invocation for copy-paste)
CURIO_HARMONYDB_HOSTS=127.0.0.1 CURIO_HARMONYDB_PORT=5432 CURIO_DB_HOST_CQL=127.0.0.1 \
  go test -v -tags='fvm,nosupraseal' -timeout 30m ./itest/ittestgroup1/ -run TestName
```

## Make targets

| Target | Description |
|--------|-------------|
| `make test-dbs-up` | Start or reuse Postgres and ScyllaDB containers |
| `make test-dbs-down` | Remove both containers |
| `make test` | Build deps and run all packages under `./itest/...` (sets DB env vars automatically) |

## Layout: `itest/ittestgroupN/`

Integration tests are split into **parallel CI shards** under `itest/ittestgroup1/`,
`itest/ittestgroup2/`, … Each directory is its own Go package (`package ittestgroup1`,
etc.). Shared helpers live in `itest/helpers/`.

- **Adding a test** to an existing group: add `*_test.go` in that folder only; CI
  picks up groups automatically via `.github/scripts/emit-test-matrix.sh`.
- **Adding a new parallel shard**: create `itest/ittestgroup6/` (next free number),
  set `package ittestgroup6`, import `github.com/filecoin-project/curio/itest/helpers`
  if needed. No workflow edits required.

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `CURIO_HARMONYDB_HOSTS` | `127.0.0.1` | Postgres host for harmonydb |
| `CURIO_HARMONYDB_PORT` | `5433` (YugabyteDB) | Postgres port; set to `5432` for local Postgres |
| `CURIO_DB_HOST_CQL` | falls back to `CURIO_HARMONYDB_HOSTS` | ScyllaDB host for indexstore CQL connections |
| `FFI_USE_OPENCL` | unset | Set to `1` to build FFI with OpenCL instead of CUDA |

## Container details

- **PostgreSQL 15** on port 5432, trust auth, user/database `yugabyte`
  (matching the existing YugabyteDB schema expectations)
- **ScyllaDB** on port 9042, single-core mode (`--smp 1 --memory 512M`)

Containers are created with `docker run` on first use and restarted with
`docker start` on subsequent runs. They are not removed automatically; use
`make test-dbs-down` to clean up.

## Running individual test suites

```bash
# HarmonyDB SQL tests (Postgres only) — see itest/ittestgroup5/
CURIO_HARMONYDB_HOSTS=127.0.0.1 CURIO_HARMONYDB_PORT=5432 \
  go test -v -tags='fvm,nosupraseal' -timeout 5m ./itest/ittestgroup5/

# PDP proving tests (ScyllaDB indexstore) — see itest/ittestgroup3/
CURIO_HARMONYDB_HOSTS=127.0.0.1 CURIO_HARMONYDB_PORT=5432 CURIO_DB_HOST_CQL=127.0.0.1 \
  go test -v -tags='fvm,nosupraseal' -timeout 5m ./itest/ittestgroup3/ -run TestPDPProving

# Indexstore unit tests (ScyllaDB only, no FFI needed)
CURIO_DB_HOST_CQL=127.0.0.1 go test -v -timeout 5m ./market/indexstore/
```

## Notes

- Postgres replaces YugabyteDB for tests; production Curio still uses YugabyteDB
- ScyllaDB is a drop-in Cassandra replacement; same CQL protocol, faster startup
- The `nosupraseal` build tag avoids requiring supraseal/CUDA for local testing
- Containers survive host reboots if Docker is configured to restart them
