# Curio AI Context Entry Point

This repository uses modular AI context files under [`scripts/ai-context/`](scripts/ai-context/).

## Start Here
- Read [`scripts/ai-context/README.md`](scripts/ai-context/README.md) for purpose and workflow.
- Use [`scripts/ai-context/index.md`](scripts/ai-context/index.md) to choose which files to load for a task.

## Runtime Context (Coding/Design Chats)
- Load context files from [`scripts/ai-context/context/`](scripts/ai-context/context/).
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
- Never add machine-specific local absolute paths or local-editor URIs to repository files; use repo-relative paths only.

## Required Verification Before Finalizing Code Edits
- Trigger: run this only when the user indicates the coding pass is done (for example: "finalize").
- For Go/backend/codegen-affecting edits, run these from repo root:
1. `LANG=en-US FFI_USE_OPENCL=1 make gen`  # OpenCL flag avoids CUDA build dependency during codegen
2. `golangci-lint run -v --timeout 15m --concurrency 4`
- `make gen` is the canonical generation/import-format path; do not run separate formatting passes by default.
- Do not rerun expensive checks unless code changed after the last verify run or the user asks for rerun.
- Do not claim checks passed unless they were actually run.
- If any check cannot be run (missing tool/dependency/environment/time), state that explicitly with the failing command.

## Prompt Templates (Context Maintenance)
- Prompt templates are in [`scripts/ai-context/prompt/`](scripts/ai-context/prompt/).
- Prompt files are for creating/updating context docs, not for normal runtime coding chats.

## Update Model
- Context is maintained on `main` via PRs.
- Rolling files should be updated from commit deltas on `main` (commit-based, not time-based).
- Use [`scripts/ai-context/context/99-context-validation-loop.md`](scripts/ai-context/context/99-context-validation-loop.md) after major context updates.
