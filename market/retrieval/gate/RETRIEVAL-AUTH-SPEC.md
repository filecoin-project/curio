# Curio Retrieval Authorization — wire-format spec

**Status:** draft for review by the PDP-retrieval and PoRep-market teams. A **working reference
implementation** of this exact scheme (the PDP side) exists on Curio branch
`feat/gated-pdp-retrievals` (`market/retrieval/gate`), verified by unit + integration tests and a
live devnet run.
**Goal:** a single credential scheme for authenticated piece retrieval through Curio, usable by any
storage subsystem (FWSS/PDP data sets, PoRep market deals, …). Both subsystems serve pieces through
the same Curio retrieval endpoint (`GET /piece/{cid}`, `/ipfs/{cid}`), so they should share one
gate and one credential format rather than two lookalikes.

This is normative. **MUST/SHOULD/MAY** per RFC 2119.

---

## 1. Model

Access is **capability + proof-of-possession (PoP)** — two EIP-712 objects, never one:

- **`RetrievalVoucher`** — the *capability*. The resource's on-chain **owner (payer)** signs it
  **once, offline**, delegating access for a whole *scope* (a data set or a deal) to a **grantee**
  address. Reusable, long-lived, freely storable/transferable.
- **`RetrievalProof`** — *proof of possession*. The requester signs it **fresh, per request**,
  binding the **exact resource CID** and a short deadline.

A gated request MUST carry a proof (always) and, for delegated access, the voucher.

> **Why not a bearer voucher.** A voucher presented alone is a bearer token: anyone who captures it
> can retrieve until its deadline; the `grantee` field is decorative. Requiring a fresh,
> resource-bound proof signed by the grantee's key means **a stolen voucher is useless** — only the
> holder of the grantee key can mint a matching proof. The grantee is typically offline when the
> voucher is issued, so this is a capability-delegation model, not an interactive (OIDC-style) grant.

**Statelessness.** Replay protection is time-bounded, not stored: the server bounds how far in the
future a proof's `deadline` may be (`MAX_PROOF_TTL`) and binds the proof to the resource. No nonce
database. A captured *full request* (proof+voucher) is therefore replayable only within the proof's
short window and only for that one piece; closing that residual window would require a server-side
seen-cache and is intentionally out of scope for v1.

---

## 2. Notation & primitives

- Signatures are secp256k1 ECDSA over the EIP-712 digest (EIP-191 `0x19 0x01` prefix), recovered via
  `ecrecover`. 65-byte `r‖s‖v`, `v ∈ {27,28}` (implementations MUST also accept `{0,1}`).
- Verification is **off-chain** (in Curio). The EIP-712 `verifyingContract` is used purely for
  domain separation; no on-chain call is required to verify a credential.
- Portable across any secp256k1 signer: `viem` / MetaMask `eth_signTypedData_v4` / `@noble/curves` /
  go-ethereum / a headless agent. No wallet interaction is required at request time for machine
  clients; a human delegates once (voucher) and their software mints proofs.

---

## 3. EIP-712 domain

```
EIP712Domain(string name, string version, uint256 chainId, address verifyingContract)
```

| Field | Value |
|---|---|
| `name` | `"CurioRetrieval"` |
| `version` | `"1"` |
| `chainId` | the FEVM chain id (e.g. `314159` calibration, `314` mainnet) |
| `verifyingContract` | **the owning service's contract** for the scope (see §5) |

`verifyingContract` MUST be the service contract that owns the scope — the **FWSS service address**
for a PDP data set, the **PoRep market contract** for a deal. This gives cross-service domain
separation: a voucher minted for a PoRep deal cannot be replayed against a PDP data set of the same
numeric id, because the digest differs.

---

## 4. Structures

### 4.1 RetrievalVoucher (capability)

```
RetrievalVoucher(address grantee, uint256 scope, uint256 issuedAt, uint256 deadline)
```

| Field | Meaning |
|---|---|
| `grantee` | the delegate's address; the proof for this voucher MUST recover to it |
| `scope` | the access unit — a **data set id** (PDP) or **deal id** (PoRep), see §5 |
| `issuedAt` | unix seconds, for audit |
| `deadline` | unix seconds; the voucher is valid while `now ≤ deadline` (MAY be long-lived) |

Signed by the scope's **owner (payer)**.

### 4.2 RetrievalProof (proof of possession)

```
RetrievalProof(uint256 scope, string resource, uint256 deadline)
```

