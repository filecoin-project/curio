# Curio Database Semantics

## Core database design style
- Curio uses Yugabyte YSQL as an active coordination plane, not only persistence.
- Most operational workflows are represented as DB state machines with explicit step fields (`task_id_*`, `after_*`, `failed*`) instead of in-memory orchestrators.
- Scheduling, liveness, locking, and retries are DB-mediated and multi-node safe by design.
- Idempotency is largely enforced at the DB boundary via unique keys, partial unique indexes, `ON CONFLICT`, and transactional compare-and-set update predicates.
- Business semantics are partly embedded in SQL functions/triggers (for example IPNI task insertion, piece/deal bookkeeping, derived fields).
- Curio also uses Yugabyte YCQL/Cassandra tables for retrieval index data; this is a separate semantic store from YSQL pipeline state.

## Important tables and what they represent
- `harmony_machines`: machine registry + liveness and scheduler flags (`unschedulable`, restart request).
- `harmony_task`: active distributed work queue (unowned, owned, retried, or completed/removed).
- `harmony_task_history`: immutable execution log for completed/failed task attempts; also used for follow-on task triggers and pipeline event timeline.
- `harmony_task_singletons`: DB guardrail for periodic singleton tasks across the cluster.
- `harmony_config` and `harmony_config_history`: effective config layers and config change timeline.

- `sectors_sdr_pipeline`, `sectors_snap_pipeline`, `sectors_unseal_pipeline`: sealing/snap/unseal workflow state machines keyed by `(sp_id, sector_number)`.
- `sectors_sdr_initial_pieces`, `sectors_snap_initial_pieces`, `open_sector_pieces`: piece membership and deal metadata during ingest-to-seal transitions.
- `sectors_meta`, `sectors_meta_pieces`: canonical per-sector and per-piece metadata used after on-chain progression.
- `sectors_pipeline_events`: link between sector identity and `harmony_task_history` entries.

- `storage_path`: storage endpoint/capacity/capability catalog.
- `sector_location`: mapping from sector filetypes to storage paths.
- `sector_path_url_liveness`: endpoint health memory used for endpoint GC decisions.
- `storage_removal_marks`, `storage_gc_pins`: two-phase storage GC intent and protection set.

- `message_sends`, `message_send_locks`, `message_waits`: Filecoin message send pipeline (enqueue, sender lock/nonce, confirmation tracking).
- `message_sends_eth`, `message_send_eth_locks`, `message_waits_eth`, `eth_keys`: Ethereum/contract tx send and confirmation tracking.

- `market_mk12_deals`: durable MK1.2/Boost deal ledger (intended as long-lived records).
- `market_mk12_deal_pipeline`: mutable MK1.2 processing pipeline.
- `market_mk20_deal`: MK2.0 deal envelope/products; `market_mk20_pipeline`, `market_mk20_pipeline_waiting`, `market_mk20_upload_waiting`, `market_mk20_download_pipeline`, `market_mk20_deal_chunk` hold mutable execution/download/upload state.
- `market_piece_metadata`, `market_piece_deal`: retrieval/indexing linkage from pieces to deal contexts and sectors.
- `parked_pieces`, `parked_piece_refs`: physical/data-source piece staging and reference indirection used by market and PDP workflows.
- `piece_cleanup`: explicit deferred cleanup workflow for piece/index/IPNI teardown.

- `ipni`, `ipni_head`, `ipni_task`, `ipni_chunks`, `ipni_peerid`: IPNI ad-chain state, publication tasks, and entry chunk metadata.
- `pdp_data_set`, `pdp_dataset_piece`, `pdp_pipeline`, `pdp_data_set_create`, `pdp_data_set_delete`, `pdp_piece_delete`: PDP dataset lifecycle and add/remove piece workflows integrated with MK2.0.
- Legacy PDP service/proofset table family (`pdp_services`, `pdp_piecerefs`, `pdp_proof_sets`, etc.) still exists and has active triggers; exact runtime primacy versus `pdp_data_set*` is **uncertain** due mixed code paths.
- `hash_space_*` tables and `open_piece`: cluster map and work queues for the `open-pieces` hash space, where finalized PDP v0 pieces live on storage paths outside piece-park (see "Hash-space tables" below).

## Hash-space tables (open-pieces)
Code: `lib/hashspace` (map, placement, rebalance, HTTP), `tasks/openpieces` (`HashSpacePlace`, `HashSpaceMove`, `HashSpaceDrop`), migration `harmony/harmonydb/sql/20260926-hash-space.sql`.

