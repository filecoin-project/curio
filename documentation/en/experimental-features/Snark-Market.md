
# 🧠 Snark Market Setup (Experimental)

> ⚠️ **Experimental Feature in Testing**  
> This feature is currently experimental and under active testing. Interfaces, behaviors, and requirements **may change without notice**.

---

## 🔧 What is the Snark Market?

The Snark Market allows any Curio node — **including Storage Providers with spare sealing/GPU capacity** — to sell or buy proof computation in exchange for FIL. It is designed for GPU nodes that want to participate in Filecoin proof offloading, enabling a decentralized proof marketplace.

This guide will walk you through how to:

- Enable proof selling on GPU nodes
- Set up the pricing and wallet
- See activity and settlement stats in the UI

---

## ⚙️ Prerequisites

Before enabling the Snark Market on your node:

- You must be running a Curio node with GPU capabilities.
- You must have a working web UI.
- You need **YugabyteDB** installed.  
  👉 Follow the official setup instructions here:  
  [https://docs.curiostorage.org/setup#setup-yugabytedb](https://docs.curiostorage.org/setup#setup-yugabytedb)

---

## ⚙️ System Requirements

- Modern **NVIDIA GPU** (recommended 12GB+ VRAM)
- Curio from `origin/feat/snkss` branch (Snark Market support)
- FIL balance on Mainnet

---

## 🚀 Setup Instructions

### 1. Install Curio

```bash
git pull
git checkout origin/feat/snkss
git submodule update
make clean
RUSTFLAGS="-C target-cpu=native -g" FFI_BUILD_FROM_SOURCE=1 FFI_USE_CUDA_SUPRASEAL=1 make clean build all
sudo make install
sudo systemctl start curio.service
```

---

## 🛠️ Step-by-step Setup

### 2. Enable the Market in Layer Configuration

Ensure you're **not running on a WindowPoSt node**. This is only supported on GPU-based PoRep or Snap nodes. In the Web UI:

1. Go to `Overview` → `Configuration`
2. Find the **Subsystems** section
3. Enable `proof_share` or `Enable Snark Market`
4. Save and restart the node

> <img width="207" alt="PROOFSHARE" src="https://github.com/user-attachments/assets/1c36e939-de4e-45ad-ba18-ce55e188c61c" />

---

### 3. Configure Provider Settings

Navigate to `Snark Market` in the sidebar. Under **Provider Settings**:

- Enable the settings checkbox
- **Create a new `f1` wallet** (do _not_ reuse existing wallet)  
  ⚠️ _Please note: This wallet can be changed later, but it is tricky_
- Set **Price (FIL/p)** to `0.005` (recommended for testing)
- Click **Update Settings**

> <img width="518" alt="Snark_Wallet" src="https://github.com/user-attachments/assets/60a52a8a-5c63-4c61-a207-e9be34084ff0" />


---

### 4. Verify Your Node

Once you've configured the provider settings:

- Your node will automatically begin queueing proof work
- The dashboard will update with:
  - Active Asks
  - SNARK Queue
  - Payment Summaries
  - Recent Settlements

> <img width="1911" alt="Snark_Overview" src="https://github.com/user-attachments/assets/c8636728-4b2e-4b69-b3b3-445c735bca8d" />

---

## 🪙 Wallet Setup Notes

- Create a **new `f1` address** and fund it (e.g. `0.1 FIL`)
- This wallet receives SNARK proof rewards
- Ensure the wallet remains **unlocked**

---

## 📈 Pricing & Payments

- Price is set per ~130M proof constraints (default granularity)
- Suggested testing price: `0.005 FIL`
- Settlements occur automatically when nonce limit is hit

---

## 🧪 Notes

- Your provider must complete **5 challenge proofs** before appearing in the global provider view
- Proofs must complete within **30 minutes**, or your node will lose its active slot and need to **re-earn trust**
- The system is **fault-tolerant** and retries failed work automatically
- You can **scale horizontally** by running more GPU workers with the same setup

---

Let us know on Slack if you’re testing `#fil-curio-help` — we’ll be actively monitoring for feedback and performance 🚀
