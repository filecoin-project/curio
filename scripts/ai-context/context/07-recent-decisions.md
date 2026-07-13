# Curio Recent Decisions

## Use Modular AI Context Files In-Repo
- Decision: Maintain numbered context documents in-repo under `scripts/ai-context/context/` instead of ad hoc chat memory.
- Rationale: Keeps durable project guidance versioned with code and reusable across chats/tools.
- Rejected alternatives: Re-explaining Curio from scratch each chat; one monolithic “everything” document.
- Implications: Future updates should modify the relevant context file(s) and commit them alongside code when semantics change.
- Confidence: settled

## Separate Runtime Context From Maintenance Prompts
- Decision: Treat `scripts/ai-context/prompt/` as offline generation/update templates, not runtime chat input.
- Rationale: Runtime chats should be lean; prompts are “context compiler” tools for maintenance.
- Rejected alternatives: Attaching all prompts to every coding chat.
- Implications: Future AI chats should consume context files; prompt files are used only when creating/updating context docs.
- Confidence: settled

## Use Selective Context Loading Per Task
- Decision: Start new chats with a small relevant subset of context files, not the entire pack/repo.
- Rationale: Reduces noise/token load and improves focus on task-specific correctness.
- Rejected alternatives: Always loading all context files and full repository text.
- Implications: Choose files by task type (for example DB tasks include DB semantics; architecture-heavy tasks include architecture/common-patterns).
- Confidence: settled

## Keep Stable And Rolling Context Bundles
- Decision: Maintain a stable core bundle and a rolling bundle.
- Rationale: Stable docs provide long-lived defaults; rolling docs capture active churn without polluting core files.
- Rejected alternatives: Single uniform bundle for every chat.
- Implications: Core files should remain durable; rolling files (`06`, `07`, optional future open-questions file) should be updated more frequently.
- Confidence: likely

## Enforce Compact, Durable Context Hygiene
- Decision: Context files should capture durable constraints/invariants/preferences, not transcript or changelog noise.
- Rationale: Compact docs remain usable in fresh chats and degrade less over time.
- Rejected alternatives: Giant transcripts, file-by-file dumps, forever changelogs.
- Implications: Use incremental update and periodic recompression passes; remove stale/contradicted entries and mark uncertainty explicitly.
- Confidence: settled

## Do Not Assume Row-Lock Semantics For Task Claim Correctness
- Decision: Scheduler/task-claim correctness should be described and designed around conditional ownership updates (`owner_id IS NULL` compare-and-set), not row-lock assumptions.
- Rationale: Current Yugabyte behavior makes lock-based claims unreliable as a correctness story.
- Rejected alternatives: Treating `FOR UPDATE SKIP LOCKED` semantics as correctness-critical.
- Implications: Future scheduler changes and context docs must preserve CAS/idempotency invariants and avoid lock-dependent claims.
- Confidence: settled
