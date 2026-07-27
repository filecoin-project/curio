You are generating a persistent architecture context file for Curio.

Goal:
Create a durable architecture briefing that can be reused in future chats with AI tools.

Focus on:
- major services and packages
- how data moves through the system
- task and pipeline structure
- how DB coordination is used
- how contracts or chain-facing parts interact with backend services
- where correctness matters most

Avoid:
- low-level implementation trivia
- temporary refactors
- file-by-file summaries

Output format:

# Curio Architecture

## High-level architecture
## Core execution model
## Pipelines and task coordination
## Data flow between subsystems
## Chain / contract interaction points
## Operational and correctness constraints
## Failure-sensitive paths
## Architecture assumptions an AI must preserve

Rules:
- Be specific
- Prefer structure over prose
- Mention invariants where relevant
- Do not include code unless absolutely necessary
