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
2. Returned deal fields match expected semantics (provider, client, piece CID, duration, allocation, state).
3. The contract's state transitions and finalization behavior are clear for payout and replay safety.

Only after this review should the contract be set to Allowed.

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

    struct DealView {
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

    function getDeal(uint256 dealId) external view returns (DealView memory);
}
```

## Versioning

`version()` must return `1` for this interface.

## Curio Verification Checks

When `market_address` is set for a DDO deal, Curio performs read-only verification:

1. Contract is allowlisted by the provider.
2. `version()` is supported and returns `1`.
3. `getDeal(market_deal_id)` returns a deal.
4. Returned values are matched against local deal details.
5. Deal state is not `Finalized`.

If `getDeal` reverts with `DealNotFound(uint256)`, Curio treats the market deal as missing.

## Read-Only Boundary

Curio does not call write methods for this verification path.
