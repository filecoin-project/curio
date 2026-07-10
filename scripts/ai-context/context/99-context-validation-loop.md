# Curio Context Validation Loop

## Purpose
- Verify that project context files remain factually aligned with current code and design intent.
- Detect contradictions, stale claims, and overconfident statements before they propagate into coding guidance.
- Provide a repeatable failure-to-update workflow for maintaining context quality over time.

## Inputs
- Current context set discovered from `scripts/ai-context/context/*.md` (excluding this file for claim validation).
- Existing validation-loop content (optional, for incremental refinement).
- New source material (optional): changed-file list, PR notes, diffs, design notes, or code excerpts.

## How to run
1. Discover context files dynamically and group by role using filename/title cues:
- overview/project
- preferences/collaboration
- architecture
- database/semantics
- patterns
- rolling/current/decisions/open-questions
2. Start a fresh AI instance with no prior Curio memory.
3. Provide discovered context files plus new source material (if any).
4. Execute baseline smoke tests first.
5. If new source material exists, execute change-aware smoke tests.
6. Record failures as one of:
- contradiction to code/source
- missing invariant
- stale claim
- uncertainty not labeled
7. Update the target context file(s), then rerun the same failing tests.

## Baseline smoke tests
## B01 Task-claim correctness semantics
- Applies to: architecture and database-semantics context files.
- Ask this to a fresh AI: How is task ownership claimed safely in Curio, and what should correctness depend on?
- Expected anchors: conditional ownership update semantics (`owner_id IS NULL` compare-and-set), idempotent retries, no lock-semantic dependence as correctness source.
- Red flags: lock-only explanations; omission of CAS/idempotency; node-local ownership claims.
- Evidence to verify against: scheduler/task claim query shape and ownership reset/retry behavior in task-engine code.
- If failed, update: architecture and database-semantics context files; relevant common-patterns file if pattern wording is wrong.
- Confidence: settled

## B02 Chain side-effect finalization rule
- Applies to: architecture, database-semantics, and common-patterns context files.
- Ask this to a fresh AI: When can Curio treat chain/contract side effects as final business state?
- Expected anchors: enqueue -> send -> watch/confirm -> finalize; send success alone is insufficient.
- Red flags: treating broadcast success as completion; missing watcher/receipt persistence.
- Evidence to verify against: sender/watch task flow and message wait tables in code/schema.
- If failed, update: architecture/common-patterns/database-semantics files.
- Confidence: settled

## B03 Nonce-ordering invariants
- Applies to: database-semantics and common-patterns context files.
- Ask this to a fresh AI: What invariants must be preserved when modifying message sending?
- Expected anchors: per-sender serialization path, sender+nonce uniqueness constraints, lock-table behavior, wait-table confirmation flow.
- Red flags: recommendations that weaken nonce uniqueness or bypass wait tables.
- Evidence to verify against: message sender/watch code and message table indexes/constraints.
- If failed, update: database-semantics and common-patterns files.
- Confidence: settled

## B04 Durable vs mutable data separation
- Applies to: database-semantics and common-patterns context files.
- Ask this to a fresh AI: Which records are durable ledger-like state vs mutable workflow state?
- Expected anchors: durable business records separated from transient pipelines; GC only removes terminal workflow rows.
- Red flags: merging durable and transient lifecycle assumptions; deleting canonical record history.
- Evidence to verify against: market/pipeline table roles and GC task behavior.
- If failed, update: database-semantics/common-patterns/recent-decisions files.
- Confidence: likely

## B05 Piece lifecycle indirection
- Applies to: database-semantics and common-patterns context files.
- Ask this to a fresh AI: How should long-lived references to staged pieces be modeled?
- Expected anchors: reference through piece-ref indirection; preserve cleanup and shared-lifecycle semantics.
- Red flags: direct durable pointers to staging rows without ref indirection.
- Evidence to verify against: piece cleanup logic and piece-ref usage in market/PDP paths.
- If failed, update: database-semantics/common-patterns files.
- Confidence: settled

