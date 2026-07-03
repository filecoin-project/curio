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

- **Admin GUI**: `http://127.0.0.1:4701` (webrpc + config editor; local access only)
- **Public PDP API**: `HTTP.ListenAddress` / `HTTP.DomainName` from the `base` config layer (`80`/`443` only)
- **Machine identity**: `127.0.0.1:skiff` (harmony task scheduling; not a public listener)

Open **only TCP 80 and 443** on your public firewall for the PDP API. The admin GUI on port `4701` is unauthenticated — keep it on localhost (`127.0.0.1:4701`) and never expose it to the internet.

The admin GUI hides PoRep, sealing, and storage-market navigation when running Skiff. It shows PDP-focused pages (overview, PDP, config, wallets, IPNI, alerts) and displays **Curio PDP** as the product name in the navigation chrome.

### Common flags

| Flag | Env | Default | Purpose |
|------|-----|---------|---------|
| `--repo` | `CURIO_REPO_PATH` | `~/.curio` | Local data directory (node identity) |
| `--db-host` | `CURIO_DB_HOST` | `127.0.0.1` | Yugabyte YSQL host (YCQL index uses same host, port `9042`) |
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

See [Enable PDP](experimental-features/Enable-PDP.md) for full-stack Curio deployment, or the [Curio-PDP runbook](curio-pdp.md) for the skiff PDP-only deployment (Dockerized Yugabyte), including [PDP signing wallet setup via the admin GUI](curio-pdp.md#3-pdp-signing-wallet-admin-gui).

### Chain API

Skiff requires an external Lotus-compatible chain node (for example [Lotus](https://lotus.filecoin.io/lotus/get-started/what-is-lotus/) or [Forest](https://docs.forest.chainsafe.io/)). Configure it via environment variable or the `base` config layer:

| Source | Precedence |
|--------|------------|
| `FULLNODE_API_INFO` env | Highest |
| `[APIs].ChainApiInfo` in config | Second |

Example:

```bash
export FULLNODE_API_INFO=/ip4/127.0.0.1/tcp/1234/http
```

Or in the `base` layer:

```toml
[Apis]
ChainApiInfo = ["/ip4/127.0.0.1/tcp/1234/http"]
```

The chain node must match the skiff build network (mainnet, calibration, etc.).

## Storage

Skiff does not use `storage.json`. On startup it scans the first three directory levels of each mount point for a folder named `filecoin-hot-data` (at most one per mount point). If a discovered path has no `sectorstore.json`, one is created automatically with store enabled.
