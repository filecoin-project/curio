# Contracts

This section covers Market 2.0 contract integration for product developers and contract integrators.

## Contract Development Basics

When designing a contract to integrate with Curio:

1. Prefer read-only verification interfaces for deal checks.
2. Use explicit interface versioning.
3. Keep returned field semantics stable and deterministic.
4. Use a deterministic not-found contract error for missing deal references.
5. Treat provider allowlisting as a separate operational policy.

## DDO

Current DDO integration uses:

1. [CurioDealView v1](./curiodealview.md)

Future products can add their own contract interfaces without changing the top-level Market 2.0 deal envelope.
