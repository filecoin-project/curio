---
description: >-
  This page explains how to set up a provider for the Curio Snark Market
  (experimental).
---

# Snark Market

> ⚠️ **Experimental Feature in Testing**\
> This feature is currently experimental and under active testing. Interfaces, behaviors, and requirements **may change without notice**.

***

## 🔧 What is the Snark Market?

The Snark Market allows any Curio node — **including Storage Providers with spare sealing/GPU capacity** — to sell or buy proof computation in exchange for FIL. It is designed for GPU nodes that want to participate in Filecoin proof offloading, enabling a decentralized proof marketplace.

This guide will walk you through how to:

* Enable proof selling on GPU nodes
* Set up the pricing and wallet
* See activity and settlement stats in the UI

***

## ⚙️ Prerequisites

Before enabling the Snark Market on your node:

* You must be running a Curio node with GPU capabilities.
* You must have a working web UI (Your browser can access ports exposed by Curio).
* You need a Lotus node installed and the Filecoin mainnet chain synced.\
  Refer to lotus documentation here:\
  [https://lotus.filecoin.io/lotus/install/linux/](https://lotus.filecoin.io/lotus/install/linux/)
* You need **YugabyteDB** installed.\
  👉 Follow the official setup instructions here:\
  [https://docs.curiostorage.org/setup#setup-yugabytedb](https://docs.curiostorage.org/setup#setup-yugabytedb)

***

## ⚙️ System Requirements

* Modern **NVIDIA GPU** (recommended 12GB+ VRAM)
* 70GB base system RAM + ~220 GB Per GPU
  * Lower amount is acceptable, but you won't be able to use the much faster SupraSeal C2 feature\
    Meaning ~10mins/proof instead of 2.
* Curio from `main` branch (Snark Market support is not yet part of a release)
* FIL balance on Mainnet

***

## 🚀 Setup Instructions

### 0. (Optional/Recommended) Benchmark your system

You can skip most of the complex setup and learn how your hardware performs with lotus-bench
* Follow build instructions from [https://lotus.filecoin.io/storage-providers/operate/benchmarks/](https://lotus.filecoin.io/storage-providers/operate/benchmarks/)
  * For faster Supraseal C2 lotus-bench do:\
    `RUSTFLAGS="-C target-cpu=native -g" FFI_BUILD_FROM_SOURCE=1 FFI_USE_CUDA_SUPRASEAL=1 make clean deps lotus-bench`
* Download example snark inputs from [https://pub-08ae819c828244bdbe5f615fd8c5e144.r2.dev/c1.json](https://pub-08ae819c828244bdbe5f615fd8c5e144.r2.dev/c1.json) (~51MB)
* Run `./lotus-bench simple commit2 c1.json`, wait a few minutes for results\
  On the first run lotus-bench may need to download the SNARK proving parameters

### 1. Install Curio

First, install dependencies (as per the [main install guide](https://docs.curiostorage.org/setup)):

```bash
sudo apt update
sudo apt install -y build-essential pkg-config libssl-dev curl git clang cmake golang
```

Then, build Curio from the Main branch:

```bash
git pull
git submodule update
make clean
RUSTFLAGS="-C target-cpu=native -g" FFI_BUILD_FROM_SOURCE=1 FFI_USE_CUDA_SUPRASEAL=1 make clean build all
sudo make install
```

Run basic setup.
* If needed set database variables (defaults work with local YugabyteDB install)\
  `CURIO_DB_NAME`, `CURIO_DB_USER`, `CURIO_DB_HOST` (can be a comma separated list of tserver IPs, one is also ok), `CURIO_DB_PASSWORD`
* Ensure your lotus-node is running
* Run `curio guided-setup` -> select `Setup non-Storage Provider cluster`
* Create empty snark-provider configuration layer -> `echo | curio config create --title snark-provider`

After all is done, [setup, enable and start the curio service](https://docs.curiostorage.org/curio-service#service-file-for-curio)
* For `CURIO_LAYERS` use `gui,snark-provider`

***

## 🛠️ Step-by-step Setup

### 2. Enable the Market in Layer Configuration

Ensure you're **not running on a WindowPoSt node**. This is only supported on GPU-based PoRep or Snap nodes. In the Web UI:

1. Go to `Overview` → `Configuration`, select the `snark-provider` if created previously
2. Find the **Subsystems** section
3. Set `EnableProofShare` to true
4. Save and restart the node

<figure><img src="https://github.com/user-attachments/assets/1c36e939-de4e-45ad-ba18-ce55e188c61c" alt="Enable PROOFSHARE toggle in configuration"><figcaption><p>Enable <code>PROOFSHARE</code> from the configuration layer</p></figcaption></figure>

***

### 3. Configure Provider Settings

Navigate to `Snark Market` in the sidebar. Under **Provider Settings**:

* Enable the settings checkbox
* **Create a new `f1` wallet** (do _not_ reuse existing wallet)\
  Use `lotus wallet new secp256k1` in the CLI\
  ⚠️ _Please note: This wallet can be changed later, but it is tricky_
* Set **Price (FIL/p)** to `0.005` (recommended for testing)
  * Single proof (`p`) should take roughly two minutes to compute, your price should be calculated based on how many proofs per hour per GPU you expect to compute and your cost to run the GPU. The snark marketplace automatically adjusts the market price to match supply to demand.
* Click **Update Settings**

<figure><img src="https://github.com/user-attachments/assets/60a52a8a-5c63-4c61-a207-e9be34084ff0" alt="Snark Wallet Setup"><figcaption><p>Snark Market provider settings with wallet and price configured</p></figcaption></figure>

***

### 4. Verify Your Node

Once you've configured the provider settings:

* Your node will automatically begin queueing proof work
* The dashboard will update with:
  * Active Asks
  * SNARK Queue
  * Payment Summaries
  * Recent Settlements
* You can also view the [global public dashboard](https://mainnet.snass.fsp.sh/ui/) 

<figure><img src="https://github.com/user-attachments/assets/c8636728-4b2e-4b69-b3b3-445c735bca8d" alt="Snark Market Dashboard"><figcaption><p>Overview showing queue, asks, settlements, and active proofs</p></figcaption></figure>

***

## 🪙 Wallet Setup Notes

* Create a **new `f1` address** and fund it (e.g. `0.1 FIL`)
* This wallet receives SNARK proof rewards
* Ensure the wallet remains **unlocked**

***

## 📈 Pricing & Payments

* Price is set per \~130M proof constraints (default granularity)
* Suggested starting point price: `0.005 FIL`
* Settlements occur automatically when network gas fee to settle is less than 0.2% of the balance to settle

***

## 🧪 Notes

* Your provider must complete **50 challenge proofs** per "work-slot"
* Each "work-slot" is allows for either one "work ask" in the market - listing of readiness to take on work on one proof for some minimum price, or one "assigned proof" which is being actively worked on.
* Number of work slots is calculated by dividing "completed challenge proofs" by **50**
* Failure to complete assigned work within the deadline reduces "completed challenge proofs" balance by **50** proofs
* Withdrawing an ask from the market (i.e. in order to adjust ask price) reduces "completed challenge proofs" balance by **1** proof
* "challenge proofs" are gained only by completing unpaid challenge proofs, which are assigned when attempting to create a work ask while the challenge balance is too low.
* The service _may_ assign real proofs as challenges, but only if they were failed by other providers and no other provider can be found who can take the payment
* Proofs must complete within **45 minutes**, or your node will lose its active slot and need to **re-earn trust**
* The system is **fault-tolerant** and retries failed work automatically
  * You may be assigned retry proofs which come with non-current, potentially lower than current, still higher than your minimum price per proof.
* You can **scale horizontally** by running more GPU workers with the same setup

***

Let us know on Slack if you’re testing `#fil-curio-help` — we’ll be actively monitoring for feedback and performance 🚀
