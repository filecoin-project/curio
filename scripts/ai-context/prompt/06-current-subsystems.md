You are generating a rolling project context file for Curio.

This file is allowed to change more often than the core files.
Its job is to tell future AI chats what parts of the system are currently active, changing, or under attention.

Focus on:
- currently active subsystems
- recent major work areas
- areas being redesigned
- areas that are stable
- areas where assumptions are still moving

Output format:

# Curio Current Subsystems

For each subsystem include:

## [Subsystem name]
- Current status:
- Why it matters:
- Recent changes:
- Stability level: stable | evolving | experimental
- Constraints future AI should know:
- Open uncertainties:

Rules:
- Keep this current and compact
- Prefer present relevance over historical completeness
- Mark uncertain or moving pieces clearly
