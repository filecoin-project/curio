# MK20 API Reference

This document describes the HTTP endpoints supported by the Market 2.0 module in Curio. These endpoints are used by clients and external systems to submit storage deals, upload data, track status, and fetch provider configuration.

---

## 🌐 Base Path

All endpoints are exposed under:

```
/market/mk20
```

---

## 🔄 POST `/store`

Submit a new MK20 deal to the storage provider.

* **Content-Type**: `application/json`
* **Body**: JSON-encoded Deal
* **Query Parameters**: None

### ✅ Response

* `200 OK` – Deal accepted successfully
* `400 Bad Request` – Malformed JSON or missing required fields
* `422 Unprocessable Entity` – Unsupported product or data source
* `426` – Deal rejected by contract
* `430` – Malformed data source
* `441` – Deal duration too short

### 🧪 Example

```http
POST /market/mk20/store
Content-Type: application/json

{
  "identifier": "01H9Y...",
  "data": { ... },
  "products": { ... }
}
```

---

## 🧾 GET `/status`

Retrieve the current processing status of a deal.

* **Query Parameters**:

    * `id`: ULID of the deal

### ✅ Response

* `200 OK`: Returns JSON-encoded status
* `400 Bad Request`: Missing or malformed `id`
* `404 Not Found`: No such deal
* `500 Internal Server Error`: Unexpected backend error

### 📄 Response Schema

```json
{
  "status": "accepted" | "processing" | "sealing" | "indexing" | "complete" | "failed",
  "error_msg": "string (optional)"
}
```

---

## 🗂 PUT `/data`

Upload raw deal data for deals that declared a `source_httpput` source.

* **Headers**:

    * `Content-Type: application/octet-stream`
    * `Content-Length`: must match declared raw size
* **Query Parameters**:

    * `id`: ULID of the deal
* **Body**: Raw byte stream

### ✅ Response

* `200 OK`: Data accepted
* `400 Bad Request`: Invalid/missing content headers
* `413 Payload Too Large`: Content exceeds allowed size
* `415 Unsupported Media Type`: Incorrect content type

---

## 📜 GET `/contracts`

Return a list of smart contract addresses currently whitelisted by the provider.

### ✅ Response

```json
{
  "contracts": [
    "0x123...",
    "0xabc..."
  ]
}
```

* `200 OK`: List of contracts
* `500 Internal Server Error`: Failure fetching contract list

---

## 🧠 GET `/info`

Returns markdown-formatted documentation describing:

* Supported deal structure
* Data source formats
* Product extensions

### ✅ Response

* `200 OK`: Markdown string
* `500 Internal Server Error`: If the info file cannot be generated

---

### 🧰 GET `/products`

Fetch a JSON list of supported deal products enabled on this provider.

- **Content-Type**: N/A
- **Body**: N/A
- **Query Parameters**: N/A

#### ✅ Response
- `200 OK`: JSON array of enabled products
- `500 Internal Server Error`: If the list cannot be fetched

#### 🧪 Example Response
```json
{
  "products": [
    "ddo_v1",
    "aclv1"
  ]
}
```

---

### 🌐 GET `/sources`

Fetch a JSON list of supported data source types enabled on this provider.

- **Content-Type**: N/A
- **Body**: N/A
- **Query Parameters**: N/A

#### ✅ Response
- `200 OK`: JSON array of enabled data sources
- `500 Internal Server Error`: If the list cannot be fetched

#### 🧪 Example Response
```json
{
  "sources": [
    "http",
    "offline",
    "put",
    "aggregate"
  ]
}
```

---

## 📑 Error Code Summary

| Code | Meaning                            |
| ---- | ---------------------------------- |
| 200  | Success                            |
| 400  | Bad proposal or malformed JSON     |
| 422  | Unsupported product or data source |
| 426  | Deal rejected by contract          |
| 430  | Malformed data source              |
| 441  | Duration too short                 |
| 500  | Internal server error              |

---

## 🧩 Status Code Values (from `/status`)

| Value        | Meaning                                         |
| ------------ | ----------------------------------------------- |
| `accepted`   | Deal was accepted and is waiting for processing |
| `processing` | Deal is being staged or fetched                 |
| `sealing`    | Deal is being sealed into a sector              |
| `indexing`   | Deal is being indexed for CID-based retrievals  |
| `complete`   | Deal has been sealed and is finalized           |
| `failed`     | Deal failed at any point in the pipeline        |

---

For full type schemas, see the `/info` endpoint or consult the documentation.
