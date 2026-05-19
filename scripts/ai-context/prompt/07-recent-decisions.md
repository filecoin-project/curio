You are updating a durable engineering decisions log for Curio.

Input will contain recent PRs, design notes, or discussion summaries.

Your task:
Extract only decisions that future AI chats should know when helping with coding or design.

For each decision include:
- decision
- why it was chosen
- what alternatives were rejected
- what future changes must respect

Output format:

# Curio Recent Decisions

## [Short decision title]
- Decision:
- Rationale:
- Rejected alternatives:
- Implications:
- Confidence: settled | likely | still evolving

Rules:
- Do not include every discussion point
- Only keep decisions that materially affect future work
- Be terse and exact
