# Curio-PDP operator runbook

Curio-PDP is the lightweight PDP storage provider build (`make curio-pdp`, Go tag `skiff`). It runs PDP proving and the FWSS registration flow without PoRep/sealing, MK20 market code, or `filecoin-ffi`.

For the skiff binary overview and build flags, see [Skiff binary](skiff-binary.md). For full-stack Curio with Yugabyte and optional PDP alongside sealing, see [Enable PDP](experimental-features/Enable-PDP.md).

## Architecture and data stores

| Deployment | HarmonyDB (tasks, config, PDP state) | Piece index (multihash → offset) |
|------------|--------------------------------------|----------------------------------|
| **Full Curio** | Yugabyte (YSQL) | Yugabyte YCQL / Cassandra-compatible |
| **Curio-PDP (skiff)** | **Yugabyte (YSQL)** | **Yugabyte YCQL** |
| **Tests / CI** | Postgres | Scylla (CQL) |

Curio-PDP is intentionally lighter on compute and dependencies: no PoRep/sealing, MK20 market code, or `filecoin-ffi`. Operators still run **Dockerized Yugabyte** for HarmonyDB and piece indexing — the same YSQL + YCQL stack as full Curio, bundled via `docker/skiff`.

Piece payload files live on disk under writable paths discovered under `/data` (see [Storage](#storage)). Index data lives in Yugabyte YCQL and must be backed up with the database (see [Yugabyte backup](administration/yugabyte-backup.md)).

## Prerequisites

* **Docker** and **Docker Compose** (recommended deployment path)
* Writable storage under `/data` (see [Storage](#storage))
* Optional public **HTTPS domain** when exposing the PDP HTTP API (`HTTP.DomainName` in config)
* FIL/tFIL to fund the PDP signing wallet before FWSS registration

## First-time setup

### 1. Start Yugabyte + Skiff (Docker)

From the repo root:

```bash
cd docker/skiff
docker compose up -d
```

This starts:

* **Yugabyte** — YSQL on port `5433`, YCQL on port `9042`, web UI on `15433`
* **Skiff** — local admin GUI on `127.0.0.1:4701`; public PDP API on `80`/`443` only

Persistent data defaults to `docker/skiff/data/` (Yugabyte, repo/Lantern state, and piece storage). Adjust paths in `.env` if needed.

{% hint style="warning" %}
**Public firewall: open only TCP 80 and 443.** The admin GUI on port `4701` is unauthenticated and for local operator access only — do not publish it to the internet. Native skiff binds the GUI to `127.0.0.1`; the Compose file maps host `127.0.0.1:4701` for the same reason. Use SSH port forwarding if you need remote GUI access.
{% endhint %}

The compose file sets `CURIO_DB_*` for the skiff container. For a native skiff binary against the same stack, export:

```bash
export CURIO_DB_HOST=127.0.0.1
export CURIO_DB_PORT=5433
export CURIO_DB_USER=yugabyte
export CURIO_DB_PASSWORD=yugabyte
export CURIO_DB_NAME=yugabyte
export CURIO_REPO_PATH=~/.curio
export SKIFF_MACHINE_HOST=127.0.0.1:skiff
```

HarmonyDB migrations run on connect and create the same `curio` schema as full Curio. The piece `IndexStore` connects to Yugabyte YCQL on the same host (port `9042` by default via `--db-cassandra-port` / `CURIO_DB_CASSANDRA_PORT`).

### 2. Start the node (native binary)

If running skiff outside Docker, start Yugabyte first (see `docker/skiff/docker-compose.yaml` for the reference single-node command), then:

```bash
./curio   # curio-pdp build
```

On first start, skiff **auto-seeds the `base` config layer** with PDP defaults (`EnablePDP`, `EnableWebGui`, `GuiAddress`, `StorageRPCSecret`). If a separate `pdp` layer already exists from a prior full-Curio setup, it is merged into `base` once at startup.

### 3. Admin GUI — wallet

Open **http://127.0.0.1:4701** → **PDP** page.

| Action | When to use |
|--------|-------------|
| **Create Key** | Generate a new secp256k1 key; private key shown once |
| **Create Delegated Key (Lantern)** | Skiff only — creates a delegated Filecoin address via embedded Lantern |
| **Assign Existing Key** | Import a hex private key you already control |

Only one `eth_keys` row with `role=pdp` is allowed. Fund the **0x address** with enough FIL/tFIL for registration and ongoing messages before proceeding.

### 4. Register with FWSS

In the GUI **Register** tab, complete provider registration, then verify with:

```bash
pdptool ping --service-url https://your-domain.com --service-name public
```

## Configuration model

Skiff reads **only the `base` layer** at runtime. Do not rely on separate `pdp` or `gui` layers — put operational settings in `base` (or let auto-seed populate defaults and edit via the GUI).

Typical `base` values:

* `Subsystems.EnablePDP = true` (forced on)
* `Subsystems.EnableWebGui = true`
* `Subsystems.GuiAddress = "127.0.0.1:4701"` (never bind the GUI to `0.0.0.0` on a host reachable from the internet)
* `HTTP.Enable = false` until a domain is configured for the public API

Only **TCP 80 and 443** should be exposed on your public firewall for FWSS registration and client traffic. See [Curio HTTP server](curio-market/curio-http-server.md) for TLS and reverse-proxy options.

Chain access defaults to embedded Lantern under `<repo>/lantern` unless `FULLNODE_API_INFO` or `[APIs].ChainApiInfo` is set.

## Storage

Curio-PDP stores piece payloads on local disk. Mount drives at **`/data`** (or bind-mount volumes beneath it). On startup the node scans `/data` and **every subdirectory**, probes each for write access, and uses every writable location as storage. Unwritable paths are skipped.

Missing `sectorstore.json` files are created automatically in each writable location.

Additionally, you can use a path other than `/data`:

```bash
DATA_STORAGE=/var/lib/curio-data ./curio
```

You can also set `[Subsystems].DataPath` in the `base` config layer, or pass `--data=/var/lib/curio-data`.

## Moving between deployment profiles

Full Curio and Curio-PDP both use **Yugabyte (YSQL + YCQL)**. CI uses Postgres + Scylla and is not an operator deployment profile.

To move **relational PDP state and piece indexes**:

1. Back up Yugabyte YSQL and YCQL (see [Yugabyte backup](administration/yugabyte-backup.md)).
2. Restore into the target Yugabyte instance.
3. Copy **piece files** separately; payloads are not in the database dump.
4. If the imported DB has a separate `pdp` config layer, skiff merges it into `base` on next startup.

There is no dedicated migration tool — Yugabyte backup/restore plus file copy is sufficient.

## Troubleshooting

| Symptom | Check |
|---------|--------|
| Alert: PDP wallet not configured | PDP page → create or assign key; verify `eth_keys` has `role=pdp` |
| Yugabyte connection errors | `docker compose ps`, `CURIO_DB_*`, YSQL on `5433`, YCQL on `9042`; see [Yugabyte troubleshooting](administration/yugabyte-troubleshooting.md) |
| No storage paths | Drives mounted under `/data` (or `DATA_STORAGE` / `--data`); write permissions on discovered paths |
| Registration fails | Wallet funded; `HTTP.DomainName` / TLS; chain sync (Lantern or external API) |
| Startup warning about missing key | Expected until wallet is configured; clears after key insert |