- Keying: every hash-space table and API is keyed by piece CID v2 only. The position on the hash circle (and the file name `open-pieces/<hex[:2]>/<hex[2:]>`) is the 32-byte tree root from the v2 multihash. PDP tables (`pdp_piecerefs`, `parked_pieces`) store v1, so code rebuilds v2 from v1 + `parked_pieces.piece_raw_size` (`commcid.PieceCidV2FromV1`) at the boundary, and derives v1 + padded size from v2 when checking PDP/parked state.
- `hash_space_meta`: single row whose `version` is bumped by every map change (`Cluster.casTx` compare-and-set). Nodes compare it to reload their local layout; concurrent planners retry on conflict.
- `hash_space_disk`: one row per participating storage path. `capacity` is the effective capacity published by the owning node, `min(configured limit, used_open + filesystem free)`; `used_open` is the node's own open-pieces byte accounting. Piece-park usage is intentionally untracked (transient) and shows up only as reduced free space. `used_acl` is reserved and currently never written (always 0).
- `hash_space_range`: single-owner tiling of each space (`open-pieces`, `acl-pieces`). A row covers `(previous end_hash, end_hash]`, wrapping. Written only by the solver path (`raise` / `CompleteMoveSource`) inside `casTx`.
- `hash_space_move_source`: an interval being moved `from_storage` → `to_storage`. While the row exists, pieces in the interval may be on either disk, new placements target the destination, and the destination node lists the interval under `MoveSources` in its `layout.json`. A `HashSpaceMove` task claims it via `task_id`, copies pieces missing on the destination, then `CompleteMoveSource` transfers range ownership, deletes the source `open_piece` rows, removes source files, and deletes the row. Any existing row blocks new rebalance planning cluster-wide.
- `hash_space_pending_event`: disk events (`full`, `arrive`) raised while a move is in flight, at most one per `(storage_id, event_kind)`. Replayed oldest-first once no `hash_space_move_source` rows remain. `full` fires when owned bytes exceed `FILL_LIMIT_PERCENT` (80%) of capacity and is dropped if the solver can't move anything; `arrive` fires when a new disk joins.
- `open_piece`: where each piece file physically is; one row per `(piece_cid, storage_id)`, with `piece_hash` and `size`. This is the read path's source of truth (`Cluster.Locations`), not range ownership. Rows are added by placement (`RecordPlaced`) and move copies, and removed by `CompleteMoveSource` (source side) and `DeleteCID` (all locations).
- `hash_space_place`: PDP pieces to move from piece-park into open-pieces, keyed by `pdp_pieceref` (`pdp_piecerefs.id`). Inserted by the `pdp_piecerefs_hash_space_place` AFTER INSERT trigger with the v1 `pdp_piece_cid` and `piece_ref`. `HashSpacePlace` writes the file (renaming a sole-ref piece-park file on the same filesystem, otherwise copying), records `open_piece`, and sets `placed = TRUE`. The row stays until the piece-park copy can go: only when no non-PDP `parked_piece_refs` remain (Market, sealing `pieceref:`, aggregation, PDP v1). Then the parked file is removed and the row deleted. Rows whose `pdp_piecerefs` row is gone are deleted.
- `hash_space_delete`: pieces (v2) whose last PDP ref was dropped; queued by PDP piece GC (`task_piece_gc.go`) and orphan-ref discard (`pdp/dataset_verify.go`). `HashSpaceDrop` claims a row only when no `pdp_piecerefs` exist for the v1 CID and no referenced parked piece lacks a piece-park file. It then deletes emptied, file-less, zero-ref `parked_pieces` rows and removes the piece from every `open_piece` location (local delete or `DELETE /hashspace/{storage}/{hash}` on the owning node). Placement cancels an unclaimed delete for the same piece and waits while one is claimed.
- Reads: PDP piece reads (`lib/cachedreader`, `lib/pieceprovider/open_piece_reader.go`) try open-pieces first and fall back to piece-park. Each ranged read opens the current location and, on not-found, re-lists `open_piece` and fails over to another location.

- Known limits: two v2 CIDs with the same root and different raw sizes share a file path (placement errors on size mismatch). A stuck move source blocks all rebalancing. `acl-pieces` transfers are planned but skipped. A piece shared with non-PDP refs occupies both piece-park and open-pieces until those refs go.

## Coordination patterns implemented in DB
- Task claiming: workers atomically claim unowned tasks and set `owner_id`; failed tasks are disowned and retried via `retries` + `update_time`.
- Scheduler correctness should be modeled as conditional ownership updates, not dependence on row-level lock semantics.
- Machine coordination: scheduler admission, liveness, and restart drains are controlled through `harmony_machines`.
- Singleton enforcement: periodic/global tasks are serialized through `harmony_task_singletons`.
- Message nonce serialization: per-sender lock tables (`message_send_locks`, `message_send_eth_locks`) plus sender/nonce partial unique indexes prevent conflicting in-flight nonce assignments.
- Confirmation ownership: watcher processes claim pending wait rows via `waiter_machine_id` to avoid duplicate confirmation work.
- Pipeline advancement: pollers transition rows only when prerequisite fields match expected state (mostly null checks + `after_*` booleans), which provides lock-free compare-and-set semantics.
- SQL functions encapsulate dedupe/ordering logic for sensitive flows (IPNI publication task insertion, ad-chain head updates, piece/deal processing and cleanup).

