# Curio Architecture

## High-level architecture
- Curio is a distributed Storage Provider runtime with three planes:
- Control plane: YugabyteDB-backed coordination (`harmony/harmonydb`, config layers, task queues, machine registry, pipeline state).
- Execution plane: many `curio` nodes running Harmony tasks (`harmony/harmonytask`, `tasks/*`) against local hardware/storage.
- Service plane: RPC/UI/public HTTP endpoints (`cmd/curio/rpc`, `web/*`, `cuhttp`, `market/http`, `pdp`).
- Main binary lifecycle (`cmd/curio run`) wires deps, starts task engine, starts RPC/UI, and optionally starts public HTTP services.
- Core architecture split by package families:
- `harmony/*`: scheduling primitives, DB abstraction, resource accounting.
- `tasks/*`: business workflows (sealing, proving, market, indexing, PDP, GC, balance, proofshare).
- `market/*`: deal protocols (mk12/mk20), retrieval, IPNI, libp2p, indexstore.
- `lib/*`: chain scheduler, storage/proof helpers, shared runtime utilities.

## Core execution model
- Every node registers resources and liveness in Harmony DB; scheduling is DB-coordinated, not leader-based.
- HarmonyTask poller model:
- periodic task scan;
- claim work by conditional ownership updates on unowned tasks (`owner_id IS NULL` compare-and-set semantics);
- run `CanAccept` + resource checks + storage claims;
- execute `Do` in goroutines;
- persist completion/failure history and retry state.
- Task contracts are explicit (`TaskInterface`): `Adder`, `CanAccept`, `Do`, `TypeDetails`.
- Work creation sources:
- listeners (`Adder`);
- follower triggers (`Follows`);
- periodic “IAmBored” work generation.
- Invariant: task logic must be idempotent at DB boundaries; duplicate prevention is task-defined (typically via unique constraints).
- Invariant: scheduling decisions are valid only when backed by DB ownership + resource claim checks.

## Pipelines and task coordination
- Sealing pipeline is state-machine-driven via DB (`sectors_sdr_pipeline`) and advanced by `tasks/seal` pollers/tasks.
- Canonical PoRep flow: SDR -> TreeD -> TreeC/TreeR -> Synthetic -> PreCommit msg -> PoRep -> Finalize -> MoveStorage -> Commit msg.
- Snap flow is separate but integrated: UpdateEncode -> UpdateProve -> UpdateBatch/Submit (+ UpdateStore for move storage).
- PoSt flow is chain-triggered:
- `lib/chainsched` emits tipset updates;
- WindowPoSt split into compute/recover/submit tasks;
- WinningPoSt tasks run per-epoch election conditions.
- Message execution is decoupled from pipeline tasks:
- tasks enqueue outgoing messages;
- `tasks/message` sender tasks assign nonce + sign + broadcast;
- watcher tasks confirm execution and feed completion state back to DB.
- Market ingestion is taskized:
- piece parking/download;
- commP/aggregation/publish workflows;
- indexing and IPNI announcement tasks;
- retrieval paths read indexed/unsealed data.
- PDP flow (alpha) is chain-contract-coupled:
- dataset/piece operations create DB intents + ETH txs;
- receipt/event watchers materialize dataset state;
- proving tasks generate proofs and submit contract calls.

## Data flow between subsystems
- Deal/storage onboarding path:
- client ingress (HTTP/libp2p/mk12/mk20) -> market tables + piece refs in Harmony DB;
- piece acquisition/parking -> sealing pipeline state;
- seal tasks produce commitments/proofs -> message queue -> on-chain confirmation;
- post-seal indexing and IPNI publication -> retrieval discoverability.
- Retrieval path:
- request to `/piece` or `/ipfs` -> denylist/filter checks -> index lookup (CQL indexstore + metadata) -> piece reader over storage -> HTTP response with cache semantics.
- Coordination/state path:
- config layers in `harmony_config` -> merged runtime config per node/layer stack;
- machine/task state in Harmony DB drives UI (`web/api/webrpc`) and operational controls.
- Observability path:
- runtime/task/DB metrics -> Prometheus;
- alert tasks evaluate cluster conditions -> external sinks (PagerDuty/Alertmanager/Slack).

## Chain / contract interaction points
- Filecoin chain integration (Lotus-compatible API):
- tipset notifications and callbacks (`lib/chainsched`);
- randomness/state queries for proofs and scheduling;
- Filecoin message creation/sending/waiting (`tasks/message`, `message_waits` tables).
- EVM/contract integration points:
- ETH client is used for market/PDP/proofshare-adjacent operations;
- `tasks/message/sender_eth.go` serializes nonce-safe ETH tx submission;
- `tasks/message/watch_eth.go` confirms receipts with confidence window;
- `pdp/contract/*` bindings connect backend tasks to on-chain contract methods/events.
- Invariant: chain/contract side-effects must not be treated as final until watcher-confirmed and persisted.
- Uncertain: Market 2.0 contract verification hooks are evolving; preserve existing interface boundaries rather than inlining policy into transport handlers.

## Operational and correctness constraints
- YugabyteDB availability is a hard dependency; loss of DB coordination halts useful cluster operation.
- A Curio cluster is assumed to serve one Filecoin network at a time.
- Storage locality matters:
- some tasks can run anywhere with resources;
- pipeline steps tied to specific cache/data paths cannot progress without path reachability.
- Cordon/uncordon controls scheduling admission, not task cancellation; in-flight tasks may finish while new claims stop.
- PoSt deadlines are strict and safety-critical; scheduling/resource changes that reduce PoSt throughput can cause faults/penalties.
- Nonce correctness is mandatory for both Filecoin and ETH senders; sender lock tables are part of correctness, not optimization.
- Security boundary assumes trusted access to Curio/Lotus/Yugabyte admin surfaces and machine-level access.

## Failure-sensitive paths
- Message-send lock or nonce-path issues can stall all outbound transactions for an address.
- DB schema/runtime mismatch during upgrades can break task creation/execution semantics.
- Pipeline pause + long maintenance can cause sector expiry and wasted sealing work.
- Chain notification interruptions can delay proof/message workflows; schedulers rely on timely head updates.
- Storage claim/path failures can create retry storms or blocked pipelines if not released/reset correctly.
- Indexing/retrieval divergence can make sealed data effectively undiscoverable even when physically present.
- Contract receipt/event parsing failures in PDP paths can leave chain state and DB state temporarily divergent.

## Architecture assumptions an AI must preserve
- Preserve DB-as-source-of-truth for cluster coordination; avoid introducing node-local authoritative state.
- Preserve task idempotency and transactional claim/update semantics.
- Preserve separation of concerns:
- compute tasks should not directly bypass message queue/watch paths;
- ingress handlers should enqueue/workflow, not embed long-running pipeline logic.
- Preserve resource-aware scheduling and fairness behavior; do not assume single-node execution.
- Preserve compatibility of schema migrations with runtime expectations.
- Preserve explicit confirmation model: enqueue -> send -> watch -> mark complete.
- Preserve multi-miner, multi-node assumptions in APIs/config; avoid single-miner shortcuts.
