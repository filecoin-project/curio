# Product: ddo_v1

## What `ddo_v1` Is For

`ddo_v1` is the storage onboarding product for MK20. Use it when a client wants a provider to ingest a piece and run normal sealing/indexing flow, optionally tied to an external market contract.

## Struct Definition

```go
type DDOV1 struct {
    Provider            address.Address          `json:"provider"`
    StartEpoch          *abi.ChainEpoch          `json:"start_epoch"`
    Duration            abi.ChainEpoch           `json:"duration"`
    AllocationId        *verifreg.AllocationId  `json:"allocation_id,omitempty"`
    MarketAddress       string                   `json:"market_address"`
    MarketDealID        *uint64                  `json:"market_deal_id"`
    NotificationAddress address.Address          `json:"notification_address"`
    NotificationPayload []byte                   `json:"notification_payload,omitempty"`
}
```

## Required And Optional Fields

- Required:
  - `provider`
  - `duration`
- Optional:
  - `start_epoch`
  - `allocation_id`
  - `market_address`
  - `market_deal_id` (required if `market_address` is set)
  - `notification_address` and `notification_payload` (must be provided together)

## Field Behavior

- `start_epoch` is optional. If set, Curio validates it against chain head and `ExpectedPoRepSealDuration`, and uses it as the deal schedule start during ingestion.
- `duration` is always part of the DDO payload. For verified allocations, it must still satisfy allocation term bounds.
- `market_address` and `market_deal_id` opt the deal into external contract verification.
- `notification_address` and `notification_payload`, when set, are attached to the eventual piece activation manifest during ingestion. They are not evaluated by `verifyDeal(...)`.

## Core Validation Rules

- `ddo_v1` must be enabled by the provider.
- Provider must be a valid address and not in MK20 disabled-miner config.
- If `start_epoch` is set, it must be greater than `0`.
- If `allocation_id` is missing, minimum duration is enforced (`>= 518400`).
- If `allocation_id` is set, it must not be `NoAllocationID`.
- If `market_address` is set, it must be a valid `0x` hex address.
- Notification address and payload must either both be set or both be unset.

## Market Contract Verification

When `market_address` is provided, MK20 performs read-only contract verification before acceptance.

Verification behavior:

1. Contract must exist in `ddo_contracts` and be allowed.
2. Contract must implement `CurioDealViewV1` and return `version() == 1`.
3. MK20 builds `CurioDealView` from local deal values and calls `verifyDeal(...)`.
4. `verifyDeal(...)` must return `true`.

This call is a read-only intake gate. It checks whether the market contract accepts the proposed deal. It does not by itself guarantee payment, payout, or notification success.

Curio passes these values into `verifyDeal(...)`:

- `dealId`
- `state` = `Open`
- provider actor id
- client identity bytes
- piece CID v2
- `start_epoch` or `0`
- duration
- `allocation_id` or `0`
- `finalizedEpoch` = `0`

If `verifyDeal` reverts with `DealNotFound(uint256)`, MK20 rejects the deal as market-missing.

## Notification Callback Behavior

If both `notification_address` and `notification_payload` are set, Curio carries them into the piece activation manifest during ingestion as a `DataActivationNotification`.

Operationally:

1. This path runs later than `verifyDeal(...)`, during sector activation work.
2. Contracts that rely on notification callbacks for settlement or state transitions should be reviewed separately from intake-time contract verification.
3. Provider setups that depend on notification callbacks generally keep `Subsystems.RequireNotificationSuccess = true` (the default). See [Default Curio Configuration](../../configuration/default-curio-configuration.md).

## Additional Sanitize Checks Before Pipeline Entry

- `retrieval_v1` must be present.
- `retrieval_v1.announce_piece` must be false.
- Provider must be in Curio miner set.
- Deal data must be present.
- Piece size must fit provider sector size.
- Raw format cannot be indexed.
- Notification address must resolve on chain when set.
- Client must pass provider allow/deny policy.
- If `start_epoch` is set, it must be greater than or equal to current chain height plus `ExpectedPoRepSealDuration`.
- If `allocation_id` is set, MK20 validates allocation ownership, provider, term, data, size, and expiration consistency.
- For verified allocations, the allocation owner may be either the client or the market contract address.
- If both `allocation_id` and `start_epoch` are set, allocation expiration must be greater than or equal to `start_epoch`.