| Field | Meaning |
|---|---|
| `scope` | MUST equal the voucher's `scope` (or, for owner-direct access, any scope the owner owns that contains the piece) |
| `resource` | the requested piece CID **exactly as it appears in the request path** (see §6) |
| `deadline` | unix seconds; MUST be near-future (`now ≤ deadline ≤ now + MAX_PROOF_TTL`) |

Signed **fresh per request** by the requester (the grantee, or the owner for owner-direct access).

`MAX_PROOF_TTL` is server policy; RECOMMENDED **≤ 5 minutes**.

---

## 5. `scope`, `resource`, and service binding

- **`scope`** is a `uint256` that a service interprets: PDP → `dataSetId`; PoRep → `dealId`. Each
  scope belongs to exactly one service, from which Curio derives the `verifyingContract` and the
  `owner`.
- **`resource`** is the CID string from the request path: the piece CID for `GET /piece/{cid}`, or
  the payload/root CID for `GET /ipfs/{cid}[/subpath]` (gated on the root CID; sub-blocks that
  resolve to other pieces are not individually re-checked).
- A piece MAY belong to multiple scopes (content-addressed dedup). The credential names the scope it
  claims through; Curio verifies the piece is actually in that scope. **Public-wins:** if the piece
  is in any non-access-controlled scope, it is served without a credential (service policy).

---

## 6. Credential token & presentation

Wire token = base64url(JSON), no padding:

```json
{
  "scheme": "eip712",
  "proof":     { "scope": "1001", "resource": "bafk…", "deadline": "1767225600" },
  "proofSig":  "0x…",
  "voucher":   { "grantee": "0xabc…", "scope": "1001", "issuedAt": "1767139200", "deadline": "1767744000" },
  "voucherSig":"0x…"
}
```

- `voucher`/`voucherSig` are **omitted for owner-direct access** (the proof signer is the owner).
- uint256 fields are **decimal strings**; addresses and signatures are `0x`-hex; `resource` is the
  CID string.

Presentation (a client MUST support at least one; a gate MUST accept both):

- `Authorization: CurioRetrieval <token>` — SDK / server / browser `fetch` / headless agent.
- `?auth=<token>` query parameter — header-less browser tags (`<img>`/`<video>`/`<a download>`).
  Because the token embeds a fresh proof, a leaked `?auth=` URL is a short-lived, single-resource
  capability, not a durable bearer token.

Gated responses MUST be `Cache-Control: private, no-store`.

---

## 7. Verification algorithm (normative)

For a request on resource CID `R`, with parsed credential `C`:

1. `C.scheme` MUST be `"eip712"`, else reject.
2. Resolve the access-controlled scopes containing `R` (per service). If `R` is in any public scope
   → **serve** (public-wins). Else continue; let `PRIV` be the set of controlling scopes.
3. `C.proof.resource` MUST equal `R` (byte-exact CID string).
4. `now ≤ C.proof.deadline ≤ now + MAX_PROOF_TTL`, else reject.
5. `C.proof.scope ∈ PRIV`, else reject.
6. Resolve `C.proof.scope` → owning service → `verifyingContract` and `owner`.
7. `requester = ecrecover(EIP712(domain(verifyingContract), RetrievalProof, C.proof), C.proofSig)`.
8. If `requester == owner` → **authorize** (owner-direct; voucher not required).
9. Else the voucher is REQUIRED:
   - `C.voucher` present; `C.voucher.scope == C.proof.scope`; `now ≤ C.voucher.deadline`;
     `C.voucher.grantee == requester`;
   - `issuer = ecrecover(EIP712(domain(verifyingContract), RetrievalVoucher, C.voucher), C.voucherSig)`;
     `issuer == owner`.
   - all hold → **authorize**.
10. Else **deny** (403).

Response codes: missing credential → **401**; present but unauthorized/invalid → **403**;
resolver/chain/DB failure → **503** (fail closed — never serve a controlled piece on error).

---

## 8. Service integration (the resolver contract)

A subsystem plugs into the shared gate by implementing a small resolver — the Curio gate's `Backend`
interface, one method group per concept:

```
ScopesForResource(ctx, resourceCID) -> []{ scope, service }   // piece → the scopes that contain it
IsScopePrivate(ctx, service, scope) -> bool                    // access-controlled?
ScopeOwner(ctx, service, scope)     -> address                 // the payer/owner (EIP-712 signer to match)
ServiceContract(service)            -> address                 // the domain verifyingContract
ChainID(ctx)                        -> uint256
```

