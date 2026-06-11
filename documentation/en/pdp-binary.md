# PDP binary (`pdp`)

The `pdp` binary is a lightweight Curio variant focused on Proof of Data Possession (PDP) storage. It shares the same database schema and configuration tables as `curio`, but omits PoRep/sealing, the deal market (MK20), and the worker JSON-RPC listener.

## Build

```bash
FFI_USE_OPENCL=1 make deps   # or CUDA on GPU hosts
make pdp
```

The binary is written to `bin/pdp` (not the repo root, to avoid clashing with the `pdp/` Go package).

## Run

```bash
./bin/pdp
```

With defaults:

- **Admin GUI**: `http://127.0.0.1:4701` (webrpc + config editor)
- **Public PDP API**: `HTTP.ListenAddress` / `HTTP.DomainName` from the `base` config layer
- **Machine identity**: `127.0.0.1:pdp` (harmony task scheduling; not a public listener)

### Common flags

| Flag | Env | Default | Purpose |
|------|-----|---------|---------|
| `--repo` | `CURIO_REPO_PATH` | `~/.curio` | Data directory |
| `--db-host` | `CURIO_DB_HOST` | `127.0.0.1` | Yugabyte/Postgres host |
| `--machine-host` | `PDP_MACHINE_HOST` | `127.0.0.1:pdp` | Harmony machine ID |

## Architecture

- **`pdpnode`**: shared library used by both `pdp` and `curio` (when `EnablePDP` is set)
- **`lib/piecestore`**: FFI-free piece I/O for PDP commp and pull tasks
- **`pdp.MountRoutes`**: shared HTTP route mounting for PDP endpoints

When PDP is enabled in full `curio`, `pdpnode.Attach()` registers PDP harmony tasks and `cuhttp` mounts PDP routes via `pdp.MountRoutes`.

## Configuration

Use a single `base` config layer. Ensure:

- `Subsystems.EnablePDP = true` (forced on by the binary)
- `Subsystems.EnableWebGui = true` for the admin UI
- `HTTP.Enable = true` for the public `/pdp/*` API

See [Enable PDP](experimental-features/Enable-PDP.md) for deployment guidance.