## B06 Uncertainty labeling discipline
- Applies to: rolling subsystem, recent decisions, and open-questions context files.
- Ask this to a fresh AI: Which areas are explicitly uncertain or still evolving?
- Expected anchors: uncertain areas are named, scoped, and treated as verify-before-change zones.
- Red flags: absolute claims in known-moving areas; uncertain areas omitted.
- Evidence to verify against: rolling context and open-questions content.
- If failed, update: rolling and open-questions files.
- Confidence: likely

## Change-aware smoke tests
- Generate these only when new source material is provided.
- If no source material is provided: record `Delta tests not generated (no new material)` and skip this section.
- Delta test generation rules:
- Extract changed subsystems from file paths, modules, table names, or design-note topics.
- For each changed subsystem, create 1-3 tests using the template below.
- Require each delta test to name at least one invariant that must remain true after the change.

## DYN-Template Changed-subsystem invariant check
- Applies to: context files whose role matches the changed subsystem.
- Ask this to a fresh AI: For `<changed subsystem>`, what changed and what invariants must still be preserved?
- Expected anchors: subsystem-specific durable invariants + explicit note of changed behavior scope.
- Red flags: changelog-only summaries; no invariant mention; contradiction with supplied diffs/notes.
- Evidence to verify against: provided diff/PR/design material for the subsystem.
- If failed, update: the context file(s) owning that subsystem and any cross-cutting file that repeats the contradicted claim.
- Confidence: still evolving

## Code-shape probes
## C01 Scheduler claim implementation shape
- Applies to: architecture and common-patterns context files.
- Ask this to a fresh AI: How would you implement task claiming for Curio’s scheduler?
- Expected anchors: candidate selection, conditional ownership update, rows-affected gating, retry/idempotency, no lock-semantic dependency.
- Red flags: lock-heavy design without CAS safeguards; single-node assumptions.
- Evidence to verify against: task-engine claim and completion/retry code paths.
- If failed, update: architecture/common-patterns/database-semantics files.
- Confidence: settled

## C02 Chain-transaction workflow implementation shape
- Applies to: architecture, database-semantics, and common-patterns context files.
- Ask this to a fresh AI: How would you add a new chain-facing operation in Curio?
- Expected anchors: intent persistence, sender path, wait/watch persistence, terminal state update, cleanup path.
- Red flags: handler-triggered direct side effects without durable wait/confirmation path.
- Evidence to verify against: existing send/watch/task/table patterns.
- If failed, update: architecture/common-patterns/database-semantics files.
- Confidence: likely

## Contradiction checks
- Cross-file contradiction pass:
- Compare architecture claims vs database semantics (coordination mechanism, ordering source, lifecycle).
- Compare current-subsystems/recent-decisions/open-questions vs stable files for drift.
- Compare coding-preferences vs recommended implementation patterns for conflict.
- Contradiction rule: if two files disagree, prefer source-backed statement and downgrade/mark uncertainty in the weaker file.

## Failure handling and update targets
- Failure triage:
- architectural contradiction -> update architecture/common-patterns first.
- schema/invariant contradiction -> update database-semantics first.
- workflow or lifecycle contradiction -> update common-patterns and relevant rolling file.
- confidence mismatch in moving areas -> update rolling/open-questions/recent-decisions.
- Update target selection:
- Choose by role, not fixed filename: update the file that owns the contradicted claim type (architecture, DB semantics, patterns, rolling status, decisions, open questions).
- After each update:
- rerun only failed tests first;
- then rerun full baseline before accepting.

## Uncertainty register
- PDP model primacy across legacy vs newer dataset flows remains deployment-sensitive.
- MK2.0 product policy boundaries and publication/index behavior remain active-change areas.
- Batching heuristics and timeout policies are operationally tuned and should not be treated as fixed architecture.
