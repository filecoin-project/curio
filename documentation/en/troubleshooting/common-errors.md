---
description: Common Curio errors seen in support, with meaning and next steps.
---

# Common errors

This page maps frequent error strings to:
- what they usually mean (in plain language),
- whether they are benign vs urgent,
- what to do next.

If your error isn’t listed, start with [Collect debug info](collect-debug-info.md) and include logs + context.

Tip: many errors are symptoms, not causes. If the same task keeps failing, scroll **up** to find the first error in the chain.

---

### API / CLI

#### `ERROR: could not get API info for Curio: could not determine API endpoint ... Try setting environment variable: CURIO_API_INFO`

What it means:
- Your CLI environment can’t find the Curio API endpoint/token.

What to do:
- If running via systemd, env vars may exist in the service unit but not in your shell.
- Export `CURIO_API_INFO` in the shell where you run `curio cli ...`, or run the command on the node/container where Curio is running.

---

### Chain / Lotus connectivity

#### `Not able to establish connection to node with addr: ws://.../rpc/v1`

What it means:
- Curio can’t reach the configured full node endpoint.

What to do:
- Verify connectivity from the Curio host (DNS, firewall, port).
- Confirm Lotus is synced and serving RPC.
- Check the configured endpoint in your layers and restart Curio after changes.

---

### Sealing / PoSt

#### `invalid partIdx 0 (deadline has 0 partitions)`

Plain-English meaning:
- Curio asked Lotus for “deadline partitions” (groups of sectors) and got **zero** partitions back. Then it tried to run a proof against partition index `0`, which doesn’t exist.

When it’s benign:
- Fresh miner / devnet / early migration: you simply don’t have proving sectors yet, so there are no partitions.

When it’s not:
- You *expect* proving sectors, but Lotus reports none (possible chain sync issue, wrong miner address, wrong network, or a migration/config mismatch).

What to do:
1) Confirm your miner actually has proving sectors (via Lotus commands / UI).
2) Confirm Curio is pointing at the right Lotus node and the right network.
3) If this is a `curio test window-post` run, remember it is a *diagnostic*; it will fail if there’s nothing meaningful to prove.

Code reference (for maintainers): `tasks/window/compute_do.go`.

#### `Error computing WindowPoSt ... no sectors to prove`

What it means:
- There are no sectors in that deadline/partition that require proofs.

Next steps:
- Confirm there are proving sectors and deadlines populated.
- If you expect proofs: look earlier in the pipeline for commit/precommit failures.

---

### Storage / paths

#### `sector {..} redeclared in ... paths/db_index.go`

What it suggests:
- The same sector appears declared in multiple storage locations, or your local store index contains conflicting entries.

What to do:
- Verify storage attach/mount configuration.
- Confirm there aren’t duplicated mount paths or reused repo paths between nodes.
- If you recently moved storage, ensure the old path is detached/removed.

---

### Market / indexing

#### `duplicate key value violates unique constraint ... (SQLSTATE 23505)` during indexing / CheckIndex

What it means:
- Two workers attempted to insert the same identity row (often a retry/concurrency artifact).

What to do:
- Determine if the pipeline is *progressing* despite the error (harmless noise) or if tasks are stuck.
- Collect: deal UUID, piece CID, task ID, and the full log span.
- If stuck: see [curio-market troubleshooting](../curio-market/troubleshooting.md).

#### `no suitable data URL found for piece_id <N>` (ParkPiece)

Plain-English meaning:
- Curio has a “parked piece” that it needs to read, but it can’t find any usable location for the bytes.
- In most setups, that location is an HTTP(S) **data URL** (and optional headers) stored in the DB.

Where it happens:
- The ParkPiece workflow reads `parked_piece_refs` rows for the piece.

Common causes:
- The data URL was never added.
- The data URL exists but is malformed (bad scheme) or requires headers that weren’t stored.
- The task is running on a node that is *not* actually responsible for ingestion (layers mismatch).

What to do (operator workflow):
1) Find how the deal was supposed to be ingested:
   - online (URL) vs offline (local file / piece locator)
2) Verify you actually added a data URL for the deal (see Storage Market docs).
3) Confirm the node running ParkPiece has the **market/ingest** layers enabled.
4) Collect: deal UUID, piece CID, and the ParkPiece task ID.

Docs:
- See `curio-market/storage-market.md` (“Add data URL for offline deals”).

Code reference (for maintainers): `tasks/piece/task_park_piece.go`.

---

### Batch sealing

#### `panic: SupraSealInit: supraseal build tag not enabled`

What it means:
- The binary you’re running does not include batch sealing support.

What to do:
- Prefer official release binaries.
- If building from source, follow the batch sealing build instructions and ensure required build flags/features are enabled.

### Batch commit failures: `all-or-nothing ... Batch successes 67/68 ... idx=N`

What it means:
- Batch submission/claiming is atomic: one failing item can cause the batch to fail.

What to do:
- Identify the failing sector/task corresponding to `idx=N` (UI + logs).
- Check wallet balance / message errors.
- Consider temporarily reducing batch size/concurrency to isolate the failing entry.

(Expanded workflow will live in the Batch Sealing page’s troubleshooting section.)

---

### YugabyteDB

#### `Could not upgrade! ...` / `Rewriting of YB table is not yet implemented (SQLSTATE 0A000)`

What it means:
- You hit a schema migration or table rewrite Yugabyte can’t perform in-place for your version.

What to do:
- Verify Yugabyte version compatibility with your Curio release.
- Always back up before upgrades/downgrades.
- Collect the migration filename and the error.

See: [YugabyteDB troubleshooting](../administration/yugabyte-troubleshooting.md)
