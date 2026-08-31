# Curio-PDP operator runbook

Curio-PDP is the lightweight PDP storage provider build (`make curio-pdp`, Go tag `skiff`). It runs PDP proving and the FWSS registration flow without PoRep/sealing, MK20 market code, or `filecoin-ffi`.

For the skiff binary overview and build flags, see [Skiff binary](skiff-binary.md). For full-stack Curio with Yugabyte and optional PDP alongside sealing, see [Enable PDP](experimental-features/Enable-PDP.md).

## Architecture and data stores

| Deployment | HarmonyDB (tasks, config, PDP state) | Piece index (multihash → offset) |
|------------|--------------------------------------|----------------------------------|
| **Full Curio** | Yugabyte (YSQL) | Yugabyte YCQL / Cassandra-compatible |
| **Curio-PDP (skiff)** | **Yugabyte (YSQL)** | **Yugabyte YCQL** |
| **Tests / CI** | Postgres | Scylla (CQL) |

Curio-PDP is intentionally lighter on compute and dependencies: no PoRep/sealing, MK20 market code, or `filecoin-ffi`. Operators still run **Dockerized Yugabyte** for HarmonyDB and piece indexing — the same YSQL + YCQL stack as full Curio, bundled via `docker/skiff`. The recommended compose stack also includes **Forest** as the chain node.

