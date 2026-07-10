# Curio Current Subsystems

## Harmony scheduling and coordination (`harmonytask`, `harmonydb`, `resources`)
- Current status: Core control-plane foundation used by all major workflows.
- Why it matters: Task ownership, retries, machine liveness, and cluster-wide execution semantics all depend on it.
- Recent changes: Ongoing operational controls and coordination hardening (machine maintenance/restart flags, singleton and retry behavior, config history support).
- Stability level: stable
- Constraints future AI should know: Preserve DB-first coordination, idempotent task transitions, and conditional ownership updates; do not introduce node-local authority.
- Open uncertainties: None significant at architecture level; tuning may continue.

## Sealing / Snap / Unseal pipelines (`tasks/seal`, `tasks/snap`, pipeline tables)
- Current status: High-throughput production path with active improvements to batching, padding, and edge-case handling.
- Why it matters: Core SP revenue path; failures or regressions directly impact sector onboarding and correctness.
- Recent changes: Commit/precommit batching controls, ingest/padding fixes, unseal-target lifecycle handling, and snap pipeline correctness adjustments.
- Stability level: evolving
- Constraints future AI should know: Preserve stage-gated pipeline semantics (`task_id_*`, `after_*`, failure fields), message wait confirmation flow, and safe retry reopening logic.
- Open uncertainties: Exact batching heuristics and failure-recovery policies are still being tuned.

## Chain message send/watch (Filecoin + ETH)
- Current status: Mission-critical shared subsystem used by sealing, market, PDP, and ops actions.
- Why it matters: Correct nonce ordering and confirmation tracking gate all chain-side effects.
- Recent changes: ETH send/watch path matured; wait-table indexing and timestamping improved for operational visibility.
- Stability level: stable
- Constraints future AI should know: Preserve sender lock tables, sender+nonce uniqueness semantics, and enqueue/send/watch/finalize workflow.
- Open uncertainties: Confirmation policy thresholds/confidence windows may be adjusted by operators.

## Market MK1.2 and MK2.0 (`market/*`, `tasks/storage-market`, MK20 tables)
- Current status: MK1.2 remains operational baseline; MK2.0 is active development focus and expanding product surface.
- Why it matters: Ingest, retrieval, indexing, upload/download orchestration, and deal lifecycle all flow here.
- Recent changes: MK2.0 schema/workflow expansion (upload waiting, download pipeline, chunk lifecycle, product/data-source controls, piece cleanup integration).
- Stability level: evolving
- Constraints future AI should know: Keep durable deal records separate from mutable pipelines; preserve piece lifecycle through `parked_piece_refs`; keep IPNI/indexing coupling consistent.
- Open uncertainties: Product-level policy behavior in MK2.0 is still moving.

## PDP and contract-coupled flows (`pdp/*`, PDP tables, ETH contract bindings)
- Current status: Active but still treated as early/alpha surface.
- Why it matters: Drives dataset operations, piece add/remove, proving-period actions, and contract event reconciliation.
- Recent changes: Data-set and piece workflows integrated with MK2.0, plus public-service defaults and continued watcher/task evolution.
- Stability level: experimental
- Constraints future AI should know: Preserve intent-table -> tx send -> watcher confirmation -> canonical state materialization pattern.
- Open uncertainties: Relative primacy between legacy PDP proofset tables and newer `pdp_data_set*` model remains deployment-dependent (**uncertain**).

## IPNI publication and retrieval indexing (`market/ipni`, `ipni*`, indexstore YCQL)
- Current status: Active production dependency with ongoing operational fixes.
- Why it matters: Discoverability and retrieval depend on accurate index + ad publication state.
- Recent changes: IPNI task/idempotency refinements, skip semantics, chunk uniqueness extensions, and piece/payload announcement split support.
- Stability level: evolving
- Constraints future AI should know: Preserve ad-chain/head semantics, task dedupe behavior, and YSQL lifecycle coupling with YCQL index rows.
- Open uncertainties: Publication policy per product/context is still moving in MK2.0/PDP-adjacent flows.

## Web UI / RPC operational surfaces (`web/*`, `web/api/webrpc`)
- Current status: Actively updated operational interface with expanding market/PDP visibility.
- Why it matters: Primary operator lens for pipeline state, deal status, maintenance controls, and diagnostics.
- Recent changes: Expanded MK2.0/PDP views, IPNI operational pages, and more workflow status surfaces.
- Stability level: evolving
- Constraints future AI should know: Keep handlers/read-models DB-backed and status-oriented; avoid embedding long-running workflow logic in UI endpoints.
- Open uncertainties: UI flows and page structure remain under active iteration.

## Proofshare and auxiliary service surfaces (`tasks/proofshare`, balance manager, related tables)
- Current status: Present and actively touched, but still treated as non-core/experimental relative to sealing.
- Why it matters: Adds additional market/service workflows and impacts message/balance interactions.
- Recent changes: Multiple proofshare schema and pipeline refinements, autosettle and payment-stat related updates.
- Stability level: experimental
- Constraints future AI should know: Preserve isolation from core sealing correctness paths; keep retry and queue behavior explicit.
- Open uncertainties: Product maturity and long-term interface boundaries are still moving (**uncertain**).

