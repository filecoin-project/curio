# Market 2.0 Architecture

## What This Page Covers

This page explains what Market 2.0 is designed to do, why key model choices were made, how the system works end to end, and what problems it does not try to solve.

## Purpose

Market 2.0 is the common interaction layer between clients and storage providers.

It is designed so PoRep-style storage requests, PDP requests, and future specialized requests all use one stable deal envelope instead of separate APIs per request type.

## Core Deal Envelope

```go
type Deal struct {
    Identifier ulid.ULID `json:"identifier"`
    Client     string    `json:"client"`
    Data       *DataSource `json:"data,omitempty"`
    Products   Products  `json:"products"`
}

type Products struct {
    DDOV1       *DDOV1       `json:"ddo_v1,omitempty"`
    RetrievalV1 *RetrievalV1 `json:"retrieval_v1,omitempty"`
    PDPV1       *PDPV1       `json:"pdp_v1,omitempty"`
}
```

Field meaning:

1. `identifier`: deal reference.
2. `client`: requester identity.
3. `products`: requested behavior.
4. `data`: operation input when required.

Product-specific structs and fields are documented in:

1. [DDO v1](./products/ddo_v1.md)
2. [Retrieval v1](./products/retrieval_v1.md)
3. [PDP v1](./products/pdp_v1.md)

## Why `client` Is Text

`client` is intentionally a text field, not a strict Filecoin address type.

Why:

1. Market 2.0 must support identity schemes beyond native Filecoin address formats.
2. Ed25519 identities are supported today.
3. L2 or product-specific identity formats can be supported without changing the deal envelope.

Identity safety comes from authentication and identity matching, not from enforcing one address type in the schema.

## Deal Identifier (ULID)

`identifier` uses ULID.

1. ULID specification: <https://github.com/ulid/spec>
2. Implementations (many languages): <https://github.com/ulid/spec#implementations-in-other-languages>
3. Go implementation: <https://github.com/oklog/ulid>
4. JavaScript/TypeScript implementation: <https://github.com/ulid/javascript>

ULID is used as a portable, sortable, globally unique deal reference.

## Authentication and Authorization

This section documents current auth behavior.

Header format:

`Authorization: CurioAuth <keyType>:<base64(pubKeyBytes)>:<base64(signatureBytes)>`

Supported key types:

1. `ed25519`
2. `secp256k1`
3. `bls`
4. `delegated`

Signing input:

1. Build message bytes as `pubKeyBytes || RFC3339HourTimestamp`.
2. Hash with SHA-256.
3. Sign the digest.

Verification window:

1. Current hour.
2. Current hour minus 59 minutes.
3. Current hour plus 59 minutes.

Authorization checks:

1. Signature must verify for one of the accepted timestamps.
2. Client allow/deny policy is read from `market_mk20_clients`.
3. If client is not listed, fallback behavior uses `DenyUnknownClients` config.
4. For routes with `{id}`, authenticated client must own that deal.

Route scope:

1. Market API routes are authenticated.
2. `/info/*` OpenAPI routes are public.

## Data Model

```go
type DataSource struct {
    PieceCID        cid.Cid              `json:"piece_cid"`
    Format          PieceDataFormat      `json:"format"`
    SourceHTTP      *DataSourceHTTP      `json:"source_http,omitempty"`
    SourceAggregate *DataSourceAggregate `json:"source_aggregate,omitempty"`
    SourceOffline   *DataSourceOffline   `json:"source_offline,omitempty"`
    SourceHttpPut   *DataSourceHttpPut   `json:"source_http_put,omitempty"`
}

type PieceDataFormat struct {
    Car       *FormatCar       `json:"car,omitempty"`
    Aggregate *FormatAggregate `json:"aggregate,omitempty"`
    Raw       *FormatBytes     `json:"raw,omitempty"`
}
```

Validation rules:

1. `piece_cid` must be valid PieceCID v2.
2. Exactly one source type is allowed.
3. Exactly one format is allowed.
4. Source type must be enabled by provider policy.

## Aggregation

Current aggregation support:

1. `AggregateTypeV1` only.
2. This is datasegment/PODSI-style aggregation based on FRC-0058:
   <https://github.com/filecoin-project/FIPs/blob/master/FRCs/frc-0058.md>

Why aggregation matters in Market 2.0:

1. It allows subpiece-aware indexing and retrieval flows.
2. It enables retrieval of subpieces, including IPFS-style single-CID usage from subpiece data paths when retrieval/indexing settings are enabled.

Current constraints:

1. Aggregate-of-aggregate is rejected.
2. `source_aggregate` mode and pre-aggregated `format.aggregate.sub` mode have different validation rules.
3. Subpiece order is meaningful and must match segment order.

Future direction:

1. Additional aggregation types can be added as new FRCs are finalized.
2. The top-level Market 2.0 deal envelope remains unchanged.

## Product Composition Rules

1. At least one top-level product must be present.
2. `ddo_v1` and `pdp_v1` cannot be used together in one deal.
3. `ddo_v1` requires `retrieval_v1`.
4. With `ddo_v1`, `retrieval_v1.announce_piece` must be false.

## How a Deal Moves Through the System

1. Client submits deal intent.
2. Market 2.0 validates identity, payload, and product composition.
3. Market 2.0 selects product execution path.
4. Market 2.0 selects ingest path (immediate source or upload-first).
5. Processing continues through common lifecycle states.
6. Deal reaches terminal success (`complete`) or terminal failure (`failed`).

Lifecycle states:

1. `accepted`
2. `uploading`
3. `processing`
4. `sealing`
5. `indexing`
6. `failed`
7. `complete`

## External Contract Boundary

Market 2.0 supports product-specific contract verification without changing the outer deal model.

Example:

1. DDO verifies through `CurioDealViewV1` read methods.
2. Provider allowlist controls which contracts are accepted.
3. Interface versioning is explicit.

## Best Practices

1. Keep `identifier` immutable.
2. Keep `client` stable within the chosen identity namespace.
3. Query provider capabilities before creating deals.
4. Keep product payloads explicit.
5. Vet contracts before allowlisting.
6. Extend existing products only when backward compatibility is preserved.
7. Add a new product when behavior or lifecycle contracts diverge.

## What Market 2.0 Does Not Solve

1. Commercial negotiation and legal agreement terms.
2. Automatic trust in external contracts.
3. Universal payment/settlement semantics across all products.
4. Product-specific business policy decisions outside declared product contracts.
5. Operational monitoring and recovery by itself.