- **PDP/FWSS:** scope = `dataSetId`; owner = FWSS-view `GetDataSet().Payer`; service contract =
  FWSS service address; "private" = the `withRetrievalACL` data-set-metadata key present.
- **PoRep market:** scope = `dealId`; owner = the deal's payer; service contract = the PoRep market
  contract; "private" = the deal's equivalent opt-in flag.

The gate's crypto/verification path is identical for both.

---

## 9. Security considerations

- **Theft resistance.** The voucher alone authorizes nothing; every request needs a fresh
  grantee-signed, resource-bound proof. A leaked voucher/token cannot be used without the grantee
  key.
- **Residual replay window.** A captured full request replays only within `MAX_PROOF_TTL` and only
  for that resource. An OPTIONAL server-side proof-`(signer,resource,deadline)` seen-cache closes it
  at the cost of statelessness.
- **Contract/multisig owners (EIP-1271).** v1 assumes an EOA owner (ecrecover). A scope whose owner
  is a contract cannot verify by ecrecover; EIP-1271 support is a documented follow-up.
- **Revocation.** There is no revocation before `deadline`; the voucher's short-to-medium lifetime is
  the only kill switch in v1. If pre-expiry revocation is later required, add a `nonce` field plus an
  owner-published deny-list — deliberately deferred until a concrete need arises.
- **`/ipfs/` scoping.** Gated on the requested root/path CID's piece(s); DAG sub-blocks resolving to
  other pieces are not individually re-checked.
- **Domain separation.** `verifyingContract` prevents cross-service and cross-network replay; clients
  MUST sign under the owning service's contract.

---

## 10. Worked example

Owner `0x47cc…` delegates PDP data set `1001` to grantee `0xabc…`, then the grantee retrieves piece
`bafk…`:

**Voucher (signed once, offline, by the owner):**
```json
{ "domain": { "name": "CurioRetrieval", "version": "1", "chainId": 314159, "verifyingContract": "0x<FWSS>" },
  "types": { "RetrievalVoucher": [
      {"name":"grantee","type":"address"},{"name":"scope","type":"uint256"},
      {"name":"issuedAt","type":"uint256"},{"name":"deadline","type":"uint256"} ] },
  "primaryType": "RetrievalVoucher",
  "message": { "grantee":"0xabc…","scope":"1001","issuedAt":"1767139200","deadline":"1767744000" } }
```

**Proof (signed fresh per request, by the grantee):**
```json
{ "domain": { "name": "CurioRetrieval", "version": "1", "chainId": 314159, "verifyingContract": "0x<FWSS>" },
  "types": { "RetrievalProof": [
      {"name":"scope","type":"uint256"},{"name":"resource","type":"string"},{"name":"deadline","type":"uint256"} ] },
  "primaryType": "RetrievalProof",
  "message": { "scope":"1001","resource":"bafk…","deadline":"1767225600" } }
```

Both go into the token (§6); the request carries it via header or `?auth=`.

---

## 11. Migration from the two current implementations

**PDP retrieval gate (this repo) — DONE (reference implementation).** Branch
`feat/gated-pdp-retrievals` already implements this exact scheme: `RetrievalVoucher` /
`RetrievalProof`, `scope`, `deadline`, `verifyingContract` (= the FWSS service address), and
string-encoded uint256s — the `market/retrieval/gate` package. Verified by unit + HTTP integration
tests and a live devnet run (owner-direct, delegated, stolen-voucher→403, resource/expiry binding).
Treat it as the working reference for the wire format; the client signer lives in
`synapse-sdk/examples/authz/retrieval.mjs`.

**PoRep market voucher — adopt the missing half + shared shape:**
- add the `RetrievalProof` PoP object and require it on every request (the current single voucher is
  bearer — a stolen voucher is usable until `deadline`);
- rename domain `PoRepPieceAccess` → `CurioRetrieval`; `dealId` → `scope`;
- add `issuedAt` to the voucher (audit);
- keep `verifyingContract` (the PoRep market contract), `grantee`, `deadline` — already aligned.

---

## 12. Open questions for the two teams
1. `MAX_PROOF_TTL` value (RECOMMENDED ≤ 5 min) and whether a seen-cache is wanted for v1.
2. Whether to unify on `scope` (uint256) or keep service-specific field names (`dataSetId`/`dealId`)
   with a shared proof — `scope` maximizes shared verification code.
3. EIP-1271 timeline (contract/multisig owners).