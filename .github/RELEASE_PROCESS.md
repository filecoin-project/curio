# Curio Release Process

This document is for Curio maintainers. It describes how to prepare, review, and publish Curio stable and release-candidate releases.

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
2. Choose the release source. Use `main` unless there is a documented release risk or compatibility reason.
3. Confirm each required PR is merged into `main` or otherwise included in the chosen release source.
4. Select the release type: stable or RC.
5. Open a version upgrade PR.
6. For stable releases, update the docs version page in the same PR.
7. For RC releases, do not update the docs version page unless there is a documented release requirement.
8. Complete review and CI for the version upgrade PR.
9. Merge the version upgrade PR.
10. Create release notes from the merged changes.
11. Review the release notes with the team.
12. Publish the release.
13. Monitor operator feedback after publication.

## Stable Release Checklist

Use this checklist for a stable release.

- [ ] Confirm the release is suitable for mainnet.
- [ ] Confirm the release source. Use `main` unless there is a documented release risk or compatibility reason.
- [ ] If the release source is not `main`, document the release-source rationale.
- [ ] Confirm all scoped PRs are merged into `main` or otherwise included in the chosen release source.
- [ ] Confirm no newer network-version line has already shipped.
- [ ] Open a version upgrade PR.
- [ ] Set `BuildVersionArray` in `build/version.go` to the target `v1.<network-version>.<patch>` version.
- [ ] Set `BuildVersionRC` in `build/version.go` to `0`.
- [ ] Run `make gen`.
- [ ] Update `documentation/en/versions.md`.
- [ ] Update `documentation/zh/versions.md` if the translated version matrix is being maintained for this release.
- [ ] Confirm CI passes, or confirm maintainers accept any known failures.
- [ ] Merge the version upgrade PR.
- [ ] Create the release tag using the stable version, for example `v1.28.0`.
- [ ] Create release notes with the stable-release template.
- [ ] Include `Upgrade prep for Storage Providers` only if operators have required preparation or upgrade-risk items.
- [ ] Confirm the compare link points from the previous release to the new release.
- [ ] Review the release notes with the team.
- [ ] Publish the GitHub release as stable and mark it as latest.
- [ ] Monitor GitHub issues, Curio support Slack, packaging/install failures, migration failures, and upgrade feedback.

## RC Release Checklist

Use this checklist only when an RC is justified by coordinated testing needs.

- [ ] Confirm an RC is required because coordinated testing needs a release artifact.
- [ ] Confirm testing from `main` or a feature branch is not sufficient.
- [ ] Confirm the RC is for calibration-network testing or another coordinated upgrade requirement.
- [ ] Confirm the release source. Use `main` unless there is a documented release risk or compatibility reason.
- [ ] If the release source is not `main`, document the release-source rationale.
- [ ] Confirm all scoped PRs for the RC are merged into `main` or otherwise included in the chosen release source.
- [ ] Confirm no newer network-version line has already shipped.
- [ ] Open a version upgrade PR.
- [ ] Set `BuildVersionArray` in `build/version.go` to the target stable version, for example `[3]int{1, 28, 0}`.
- [ ] Set `BuildVersionRC` in `build/version.go` to the RC number, for example `1`.
- [ ] Run `make gen`.
- [ ] Do not update the docs version page unless there is a documented release requirement.
- [ ] Confirm CI passes, or confirm maintainers accept any known failures.
- [ ] Merge the version upgrade PR.
- [ ] Create the release tag using the RC suffix, for example `v1.28.0-rc1`.
- [ ] Create release notes that clearly mark the release as an RC.
- [ ] Document the test scope and intended testers.
- [ ] Do not include mainnet upgrade recommendation language.
- [ ] Confirm the compare link points from the previous release or previous RC to the new RC.
- [ ] Review the release notes with the team.
- [ ] Publish the GitHub release as a pre-release.
- [ ] Monitor testing feedback.
- [ ] Maintain at least a 48-hour observation window before publishing the corresponding stable release unless there is an urgent security or correctness reason.

## Version Upgrade PR

Keep the version upgrade PR small and focused. Do not include unrelated feature work.

The version upgrade PR must target the selected release source. For the standard release path, this is `main`. If it targets a feature branch, release branch, or cherry-pick branch, document the release-source rationale in the PR.

Update `build/version.go`:

- Set `BuildVersionArray` to `[3]int{1, <network-version>, <patch>}`.
- Set `BuildVersionRC` to `0` for stable releases.
- Set `BuildVersionRC` to the RC number for RC releases.

Run `make gen` after updating the version and include any generated changes in the PR.

For stable releases, also update:

- `documentation/en/versions.md`
- `documentation/zh/versions.md` if the translated version matrix is being maintained for that release

For RC releases:

- A version upgrade PR is still required.
- The docs version page does not need to be updated.

Before merging the version upgrade PR, verify:

- The intended PRs are all in `main` or otherwise included in the chosen release source.
- The version in `build/version.go` matches the intended release.
- `BuildVersionRC` is correct for stable vs RC.
- CI passes, or any failures are understood and accepted by maintainers.

## RC Policy

Default to stable releases.

Use an RC only when the team needs a release artifact for coordinated testing. Do not create an RC just because a change needs testing. Prefer testing from `main` or from a feature branch when possible.

Valid RC reasons include:

- A network upgrade needs provider testing before mainnet.
- A contract upgrade needs coordinated user or provider validation.
- Calibration-network operators need to test upgrade behavior.
- Packaging, versioning, or deployment behavior must be tested before stable.

Insufficient RC reasons include:

- General confidence-building.
- Feature-branch validation is still in progress.
- A fix is important but could ship as stable after review and CI.
- General uncertainty without a documented coordinated-upgrade requirement.

After publishing an RC, maintain a minimum 48-hour observation window before publishing the corresponding stable release unless there is an urgent security or correctness reason to move faster.

## Stable Release Policy

Use a stable release when maintainers conclude the release is suitable for mainnet.

Stable releases require:

- All scoped PRs merged to `main` or otherwise included in the chosen release source.
- A merged version upgrade PR.
- Docs version page update.
- Team-reviewed release notes.
- A GitHub compare link from the previous release.

Stable release notes must state the intended upgrade audience and rationale.

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
