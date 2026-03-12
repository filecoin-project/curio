# Market 2.0

Market 2.0 is Curio’s unified, extensible deal interface between clients and storage providers.

It exists to abstract away deal-type-specific integration.  
A client can request PoRep-style storage, PDP operations, or future specialized services through one consistent model and one consistent API surface.

## Deal Model

Every deal is expressed as:

- `id`: deal reference
- `client`: requester identity
- `product`: requested service
- `data source` (optional): operation input data when required

This model reflects real deal-making: who is asking, what they want, how it is referenced, and what inputs are needed.

## Clients

Use these guides to submit and manage deals:

1. [HTTP API: create, update, and query deals](./http-api.md)
2. [Deal intake paths and upload ordering](./deal-processing.md#deal-intake-paths)
3. [Lifecycle and status tracking](./deal-processing.md)
4. [Data model and source/format requirements](./architecture.md#data-model)
5. [Error codes and troubleshooting](./http-api.md#endpoint-details)

Contact your storage provider when:

1. You receive repeated `500` or `503` responses.
2. A deal stays non-terminal longer than expected.
3. A contract-related rejection needs provider policy confirmation.

## Storage Providers

Use these guides to operate and support Market 2.0:

1. [Lifecycle and operational behavior](./deal-processing.md)
2. [Contract integration and provider allowlisting](./contracts/README.md)
3. [Error codes for support triage](./http-api.md#endpoint-details)

Operational note:

1. Product/source controls and contract allowlisting are provider policy decisions managed through the Curio GUI.
2. DDO contracts must be vetted before allowlisting.

## Developers

### SDK and Client Library Developers

1. [HTTP interaction model](./http-api.md)
2. [Lifecycle semantics](./deal-processing.md)
3. [Error handling contract](./http-api.md#endpoint-details)
4. [Data model and validation context](./architecture.md)
5. [Data source and format constraints](./architecture.md#data-model)

### Contract Integrators

1. [Contract integration guide](./contracts/README.md)
2. [Current DDO contract interface (CurioDealView v1)](./contracts/curiodealview.md)

Current integration surface:

1. DDO uses `CurioDealViewV1` today.
2. Additional interfaces may be introduced in future product/version evolution without changing the top-level Market 2.0 deal model.

### Curio Product Developers

1. [Extending products](./extending-mk20.md)
2. [DDO product behavior](./products/ddo_v1.md)
3. [Retrieval product behavior](./products/retrieval_v1.md)
4. [PDP product behavior](./products/pdp_v1.md)

Design rule:

1. Extend an existing product when behavior remains backward compatible and JSON evolution is safe.
2. Introduce a new product when behavior, lifecycle, or compatibility contracts diverge.
