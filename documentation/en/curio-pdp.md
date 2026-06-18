# Curio-PDP operator runbook

Curio-PDP is the lightweight PDP storage provider build (`make curio-pdp`, Go tag `skiff`). It runs PDP proving and the FWSS registration flow without PoRep/sealing, MK20 market code, or `filecoin-ffi`.

For the skiff binary overview and build flags, see [Skiff binary](skiff-binary.md). For full-stack Curio with Yugabyte and optional PDP alongside sealing, see [Enable PDP](experimental-features/Enable-PDP.md).

## Architecture and data stores

| Deployment | HarmonyDB (tasks, config, PDP state) | Piece index (multihash → offset) |
|------------|--------------------------------------|----------------------------------|
| **Full Curio** | Yugabyte (YSQL) | Yugabyte YCQL / Cassandra-compatible |
| **Tests / CI** | Postgres | Scylla (CQL) |
| **Curio-PDP (skiff)** | **Postgres** | **Disk-based indexing** (target; see note below) |

Curio-PDP is intentionally lighter: no Yugabyte requirement and no separate CQL/Scylla service for operators who only run PDP.

Piece payload files live on disk under discovered `filecoin-hot-data/` paths (see [Storage](skiff-binary.md#storage) in the skiff binary doc). Index data on disk must be copied separately when moving hosts; it is not included in SQL dumps.

{% hint style="info" %}
**Index store note:** The target Curio-PDP stack uses disk-based piece indexing. Current builds may still open a CQL `IndexStore` via `--db-cassandra-port`; disk-only indexing is follow-up work. Plan deployments accordingly.
{% endhint %}

## Prerequisites

* **Postgres** for HarmonyDB (`CURIO_DB_*` env vars or `--db-host` flags)
* Mount points containing `filecoin-hot-data/` directories (auto-discovered; see skiff storage section)
* Optional public **HTTPS domain** when exposing the PDP HTTP API (`HTTP.DomainName` in config)
* FIL/tFIL to fund the PDP signing wallet before FWSS registration

## First-time setup

### 1. Database

Point Curio-PDP at Postgres:

```bash
export CURIO_DB_HOST=127.0.0.1
export CURIO_DB_PORT=5432
export CURIO_DB_USER=curio
export CURIO_DB_PASSWORD=...
export CURIO_DB_NAME=curio
export CURIO_REPO_PATH=~/.curio
export SKIFF_MACHINE_HOST=127.0.0.1:skiff
```

HarmonyDB migrations run on connect and create the same `curio` schema as full Curio.

### 2. Start the node

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
* `Subsystems.GuiAddress = "127.0.0.1:4701"`
* `HTTP.Enable = false` until a domain is configured for the public API

Chain access defaults to embedded Lantern under `<repo>/lantern` unless `FULLNODE_API_INFO` or `[APIs].ChainApiInfo` is set.

## Storage

Skiff discovers `filecoin-hot-data/` under mount points (see [skiff-binary.md](skiff-binary.md#storage)). Missing `sectorstore.json` files are created automatically.

## Moving between deployment profiles

Full Curio (Yugabyte + YCQL), CI (Postgres + Scylla), and Curio-PDP (Postgres + disk index) are **separate profiles** — not co-located by default.

To move **relational PDP state**:

1. Export the `curio` schema from the source DB (`pg_dump`, `ysql_dump`, etc.; see [Yugabyte backup](administration/yugabyte-backup.md)).
2. Restore into the target Postgres (or Yugabyte YSQL) instance.
3. Copy **piece files** and **on-disk index data** separately; neither is in the SQL dump.
4. If the imported DB has a separate `pdp` config layer, skiff merges it into `base` on next startup.

There is no dedicated migration tool — SQL export/import plus file copy is sufficient.

## Troubleshooting

| Symptom | Check |
|---------|--------|
| Alert: PDP wallet not configured | PDP page → create or assign key; verify `eth_keys` has `role=pdp` |
| Postgres connection errors | `CURIO_DB_*`, firewall, migrations |
| No storage paths | Mount points with `filecoin-hot-data/`; permissions on discovered paths |
| Registration fails | Wallet funded; `HTTP.DomainName` / TLS; chain sync (Lantern or external API) |
| Startup warning about missing key | Expected until wallet is configured; clears after key insert |
