# Products

Products are the service contract between client intent and provider execution in Market 2.0.

This section documents the currently supported product families, how they fit into the common deal model, and how to evolve product behavior without breaking compatibility.

In practice, products answer one question: what operation should Curio perform for this deal.

## Current Products

1. [DDO v1](./ddo_v1.md)
2. [Retrieval v1](./retrieval_v1.md)
3. [PDP v1](./pdp_v1.md)

## Product Model Basics

1. Product-specific payloads live under `deal.products`.
2. Product-specific validation must be explicit and deterministic.
3. Product behavior should map cleanly to Market 2.0 lifecycle/status behavior.
4. Product errors should map to existing MK20 error codes where possible.

## Product Definition Checklist

A product definition should clearly specify:
1. Payload schema under `products.<name>`.
2. Product name and compatibility scope.
3. Required vs optional fields.
4. Validation rules and rejection conditions.
5. Source/format assumptions (if data is required).
6. Lifecycle/status effects.
7. Error mapping expectations.
8. Operational controls required by storage providers.

## Extending Products

### When to Extend an Existing Product

Extend an existing product when:
1. Behavior remains backward compatible.
2. Existing clients can continue to parse and use payloads safely.
3. JSON marshal/unmarshal compatibility is preserved.

### When to Add a New Product

Add a new product when:
1. Behavior or lifecycle differs materially.
2. Compatibility contracts diverge from existing product semantics.
3. Overloading an existing product would create ambiguous behavior.

### Implementation Checklist

1. Add product type in MK20 deal/product types.
2. Register product name and validation rules.
3. Add intake/processing/finalize handling.
4. Add upload-path handling if product supports `source_http_put`.
5. Extend status derivation if lifecycle differs.
6. Add DB migrations for product state.
7. Add WebRPC/UI support if operator controls are required.
8. Add/update product docs in this directory.

### Compatibility Rules

1. Do not weaken auth and deal ownership guarantees.
2. Keep piece identity invariants intact.
3. Avoid breaking existing product behavior.

### Documentation Requirements

For each product, document:
1. Use case
2. Payload fields
3. Validation rules
4. Lifecycle/status effects
5. Common rejection scenarios
