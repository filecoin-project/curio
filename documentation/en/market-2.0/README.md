# Market 2.0

This guide introduces the new Filecoin Market 2.0 architecture for clients, developers, and aggregators. It explains how to use the new modular, contract-governed storage market and how to interact with Curio-based storage providers under this new system.

---

## 🧭 Overview

Filecoin's Market 2.0 removes legacy assumptions of the built-in storage market actor. Instead, deals are processed through **user-defined smart contracts**, allowing:

* Flexible pricing and service terms
* Support for custom retrieval logic
* Contract-governed deal lifecycle
* Composability via extensible "products"

Curio's role is purely to onboard data and respect contract terms—it does not mediate pricing, payments, or retrieval policy.

---

## 🔧 Lotus prerequisites (for Curio market nodes)

Curio market nodes rely on Lotus’ Ethereum JSON-RPC compatibility layer for some operations.

Make sure your Lotus node has:

- `EnableEthRPC = true`

If this is disabled, Curio market operations may fail.

## 📡 Supported Endpoints

### 🔄 POST `/market/mk20/store`

Accept a new deal (JSON body).

* Auto-validates structure, products, sources, contract
* If valid, returns `200 OK`
* Otherwise returns appropriate error code (e.g. `400`, `422`, etc)

### 🧾 GET `/market/mk20/status?id=<ulid>`

Check the status of a deal.

* Returns one of: `accepted`, `processing`, `sealing`, `indexing`, `complete`, or `failed`

### 🗂 PUT `/market/mk20/data?id=<ulid>`

Used only when `source_httpput` is selected.

* Clients upload raw bytes
* `Content-Length` must match raw size

### 📜 GET `/market/mk20/contracts`

Return list of supported contract addresses

### 🧠 GET `/market/mk20/info`

Markdown documentation of deal format and validation rules

### 🧠 GET `/market/mk20/producs`

Json list of products supported by the provider

### 🧠 GET `/market/mk20/sources`

Json list of data sources supported by the provider

---

## 🧑‍💻 Clients

### 📝 Submitting a Deal

Clients submit a deal to a Curio node using the `/market/mk20/store` endpoint. A deal includes:

* A unique ULID identifier
* A `DataSource` (e.g. HTTP, offline, PUT)
* One or more `Products` (like `ddov1`) that define how the deal should behave

#### Example Products:

* `ddov1`: governs how the data should be stored and verified
* (future) `aclv1`: may define retrieval access controls
* (future) `retrievalv1`: may define retrieval SLA or payment terms

### 🛠 Smart Contract Control

Clients must select a contract that:

* Is supported by the SP
* Implements a deal validation method that returns a valid DealID

Clients pass the contract address, method name, and encoded params.

### 🔁 Deal Lifecycle

The contract governs whether the deal is valid. If valid:

* The SP accepts and starts onboarding the data
* The deal may be indexed and/or announced to IPNI, based on deal config
* Data may be retrieved later via PieceCIDv2, PayloadCID, or subpiece CID

---

## 🧱 Developers

### 🧩 Building New Products

Each product is a self-contained struct in the deal payload. Developers can:

* Define new product types (e.g., `aclv1`, `retrievalv1`, `auditv1`)
* Implement validation logic on the SP side
* Optionally affect indexing, retrievals, ACLs, or other lifecycle aspects

This makes the deal structure extensible **without requiring protocol or DB changes.**

### 🧠 Writing Market Contracts

A contract must:

* Be added to the SP's whitelist
* Implement a method (e.g. `verifyDeal`) that takes a single `bytes` parameter
* Return a valid deal ID if the deal is accepted

Contracts can implement features like:

* Off-chain or on-chain ACL logic
* Multi-party deal approval
* FIL+ verification
* SLA enforcement

---

## 🔁 Aggregators

Data aggregators in Market 2.0 should:

* No longer implement protocol-level workarounds (like ACLs or approvals)
* Provide value-added services like dashboards, alerts, analytics, SDKs
* Optionally act as data sources for multi-client deal generation

Market 2.0 aims to reduce dependency on aggregators by letting providers and contracts do the heavy lifting.

---

## 📦 Retrievals

Curio supports the following retrieval inputs:

* **PieceCIDv2**: required for all piece-level retrievals
* **PayloadCID**: if indexing is enabled
* **Subpiece CID**: if the deal was aggregated and subpieces were indexed

ACL-based gating is not yet implemented, but future products can enable it.

---

## ♻️ Deal Lifecycle in Curio

1. **Client submits** deal with products, data, and contract call info
2. **Curio validates** all inputs and uses the contract to get a DealID
3. **Data is onboarded** via HTTP, offline import, PUT, or aggregation
4. **Products control** indexing, IPNI, and future extensibility
5. **Data is removed** from disk and DB when the sector expires

---

## 🧪 Current Product: `ddov1`

This product represents the first non-native Filecoin market product. It includes:

* Provider, client, and piece manager addresses
* Optional AllocationID (or minimum duration)
* Contract + verification method for DealID
* Indexing and IPNI flags
* Notification hooks for deal lifecycle events

More details can be found in the product schema or SP guide.

---

## 🔮 Future Directions

* ACL enforcement via `aclv1`
* Retrieval policy enforcement via `retrievalv1`
* Sealed sector access / download pricing
* Smart contract SLAs and renewals
* Market UIs and dashboards

---

## ✅ Summary

Market 2.0 enables:

* Composable, contract-governed storage deals
* Modular product design
* Client-friendly HTTP-first onboarding
* Decoupled market innovation from SP software
* Stronger integration paths for aggregators and external tools