Piece payload files live on disk under operator-selected folders (see [Storage](#storage)). Index data lives in Yugabyte YCQL and must be backed up with the database (see [Yugabyte backup](administration/yugabyte-backup.md)).

## Prerequisites

* **Docker** and **Docker Compose** (recommended deployment path)
* Writable storage directories — attach them in the admin GUI **Storage** page after start (see [Storage](#storage))
* Optional public **HTTPS domain** when exposing the PDP HTTP API (`HTTP.DomainName` in config)
* FIL/tFIL to fund the PDP signing wallet before FWSS registration

The compose stack ships with Forest. You can still point `FULLNODE_API_INFO` at an external Lotus or Forest node instead.

## Published image

Release builds publish to Docker Hub as **`curiostorage/curio-pdp`**:

| Tag | Network |
|-----|---------|
| `curiostorage/curio-pdp:<version>` / `:latest` | Mainnet |
| `curiostorage/curio-pdp:<version>-calibnet` / `:calibnet` | Calibration |

Local builds use `curiostorage/curio-pdp:dev` (`make docker/curio-pdp` / `make skiff/up`).

## First-time setup

### 1. Configure `docker/skiff/.env` (optional)

Copy or edit `docker/skiff/.env` before starting the stack. Defaults are enough for a local trial.

**Chain node.** Leave `FULLNODE_API_INFO` empty to use the bundled Forest service. To use an external Lotus/Forest instead:

```bash
# Lotus on the Docker host (macOS / Windows / Linux with host-gateway)
FULLNODE_API_INFO=/ip4/host.docker.internal/tcp/1234/http

# Lotus on another host on your LAN
FULLNODE_API_INFO=/ip4/192.168.1.50/tcp/1234/http
```

See [Skiff binary — Chain API](skiff-binary.md#chain-api) for config-layer alternatives (`[APIs].ChainApiInfo`).

**Storage / data paths** default to `./data/` under `docker/skiff/`. Adjust `YUGABYTE_DATA`, `FOREST_DATA`, `SKIFF_REPO_DATA`, and `SKIFF_STORAGE` if needed.

### 2. Start Forest + Yugabyte + Curio-PDP (Docker)

From the repo root:

```bash
cd docker/skiff
docker compose up -d
```

Or from the repo root: `make skiff/up` (builds the image locally first).

**Calibration network:**

```bash
docker compose -f docker-compose.yaml -f docker-compose.calibnet.yaml up -d
# or: make skiff/calibnet/up
```

This starts:

* **Forest** — Filecoin chain node (official `ghcr.io/chainsafe/forest`); first start downloads a snapshot (mainnet is large)
* **Yugabyte** — YSQL on port `5433`, YCQL on port `9042`, web UI on `15433`
* **Curio-PDP (skiff)** — local admin GUI on `127.0.0.1:4701`; public PDP API on `80`/`443` only

Persistent data defaults to `docker/skiff/data/` (Yugabyte, Forest, repo state, and piece storage).

{% hint style="warning" %}
**Public firewall: open only TCP 80 and 443.** The admin GUI on port `4701` is unauthenticated and for local operator access only — do not publish it to the internet. The Compose file maps host `127.0.0.1:4701` for the same reason. Use SSH port forwarding if you need remote GUI access (see [PDP signing wallet](#3-pdp-signing-wallet-admin-gui)).
{% endhint %}

HarmonyDB migrations run on connect and create the same `curio` schema as full Curio. The piece `IndexStore` connects to Yugabyte YCQL on the same host (port `9042` by default via `--db-cassandra-port` / `CURIO_DB_CASSANDRA_PORT`).

Curio starts once Forest RPC accepts connections; wallet balance and FWSS registration need Forest to finish syncing.

For a native skiff binary against the same Yugabyte stack (without the skiff container), export:

```bash
export CURIO_DB_HOST=127.0.0.1
export CURIO_DB_PORT=5433
export CURIO_DB_USER=yugabyte
export CURIO_DB_PASSWORD=yugabyte
export CURIO_DB_NAME=yugabyte
export CURIO_REPO_PATH=~/.curio
export SKIFF_MACHINE_HOST=127.0.0.1:skiff
export FULLNODE_API_INFO=/ip4/127.0.0.1/tcp/2345/http
```

### 3. Select storage folders (admin GUI)

Open **http://127.0.0.1:4701** → **Storage** (or **PDP Guide** → Select storage folders).

1. Attach any existing directory Curio-PDP should use — either enter a custom path on the **Storage** page, or pick a suggested folder under `/data` (compose bind-mounts `./data` there).
2. Paths are **not** auto-registered.
3. Attached paths get a `sectorstore.json` and are persisted in the repo `storage.json`.

### 4. PDP signing wallet (admin GUI)

Skiff needs a **PDP signing key** stored in HarmonyDB (`eth_keys` with `role=pdp`) before FWSS registration. Configure it through the admin GUI — the key is **not** set in `.env`.

**Open the GUI**

* On the Docker host: **http://127.0.0.1:4701**
* From a remote machine (SSH tunnel):

  ```bash
  ssh -L 4701:127.0.0.1:4701 user@your-server
  ```

  Then browse to **http://127.0.0.1:4701** on your laptop.

Go to **PDP** → wallet section.

| Action | When to use |
|--------|-------------|
| **Create** | Generate a new secp256k1 key on this node; the private key is shown **once** — save it before closing the dialog |
| **Import** | Paste a hex private key for an existing 0x address you already control |

Only one PDP key is allowed per cluster. After create or import, fund the displayed **0x address** with enough FIL/tFIL for registration and ongoing on-chain messages (see [Enable PDP — Import your Filecoin Wallet Private Key](experimental-features/Enable-PDP.md#import-your-filecoin-wallet-private-key) for recommended amounts and a Lotus delegated-wallet import workflow).

{% hint style="danger" %}
The GUI has no login. Anyone who can reach port `4701` can manage keys and config. Keep it on localhost or behind an SSH tunnel only.
{% endhint %}

The wallet private key is stored in Yugabyte and survives container restarts as long as `YUGABYTE_DATA` is preserved. Back up Yugabyte before redeploying (see [Yugabyte backup](administration/yugabyte-backup.md)).

### 5. Register with FWSS

In the GUI **Register** tab, complete provider registration, then verify with:

```bash
pdptool ping --service-url https://your-domain.com --service-name public
```

### Native binary (optional)

If running skiff outside Docker, start Yugabyte (and a chain node) first, then:

```bash
./curio   # curio-pdp build
```

On first start, skiff **auto-seeds the `base` config layer** with PDP defaults (`EnablePDP`, `EnableWebGui`, `GuiAddress`, `StorageRPCSecret`). If a separate `pdp` layer already exists from a prior full-Curio setup, it is merged into `base` once at startup. Configure storage and the PDP wallet via the GUI steps above.

## Configuration model

Skiff reads **only the `base` layer** at runtime. Do not rely on separate `pdp` or `gui` layers — put operational settings in `base` (or let auto-seed populate defaults and edit via the GUI).

Typical `base` values:

* `Subsystems.EnablePDP = true` (forced on)
* `Subsystems.EnableWebGui = true`
* `Subsystems.GuiAddress = "127.0.0.1:4701"` (never bind the GUI to `0.0.0.0` on a host reachable from the internet)
* `HTTP.Enable = false` until a domain is configured for the public API

Only **TCP 80 and 443** should be exposed on your public firewall for FWSS registration and client traffic. See [Curio HTTP server](curio-market/curio-http-server.md) for TLS and reverse-proxy options.

Chain API: bundled Forest via compose, or set `FULLNODE_API_INFO` / `[APIs].ChainApiInfo` (see [Skiff binary — Chain API](skiff-binary.md#chain-api)).

## Storage

Curio-PDP stores piece payloads on local disk. Attach folders in the admin GUI (**Storage** page) by entering any existing path, or by selecting a suggested candidate under `/data`. Paths are persisted in `$CURIO_REPO_PATH/storage.json`.

On a full Curio cluster that also seals sectors, keep PDP data off certain disks with the `"piece"` DenyTypes filter. Or add a dedicated storage location with AllowTypes: ["piece"]. See [Separate PDP / parked pieces from sealed storage](storage-configuration.md#separate-pdp-parked-pieces).

Missing `sectorstore.json` files are created when you attach a folder.

Suggested candidates are scanned under `/data` by default. To change that scan root (without limiting custom attach):

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
| Forest still syncing / wallet balance Err | Wait for snapshot import + sync; `docker compose logs forest` |
| Skiff cannot reach chain node | Bundled Forest: check `forest` health and token volume; external: use `host.docker.internal` or LAN IP, not `127.0.0.1` inside the container |
| Alert: PDP wallet not configured | [PDP signing wallet](#4-pdp-signing-wallet-admin-gui) → Create or Import; verify `eth_keys` has `role=pdp` |
| Yugabyte connection errors | `docker compose ps`, `CURIO_DB_*`, YSQL on `5433`, YCQL on `9042`; see [Yugabyte troubleshooting](administration/yugabyte-troubleshooting.md) |
| No storage paths | **Storage** → enter a path or Attach a suggested folder; write permissions on selected folders |
| Registration fails | Wallet funded; `HTTP.DomainName` / TLS; chain node synced and reachable |
| Startup warning about missing key | Expected until wallet is configured; clears after key insert |
