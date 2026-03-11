# Extending MK20 With New Products

This guide is for developers adding new MK20 products while keeping client and operator behavior predictable.

## Product Definition Checklist

A new product should define:

- payload schema under `mk20.Products`
- product name
- product validation behavior
- clear failure mapping to existing MK20 status/error codes

Before implementation, define:

- required vs optional fields
- cross-product compatibility rules
- source/format assumptions
- expected lifecycle and status transitions

## Integration Checklist

1. Add product type and field in MK20 deal types.
2. Register product name and include it in validation composition rules.
3. Add execution handling for accept/process/finalize paths.
4. Add upload-first handling if the product supports `put` flow.
5. Extend status derivation if lifecycle differs from existing products.
6. Add operational WebRPC methods when operator visibility/control is required.
7. Add migrations for product-specific persistent state.
8. Add product documentation under `documentation/en/market-2.0/products/`.

## Compatibility Expectations

New products should not weaken these platform guarantees:

- authenticated client identity checks
- deal ownership checks on `{id}` routes
- piece identity invariants
- existing product behavior stability

## Documentation Expectations

For each new product, document:

- target use case
- field contract
- validation rules
- lifecycle effects
- common rejection scenarios
