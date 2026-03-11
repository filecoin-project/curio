# Product: pdp_v1

## What `pdp_v1` Is For

`pdp_v1` drives PDP dataset and piece lifecycle operations through MK20.

## Struct Definition

```go
type PDPV1 struct {
    CreateDataSet bool    `json:"create_data_set"`
    DeleteDataSet bool    `json:"delete_data_set"`
    AddPiece      bool    `json:"add_piece"`
    DeletePiece   bool    `json:"delete_piece"`
    DataSetID     *uint64 `json:"data_set_id,omitempty"`
    RecordKeeper  string  `json:"record_keeper"`
    PieceIDs      []uint64 `json:"piece_ids,omitempty"`
    ExtraData     []byte   `json:"extra_data,omitempty"`
}
```

## Action Model

Exactly one of these flags must be set in each deal:

- `create_data_set`
- `delete_data_set`
- `add_piece`
- `delete_piece`

## Key Fields

- `data_set_id`: required for all actions except `create_data_set`.
- `record_keeper`: required when creating a dataset.
- `piece_ids`: required for `delete_piece`.
- `extra_data`: optional auxiliary payload for verifier/service interactions.

## Validation Rules

- `pdp_v1` must be enabled by provider.
- Provider must have PDP signing key configured (`eth_keys.role = 'pdp'`).
- Only one action flag is allowed per deal.
- `create_data_set`:
  - `data_set_id` must be absent
  - `record_keeper` must be a valid hex address
- `delete_data_set` / `add_piece`:
  - `data_set_id` must be present
  - dataset must exist and be active
- `delete_piece`:
  - `data_set_id` must be present
  - `piece_ids` must be present
  - all listed pieces must exist and be active in that dataset

## Runtime Sanitize Rules

- `add_piece` requires `retrieval_v1`.
- `offline` source is rejected for PDP flows.
- Raw-format data cannot use `announce_payload`.
- Dataset and piece ownership checks are revalidated.

## Processing Behavior

- `add_piece` with HTTP/aggregate data goes directly into PDP processing pipelines.
- `add_piece` with `put` source (or upload-first mode) enters upload-waiting first.
- Create/delete operations enqueue PDP lifecycle actions without upload flow.
