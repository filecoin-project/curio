# Curio Open Questions

## PDP canonical model convergence
- Why it matters: PDP has legacy proofset/service tables and newer `pdp_data_set*`/`pdp_pipeline` flows; incorrect assumptions break feature work and cleanup logic.
- Current known state: Both models are present and active in code/schema; relative primacy appears deployment-dependent.
- What future AI should do: Treat model primacy as uncertain, verify target deployment path before schema or workflow changes.
- Risk if wrong: Mixed-table updates, orphaned state, or incorrect API behavior.
- Confidence: still evolving

## MK2.0 product policy boundaries
- Why it matters: Retrieval/indexing/IPNI behavior is product-driven in MK2.0 and directly affects lifecycle and cleanup semantics.
- Current known state: Product/data-source controls and pipelines are expanding; policy details continue to move.
- What future AI should do: Preserve existing product gates and validate policy assumptions in code paths before broad refactors.
- Risk if wrong: Incorrect announce/index behavior or incompatible deal lifecycle transitions.
- Confidence: still evolving

## Batching heuristics and timeout policy
- Why it matters: Sealing precommit/commit batching impacts throughput, deadlines, and failure behavior.
- Current known state: Batching logic and timeout behavior are actively tuned.
- What future AI should do: Preserve stage invariants first; treat heuristic constants as operational policy, not fixed architecture.
- Risk if wrong: Deadline misses, retry storms, or reduced sealing performance.
- Confidence: still evolving

## IPNI publication policy per context/product
- Why it matters: Announcement/removal behavior spans MK12, MK20, and PDP; wrong assumptions affect discoverability and cleanup.
- Current known state: IPNI task/idempotency/skip semantics exist, but cross-product publication policy still moves.
- What future AI should do: Keep ad-chain and dedupe invariants intact and verify product-specific publication expectations before changes.
- Risk if wrong: Stale ads, missing ads, or mismatched retrieval visibility.
- Confidence: still evolving

## Proofshare long-term interface boundaries
- Why it matters: Proofshare touches message/balance/task flows and can leak instability into core paths if coupled incorrectly.
- Current known state: Feature area is active with ongoing schema/pipeline changes.
- What future AI should do: Isolate proofshare changes from core sealing/message correctness assumptions.
- Risk if wrong: Regressions in core operational reliability.
- Confidence: still evolving

