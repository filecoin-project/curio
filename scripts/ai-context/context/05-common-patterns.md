# Curio Common Patterns

## Recurring backend patterns
- Constructor-based dependency wiring with narrow interfaces for external systems (chain APIs, senders, storage, DB).
- Strong separation between control logic and execution logic:
- pollers/selectors decide what should run;
- task `Do` methods execute one claimed unit.
- Explicit resource-gated execution (`TypeDetails` + `resources.Resources` + per-task max concurrency).
- Cooperative ownership checks (`stillOwned`) before single-writer side effects.
- Structured error propagation (`xerrors.Errorf`) and contextual logging around miner/sector/task identifiers.
- Periodic singleton/background work is implemented as DB-coordinated singleton tasks, not process-local timers.

## Recurring database patterns
- DB is the coordination source of truth: task ownership, machine liveness, retries, locks, and pipeline progression are persisted first.
- Work creation is transactional:
- insert into `harmony_task`;
- write task-specific rows in the same transaction.
- Work claiming uses compare-and-set ownership updates (`owner_id IS NULL` -> `owner_id = machine`); do not rely on row-lock semantics for correctness.
- Pipeline updates are conditional (`... WHERE current_state ...`) to enforce idempotent transitions and avoid double advancement.
- Uniqueness is used as behavior control, including partial unique indexes (for example sender+nonce where send is in-flight/successful).
- SQL functions/triggers encode multi-table invariants (piece/deal linkage, derived fields, ad-chain behavior, lifecycle transitions).
- Two-phase destructive operations are common: mark/intent table first, irreversible action later (for example storage GC, piece cleanup).

## Task and pipeline patterns
- Pipeline rows are state machines keyed by stable identities (typically `(sp_id, sector_number)` or `(deal_id, index)`).
- Stage fields are explicit (`task_id_*`, `after_*`, `failed*`) and are advanced monotonically.
- Retries reopen a stage by clearing specific fields and re-queuing, rather than mutating historical records.
- Completion/failure writes are coupled with history append (`harmony_task_history`), and pipeline events are attached via task-history IDs.
- Follower/derived tasks are driven from durable DB signals rather than in-memory callbacks alone.
- Pipeline GC removes mutable workflow rows once terminal invariants are satisfied; durable business rows remain.

## Contract and chain interaction patterns
- Chain/contract actions follow an enqueue -> send -> wait/watch -> finalize model.
- Nonce ordering is enforced with DB locks per sender plus DB nonce reconciliation against chain state.
- Sender tasks persist unsigned payload, signed payload, and send result explicitly; watchers are responsible for final confirmation materialization.
- Final business state changes are gated on confirmed receipts/messages, not optimistic send success.
- Contract-driven flows use intent tables (`*_create`, `*_delete`, etc.) and watcher tasks to materialize on-chain outcomes into canonical tables.
- Filecoin and EVM paths follow the same coordination pattern, even with different transport/receipt formats.

## API / transport patterns
- Handlers are thin orchestration layers:
- validate/authenticate input;
- persist intent/status rows;
- return quickly while background tasks do long-running work.
- Status endpoints read from DB-derived workflow state rather than holding in-memory session state.
- Transport boundaries normalize protocol differences (for example piece CID format conversion at handler edge).
- Public HTTP, libp2p, and UI/RPC surfaces converge on shared DB-backed workflows rather than separate execution engines.

## Migration / upgrade patterns
- Migrations are additive and idempotent (`IF NOT EXISTS`, guarded triggers/functions, conflict-safe inserts).
- Semantics changes are introduced through schema + runtime coupling, not schema-only changes.
- Backfills and transition helpers are often encoded as SQL functions to keep behavior deterministic during rollout.
- Derived/maintained fields are preserved via triggers when semantics require cross-table consistency.
- Upgrade-safe behavior favors compatibility with partially upgraded fleets; avoid assumptions that all nodes switch instantly.

## Preferred implementation shapes
- For new asynchronous features, follow this shape:
- durable intent row/table;
- task adder/poller selecting eligible rows;
- worker task with idempotent DB transition;
- optional message/receipt wait path;
- status query endpoint;
- lifecycle cleanup path.
- Keep durable records separate from mutable pipeline records.
- Model ordering/correctness via explicit DB fields and unique constraints, not implicit timing.
- Encapsulate cross-table semantic operations in SQL function(s) when multiple callers need identical behavior.

## Patterns an AI should reuse
- Transactional add-task + extra-info writes.
- Conditional update patterns that encode state preconditions.
- DB-level dedupe (`UNIQUE`, `ON CONFLICT`) for externally repeated triggers.
- Lock-per-sender + watcher confirmation for chain transactions.
- Explicit terminal markers (`complete`, `failed`, `after_*_success`) and GC after terminalization.
- Piece lifecycle indirection through `parked_piece_refs` instead of direct durable references to staging rows.
- Clear separation of durable ledger tables from ephemeral pipeline tables.

## Patterns an AI should not introduce
- Node-local authoritative queues, locks, or state machines that bypass DB coordination.
- Direct chain/contract side effects from handlers without sender/watcher persistence.
- Multi-table state transitions performed outside transactions.
- Refactors that collapse durable and transient tables into one mutable record.
- Hidden state progression inferred only from logs/timeouts without explicit DB state.
- Destructive cleanup that removes canonical historical/business records needed for operations or auditability.
- Changes that weaken uniqueness/ordering invariants around task ownership, nonce assignment, or pipeline identity keys.
