# DDO Contract Review and Allowlisting

This page is for storage providers reviewing DDO market contracts, and for builders deploying them for Market 2.0.

## What Curio Actually Checks

When a DDO deal includes `market_address`, Curio performs read-only intake verification before accepting the deal.

Current checks:

1. `market_address` must exist in `ddo_contracts` and be marked `allowed = true`.
2. `market_deal_id` must be present.
3. Curio calls `version()` and requires `1`.
4. Curio builds `ICurioDealViewV1.CurioDealView` from the local deal and calls `verifyDeal(...)` via `eth_call`.
5. `verifyDeal(...)` must return `true`.
6. If `verifyDeal(...)` reverts with `DealNotFound(uint256)`, Curio rejects the deal as market-missing.

What this does not mean:

1. This call only answers whether the contract accepts the proposed deal at intake time.
2. It does not by itself prove payment, payout, callback success, or settlement behavior.
3. `AddMarketContract` only validates address syntax and that the address resolves to a Filecoin actor before inserting it into Curio policy state.
4. Curio does not inspect proxy admin, upgrade controls, ownership, or source verification for the contract.

## Provider Control States

Provider policy is separate from contract code compatibility.

Current states:

1. Added and allowed: new DDO deals can reference the contract.
2. Added but blocked (`allowed = false`): new DDO deals using that contract are rejected.
3. Removed: contract is no longer configured; new DDO deals using it are rejected.
4. Existing accepted deals continue through processing even if the contract is later blocked or removed.
5. `GET /contracts` returns only allowed contracts.

These controls are managed through Curio operator tooling and UI.

## Provider Review Checklist

Before allowing a DDO market contract, providers should review:

1. Verified contract source and ABI from a source they trust.
2. The deployed address, preferably confirmed through a trusted builder channel.
3. `verifyDeal(...)` behavior, including validation of `providerActorId`, `clientId`, `pieceCidV2`, `startEpoch`, `duration`, `allocationId`, and the expected `state` / `finalizedEpoch` semantics.
4. Notification-driven settlement or callback paths separately from the intake verification path.
5. Ownership, upgrade authority, and any governance or timelock around the contract if it is proxy-based or upgradeable.

Allocation note:

1. For allocation-backed DDO flows, Curio can resolve the allocation against either the deal client or the market contract address.
2. If the market contract allocates on behalf of end users, providers should make sure that ownership model is intentional and documented.

## Upgradeable Contract Risk

Curio does not distinguish immutable and upgradeable contracts during allowlisting.

Providers should treat upgradeable contracts as an ongoing trust decision:

1. Review who controls upgrades.
2. Review whether upgrades are gated by a timelock or multisig.
3. Re-review the contract when implementation or ownership changes.
4. Block or remove the contract if governance or implementation changes become unclear.

## Builder Guidance

Builders integrating a DDO market contract should:

1. Publish verified source and a reliable ABI reference.
2. Implement `CurioDealViewV1` exactly and return `version() == 1`.
3. Keep `verifyDeal(...)` deterministic and read-only.
4. Validate the fields Curio sends rather than blindly accepting every call.
5. Use `DealNotFound(uint256)` for missing market deal identifiers.
6. Keep notification-driven callbacks efficient and document the expected failure behavior for providers if the contract depends on them.
7. Communicate upgrade process and governance model clearly if the contract is upgradeable.

## Related Pages

1. [CurioDealView v1 interface](./curiodealview.md)
2. [DDO product behavior](../products/ddo_v1.md)
