# Smart Contract Integration Guide

This guide explains how to write, deploy, and integrate a smart contract that governs storage deals in the Market 2.0 architecture. Contracts are responsible for determining whether a deal is valid and returning a DealID.

---

## 🎯 Purpose of the Contract

In Market 2.0, contracts are used to:

* Accept or reject deals
* Optionally implement additional business logic (e.g. FIL+ validation, payments, approvals)
* Return a DealID string if accepted

The contract does **not** manage storage or retrieval itself—that is handled by the SP.

---

## ✅ Requirements

A valid Market 2.0 contract must:

1. Be deployed on a supported chain (e.g. Filecoin EVM, Hyperspace, etc)
2. Be whitelisted by the storage provider (via UI or admin tool)
3. Have its ABI uploaded
4. Expose a method that:

    * Accepts a single `bytes` input
    * Returns a string (representing the DealID)

---

## 🔁 Flow

1. Client encodes parameters for your method
2. Client submits deal to Curio with:

    * Contract address
    * Method name
    * ABI-encoded parameters
3. Curio:

    * Loads ABI
    * Packs the method call
    * Calls `eth_call`
    * Unpacks the return value

If the method returns a string → deal is accepted. If empty string or call fails → deal is rejected.

---

## 🧪 Example Contract Method

```solidity
function verifyDeal(bytes calldata params) external view returns (string memory) {
    // decode params into your structure
    // perform validation
    // return deal ID if valid
    return "deal-123";
}
```

---

## 📜 ABI Upload

The SP must upload the ABI JSON for your contract when whitelisting it:

* This enables Curio to find and call the method
* ABI must include the method name, inputs, and return types

---

## 🔐 Client Responsibilities

Clients must:

* Choose a contract accepted by the SP
* Encode call parameters into `[]byte`
* Provide method name and contract address in the deal

---

## 🧩 Products and Contract Use

Contracts are typically used from within a **product** (e.g. `ddov1`). The product defines:

* Contract address
* Method name
* Encoded params (using ABI rules)

This decouples contract logic from storage logic and keeps deals composable.

---

## 🚫 Common Errors

| Error                           | Cause                                          |
| ------------------------------- | ---------------------------------------------- |
| `426 Deal rejected by contract` | Returned string is empty or `eth_call` fails   |
| `ABI not found`                 | Contract not whitelisted or ABI missing        |
| `Invalid method`                | Method name not found in ABI                   |
| `Incorrect input format`        | Method doesn’t accept single `bytes` parameter |

---

## ✅ Checklist for Integration

* [ ] Deploy contract on supported chain
* [ ] Expose a `function(bytes) returns (string)` method
* [ ] Whitelist contract via SP UI
* [ ] Upload ABI including the method
* [ ] Coordinate with clients on method + param encoding

---

This guide enables market developers to plug in custom contract logic without requiring any changes to Curio or the storage pipeline.

Welcome to programmable storage governance.
