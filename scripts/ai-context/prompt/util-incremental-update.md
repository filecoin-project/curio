You are updating an existing long-term AI context file for Curio.

You will be given:
1. the current file content
2. new source material such as diffs, PR notes, code, or design notes

Task:
Update the file so it remains a high-quality persistent context document for future chats.

Priorities:
- preserve existing useful structure
- add new information only if it is durable or materially important
- remove stale or contradicted information
- do not turn the file into a changelog
- keep it compact and reusable

Output requirements:
- return the full revised file
- keep the same top-level structure unless there is a strong reason to change it
- clearly mark anything that is still uncertain
- do not invent details

Before writing, silently classify each new item as:
- stable long-term context
- rolling near-term context
- noise
Only include the first two.
