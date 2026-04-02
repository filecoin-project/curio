---
description: >-
  This page explains how to set up a consumer (GPU saver) for the Curio Snark
  Market (experimental).
---

# Snark Market (Consumer)

> ⚠️ **Experimental Feature in Testing**\
> This feature is currently experimental and under active testing. Interfaces, behaviors, and requirements **may change without notice**.

***

## 🔧 What is the Snark Market (Consumer)?

The Snark Market allows Storage Providers to **buy** proof computation from the market instead of running GPUs locally. You offload PoRep and Snap proof work to providers in exchange for FIL — a "GPU saver" approach that lets you complete the C2 (proof) phase of sealing without local GPU capacity.

To sell proof compute (be a provider), see [Snark Market](Snark-Market.md).

***

## ⚙️ Prerequisites

Before enabling the Snark Market consumer on your node:

* You must be running a Curio node with a **sealing pipeline** (PoRep or Snap tasks).
* You must have a working web UI (your browser can access ports exposed by Curio).
* You need a Lotus node installed and the Filecoin mainnet chain synced.\
  Refer to lotus documentation here:\
  [https://lotus.filecoin.io/lotus/install/linux/](https://lotus.filecoin.io/lotus/install/linux/)
* You need **YugabyteDB** installed.\
  👉 Follow the official setup instructions here:\
  [https://docs.curiostorage.org/setup#setup-yugabytedb](https://docs.curiostorage.org/setup#setup-yugabytedb)

***

## ⚙️ System Requirements

* **No GPU required** for consumers — you buy proofs from the market.
* Standard sealing node RAM and storage.
* Curio **v1.27.0 or later** (Snark Market is included in official releases).
* FIL balance on Mainnet (to pay for proof compute).

***

## 🛠️ Step-by-step Setup

### 1. Enable Remote Proofs in Configuration

1. Go to `Overview` → `Configuration`, and select your miner or sealing layer.
2. Find the **Subsystems** section.
3. Set `EnableRemoteProofs` to true.
4. Save and restart the node.

***

### 2. Add and Fund Client Wallets

Navigate to **Snark Market** in the sidebar. Under **Client Wallets**:

* **Add Wallet**: Click **Add Wallet** and enter an f1 address you control. You can use an existing wallet (e.g. worker, collateral) or create a dedicated one — SPs typically already have funded wallets.
* **Deposit**: Click **Deposit** to move FIL from that wallet's chain balance into the payment router. You need available balance in the router to pay for proofs.

***

### 3. Configure Client Settings

Under **Client Settings** (right side of the page):

1. Accept the **Client Terms of Service** when prompted.
2. Click **Add SP**. Enter your SP address and the client wallet address (use a wallet you added in Client Wallets).
3. For each SP row:
   * Check **Enabled**.
   * Set **Wallet** to the f1 address you will use for payments.
   * Set **buy_delay_secs** — delay before outsourcing work (allows local GPU to take it first if you have one).
   * Enable **do_porep** and/or **do_snap** for the proof types you want to buy.
   * Set **FIL/P** (max price per proof; market price must be ≤ this).
4. Click **Save**.

***

## 📈 Pricing

**FIL/P** is the maximum you are willing to pay per **P**. One **P** (one "proof unit") equals the cost of a single **32 GiB C2 (PoRep) proof** — the SNARK proof generated after C1 during sector sealing.

| Proof type              | Multiplier | Cost formula        | Example at 0.005 FIL/P |
| ----------------------- | ---------- | ------------------- | ---------------------- |
| **32 GiB C2** (PoRep)   | 1×         | 1 × (FIL/P)         | **0.005 FIL**          |
| **32 GiB Snap** (UpdateEncode) | 1.6×       | 1.6 × (FIL/P)       | **0.008 FIL**          |

Example: If you set **FIL/P = 0.005** and the market price is at or below that, a 32 GiB C2 will cost ~0.005 FIL and a 32 GiB Snap proof will cost ~0.008 FIL. The market price fluctuates; your configured FIL/P is the maximum you will pay — proofs are only purchased when the current market price is at or below your limit.

Check the [public dashboard](https://mainnet.snass.fsp.sh/ui/) for the current **Min price**; use it to set your FIL/P accordingly.

***

## 🔄 Balance Manager (Optional)

To automatically top up client wallet balances:

1. Go to **Wallet** → **Balance Manager**.
2. Click **Add SnarkMarket Client Rule**.
3. Set **Subject** to your client wallet address.
4. Set **Low** and **High** watermarks (in FIL).
5. Save.

The Balance Manager will deposit FIL from the wallet's chain balance into the router when the available balance falls below the low watermark.

***

## ✅ Verify

* **View Requests**: For each SP, click **View Requests** to see in-flight and completed proof requests.
* **Client Messages**: Shows deposit and withdrawal status for client wallets.

***

## 🧪 Notes

* **buy_delay_secs** gives your local GPU (if any) time to take work before outsourcing. Set to 0 to outsource immediately when local capacity is idle.
* Work is only sent to the market when the current price is ≤ your configured max FIL/P.
* Ensure client wallets stay funded; otherwise proofs cannot be purchased.

***

Let us know on Slack if you're testing `#fil-curio-help` — we'll be actively monitoring for feedback and performance 🚀
