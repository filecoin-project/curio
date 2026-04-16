# Curio Chat Bootstrap

## Purpose
- Define a lightweight, repeatable way to start new AI chats without overloading context.
- Curio is a clustered Filecoin Storage Provider runtime replacing lotus-miner, coordinated through YugabyteDB. Single binary, multi-node, multi-miner.
- Define a lightweight, repeatable way to start new AI chats without overloading context.

## Core rule
- Do not attach all prompts, all context files, and full repo text by default.
- Use: minimal briefing + task-relevant context + task input.

## Preferred load order
1. `04-coding-preferences.md`
2. `00-project-overview.md`
3. `01-architecture.md`
4. `03-database-semantics.md`
5. `05-common-patterns.md`
6. `06-current-subsystems.md` (only when active churn matters)
7. `07-recent-decisions.md` (only when decision history is relevant)
8. `08-open-questions.md` (when uncertainties are likely to affect the task)

## Stable vs rolling bundle
- Stable bundle: `00`, `01`, `03`, `04`, `05`.
- Rolling bundle: `06`, `07`, `08`.
- Default to stable; add rolling only when task-relevant.

## Task-to-context mapping
- DB semantics/migrations: `04` + `03` (+ `01` if pipeline coupling exists).
- Pipeline/task engine work: `04` + `01` + `05` (+ `06` when currently evolving).
- API/handler/product behavior: `04` + `01` + `05` (+ `07` for recent directional decisions).
- Operational triage/current state: `04` + `06` + `07` (+ `08` for known uncertainty zones).

## Starter message template
Read the attached Curio context files first and treat them as the project briefing for this conversation.
Use them to align design and code with project conventions.
If task-specific material conflicts with older context, prefer task-specific material and call out the conflict explicitly.
Do not summarize the context files unless asked.
Wait for the actual task after reading them.

## Maintenance rule
- Prompt files in `scripts/ai-context/prompt/` are maintenance templates, not runtime chat inputs.
