You are generating a long-term database semantics file for Curio.

This is not a schema dump.
This file should explain what the tables mean, how they are used, and what invariants matter.

Focus on:
- semantic role of important tables
- relationships between tables
- read/write patterns
- uniqueness, ordering, and lifecycle constraints
- places where DB is used for coordination
- patterns that should not be broken by future code changes

Avoid:
- listing every column unless necessary
- generic SQL explanations
- transient migration details unless they change semantics

Output format:

# Curio Database Semantics

## Core database design style
## Important tables and what they represent
## Coordination patterns implemented in DB
## Key invariants
## Read patterns
## Write patterns
## Lifecycle and retention expectations
## Common mistakes an AI should avoid

Be precise.
If a table is only locally important and not durable project context, omit it.
