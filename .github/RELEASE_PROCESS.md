# Curio Release Process

This document is for Curio maintainers. It is the canonical reference for how to prepare, review, publish, and announce a Curio release. It is linked from the repository `README` and from the Curio support Slack release pin so maintainers can find it without searching the tree.

Curio releases default to stable. Release candidates are the exception, not a parallel track (see Release Principles). The checklist below is written stable-first: run the Release Checklist for every release, and apply the short RC Delta only on the rare occasion an RC is justified.

## Release Principles

Stable is the default release type for Curio.

Release candidates require explicit justification. Do not create an RC when branch-based testing can validate the change. RCs delay fixes from reaching production providers when those providers are unlikely to deploy pre-releases.

Testing alone is not sufficient justification for an RC. Prefer testing from `main` or from a feature branch. Use an RC only when validation requires providers or users to upgrade to a release artifact, for example:

- Filecoin network upgrades
- Contract upgrades
- Changes that require coordinated calibration-network or provider rollout
- Changes that require testing release packaging, version strings, or upgrade behavior

Mainnet releases must be stable releases.

Do not publish releases in rapid succession for non-urgent changes. PDP users may be able to update quickly, but PoRep operators can have multiple nodes and may need to drain or empty active pipelines before upgrading. If an RC is published, maintain at least a 48-hour observation window before publishing the corresponding stable release unless there is an urgent security or correctness reason to move faster.

## Versioning Rules

Curio release versions follow this format:

```text
v1.<network-version>.<patch>
```

Version components:

- Major is `1`.
- Minor tracks the Filecoin network upgrade version.
- Patch increments within that network-version release line.
- RC tags append `-rcN`, for example `v1.28.0-rc1`.

Once a release is published for a newer network-version line, Curio must not publish older line releases. For example, after `v1.28.0` is published, there must not be a later `v1.27.x` release. Fixes must ship forward on the active release line.

## Release Source

Use `main` as the default release source.

Exceptions are permitted when a feature branch, release branch, or cherry-pick on top of the last release is the safer release source. Those cases require a documented release risk or compatibility reason.

Use `main` when it contains the release-scoped changes and maintainers have not identified a blocking regression or compatibility risk. This is preferred because:

- The target fix may depend on earlier PRs already merged to `main`.
- Providers may already be running `main` to pick up fixes ahead of a release.
- Cutting from an older source can remove behavior that providers already depend on.
- Forward-compatible releases are preferred when they are stable.

Valid reasons to release from a non-`main` source include:

- `main` has an identified blocking regression.
- The release must be limited to a narrow hotfix.
- The fix must be cherry-picked onto the last release to reduce risk.
- The release is tied to a feature branch or release branch that cannot safely include all of `main`.

When releasing from a source other than `main`, document the release-source rationale in the version upgrade PR and verify the release does not drop fixes or compatibility behavior that providers may already rely on.

## Release Flow

