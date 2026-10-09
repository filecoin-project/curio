---
description: Let your AI agent help you earn on Filecoin using your spare hardware and storage capacity.
---

# Filecoin PDP Agent

Put your AI agent to work earning for you. With Filecoin PDP Agent, it can use your spare hardware and storage capacity to run a Curio PDP provider and earn storage payments on your behalf. You choose the resources and funds to allocate; your agent handles deployment and ongoing operation.

Start with a fresh machine or an existing provider on Linux, macOS or another compatible host. Your agent follows the documentation for your Curio release, handles installation and configuration, and keeps track of provider health and updates.

## Full agent control and private keys

{% hint style="warning" %}
**This skill is designed for full agent control, including access to the private keys and credentials Curio uses.** The agent can read and use the provider's signing key, sign transactions, and spend funds controlled by that key. Choose this option only for a provider environment, keys and funds you intend to place under agent control.
{% endhint %}

Treat the agent as a trusted administrator of the provider. It can install and upgrade software, change configuration, manage services and storage, and create or import the dedicated provider wallet. On an existing shared cluster, administrative access can also expose other keys and credentials available to that environment; account for that access when choosing the target.

The agent follows your resource allocation, spending limits and upgrade policy. These are operating instructions, not a technical barrier preventing access to keys or funds. Secret-handling instructions keep private keys out of chat, ordinary logs, source control and issue reports; the agent still has access to the keys it manages. Keep wallet recovery material in your chosen protected storage.

## Install the skill

Choose one installation method. The agent needs local or remote access to the machine that will run the provider. GitHub installation requires the skill to be available on the referenced branch; the examples below use the repository's default branch or `main`.

### Skills CLI

With Node.js/npm available:

```sh
npx skills add filecoin-project/curio --skill filecoin-pdp-agent --global
```

Choose your agent when prompted. The [Skills CLI](https://github.com/vercel-labs/skills#install-a-skill) supports Claude Code, Codex, Cursor, OpenClaw and other agents. Global installation makes the skill available across working directories; you do not need to clone Curio yourself first.

### Codex's built-in installer

Ask Codex:

```text
Use $skill-installer to install https://github.com/filecoin-project/curio/tree/main/skills/filecoin-pdp-agent
```

The [Codex skill installer](https://learn.chatgpt.com/docs/build-skills#install-curated-skills-for-local-use) accepts skills from other repositories. If the skill does not appear after installation, restart Codex.

## Start operation

After installing the skill, give the agent its operating mandate. For example:

```text
Use filecoin-pdp-agent to set up and run a Filecoin PDP provider on this machine. I authorize full agent control of this provider, including access to the private keys Curio uses and the funds I allocate. Handle installation and ongoing operation. Ask me for any missing storage, funding or access decisions.
```

The agent inspects the host and existing services, obtains any missing resource decisions, and follows the appropriate workflow:

1. Install supported prerequisites and obtain the selected release's deployment files, cloning Curio when required.
2. Deploy the required services or adopt the existing provider, preserving its identity and data.
3. Configure storage, the provider wallet, chain access and public endpoint; fund the next operations from an authorized source or ask you to supply funds.
4. Register or reconcile the provider offering and verify readiness. Report upload, retrieval and proving verification separately from setup readiness.
5. Establish recurring checks through the agent host's scheduler and verify that service supervision persists. Report whether scheduling and notifications are actually working.

Provide the storage, operating funds and host access the agent needs. It handles routine work within your delegation and asks for human action when a necessary decision, credential or capability is missing. Installing the skill adds the instructions; invoking it starts provider setup and operation.

As part of setup, the skill sets the public registry capability `curioOperator: filecoin-pdp-agent` to identify providers it operates. This single marker stays unchanged across agent sessions and skill upgrades.

## Ongoing operation

The agent monitors provider health, proof deadlines, storage, transaction progress and operating funds through Curio's existing interfaces. It investigates failures, applies supported remedies within its authority, and verifies the affected operation before reporting recovery. It checks releases, performs authorized upgrades or alerts you when an upgrade decision is needed, and maintains an operating record for future sessions.

Curio continues to execute ingestion, proving, transaction tracking, settlement and cleanup independently of agent sessions. Recurring agent checks require an available scheduler; a single conversation does not establish future monitoring. GitHub reporting is optional and requires a connected account and publication authority. The agent searches for existing fixes and reports before deciding whether new evidence warrants a comment or issue.

For the underlying deployment procedure, see [Curio-PDP](curio-pdp.md). For skill instructions and references, see the [skill source](https://github.com/filecoin-project/curio/tree/main/skills/filecoin-pdp-agent).
