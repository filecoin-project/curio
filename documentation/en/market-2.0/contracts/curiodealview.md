# CurioDealView

This page defines the DDO market contract interface expected by Market 2.0.

## Contract Control for Storage Providers

Storage providers decide which DDO market contracts Curio will trust.

From the Curio UI, each contract is either:

1. Allowed: new DDO deals can reference this contract.
2. Blocked: new DDO deals using this contract are rejected.
3. Removed: contract is no longer configured; new DDO deals using this contract are rejected.

### What This Means Operationally

1. Allowing a contract enables new intake for that contract.
2. Blocking or removing a contract stops new intake immediately.
3. This policy applies to new submissions and deal updates that introduce DDO market verification.

### Adding a New Contract Safely

Before allowing a contract, providers should verify:

1. The contract implements `CurioDealViewV1` correctly.
2. `verifyDeal` checks the same deal semantics Curio will pass in (`provider`, `client`, `piece CID`, `start epoch`, `duration`, `allocation`, finalization fields).
3. `getDealState` uses the expected `Open` / `Active` / `Finalized` meanings.
4. The contract's state transitions and finalization behavior are clear for payout and replay safety.

Only after this review should the contract be set to Allowed.

Operational review guidance is documented in:

1. [DDO contract review and allowlisting](./ddo-contract-review.md)

## Interface

Contracts integrated with Curio for DDO verification should implement `ICurioDealViewV1`.

```solidity
interface ICurioDealViewV1 {
    error DealNotFound(uint256 dealId);

    enum DealState {
        Open,
        Active,
        Finalized
    }

    struct CurioDealView {
        uint256 dealId;
        DealState state;
        uint256 providerActorId;
        bytes clientId;
        bytes pieceCidV2;
        uint256 startEpoch;
        uint256 duration;
        uint256 allocationId;
        uint256 finalizedEpoch;
    }

    function version() external pure returns (uint256);

    function verifyDeal(CurioDealView calldata deal) external view returns (bool);

    function getDealState(uint256 dealId) external view returns (DealState);
}
```

## Versioning

`version()` must return `1` for this interface.

## `CurioDealView` Field Semantics

1. `dealId`: the market deal identifier referenced by `market_deal_id`.
2. `state`: Curio sends `Open` during intake verification.
3. `startEpoch`: must be `0` when unused.
4. `allocationId`: must be `0` when unused.
5. `finalizedEpoch`: must be `0` for non-finalized intake verification.

## Curio Verification Checks

When `market_address` is set for a DDO deal, Curio performs read-only verification:

1. Contract is allowlisted by the provider.
2. `version()` is supported and returns `1`.
3. Curio builds a `CurioDealView` from local deal details and calls `verifyDeal(...)`.
4. `verifyDeal(...)` must return `true`.

If `verifyDeal` reverts with `DealNotFound(uint256)`, Curio treats the market deal as missing.

The interface also includes `getDealState(...)` for deal-state queries.

## Read-Only Boundary

1. Curio does not call write methods for this verification path.
2. This verification path only decides whether the contract accepts the proposed deal during intake.
3. Payment, settlement, and notification-driven state transitions are outside this read-only boundary.
