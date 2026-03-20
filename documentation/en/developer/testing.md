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
  go test -v -tags='fvm,nosupraseal' -timeout 30m ./itests/ -run TestName
```

## Make targets

| Target | Description |
|--------|-------------|
| `make test-dbs-up` | Start or reuse Postgres and ScyllaDB containers |
| `make test-dbs-down` | Remove both containers |
| `make test` | Build deps and run all itests (sets DB env vars automatically) |

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
# HarmonyDB SQL tests (Postgres only)
CURIO_HARMONYDB_HOSTS=127.0.0.1 CURIO_HARMONYDB_PORT=5432 \
  go test -v -tags='fvm,nosupraseal' -timeout 5m \
  -run "TestCrud|TestTransaction|TestSQLIdempotent" ./itests/

# PDP proving tests (ScyllaDB indexstore)
CURIO_HARMONYDB_HOSTS=127.0.0.1 CURIO_HARMONYDB_PORT=5432 CURIO_DB_HOST_CQL=127.0.0.1 \
  go test -v -tags='fvm,nosupraseal' -timeout 5m \
  -run "TestPDPProving" ./itests/

# Indexstore unit tests (ScyllaDB only, no FFI needed)
CURIO_DB_HOST_CQL=127.0.0.1 go test -v -timeout 5m ./market/indexstore/
```

## Notes

- Postgres replaces YugabyteDB for tests; production Curio still uses YugabyteDB
- ScyllaDB is a drop-in Cassandra replacement; same CQL protocol, faster startup
- The `nosupraseal` build tag avoids requiring supraseal/CUDA for local testing
- Containers survive host reboots if Docker is configured to restart them
