You are generating a rolling open-questions context file for Curio.

This file is allowed to change frequently.
Its job is to capture unresolved technical questions and moving assumptions that future AI chats must handle carefully.

Focus on:
- unresolved architecture or subsystem questions
- areas where multiple models coexist
- policy or behavior boundaries still being refined
- assumptions that require verification before code changes
- risks if assumptions are wrong

Output format:

# Curio Open Questions

For each item include:

## [Question title]
- Why it matters:
- Current known state:
- What future AI should do:
- Risk if wrong:
- Confidence: settled | likely | still evolving

Rules:
- Keep this compact and current.
- Prefer open technical questions over historical narration.
- Mark uncertain or moving pieces clearly.
- Do not invent missing details.
