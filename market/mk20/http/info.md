# Storage Market Interface

This document describes the storage market types and supported HTTP methods for making deals with Curio Storage Provider.

## 📡 MK20 HTTP API Overview

The MK20 storage market module provides a set of HTTP endpoints under `/market/mk20` that allow clients to submit, track, and finalize storage deals with storage providers. This section documents all available routes and their expected behavior.

### Base URL

The base URL for all MK20 endpoints is: 

```

/market/mk20

```

### 🔄 POST /store

Submit a new MK20 deal.

- **Content-Type**: N/A
- **Body**: N/A
- **Query Parameters**: N/A
- **Response**:
  - `200 OK`: Deal accepted
  - Other [HTTP codes](#constants-for-errorcode) indicate validation failure, rejection, or system errors

### 🧾 GET /status?id=<ulid>

Retrieve the current status of a deal.

- **Content-Type**: `application/json`
- **Body**: N/A
- **Query Parameters**:
  - `id`: Deal identifier in [ULID](https://github.com/ulid/spec) format
- **Response**:
  - `200 OK`: JSON-encoded [deal status](#dealstatusresponse) information
  - `400 Bad Request`: Missing or invalid ID
  - `500 Internal Server Error`: If backend fails to respond

### 📜 GET /contracts

- **Content-Type**: N/A
- **Body**: N/A
- **Query Parameters**: N/A
Return the list of contract addresses supported by the provider.

- **Response**:
  - `200 OK`: [JSON array of contract addresses](#supportedcontracts)
  - `500 Internal Server Error`: Query or serialization failure

### 🗂 PUT /data?id=<ulid>

Upload deal data after the deal has been accepted.

- **Content-Type**: `application/octet-stream`
- **Body**: Deal data bytes
- **Query Parameter**:
 -`id`: Deal identifier in [ULID](https://github.com/ulid/spec) format
- **Headers**:
  - `Content-Length`: must be deal's raw size
- **Response**:
  - `200 OK`: if data is successfully streamed
  - `400`, `413`, or `415`: on validation failures

### 🧠 GET /info

- **Content-Type**: N/A
- **Body**: N/A
- **Query Parameters**: N/A
Fetch markdown-formatted documentation that describes the supported deal schema, products, and data sources.

- **Response**:
  - `200 OK`: with markdown content of the info file
  - `500 Internal Server Error`: if file is not found or cannot be read

## Supported Deal Types

This document lists the data types and fields supported in the new storage market interface. It defines the deal structure, accepted data sources, and optional product extensions. Clients should use these definitions to format and validate their deals before submission.

### Deal

Deal represents a structure defining the details and components of a specific deal in the system.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| Identifier | [ulid.ULID](https://pkg.go.dev/github.com/oklog/ulid#ULID) | json:"identifier" | Identifier represents a unique identifier for the deal in UUID format.  |
| Data | [mk20.DataSource](#datasource) | json:"data" | Data represents the source of piece data and associated metadata.  |
| Products | [mk20.Products](#products) | json:"products" | Products represents a collection of product-specific information associated with a deal  |

### DataSource

DataSource represents the source of piece data, including metadata and optional methods to fetch or describe the data origin.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| PieceCID | [cid.Cid](https://pkg.go.dev/github.com/ipfs/go-cid#Cid) | json:"piece_cid" | PieceCID represents the unique identifier for a piece of data, stored as a CID object.  |
| Size | [abi.PaddedPieceSize](https://pkg.go.dev/github.com/filecoin-project/go-state-types/abi#PaddedPieceSize) | json:"size" | Size represents the size of the padded piece in the data source.  |
| Format | [mk20.PieceDataFormat](#piecedataformat) | json:"format" | Format defines the format of the piece data, which can include CAR, Aggregate, or Raw formats.  |
| SourceHTTP | [*mk20.DataSourceHTTP](#datasourcehttp) | json:"source_http" | SourceHTTP represents the HTTP-based source of piece data within a deal, including raw size and URLs for retrieval.  |
| SourceAggregate | [*mk20.DataSourceAggregate](#datasourceaggregate) | json:"source_aggregate" | SourceAggregate represents an aggregated source, comprising multiple data sources as pieces.  |
| SourceOffline | [*mk20.DataSourceOffline](#datasourceoffline) | json:"source_offline" | SourceOffline defines the data source for offline pieces, including raw size information.  |
| SourceHttpPut | [*mk20.DataSourceHttpPut](#datasourcehttpput) | json:"source_httpput" | SourceHTTPPut // allow clients to push piece data after deal accepted, sort of like offline import  |

### Products

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| DDOV1 | [*mk20.DDOV1](#ddov1) | json:"ddo_v1" | DDOV1 represents a product v1 configuration for Direct Data Onboarding (DDO)  |

### DDOV1

DDOV1 defines a structure for handling provider, client, and piece manager information with associated contract and notification details
for a DDO deal handling.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| Provider | [address.Address](https://pkg.go.dev/github.com/filecoin-project/go-address#Address) | json:"provider" | Provider specifies the address of the provider  |
| Client | [address.Address](https://pkg.go.dev/github.com/filecoin-project/go-address#Address) | json:"client" | Client represents the address of the deal client  |
| PieceManager | [address.Address](https://pkg.go.dev/github.com/filecoin-project/go-address#Address) | json:"piece_manager" | Actor providing AuthorizeMessage (like f1/f3 wallet) able to authorize actions such as managing ACLs  |
| Duration | [abi.ChainEpoch](https://pkg.go.dev/github.com/filecoin-project/go-state-types/abi#ChainEpoch) | json:"duration" | Duration represents the deal duration in epochs. This value is ignored for the deal with allocationID. It must be at least 518400  |
| AllocationId | [*verifreg.AllocationId](https://pkg.go.dev/github.com/filecoin-project/go-state-types/builtin/v16/verifreg#AllocationId) | json:"allocation_id" | AllocationId represents an aggregated allocation identifier for the deal.  |
| ContractAddress | [string](https://pkg.go.dev/builtin#string) | json:"contract_address" | ContractAddress specifies the address of the contract governing the deal  |
| ContractVerifyMethod | [string](https://pkg.go.dev/builtin#string) | json:"contract_verify_method" | ContractDealIDMethod specifies the method name to verify the deal and retrieve the deal ID for a contract  |
| ContractVerifyMethodParams | [[]byte](https://pkg.go.dev/builtin#byte) | json:"contract_verify_method_params" | ContractDealIDMethodParams represents encoded parameters for the contract verify method if required by the contract  |
| NotificationAddress | [string](https://pkg.go.dev/builtin#string) | json:"notification_address" | NotificationAddress specifies the address to which notifications will be relayed to when sector is activated  |
| NotificationPayload | [[]byte](https://pkg.go.dev/builtin#byte) | json:"notification_payload" | NotificationPayload holds the notification data typically in a serialized byte array format.  |
| Indexing | [bool](https://pkg.go.dev/builtin#bool) | json:"indexing" | Indexing indicates if the deal is to be indexed in the provider's system to support CIDs based retrieval  |
| AnnounceToIPNI | [bool](https://pkg.go.dev/builtin#bool) | json:"announce_to_ipni" | AnnounceToIPNI indicates whether the deal should be announced to the Interplanetary Network Indexer (IPNI).  |

### DataSourceAggregate

DataSourceAggregate represents an aggregated data source containing multiple individual DataSource pieces.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| Pieces | [[]mk20.DataSource](#datasource) | json:"pieces" |  |

### DataSourceHTTP

DataSourceHTTP represents an HTTP-based data source for retrieving piece data, including its raw size and associated URLs.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| RawSize | [uint64](https://pkg.go.dev/builtin#uint64) | json:"rawsize" | RawSize specifies the raw size of the data in bytes.  |
| URLs | [[]mk20.HttpUrl](#httpurl) | json:"urls" | URLs lists the HTTP endpoints where the piece data can be fetched.  |

### DataSourceHttpPut

DataSourceHttpPut represents a data source allowing clients to push piece data after a deal is accepted.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| RawSize | [uint64](https://pkg.go.dev/builtin#uint64) | json:"raw_size" | RawSize specifies the raw size of the data in bytes.  |

### DataSourceOffline

DataSourceOffline represents the data source for offline pieces, including metadata such as the raw size of the piece.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| RawSize | [uint64](https://pkg.go.dev/builtin#uint64) | json:"raw_size" | RawSize specifies the raw size of the data in bytes.  |

### DealStatusResponse

DealStatusResponse represents the response of a deal's status, including its current state and an optional error message.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| State | [mk20.DealState](#constants-for-dealstate) | json:"status" | State indicates the current processing state of the deal as a DealState value.  |
| ErrorMsg | [string](https://pkg.go.dev/builtin#string) | json:"error_msg" | ErrorMsg is an optional field containing error details associated with the deal's current state if an error occurred.  |

### FormatAggregate

FormatAggregate represents the aggregated format for piece data, identified by its type.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| Type | [mk20.AggregateType](https://pkg.go.dev/github.com/filecoin-project/curio/market/mk20#AggregateType) | json:"type" | Type specifies the type of aggregation for data pieces, represented by an AggregateType value.  |
| Sub | [[]mk20.PieceDataFormat](#piecedataformat) | json:"sub" | Sub holds a slice of PieceDataFormat, representing various formats of piece data aggregated under this format. The order must be same as segment index to avoid incorrect indexing of sub pieces in an aggregate  |

### FormatBytes

FormatBytes defines the raw byte representation of data as a format.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|

### FormatCar

FormatCar represents the CAR (Content Addressable archive) format for piece data serialization.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|

### HttpUrl

HttpUrl represents an HTTP endpoint configuration for fetching piece data.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| URL | [string](https://pkg.go.dev/builtin#string) | json:"url" | URL specifies the HTTP endpoint where the piece data can be fetched.  |
| Headers | [http.Header](https://pkg.go.dev/net/http#Header) | json:"headers" | HTTPHeaders represents the HTTP headers associated with the URL.  |
| Priority | [uint64](https://pkg.go.dev/builtin#uint64) | json:"priority" | Priority indicates the order preference for using the URL in requests, with lower values having higher priority.  |
| Fallback | [bool](https://pkg.go.dev/builtin#bool) | json:"fallback" | Fallback indicates whether this URL serves as a fallback option when other URLs fail.  |

### PieceDataFormat

PieceDataFormat represents various formats in which piece data can be defined, including CAR files, aggregate formats, or raw byte data.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| Car | [*mk20.FormatCar](#formatcar) | json:"car" | Car represents the optional CAR file format, including its metadata and versioning details.  |
| Aggregate | [*mk20.FormatAggregate](#formataggregate) | json:"aggregate" | Aggregate holds a reference to the aggregated format of piece data.  |
| Raw | [*mk20.FormatBytes](#formatbytes) | json:"raw" | Raw represents the raw format of the piece data, encapsulated as bytes.  |

### SupportedContracts

SupportedContracts represents a collection of contract addresses supported by a system or application.

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| Contracts | [[]string](https://pkg.go.dev/builtin#string) | json:"contracts" | Contracts represents a list of supported contract addresses in string format.  |

### Constants for ErrorCode

| Constant | Code | Description |
|----------|------|-------------|
| Ok | 200 | Ok represents a successful operation with an HTTP status code of 200. |
| ErrBadProposal | 400 | ErrBadProposal represents a validation error that indicates an invalid or malformed proposal input in the context of validation logic. |
| ErrMalformedDataSource | 430 | ErrMalformedDataSource indicates that the provided data source is incorrectly formatted or contains invalid data. |
| ErrUnsupportedDataSource | 422 | ErrUnsupportedDataSource indicates the specified data source is not supported or disabled for use in the current context. |
| ErrUnsupportedProduct | 423 | ErrUnsupportedProduct indicates that the requested product is not supported by the provider. |
| ErrProductNotEnabled | 424 | ErrProductNotEnabled indicates that the requested product is not enabled on the provider. |
| ErrProductValidationFailed | 425 | ErrProductValidationFailed indicates a failure during product-specific validation due to invalid or missing data. |
| ErrDealRejectedByMarket | 426 | ErrDealRejectedByMarket indicates that a proposed deal was rejected by the market for not meeting its acceptance criteria or rules. |
| ErrServiceMaintenance | 503 | ErrServiceMaintenance indicates that the service is temporarily unavailable due to maintenance, corresponding to HTTP status code 503. |
| ErrServiceOverloaded | 429 | ErrServiceOverloaded indicates that the service is overloaded and cannot process the request at the moment. |
| ErrMarketNotEnabled | 440 | ErrMarketNotEnabled indicates that the market is not enabled for the requested operation. |
| ErrDurationTooShort | 441 | ErrDurationTooShort indicates that the provided duration value does not meet the minimum required threshold. |

### Constants for DealState

| Constant | Code | Description |
|----------|------|-------------|
| DealStateAccepted | "accepted" | DealStateAccepted represents the state where a deal has been accepted and is pending further processing in the system. |
| DealStateProcessing | "processing" | DealStateProcessing represents the state of a deal currently being processed in the pipeline. |
| DealStateSealing | "sealing" | DealStateSealing indicates that the deal is currently being sealed in the system. |
| DealStateIndexing | "indexing" | DealStateIndexing represents the state where a deal is undergoing indexing in the system. |
| DealStateFailed | "failed" | DealStateFailed indicates that the deal has failed due to an error during processing, sealing, or indexing. |
| DealStateComplete | "complete" | DealStateComplete indicates that the deal has successfully completed all processing and is finalized in the system. |

