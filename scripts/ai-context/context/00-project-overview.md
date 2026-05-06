# Curio Project Overview

## What Curio is
Curio is a clustered Filecoin Storage Provider runtime focused on replacing the legacy `lotus-miner`/`lotus-worker` operational model with a single `curio` binary plus shared control-plane state in YugabyteDB. It is designed for high availability, multi-node scheduling, and multi-miner operation with centralized configuration layering.

## Main problem areas
- Distributed sealing and proving orchestration (SDR, trees, PoRep, PoSt, commit/precommit messaging).
- Reliable on-chain interaction under strict timing and nonce constraints.
- Deal intake, indexing, IPNI announcements, and retrieval serving across a cluster.
- Multi-node storage management (sealing vs long-term storage, path metadata, GC, recovery).
- Operator ergonomics: layered config, GUI/RPC control, alerting, and maintenance workflows.

## Major subsystems
- `harmony/harmonydb`: DB abstraction, schema migrations, and cluster coordination state.
- `harmony/harmonytask` + `harmony/resources`: distributed task engine (polling, claiming, retries, resource gating).
- `tasks/*`: concrete task implementations (sealing, snap, window/winning post, message queue, indexing, GC, balance manager, proofshare, PDP).
- `lib/chainsched` + `tasks/message`: chain head callbacks and message send/watch pipelines.
- `market/*`: market implementations (`mk12`, `mk20`), libp2p endpoints, retrieval, and indexstore integration.
- `cuhttp` + `market/http` + `pdp`: public HTTP server for market/retrieval/IPNI/PDP routes.
- `web/*`: Curio Web UI + JSON-RPC-backed dashboards and admin views.
- `deps/*` + `deps/config/*`: dependency wiring and typed config system (including dynamic/layered behavior).

## External systems and dependencies
- Filecoin chain nodes (Lotus API; ETH-RPC compatibility is required for some market/PDP paths).
- YugabyteDB is core infrastructure (YSQL for HarmonyDB state; YCQL/Cassandra protocol for indexstore).
- Filecoin proving stack via `filecoin-ffi`; optional CUDA/OpenCL/SupraSeal acceleration paths.
- Libp2p + IPNI ecosystem for deal/discovery/index announcements.
- Prometheus metrics + optional Alertmanager/PagerDuty/Slack integrations.
- Let’s Encrypt/autocert support in Curio HTTP mode when TLS is not delegated.

## Primary languages and where they are used
- Go: primary implementation across runtime, tasks, APIs, market, HTTP, and tooling.
- SQL: HarmonyDB schema migrations in `harmony/harmonydb/sql`.
- CQL: indexstore schema in `market/indexstore/cql`.
- JavaScript/HTML/CSS: Web UI under `web/static` (ES modules + Lit-based components).
- Shell/Make: build/test/devnet automation in `scripts/makefiles`, `docker/`, CI workflows.

## What makes this codebase non-trivial
- The scheduler is distributed and DB-coordinated, not a single-process queue.
- Task correctness depends on chain timing, retries, and reorg-aware callbacks.
- Hardware-sensitive execution paths (GPU/CPU/OpenCL/CUDA/SupraSeal) materially change behavior and build requirements.
- State is long-lived in DB and frequently evolved via migrations; runtime behavior is tightly coupled to schema.
- Multiple product surfaces coexist: sealing/proving, market MK1.2/MK2.0, retrieval, IPNI, PDP, proofshare.

## Things an AI should understand before suggesting changes
- Treat Yugabyte-backed schema/state as part of the protocol of the system; schema and task logic must evolve together.
- Respect task idempotency and ownership/claim semantics in HarmonyTask; duplicate work prevention is task-defined.
- On-chain messaging logic is safety-critical (nonce ordering, batching, retries, failure handling).
- Config is layered (`base` always included) and can affect many machines/miners at once; avoid node-local assumptions.
- Curio is cluster software: scheduling, storage access, and maintenance actions (cordon/uncordon) have cross-node effects.
- PoSt and sealing concurrency has explicit operational safety warnings in code/docs.
- Test strategy is integration-heavy (local PG + Scylla for dev; CI matrix shards); unit-only validation is insufficient.
- Network boundaries matter: one Curio cluster should not mix miners from different Filecoin networks.

## Known areas of ongoing evolution
- Market 2.0 (`mk20`) is active and contract/product-driven; docs and code both indicate ongoing expansion.
- PDP is explicitly labeled alpha/under development.
- Proofshare/Snark market features are marked experimental.
- GUI is under active iteration; pages and behaviors may change.
- Dynamic config hot-reload coverage is growing (not all settings are restart-free yet).
- Forest integration status is **uncertain** across docs (references exist, but some docs still call it in-progress).
