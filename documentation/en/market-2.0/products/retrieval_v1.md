# Product: retrieval_v1

## What `retrieval_v1` Is For

`retrieval_v1` controls indexing and announcement behavior for data submitted through MK20 products.

## Struct Definition

```go
type RetrievalV1 struct {
    Indexing        bool `json:"indexing"`
    AnnouncePayload bool `json:"announce_payload"`
    AnnouncePiece   bool `json:"announce_piece"`
}
```

## Fields

- `indexing`: enable indexing flow.
- `announce_payload`: announce payload-level information.
- `announce_piece`: announce piece-level information.

## Validation Rules

- `retrieval_v1` must be enabled by provider.
- `announce_payload` requires `indexing = true`.

## Product Compatibility Rules

- `ddo_v1` requires `retrieval_v1`.
- With `ddo_v1`, `announce_piece` must be false.
- Additional context-dependent checks are applied during DDO/PDP sanitize stages.