1. Scope the PRs required for the release.
2. Choose the release source. Use `main` unless there is a documented release risk or compatibility reason (see [Release Source](#release-source)).
3. Confirm each required PR is merged into `main` or otherwise included in the chosen release source.
4. Determine the release type: stable or RC. Default to stable; an RC requires explicit justification (see [Release Principles](#release-principles)).
5. Open a version upgrade PR (see [Version Upgrade PR](#version-upgrade-pr)).
6. For stable releases, update the docs version page in the same PR.
7. For RC releases, do not update the docs version page unless there is a documented release requirement.
8. Complete review and CI for the version upgrade PR.
9. Merge the version upgrade PR. The release tag is not created until publish time, so the version PR merges before release notes exist; release notes are generated from the merged changes.
10. Create release notes from the merged changes (see [Release Notes](#release-notes)).
11. Review the release notes with the team.
12. Publish the release. Creating the GitHub release is what creates the tag.
13. Announce the release on the appropriate channels (see [Release Announcements](#release-announcements)).
14. Monitor operator feedback after publication.

## Release Checklist

Run this checklist for every release. It is written for a stable release, which is the default. If, and only if, an RC is justified (see [Release Principles](#release-principles)), apply the RC Delta below instead of the stable-specific lines it overrides.

- [ ] Confirm the release is suitable for mainnet.
- [ ] Confirm the release source. Use `main` unless there is a documented release risk or compatibility reason.
- [ ] If the release source is not `main`, document the release-source rationale.
- [ ] Confirm all scoped PRs are merged into `main` or otherwise included in the chosen release source.
- [ ] Confirm no newer network-version line has already shipped.
- [ ] Open a version upgrade PR (see [Version Upgrade PR](#version-upgrade-pr)). In that PR:
  - [ ] Set `BuildVersionArray` in `build/version.go` to `[3]int{1, <network-version>, <patch>}`.
  - [ ] Set `BuildVersionRC` in `build/version.go` to `0`.
  - [ ] Run `make gen` and include any generated changes.
  - [ ] Update `documentation/en/versions.md`.
  - [ ] Update `documentation/zh/versions.md` if the translated version matrix is being maintained for this release.
- [ ] Confirm CI passes, or confirm maintainers accept any known failures.
- [ ] Merge the version upgrade PR.
- [ ] Create release notes with the stable-release template (see [Release Notes](#release-notes)).
- [ ] Include `Upgrade prep for Storage Providers` only if operators have required preparation or upgrade-risk items.
- [ ] Confirm the compare link points from the previous release to the new release.
- [ ] Review the release notes with the team.
- [ ] Publish the GitHub release as stable and mark it as latest. Publishing creates the tag (`v1.<network-version>.<patch>`, for example `v1.28.0`).
- [ ] Announce the stable release in the Curio support Slack, naming the version, release type, and intended upgrade audience (see [Release Announcements](#release-announcements)).
- [ ] Confirm the docs version page reflects this release as the current recommended version.
- [ ] If an RC preceded this release, state in the announcement that the RC is superseded.
- [ ] Monitor GitHub issues, Curio support Slack, packaging/install failures, migration failures, and upgrade feedback.

### RC Delta

RCs are the exception. Only cut one when coordinated testing genuinely needs a release artifact and testing from `main` or a feature branch is not sufficient (see [Release Principles](#release-principles)). When that bar is met, take the Release Checklist above and override these lines:

- [ ] First confirm the RC is justified: coordinated testing needs a release artifact, `main`/feature-branch testing is not sufficient, and the RC targets calibration-network or another coordinated upgrade requirement.
- [ ] Set `BuildVersionRC` to the RC number instead of `0` (for example `1`). `BuildVersionArray` stays the target stable version, for example `[3]int{1, 28, 0}`.
- [ ] Do not update the docs version page unless there is a documented release requirement.
- [ ] Release notes clearly mark the release as an RC, document the test scope and intended testers, and include no mainnet upgrade recommendation language.
- [ ] The compare link points from the previous release or previous RC to the new RC.
- [ ] Publish the GitHub release as a pre-release; the tag uses the `-rcN` suffix (for example `v1.28.0-rc1`). Do not mark it latest.
- [ ] Announce only to the intended testing audience, with the test scope and an explicit statement that it is not a mainnet upgrade recommendation.
- [ ] Maintain at least a 48-hour observation window before publishing the corresponding stable release unless there is an urgent security or correctness reason.

## Version Upgrade PR

Keep the version upgrade PR small and focused. Do not include unrelated feature work.

The version upgrade PR must target the selected release source. For the standard release path, this is `main`. If it targets a feature branch, release branch, or cherry-pick branch, document the release-source rationale in the PR.

The version upgrade PR makes exactly these changes:

- Update `build/version.go`:
  - Set `BuildVersionArray` to `[3]int{1, <network-version>, <patch>}`.
  - Set `BuildVersionRC` to `0` for stable releases, or to the RC number for RC releases.
- Run `make gen` after updating the version and include any generated changes in the PR.
- For stable releases, also update:
  - `documentation/en/versions.md`
  - `documentation/zh/versions.md` if the translated version matrix is being maintained for that release
- For RC releases, a version upgrade PR is still required, but the docs version page does not need to be updated.

Before merging the version upgrade PR, verify:

- The intended PRs are all in `main` or otherwise included in the chosen release source.
- The version in `build/version.go` matches the intended release.
- `BuildVersionRC` is correct for stable vs RC.
- CI passes, or any failures are understood and accepted by maintainers.

## Release Notes

Release notes must describe Storage Provider impact, not only list merged PRs.

Start with an overview that explains:

- What changed.
- Intended upgrade audience.
- Whether the release is stable or RC.
- Any build requirement changes.
- Any schema, migration, config, protocol, market, PDP, PoRep, retrieval, or packaging impact.

Use this general structure:

```markdown
# Curio vX.Y.Z

## Overview

Short release summary and upgrade recommendation.

| Field | Value |
| :--- | :--- |
| Version | `vX.Y.Z` |
| Type | Stable or RC |
| Compare | `vA.B.C...vX.Y.Z` |
| Build | Build requirement summary, if changed |
| Schema | Schema or migration summary, if relevant |

## Highlights

Major user-visible changes.

## Upgrade prep for Storage Providers

Only include this section when operators need to take preparation or mitigation steps before upgrading.

## Bug fixes

Fixes grouped by operator impact where possible.

## Improvements

User-visible improvements and operational changes.

## Dependencies

Important dependency updates.

## Build and CI

Build, packaging, test, and CI changes when relevant.

## Reverts

Reverts and their release impact.

## Contributors

Contributor list.

Full changelog, issue tracker, and support links.
```

The `Upgrade prep for Storage Providers` section is optional. Include it only when operators need to prepare before upgrading or need to know about a specific upgrade risk. Do not include an empty or generic prep section.

## Release Notes Review

Before publishing, review the notes with the team for:

- Incorrect upgrade guidance.
- Missing operator-impacting changes.
- Missing migration or schema notes.
- Missing config default changes.
- Missing build requirement changes.
- Misclassified stable vs RC messaging.
- PRs that require highlight coverage.
- Internal-only PRs to omit from release notes.

## Release Announcements

Publishing a GitHub release is not the same as announcing it. A release is not considered shipped until operators have been told what to run and why. The GitHub releases page alone is not a sufficient announcement channel: it can show pre-releases above stable releases, lists multiple recent tags, and does not by itself tell an operator which version is current and safe for mainnet.

### Announcement Principles

- Every stable release gets an announcement. An RC is announced only to the testing audience that is expected to act on it.
- The announcement names the current recommended version explicitly. Operators should not have to infer "what should I run" from the tag list.
- Announce after the release is published and marked latest, not before.
- Keep one canonical "current recommended Curio version" pointer that operators can check at any time, independent of scrolling release history. The docs version page (`documentation/en/versions.md`) is that pointer.

### Channels

| Channel | Stable | RC | Purpose |
| :--- | :--- | :--- | :--- |
| Curio support Slack | Required | Required (testers only) | Primary operator announcement channel. Post the version, release type, intended audience, upgrade guidance, and any migration note. |
| GitHub release notes | Required | Required | Canonical detailed notes. Stable marked latest; RC marked pre-release. |
| Docs version page (`documentation/en/versions.md`) | Required | Only with a documented release requirement | Canonical "current recommended version" pointer. |
| `curiostorage` on X | Breaking or major new features only | No | Optional public post for notable feature releases. Not required for routine patch or RC releases. |

The Curio support Slack announcement is the baseline requirement. Routine releases do not need any additional public posts; reserve the `curiostorage` X account for breaking changes or significant new features.

### Announcement Content

A stable release announcement states, in this order:

1. Version and release type. For stable mainnet releases, say so explicitly.
2. Who should upgrade and how urgently (for example all mainnet SPs, PDP-only, PoRep-only, FOC participants, or calibration testers).
3. Upgrade ordering and prerequisites (for example "upgrade Lotus first"), only when relevant.
4. Migration or schema impact (automatic vs manual), only when relevant.
5. The compare link or release-notes link.

An RC announcement additionally states the test scope, the intended testers, and an explicit statement that it is not a mainnet upgrade recommendation.

Do not announce a stable release and its preceding RC as if they were two separate things to run. If an RC preceded the stable release, the stable announcement supersedes the RC and must say so.

### Announcement Timing

- Announce stable releases promptly after publishing and marking latest.
- Do not pre-announce a version that is not yet published and marked latest.
- When an RC is followed by a stable release, the stable announcement must make clear the RC is superseded so operators do not run the pre-release by mistake.
- Apply the same cadence discipline as releases: do not flood operator channels with rapid back-to-back announcements for non-urgent follow-up releases.

## Publishing Checklist

Before publishing:

- Confirm the release type is correct.
- Confirm the tag name matches `build/version.go`.
- Confirm the tag points at the intended release source.
- Confirm RC tags use `-rcN`.
- Confirm stable tags do not include an RC suffix.
- Confirm the compare link points from the previous release to the new release.
- Confirm release notes were reviewed by the team.
- Confirm mainnet releases are stable releases.
- Confirm stable releases are marked as latest.
- Confirm RC releases are marked as pre-releases and are not marked as latest.
- Confirm the release has been announced on the appropriate channels (see Release Announcements).

After publishing:

- Monitor GitHub issues.
- Monitor Curio support Slack.
- Monitor installation, packaging, migration, and upgrade failures.
- Do not shorten release cadence for non-urgent follow-up releases.

## Common Release Mistakes

- Publishing an older minor line after a newer network-version line has shipped.
- Releasing from a non-`main` source without a documented release risk or compatibility reason.
- Releasing from an older source that drops fixes or compatibility behavior providers may already rely on.
- Creating an RC when branch or `main` testing would work.
- Leaving important fixes in an RC that providers are unlikely to deploy.
- Updating the docs version page for an RC without a documented release requirement.
- Forgetting to update the docs version page for a stable release.
- Including a generic upgrade-prep section when no prep is required.
- Shipping rapid back-to-back releases without accounting for PoRep operator upgrade cost.
- Publishing a release without announcing it, leaving operators to infer the current version from the tag list.
- Announcing a stable release without noting that a preceding RC is superseded.
