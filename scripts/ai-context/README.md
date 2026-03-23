# Curio AI Context

## What this is
`scripts/ai-context/` is Curio's repository-native AI briefing system.

It has two parts:
- `context/`: reusable project briefings for runtime coding/design chats.
- `prompt/`: templates for generating or updating context files.

This directory is intended for both humans and AIs.

## Directory layout
- [`context/`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/context): project context documents (`00`-style numbered files).
- [`prompt/`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/prompt): context maintenance prompts (`00`-style and utility prompts).
- [`index.md`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/index.md): quick lookup for which file to load/use.

## How to start a new chat
Use a small relevant subset of `context/` files, not the full pack.

Suggested baseline:
1. `04-coding-preferences.md`
2. `00-project-overview.md`
3. `01-architecture.md`
4. `03-database-semantics.md`
5. `05-common-patterns.md`

Add when needed:
- `06-current-subsystems.md` for active moving areas.
- `07-recent-decisions.md` for decision-sensitive work.
- `08-open-questions.md` when uncertainty may affect design choices.

## Context maintenance model
- Source of truth branch: `main`.
- Updates happen through PRs.
- Rolling files should use commit-based deltas from `main`.

Commit-based update flow (example):
1. Pick target context file (for example `context/07-recent-decisions.md`).
2. Find baseline commit = last commit that touched that file.
3. Review commits/diffs from baseline to current `main`.
4. Update only durable or materially relevant content.

Useful commands:
```bash
# from repo root
TARGET=scripts/ai-context/context/07-recent-decisions.md
BASE=$(git log -1 --format=%H -- "$TARGET")

# inspect what changed on main since that context file was last updated
git log --oneline "${BASE}..main"
git diff --name-only "${BASE}..main"
git diff "${BASE}..main"
```

## Minimal edit checklist
Use this checklist for edits to `context/` and `prompt/`:
- Preserve the existing top-level structure unless there is a strong reason to change it.
- Do not invent details; if uncertain, label uncertainty explicitly.
- Keep context compact and reusable; remove stale/contradicted claims.
- For rolling files (`06`, `07`, `08`), include commit-based input from `main` (commit range used).

## Which prompt to use
- Create/regenerate a specific context file: corresponding prompt in `prompt/` (same number/name when present).
- Incremental update of an existing context file: `prompt/util-incremental-update.md`.
- Recompress/clean noisy context file: `prompt/util-recompress.md`.
- Regenerate validation loop: `prompt/99-context-validation-loop.md`.

## Validation
After major context updates, run the process in:
- [`context/99-context-validation-loop.md`](/Users/lexluthr/github/filecoin-project/curio/scripts/ai-context/context/99-context-validation-loop.md)

This catches contradictions, stale claims, and missing invariants.

For a quick local sanity check (headings + broken links):
```bash
bash scripts/ai-context/check-context.sh
```
