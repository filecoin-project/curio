# Skiff binary (`skiff`)

Skiff is a lightweight Curio variant focused on Proof of Data Possession (PDP) storage. It shares the same database schema and configuration tables as `curio`, but omits PoRep/sealing, the deal market (MK20), and the worker JSON-RPC listener.

## Build

```bash
make curio-pdp  # → curio (main-net PDP build), no filecoin-ffi required
make skiff      # synonym for make curio-pdp
make calibnet-curio-pdp  # → curio with calibnet tag
make 2k-curio-pdp        # → curio with 2k tag
```

`make build`, `make calibnet`, and `make 2k` build the full `curio` + `sptool` pair only. Build the PDP variant explicitly with `make curio-pdp`. Network selection follows `CURIO_TAGS` (`calibnet`, `2k`, etc.) via `SKIFF_TAGS`.

The `skiff` build uses the `skiff` Go build tag and does not link `filecoin-ffi`. Sealing/PoRep, MK20 market handlers, and curio worker proving code are excluded via build tags.

Curio (`make build`) is unchanged and still builds with full FFI support.

## Run

```bash
./curio
```

With defaults:

- **Admin GUI**: `http://127.0.0.1:4701` (webrpc + config editor)
- **Public PDP API**: `HTTP.ListenAddress` / `HTTP.DomainName` from the `base` config layer
- **Machine identity**: `127.0.0.1:skiff` (harmony task scheduling; not a public listener)

The admin GUI hides PoRep, sealing, and storage-market navigation when running Skiff. It shows PDP-focused pages (overview, PDP, config, wallets, IPNI, alerts).

### Common flags

| Flag | Env | Default | Purpose |
|------|-----|---------|---------|
| `--repo` | `CURIO_REPO_PATH` | `~/.curio` | Local data directory (Lantern chain state) |
| `--db-host` | `CURIO_DB_HOST` | `127.0.0.1` | Yugabyte/Postgres host |
| `--machine-host` | `SKIFF_MACHINE_HOST` | `127.0.0.1:skiff` | Harmony machine ID |

## Architecture

- **`pdpnode`**: shared library used by both `skiff` and `curio` (when `EnablePDP` is set)
- **`lib/piecestore`**: FFI-free piece I/O for PDP commp and pull tasks
- **`pdp.MountRoutes`**: shared HTTP route mounting for PDP endpoints

When PDP is enabled in full `curio`, `pdpnode.Attach()` registers PDP harmony tasks and `cuhttp` mounts PDP routes via `pdp.MountRoutes`.

## Configuration

Use a single `base` config layer. On first start, skiff **auto-seeds `base`** with PDP defaults (`EnablePDP`, `EnableWebGui`, `GuiAddress`, `StorageRPCSecret`). If a `pdp` layer exists from a prior setup, it is merged into `base` once. Ensure:

- `Subsystems.EnablePDP = true` (forced on by the binary)
- `Subsystems.EnableWebGui = true` for the admin UI
- `HTTP.Enable = true` for the public `/pdp/*` API

See [Enable PDP](experimental-features/Enable-PDP.md) for full-stack Curio deployment, or the [Curio-PDP runbook](curio-pdp.md) for the lightweight Postgres-based skiff build.

### Chain backend (Lantern)

Skiff embeds [Lantern](https://github.com/Reiers/lantern) as its default chain backend when no external API is configured. State is stored under `<repo>/lantern`.

| Source | Precedence |
|--------|------------|
| `FULLNODE_API_INFO` env | Highest |
| `[APIs].ChainApiInfo` in config | Second (overrides ChainBackend) |
| `[APIs].ChainBackend = "lantern"` | Default for skiff (embedded Lantern) |
| Embedded Lantern | Used when ChainBackend is `lantern` and ChainApiInfo is unset |

Set `[APIs].ChainBackend = "external"` and configure `ChainApiInfo` to use an external Lotus-compatible RPC instead of embedded Lantern.

Embedded Lantern supports **mainnet** and **calibration** builds only. For `2k` / debug builds, set `ChainBackend = "external"` and `ChainApiInfo` or `FULLNODE_API_INFO` to an external Lotus-compatible RPC endpoint.

## Storage

Skiff does not use `storage.json`. On startup it scans the first three directory levels of each mount point for a folder named `filecoin-hot-data` (at most one per mount point). If a discovered path has no `sectorstore.json`, one is created automatically with store enabled.
