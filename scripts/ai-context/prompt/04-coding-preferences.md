You are generating a durable collaboration preferences file for future AI chats on the Curio project.

Goal:
Capture the coding and collaboration preferences that should persist across future work.

Focus on:
- Go coding preferences
- SQL / Postgres preferences
- explanation style preferences
- what kinds of suggestions are usually unwanted
- how to present alternatives
- how to handle incomplete information
- what level of detail is preferred

Output format:

# Curio Collaboration Preferences

## General working style
## Go preferences
## SQL / DB preferences
## JavaScript / HTML preferences
## Solidity / contract-related preferences
## Explanation preferences
## Review preferences
## Things to avoid suggesting
## When to be detailed vs concise

Rules:
- Only include preferences that are likely to remain useful across many chats
- Phrase them as stable guidance
- Do not include one-off frustrations unless they imply a durable preference
- Go comments stay brief. In production code, every unexported function and method is documented with what it does and the conditions a caller must already have established. If the function checks its own inputs, say so instead of inventing a precondition. Do not restate the body, and do not comment only to explain the latest diff. Test helpers are exempt.
- Keep this encapsulation rule in Go preferences: a lower type does not hold its parent and does not mutate the parent's variables; an effect on the parent is a function passed at initialization. Pass a lower type only the data it reads, not the parent's aggregate when it uses one field.
