# Market 2.0 HTTP API

Base path: `/market/mk20`  
`{id}` is the deal identifier (ULID).

## Authentication

All Market 2.0 API routes require auth except `/info/*`.

Auth details are documented in:
[Architecture: Authentication and Authorization](./architecture.md#authentication-and-authorization-current-implementation)

## API Compatibility

Market 2.0 APIs are expected to evolve conservatively, with backward compatibility as a primary consideration.

## Swagger and OpenAPI Specs

Use the running storage provider spec first for integration and debugging.

Running provider endpoints:

1. `GET /market/mk20/info/`
2. `GET /market/mk20/info/swagger.yaml`
3. `GET /market/mk20/info/swagger.json`

Source tree specs:

1. [`market/mk20/http/swagger.yaml`](../../../market/mk20/http/swagger.yaml)
2. [`market/mk20/http/swagger.json`](../../../market/mk20/http/swagger.json)

Version note:

1. A running provider may be on a different Curio version than this documentation.
2. If behavior differs, use that provider's `/info/swagger.*` as the operational reference.

## Endpoint Map

1. `POST /deal`
2. `GET /status/{id}`
3. `POST /update/{id}`
4. `GET /products`
5. `GET /sources`
6. `GET /contracts`
7. `POST /uploads/{id}`
8. `GET /uploads/{id}`
9. `PUT /uploads/{id}/{chunkNum}`
10. `POST /uploads/finalize/{id}`
11. `PUT /upload/{id}`
12. `POST /upload/{id}`

## Endpoint Details

## `POST /deal`

Creates a deal.

Request:

1. Body: `mk20.Deal` JSON.

Success:

1. `200` `Ok`

Error codes:

1. `400` `ErrBadProposal`
2. `401` `ErrUnAuthorized`
3. `404` `ErrDealNotFound`
4. `422` `ErrUnsupportedDataSource`
5. `423` `ErrUnsupportedProduct`
6. `424` `ErrProductNotEnabled`
7. `425` `ErrProductValidationFailed`
8. `426` `ErrDealRejectedByMarket`
9. `429` `ErrServiceOverloaded`
10. `430` `ErrMalformedDataSource`
11. `440` `ErrMarketNotEnabled`
12. `441` `ErrDurationTooShort`
13. `500` `ErrServerInternalError`
14. `503` `ErrServiceMaintenance`

Response note:

1. Error body is plain text: `Reason: <text>`.

## `GET /status/{id}`

Returns deal status by product.

Success:

1. `200` `mk20.DealProductStatusResponse`

Error codes:

1. `400` invalid/missing id
2. `401` `ErrUnAuthorized`
3. `404` `ErrDealNotFound`
4. `500` `ErrServerInternalError`

## `POST /update/{id}`

Updates an existing deal in supported update paths.

Request:

1. Body: `mk20.Deal` JSON.

Success:

1. `200` `Ok`

Error codes:

1. `400` `ErrBadProposal`
2. `401` `ErrUnAuthorized`
3. `404` `ErrDealNotFound`
4. `422` `ErrUnsupportedDataSource`
5. `423` `ErrUnsupportedProduct`
6. `424` `ErrProductNotEnabled`
7. `425` `ErrProductValidationFailed`
8. `426` `ErrDealRejectedByMarket`
9. `429` `ErrServiceOverloaded`
10. `430` `ErrMalformedDataSource`
11. `440` `ErrMarketNotEnabled`
12. `441` `ErrDurationTooShort`
13. `500` `ErrServerInternalError`
14. `503` `ErrServiceMaintenance`

## `GET /products`

Returns enabled products for this provider.

Success:

1. `200` `{ "products": [...] }`

Error codes:

1. `401` `ErrUnAuthorized`
2. `500` `ErrServerInternalError`

## `GET /sources`

Returns enabled data source types for this provider.

Success:

1. `200` `{ "sources": [...] }`

Error codes:

1. `401` `ErrUnAuthorized`
2. `500` `ErrServerInternalError`

## `GET /contracts`

Returns allowlisted DDO market contracts for this provider.

Success:

1. `200` `{ "contracts": [...] }`

Error codes:

1. `401` `ErrUnAuthorized`
2. `404` no supported contracts found
3. `500` `ErrServerInternalError`

## `POST /uploads/{id}`

Starts chunked upload session.

Request:

1. Body: `StartUpload` (`raw_size`, `chunk_size`).

Success:

1. `200` `UploadStartCodeOk`

Error codes:

1. `400` `UploadStartCodeBadRequest`
2. `401` `ErrUnAuthorized`
3. `404` `UploadStartCodeDealNotFound`
4. `409` `UploadStartCodeAlreadyStarted`
5. `500` `UploadStartCodeServerError`

## `GET /uploads/{id}`

Returns chunked upload progress.

Success:

1. `200` `UploadStatusCodeOk`

Error codes:

1. `400` invalid/missing id
2. `401` `ErrUnAuthorized`
3. `404` `UploadStatusCodeDealNotFound`
4. `425` `UploadStatusCodeUploadNotStarted`
5. `500` `UploadStatusCodeServerError`

## `PUT /uploads/{id}/{chunkNum}`

Uploads one chunk.

Success:

1. `200` `UploadOk`

Error codes:

1. `400` `UploadBadRequest`
2. `401` `ErrUnAuthorized`
3. `404` `UploadNotFound`
4. `409` `UploadChunkAlreadyUploaded`
5. `429` `UploadRateLimit`
6. `500` `UploadServerError`

## `POST /uploads/finalize/{id}`

Finalizes chunked upload. Body may be empty or full `mk20.Deal`.

Success:

1. `200` `Ok`

Error codes:

1. `400` `ErrBadProposal`
2. `401` `ErrUnAuthorized`
3. `404` `ErrDealNotFound`
4. `422` `ErrUnsupportedDataSource`
5. `423` `ErrUnsupportedProduct`
6. `424` `ErrProductNotEnabled`
7. `425` `ErrProductValidationFailed`
8. `426` `ErrDealRejectedByMarket`
9. `429` `ErrServiceOverloaded`
10. `430` `ErrMalformedDataSource`
11. `440` `ErrMarketNotEnabled`
12. `441` `ErrDurationTooShort`
13. `500` `ErrServerInternalError`
14. `503` `ErrServiceMaintenance`

## `PUT /upload/{id}`

Uploads full payload in serial flow.

Success:

1. `200` `UploadOk`

Error codes:

1. `400` `UploadBadRequest`
2. `401` `ErrUnAuthorized`
3. `404` `UploadStartCodeDealNotFound`
4. `500` `UploadServerError`

## `POST /upload/{id}`

Finalizes serial upload. Body may be empty or full `mk20.Deal`.

Success:

1. `200` `Ok`

Error codes:

1. `400` `ErrBadProposal`
2. `401` `ErrUnAuthorized`
3. `404` `ErrDealNotFound`
4. `422` `ErrUnsupportedDataSource`
5. `423` `ErrUnsupportedProduct`
6. `424` `ErrProductNotEnabled`
7. `425` `ErrProductValidationFailed`
8. `426` `ErrDealRejectedByMarket`
9. `429` `ErrServiceOverloaded`
10. `430` `ErrMalformedDataSource`
11. `440` `ErrMarketNotEnabled`
12. `441` `ErrDurationTooShort`
13. `500` `ErrServerInternalError`
14. `503` `ErrServiceMaintenance`

## Operational Notes

1. Upload finalize endpoints accept empty body or full deal payload.
2. Provider policies for products, sources, and contracts affect acceptance behavior.
3. When behavior differs from docs, trust the running provider `/info/swagger.*`.
