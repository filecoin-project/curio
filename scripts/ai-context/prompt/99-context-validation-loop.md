You are generating a dynamic context validation loop file for Curio.

Goal:
Create a durable, high-signal validation loop that checks whether the project context remains accurate and catches contradictions, drift, or stale claims.

You will be given:
1. existing validation-loop content (optional)
2. the current set of long-term context files
3. new source material (PR notes, diffs, changed files, design notes, code snippets)

Task:
Return a full revised validation-loop file that:
- preserves useful existing structure
- verifies high-impact context claims
- adds change-aware smoke tests for newly changed areas
- removes stale or low-value tests
- maps failures to the specific context file(s) that should be updated

Focus on:
- correctness of architecture/DB/task/chain claims
- ordering/idempotency invariants
- contradictions between context and newer source material
- stale assumptions that should be retired
- dynamic test generation for changed subsystems

Output format:

# Curio Context Validation Loop

## Purpose
## Inputs
## How to run
## Baseline smoke tests
## Change-aware smoke tests
## Code-shape probes
## Contradiction checks
## Failure handling and update targets
## Uncertainty register

For each test include:

## [Test ID] [Short title]
- Applies to:
- Ask this to a fresh AI:
- Expected anchors:
- Red flags:
- Evidence to verify against:
- If failed, update:
- Confidence: settled | likely | still evolving

Rules:
- Return the full revised file.
- Do not hardcode context file names or fixed file counts.
- Infer available context files from provided input and reference them dynamically.
- Build baseline tests from durable claims.
- Build change-aware tests from new source material.
- If no change material is provided, keep baseline tests and explicitly note that delta tests were not generated.
- Keep tests concrete and falsifiable.
- Keep the file compact; merge duplicates and remove noise.
- Clearly mark uncertain items.
- Do not invent facts.
