# Curio AI Context Index

## Runtime context files
Use files from [`context/`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/context) as runtime briefing inputs.

| File | Purpose | Load when |
| --- | --- | --- |
| `00-project-overview.md` | Stable project-wide overview | Any non-trivial chat |
| `01-architecture.md` | Service/task/pipeline architecture | Backend workflow or design work |
| `03-database-semantics.md` | DB meaning/invariants/coordination | SQL/schema/pipeline-state work |
| `04-coding-preferences.md` | Collaboration and coding style defaults | Always first |
| `05-common-patterns.md` | Preferred implementation patterns | Refactor/new feature alignment |
| `06-current-subsystems.md` | Active and changing subsystem status | Work touching moving areas |
| `07-recent-decisions.md` | Durable recent decisions | Design tradeoffs / consistency checks |
| `08-open-questions.md` | Explicit unresolved areas | When assumptions might be unstable |
| `09-chat-bootstrap.md` | Chat startup recipe | Starting a new chat/session |
| `99-context-validation-loop.md` | Context accuracy validation loop | Updating/reviewing context quality |

## Prompt files
Use files from [`prompt/`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/prompt) for context maintenance (not runtime coding chats).

| File | Use for |
| --- | --- |
| `00`-`09` matching prompts | Generate or refresh corresponding context files |
| `util-incremental-update.md` | Update an existing context file with new material |
| `util-recompress.md` | Rewrite a noisy file into compact durable form |
| `99-context-validation-loop.md` | Regenerate dynamic validation loop content |

## Recommended bundles
- Stable bundle: `04`, `00`, `01`, `03`, `05`
- Rolling bundle: `06`, `07`, `08`

Use stable by default; add rolling when task-relevant.

## Update source
- For rolling updates, inspect commit deltas on `main` since the target file's last update commit.
