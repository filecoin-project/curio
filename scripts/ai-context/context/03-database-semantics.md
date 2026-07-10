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
