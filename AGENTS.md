# Curio AI Context Entry Point

This repository uses modular AI context files under [`scripts/ai-context/`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context).

## Start Here
- Read [`scripts/ai-context/README.md`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/README.md) for purpose and workflow.
- Use [`scripts/ai-context/index.md`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/index.md) to choose which files to load for a task.

## Runtime Context (Coding/Design Chats)
- Load context files from [`scripts/ai-context/context/`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/context).
- Default context loading (for every new coding/design chat):
1. `04-coding-preferences.md`
2. `00-project-overview.md`
3. `01-architecture.md`
4. `05-common-patterns.md`
- Conditional context (load only when relevant):
- DB/schema/pipeline-state work:
- `03-database-semantics.md`
- Active or moving subsystem work:
- `06-current-subsystems.md`
- `07-recent-decisions.md`
- `08-open-questions.md`
- Runtime rules:
- Do not summarize context files unless asked.
- If task-specific input conflicts with older context, call out the conflict and follow task-specific input.
- Treat prompt files under `scripts/ai-context/prompt/` as maintenance-only, not runtime context.

## Prompt Templates (Context Maintenance)
- Prompt templates are in [`scripts/ai-context/prompt/`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/prompt).
- Prompt files are for creating/updating context docs, not for normal runtime coding chats.

## Update Model
- Context is maintained on `main` via PRs.
- Rolling files should be updated from commit deltas on `main` (commit-based, not time-based).
- Use [`scripts/ai-context/context/99-context-validation-loop.md`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/context/99-context-validation-loop.md) after major context updates.