## Key invariants
- `harmony_task` rows represent unfinished work only; completion or terminal failure removes row and appends `harmony_task_history`.
- Task names are part of contract surface (handler registration + DB row `name`).
- For each sender, at most one successful or in-flight nonce entry may exist:
- Filecoin: unique `(from_key, nonce)` where `send_success IS NOT FALSE`.
- ETH: unique `(from_address, nonce)` where `send_success IS NOT FALSE`.
- `message_waits`/`message_waits_eth` are keyed by signed CID/hash and are the authoritative confirmation records.
- Sealing/snap pipeline rows are unique per `(sp_id, sector_number)` and are progressed by monotonic stage flags; retries intentionally clear specific send fields to reopen the stage.
- `sectors_meta` is canonical sector metadata once chain-visible; pipeline GC assumes metadata/piece consistency before deleting pipeline rows.
- `sectors_meta.is_cc` is derived by trigger logic and must not be treated as an arbitrary writable flag.
- References to staged pieces should flow through `parked_piece_refs` indirection, not direct ad hoc links to `parked_pieces`.
- IPNI ad ordering is append-like by `order_number`; `ipni_head` points to the current ad per provider and `previous` links the chain.
- Hash-space map changes (`hash_space_range`, `hash_space_move_source`) happen only inside `Cluster.casTx`, which bumps `hash_space_meta.version`.
- `open_piece.piece_cid` and `hash_space_delete.piece_cid` are piece CID v2; `hash_space_place.pdp_piece_cid` is v1 (copied from `pdp_piecerefs`).
- A piece-park file backing a PDP piece is removed only after its open-pieces copy is recorded and no non-PDP `parked_piece_refs` remain.

## Read patterns
- Pollers read by state predicates (`after_*`, `task_id_* IS NULL`, `failed = FALSE`, retry timing) and usually batch by miner/proof/scheduling windows.
- Schedulers read unowned tasks ordered by `update_time`; retries are time-gated by per-task retry policies.
- Watchers repeatedly read pending message wait rows assigned to the current machine and transition them when confirmations appear.
- UI/API endpoints aggregate across pipeline tables, metadata tables, wait tables, and piece/deal tables for status views.
- Retrieval paths read piece/deal metadata from YSQL and payload/block mappings from YCQL index tables.
- GC tasks read storage layout (`storage_path`, `sector_location`), marks/pins, and pipeline/meta state to compute safe deletions.

## Write patterns
- Task creation is transactional: create `harmony_task` row, then insert task-specific extra rows in the same transaction.
- Progression writes are predominantly conditional updates (`... WHERE current_state ...`) to ensure idempotent/serialized transitions.
- Completion writes are transactional: mutate/remove active task + append history + append optional pipeline event.
- Message senders persist unsigned payload first, then nonce/signature, then send outcome; watchers later populate execution/receipt fields.
- Piece/deal updates often use DB functions (`process_piece_deal`, `remove_piece_deal`) to keep cross-table invariants in one place.
- IPNI writes use helper functions to enforce context/provider dedupe behavior and head update consistency.
- Serializable conflict handling is expected; code frequently wraps writes in retrying transactions.

## Lifecycle and retention expectations
- `harmony_task` is short-lived queue state; `harmony_task_history` is long-lived and expected to grow.
- Pipeline tables are medium-lived and cleaned once terminal conditions are met (plus indexing/IPNI prerequisites where applicable).
- `market_mk12_deals` is intended to be durable, even after pipeline completion.
- `market_mk12_deal_pipeline`, `market_mk20_pipeline`, `pdp_pipeline`, `ipni_task`, `pdp_ipni_task`, and `piece_cleanup` are workflow state and are expected to be garbage-collected when complete.
- `parked_pieces`/`parked_piece_refs` are lifecycle-managed staging references and are removed when no longer needed by active deals/datasets.
- Message wait rows are retained after confirmation as execution records (no general short TTL implied by core logic).
- Config snapshots in `harmony_config_history` are append-only history.

## Common mistakes an AI should avoid
- Treating DB tables as passive storage and moving scheduler/lock logic into process-local memory.
- Modifying pipeline rows without preserving compare-and-set predicates and idempotent transitions.
- Breaking sender nonce invariants by changing lock/index semantics in `message_sends*`.
- Bypassing `parked_piece_refs` and attaching long-lived references directly to `parked_pieces`.
- Deleting or repurposing `market_mk12_deals` as ephemeral pipeline state.
- Writing directly to trigger-maintained semantics (`is_cc`, PDP proofset refcounts, upload readiness timestamps) without preserving trigger logic.
- Changing piece lifecycle semantics in YSQL without corresponding YCQL indexstore cleanup/update paths.
- Assuming only one PDP table family is active; legacy and MK2.0-coupled schemas coexist (**uncertain priority** across deployments).
- Passing piece CID v1 into hash-space tables or APIs (`CIDHash` rejects it), or comparing v2 hash-space columns directly against v1 PDP columns in SQL.
- Treating `hash_space_range` ownership as where a piece is; reads and deletes must go through `open_piece`.
- Removing piece-park bytes for a PDP piece while Market, sealing or aggregation still hold `parked_piece_refs` for it.
