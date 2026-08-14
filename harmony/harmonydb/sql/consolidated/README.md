# Consolidated PDP schema

`pdp_closure_schema.sql` is the **final post-migration state** of the PDP tables plus the
minimal set of non-PDP tables they depend on, collapsed from the ~24 migrations under
`harmony/harmonydb/sql/` (which include a v1→v0 `proofset/root`→`data_set/piece` rename and
many alters). It exists so another project (Piri) can create exactly the tables the
`tasks/pdpv0` pipeline reads/writes without running all of Curio's migrations.

## What's in it

- **All `pdp_*` tables** (21) — both the pdpv0 pipeline tables (`pdp_data_sets`,
  `pdp_data_set_pieces`, `pdp_data_set_piece_adds`, `pdp_piecerefs`, `pdp_delete_data_set`,
  `pdp_piece_pulls`, `pdp_piece_pull_items`, `pdp_piece_uploads`, `pdp_services`,
  `pdp_prove_tasks`, …) and the unused mk20-era ones (harmless; they only depend on the closure).
- **Closure tables the PDP FKs require** (verified: PDP tables FK out only to these):
  - `harmony_*` — the harmonytask scheduler
  - `message_sends_eth`, `message_send_eth_locks`, `message_waits_eth`, `eth_keys` — the eth sender
  - `parked_pieces`, `parked_piece_refs` — piece storage
- **8 plpgsql functions + their triggers** — the `data_set`/`parked` refcount triggers and the
  two `update_pdp_data_set_creates`/`update_pdp_data_set_piece_adds` triggers on
  `message_waits_eth`. Cross-subsystem triggers on `message_waits_eth` (proofshare / sectors /
  balance-manager) are **pruned** — they reference tables Piri doesn't have and would fail at runtime.
- **Excluded:** `ipni*` (only used by the `piece_gc` task, which Piri doesn't run) and every
  other Curio subsystem (market, sectors, etc.).

## How it was regenerated

1. `docker run -d --name curio-schema-pg -e POSTGRES_PASSWORD=postgres -p 127.0.0.1:5432:5432 postgres:16`
2. `go run ./tools/schemadump/` — applies **all** embedded migrations via harmonyquery's own
   migrator into schema `curio` (faithful ordering + the custom statement parser).
3. Prune the DB to the keep-set: drop every table not matching `pdp_*` / `harmony_*` /
   the 6 closure tables (CASCADE), then drop every function except the 8 kept ones (CASCADE,
   which removes the unwanted `message_waits_eth` triggers).
4. `docker exec curio-schema-pg pg_dump -U postgres -d postgres --schema=curio --schema-only
   --no-owner --no-privileges > pdp_closure_schema.sql`

Because the file is the migration result with only *unrelated* objects dropped, the `pdp_*`
DDL is identical-by-construction to running the full migration chain. Verified: it loads clean
(`ON_ERROR_STOP`) into an empty database.

## Note for consumers (Piri / harmonyquery)

This is raw `pg_dump` output: it is **psql-loadable** as-is, but harmonyquery's migrator applies
files via `pgx.Exec`, which does **not** understand psql meta-commands. Before embedding it as a
harmonyquery migration, strip the `\restrict`/`\unrestrict` lines and reconcile the schema
handling (`CREATE SCHEMA curio` / `curio.`-qualified names vs harmonyquery's own
`search_path`/`ensureSchemaExists`). That packaging happens in the Piri-integration step.
