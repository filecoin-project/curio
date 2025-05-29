# Products & Extensibility Guide

Market 2.0 introduces a fully extensible framework for storage deal configuration. This guide explains how products work, how new ones can be added, and how developers and providers can safely evolve without changing core Curio logic.

---

## 🧩 What Is a Product?

A **product** is a named section of a deal that adds optional logic or configuration. Each product defines one or more aspects of the deal lifecycle.

Examples:

* `ddov1` – controls how data is onboarded and what contract governs it
* `aclv1` *(future)* – may define access control permissions
* `retrievalv1` *(future)* – may define retrieval conditions or SLA pricing

Each product is a top-level field in the `products` object in a deal:

```json
"products": {
  "ddo_v1": { ... },
  "aclv1": { ... }
}
```

---

## 🛠 Product Responsibilities

A product may:

* Validate a deal before acceptance
* Provide smart contract call details
* Affect retrieval behavior (e.g. IPNI, ACLs)
* Receive notifications (e.g. on sector sealing)

A product **must not**:

* Trigger storage actions directly (Curio handles onboarding)
* Conflict with other products
* Depend on runtime configuration (products are static per deal)

---

## 📐 Product Structure

All products are Go structs that implement the following interface-like behavior:

* A `.Validate(*DB, *Config) (ErrorCode, error)` method
* Optional `.GetDealID()` logic if a contract call is needed
* Unique product name (`ProductNameDDOV1`, etc)

Products are stored in JSON under the `products` field.

---

## 🧪 Example: `ddov1`

The `ddov1` product includes:

* Provider, client, and piece manager addresses
* Duration or allocation ID
* Smart contract call details: address, method, params
* Flags for indexing and IPNI
* Optional notification hooks

Curio uses these fields to validate the deal, determine storage lifecycle, and optionally announce to IPNI.

---

## 🛡 ACLs and Retrieval Products (Future)

Market 2.0 was designed to support retrieval-layer enforcement through:

* ACL products (e.g., define who can retrieve what, when)
* Retrieval policy products (e.g., define pricing, terms)

These will live alongside onboarding products like `ddov1`.

---

## ✅ Design Philosophy

* Each product handles one concern
* Multiple products can be included in one deal
* Future products won't require code changes to existing ones
* Extensibility is done via composition, not inheritance

---

## 📦 Summary

| Concept      | Description                                              |
| ------------ | -------------------------------------------------------- |
| Product      | Modular block in a deal defining optional behavior       |
| Validation   | Each product validates its own logic                     |
| Contract     | Products may define how to obtain deal ID                |
| Future-proof | New products can be added without DB or protocol changes |

Products are the core of Market 2.0's flexibility—allowing new ideas to be layered in without disrupting existing workflows.

# Write Your Own Product – Developer Guide

This guide walks developers through creating a custom **product** for Market 2.0. Products add modular capabilities to deals—ranging from storage control to retrieval logic, ACLs, SLAs, and beyond.

---

Each product must:

* Implement validation
* Optionally provide contract call instructions (if needed)
* Return its canonical product name

---

## 🧱 Structure of a Product

Each product is a Go struct with a few key methods:

```go
type MyProduct struct {
    SomeField string `json:"some_field"`
    // More fields...
}

func (p *MyProduct) Validate(db *harmonydb.DB, cfg *config.MK20Config) (ErrorCode, error) {
    // Check for required fields
    // Enforce constraints
    return Ok, nil
}

func (p *MyProduct) ProductName() ProductName {
    return "myproductv1"
}
```

---

## 🛠 Adding a New Product (Step-by-Step)

### 1. Define Struct in `types.go`

Add a new `MyProduct` struct to the `Products` block:

```go
type Products struct {
    DDOV1 *DDOV1 `json:"ddo_v1"`
    MyProduct *MyProduct `json:"myproduct_v1"`
}
```

### 2. Implement `.Validate()`

Use `Validate()` to define how the product ensures the deal is valid.
You may:

* Check required fields
* Enforce logic (e.g. if X is true, Y must also be set)
* Query DB if needed

Return `ErrorCode` and reason for failure.

### 3. Optionally: Contract Integration

If your product relies on a contract, implement:

```go
func (p *MyProduct) GetDealID(...) (string, ErrorCode, error)
```

This is how `ddov1` fetches DealID via contract call.

### 4. Add to JSON Marshal/Unmarshal

Nothing needed—`Products` already uses JSON tags.
Curio stores each product as a JSON field under `products` in DB.

### 5. Update UI Toggle Support (Optional)

Add a toggle entry in the admin panel:

* `market_mk20_products` table
* Use your product name as key (`myproduct_v1`)
* Enable or disable per deployment

### 6. Document via `/info`

Update the markdown generator so your product shows up in `/market/mk20/info`.

---

## 🧪 Example Use Case: Retrieval Policy

You might want to create `retrievalv1` with:

```go
type RetrievalV1 struct {
    PayPerByte bool   `json:"pay_per_byte"`
    MaxBandwidth int  `json:"max_bandwidth_kbps"`
    AllowedIPs []string `json:"allowed_ips"`
}
```

And enforce in `.Validate()`:

```go
if p.PayPerByte && p.MaxBandwidth == 0 {
    return ErrProductValidationFailed, xerrors.Errorf("bandwidth limit required for paid retrieval")
}
```

Later, your retrieval service can look up this product and apply pricing.

---

## ✅ Guidelines

| Rule               | Description                               |
| ------------------ | ----------------------------------------- |
| ✅ Modular          | Product should only affect its own logic  |
| ✅ Optional         | Products are opt-in per deal              |
| ✅ Composable       | Multiple products can exist in one deal   |
| ❌ No Runtime State | Product logic is static and stateless     |
| ❌ No Storage Logic | Curio handles onboarding, not the product |

---

## 🔄 Deployment Considerations

* Curio does not require a restart to recognize new products
* Products not enabled in DB will be rejected during validation
* Ensure all field names are `snake_case` in JSON

---

## 📦 Summary

Products are the extension mechanism of Market 2.0:

* Validated independently
* Optional per deal
* Zero-conflict by design
* Fully extensible without schema or protocol changes

Use them to inject new behaviors into Curio without touching the base system.

