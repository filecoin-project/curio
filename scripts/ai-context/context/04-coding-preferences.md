# Curio Collaboration Preferences

## General working style
- Be pragmatic and execution-focused: inspect code first, then implement.
- Prefer direct, concrete reasoning over broad theory.
- Preserve existing architecture boundaries unless change is explicitly requested.
- Treat Curio as clustered software; avoid single-node assumptions.
- State assumptions and uncertainty explicitly when facts are incomplete.

## Go preferences
- Keep changes idiomatic and straightforward; optimize for maintainability over cleverness.
- Preserve task idempotency and DB-backed coordination semantics in `harmonytask` and `tasks/*`.
- Use explicit error handling and wrap errors with context.
- Keep comments sparse and high-signal (non-obvious invariants, coordination logic, edge cases).
- Avoid unnecessary abstractions and wide refactors when scoped fixes are sufficient.

## SQL / DB preferences
- Treat Yugabyte YSQL state as part of Curio’s runtime protocol, not just storage.
- Preserve transactional semantics for multi-table state transitions.
- Preserve uniqueness/locking invariants (especially sender+nonce semantics and task ownership patterns).
- Prefer compare-and-set style updates for pipeline progression and retries.
- Make schema changes through migrations and keep runtime code aligned with schema semantics.
- Keep YSQL workflow changes consistent with YCQL indexstore lifecycle when piece/index behavior is affected.

## JavaScript / HTML preferences
- Work within existing Curio web patterns (ES modules + Lit components) unless asked otherwise.
- Prefer clear operational UI behavior over visual churn.
- Keep frontend changes targeted; avoid introducing new framework/tooling complexity by default.
- Preserve existing design/system conventions on touched pages.

## Solidity / contract-related preferences
- Keep chain/contract interactions mediated through backend services/tasks and DB-tracked transaction lifecycle.
- Do not treat contract effects as final until watcher-confirmed and persisted.
- Preserve ETH transaction ordering and lock semantics used by sender/watch paths.
- When suggesting contract-facing changes, include backend, watcher, and DB coordination impact.

## Explanation preferences
- Use concise, factual, actionable language.
- Lead with outcome and impact; include rationale only as needed.
- Present assumptions and uncertainties explicitly.
- Provide one recommended path first; include alternatives only when materially useful.

## Review preferences
- Findings first, ordered by severity, with precise file/line references.
- Prioritize correctness, regressions, invariants, and missing tests over style nits.
- Call out operational risk areas clearly (coordination, retries, chain-facing behavior, migration risk).
- If no findings, state that directly and note remaining risk/test gaps.

## Things to avoid suggesting
- Large speculative rewrites when targeted changes solve the problem.
- Replacing DB coordination with node-local state or ad hoc locking.
- Destructive git cleanup or reverting unrelated local changes.
- Broad formatting/refactor churn unrelated to requested behavior.
- Generic “best practices” advice without project-specific grounding.

## When to be detailed vs concise
- Be concise for routine edits, status updates, and straightforward bug fixes.
- Be detailed for coordination-sensitive changes: scheduler/task semantics, DB invariants, chain messaging, and migrations.
- Increase detail when changes are high-impact, hard to verify, or involve uncertain assumptions.
- Include verification/testing detail whenever behavior or safety-critical paths are modified.
