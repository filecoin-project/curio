# Product: ddo_v1

## What `ddo_v1` Is For

`ddo_v1` is the storage onboarding product for MK20. Use it when a client wants a provider to ingest a piece and run normal sealing/indexing flow, optionally tied to an external market contract.

## Struct Definition

```go
type DDOV1 struct {
    Provider            address.Address         `json:"provider"`
    Duration            abi.ChainEpoch          `json:"duration"`
    AllocationId        *verifreg.AllocationId `json:"allocation_id,omitempty"`
    MarketAddress       string                  `json:"market_address"`
    MarketDealID        *uint64                 `json:"market_deal_id"`
    NotificationAddress address.Address         `json:"notification_address"`
    NotificationPayload []byte                  `json:"notification_payload,omitempty"`
}
```

## Required And Optional Fields

- Required:
  - `provider`
  - `duration` (unless allocation-driven semantics apply)
- Optional:
  - `allocation_id`
  - `market_address`
  - `market_deal_id` (required if `market_address` is set)
  - `notification_address` and `notification_payload` (must be provided together)

## Core Validation Rules

- `ddo_v1` must be enabled by the provider.
- Provider must be a valid address and not in MK20 disabled-miner config.
- If `allocation_id` is missing, minimum duration is enforced (`>= 518400`).
- If `allocation_id` is set, it must not be `NoAllocationID`.
- If `market_address` is set, it must be a valid `0x` hex address.
- Notification address and payload must either both be set or both be unset.

## Market Contract Verification

When `market_address` is provided, MK20 performs read-only contract verification before acceptance.

Verification behavior:

1. Contract must exist in `ddo_contracts` and be allowed.
2. Contract must implement `CurioDealViewV1` and return `version() == 1`.
3. `getDeal(market_deal_id)` must succeed.
4. Returned deal values must match local deal values for:
   - provider actor id
   - client identity bytes
   - piece CID v2
   - allocation semantics
   - duration
5. Market deal state must not be `Finalized`.

If `getDeal` reverts with `DealNotFound(uint256)`, MK20 rejects the deal as market-missing.

## Additional Sanitize Checks Before Pipeline Entry

- `retrieval_v1` must be present.
- `retrieval_v1.announce_piece` must be false.
- Provider must be in Curio miner set.
- Deal data must be present by finalize time.
- Piece size must fit provider sector size.
- Raw format cannot be indexed.
- Notification address must resolve on chain when set.
- If `allocation_id` is set, MK20 validates allocation ownership, term, data, and size consistency.

## Build Availability

DDO execution path is currently available on `build2k` and `builddebug` builds.
