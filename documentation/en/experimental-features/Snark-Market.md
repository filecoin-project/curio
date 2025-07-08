
# Curio Snark Market – Provider Guide

This document explains how to operate a SNARK provider node for the Curio Snark Market. It is intended for both dedicated infrastructure teams and storage providers with spare sealing hardware who wish to monetize idle GPU capacity.

> ⚠️ **Experimental Feature Warning**  
> The Snark Market is currently **experimental** and in **active testing**.  
> Configuration, pricing mechanisms, and APIs may **change without notice**.  
> Use only on non-critical infrastructure or with explicit understanding of the risks.

---

## 🧠 What is the Snark Market?

The Curio Snark Market enables **remote, paid computation of zk-SNARKs** for Filecoin proof pipelines. It allows:

- **Storage providers** to outsource heavy PoRep/Snap proof jobs
- **SNARK providers** (you) to sell GPU compute for FIL
- **Fast, verifiable, decentralized off-chain proof routing**

The system operates with:

- A shared coordination hub (`snark-hub.curiostorage.org`)
- A price-per-constraint unit marketplace (FIL/P)
- Fully permissionless proof exchange between Curio nodes

> ❗ Do not configure this role on your PoSt workers. This is a dedicated role for proof compute only.

---

## ⚙️ System Requirements

- Modern **NVIDIA GPUs** (recommended 12GB+ VRAM)
- Curio from `origin/feat/snkss` branch (with Snark Market support)
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
sudo systemctl restart curio.service
```

### 2. Initialize Standalone SNARK Node

```bash
mkdir -p ~/.curio-snark
curio config new-cluster --repo-path ~/.curio-snark --db-host 127.0.0.1
```

---

### 3. Start Local Yugabyte

```bash
docker run -d --name yugabyte \
  -p 5433:5433 -p 7000:7000 \
  yugabytedb/yugabyte:latest bin/yugabyted start --daemon=false
```

---

### 4. Configure the SNARK Provider

Edit `~/.curio-snark/config.toml`:

```toml
[Subsystems]
EnableSnarkMarketProvider = true

[SnarkMarket]
HubAddress = "/dns4/snark-hub.curiostorage.org/tcp/19000"
ProviderWallet = "t3..."   # mainnet wallet for rewards
```

- **Do not** set `EnableWindowPost = true` – this must be omitted
- **Price per constraint** is configured via the UI (not here)

---

### 5. Start the Node

```bash
curio run --repo-path ~/.curio-snark --layers=snark-worker
```

The node connects to the hub and registers as a proof provider.

---

## 📊 Snark Market UI

Access: `http://localhost:4701/pages/proofshare/`

From the UI, you can:

- Set `FIL/P` pricing for proofs
- View completed proof tasks
- Monitor settlements and reward history

---

### ✅ Active Provider Dashboard Example

![Screenshot 1](Screenshot%202025-06-29%20at%2017.06.45.jpeg)

### 🟡 Fresh Node Example

![Screenshot 2](Screenshot%202025-06-29%20at%2017.06.58.jpeg)

---

## 🧾 Settlement and Earnings

- Each proof task is listed under **Queue**
- Rewards are shown per task with earned FIL
- Settlements are batched and CID-linked

