---
description: >-
  This pages covers some basic concepts useful to be a Filecoin storage
  providers
---

# Filecoin Storage Provider

## 1. Overview of Filecoin Storage Providers

A Storage Provider in the Filecoin network:

* **Stores client data** in exchange for fees and potential block rewards.
* Must **continuously prove** that stored data is available (via cryptographic proofs).
* Must **pledge collateral** in the form of FIL tokens to ensure honest behavior.
* Is subject to **penalties** if data is lost or if proofs are missed.

Becoming a Storage Provider blends **technical** and **economic** considerations. It requires:

1. **Sufficient hardware** to store, seal, and prove data reliably.
2. **Continuous on-chain proofs** to show you are storing data as promised.
3. **Economic planning** to handle locked collateral, fees, and eventual block rewards.

***

## 2. Key Concepts in Filecoin

Below are the foundational ideas you need to understand thoroughly before running a Storage Provider.

#### 2.1 Sectors

* **Definition**: The basic unit of storage in Filecoin. You commit user data to the network by packaging it into _sectors_.
* **Sizes**: Commonly 32 GiB (the most popular) or 64 GiB. Once you choose a sector size for your provider setup, you typically **cannot change** it without a new identity.
* **Committed Capacity (CC)**: You can commit “empty” sectors to the network to build storage power without actual client data initially. These sectors can be upgraded with real data later via a feature called **SnapDeals**.

**Sealed vs. Unsealed Sectors**

1. **Unsealed sector**: Contains the original (plaintext) data. Clients sometimes request that you keep an unsealed copy for faster retrieval.
2. **Sealed sector**: Cryptographically processed data ready for continuous proofs (Proof-of-Spacetime). Sealed data cannot be directly read until it’s unsealed (if needed for retrieval).

#### 2.2 Proof-of-Spacetime (PoSt)

Two key proof mechanisms ensure a provider’s data is verifiably present:

1. **WindowPoSt**: Happens in \~24-hour windows; you must prove that each sector is still stored. If you miss this proof, you incur faults and penalties.
2. **WinningPoSt**: A small subset of providers is elected every epoch (\~30 seconds) to produce a new block. If chosen, you submit a short proof showing you hold your pledged data. In return, you can earn a **block reward**.

#### 2.3 Deals and Retrieval

* **Storage Deals**: Contracts made with clients who pay you to store their data for a specified duration.
* **Retrieval Deals**: When clients pull their data, they pay you retrieval fees, typically via payment channels.
* **Verified Clients**: Some clients store “verified” data. Providers who store verified data get higher _quality-adjusted power_ (and thus a higher chance for block rewards).

#### 2.4 Epoch

Filecoin time is measured in **epochs** (\~30-second intervals).

* Each epoch triggers new tasks like verifying proofs, adding blocks, distributing block rewards, and so on.

***

## 3. Economics & Rewards

#### 3.1 Rewards

1. **Storage fees**: Paid over time by clients whose data you store.
2. **Block rewards**: Awarded by the network when you produce a block. You typically need **at least 10 TiB** of raw power to be eligible for these. A portion of each block reward is locked and vests linearly over \~180 days.

#### 3.2 Collateral and Locked Funds

1. **PreCommit Deposits**: You lock up some FIL when you “pre-commit” a sector. If the sector never proceeds to full commitment, the deposit is lost.
2. **Initial Pledge**: More FIL is locked when you fully commit a sector (prove-commit). This collateral is held to ensure you keep data online.
3. **Locked Rewards**: 75% of any block reward vests over \~180 days; 25% is immediately available.

#### 3.3 Penalties & Slashing

* **Fault Fees**: If you fail to prove a sector on time (WindowPoSt), you pay a daily fault fee until you fix or terminate the sector.
* **Sector Penalties**: If you do not declare a fault before a scheduled proof check, you pay an immediate penalty.
* **Termination Fees**: For sectors that are terminated early (voluntarily or involuntarily).
* **Consensus Fault Slashing**: Severe penalty for malicious consensus-level actions.

***

## 4. Theory of Sector Lifecycle

Although exact commands differ, every Filecoin SP software follows a similar lifecycle for sector creation and maintenance:

1. **Sector Allocation**
   * The provider decides to add capacity, either in the form of empty (CC) or client-filled sectors.
2. **PreCommit**
   * You produce a sector with partial sealing steps (encoding the data) and record this “precommit” on-chain with the required deposit.
3. **ProveCommit**
   * Final sealing steps generate a proof of replication (PoRep).
   * You submit this proof on-chain to finalize the commitment, locking your initial pledge collateral.
4. **Continuous Proof (WindowPoSt)**
   * Regularly (every \~24 hours), you must prove that each committed sector remains intact.
   * Missing this proof for a sector causes it to become **faulty**.
5. **Maintenance**
   * You can recover faulty sectors by re-running proofs.
   * You can terminate sectors early if needed, but you pay a termination penalty.
6. **Expiration or Upgrades**
   * Deals eventually expire. You can extend deals or reuse the sector for new deals (SnapDeals).
   * Once the entire sector’s lifetime ends, you can choose to remove it from the network or keep it sealed if you plan to extend.

***

## 5. Daily Operations & Tasks (General)

#### 5.1 Monitoring Proofs

* **WindowPoSt** schedules are strict. Know your deadlines and ensure your system has enough CPU/GPU resources to generate proofs on time.
* Watch for I/O bottlenecks that could slow proof generation.

#### 5.2 Accepting and Managing Deals

* **Negotiation**: Clients discover your provider (through a market or index) and propose deals.
* **Data Transfer**: You receive client data. This data is prepared for sealing.
* **Sealing**: The data is integrated into new or partially filled sectors.
* **Payments**: Storage fees accumulate over time, typically locked and then released to your available balance after each deal cycle.

#### 5.3 Retrievals

* **Unsealing on Demand**: If you do not maintain an unsealed copy of the data, you need to unseal it when a client requests retrieval (this can be slow).
* **Incremental Payments**: Retrieval fees are often paid via payment channels, chunk by chunk.

#### 5.4 Maintenance

* **Fault Recovery**: If a sector goes faulty, you can declare it in advance (to reduce penalties) and later recover it.
* **Hardware Upgrades**: As you add more capacity or want to speed up sealing, you may add more RAM, GPUs, or worker machines.
* **Software Updates**: Regularly update your chosen node/miner software to get protocol improvements and security fixes.

***

## 6. SnapDeals and Batching (Advanced Concepts)

#### 6.1 SnapDeals

* **Upgrade CC Sectors**: SnapDeals lets you add real client data to a sector that was previously committed as empty capacity.
* **Advantages**:
  * Saves time and resources: no need to run a full seal again.
  * Increases your effective power if the data is from verified clients (quality-adjusted power boost).

#### 6.2 Batching

* **On-Chain Efficiency**: Sending many single-sector proofs can be expensive in network fees (gas). Batching helps you combine multiple proofs into a single message.
* **Collateral Savings**: Batching can also help group sector pledges, reducing overhead.

***

## 7. Handling Balances & Collateral

1. **Ensure Adequate FIL** in your provider or worker wallets for day-to-day operations.
2. **Monitor** locked vs. unlocked balances. Collateral must be readily available to avoid interruptions in sealing and deal-making.
3. **Reward Vesting**: Remember that block rewards vest linearly over \~180 days, so you can’t immediately withdraw 100% of your earnings.

***

## 8. Penalties and How to Avoid Them

1. **Declare Faults in Time**: If you know a sector or machine is failing, declare a fault early. This is cheaper than letting a proof window pass without submission.
2. **Maintain Redundancy**: Keep backups or have spares for crucial components (power supplies, GPUs, or entire machines).
3. **Stay Online**: A stable network connection is key. If your system is offline at your WindowPoSt deadline, you’ll incur faults.

***

## 9. Scaling Your Operation

As you gain experience, you may want to scale:

* **Multiple Storage Locations**: Distribute sealed data across multiple racks or data centers.
* **Parallel Sealing**: Invest in more CPU cores, more RAM, and multiple GPUs to process many sectors at once.
* **Distributed Workers**: In some implementations, you can run separate “worker” processes on different machines to handle sealing tasks more efficiently.

***

## 10. Best Practices and Tips

1. **Monitor Everything**: Use dashboards and logs to watch for unusual CPU, RAM, or disk usage. Keep track of system health.
2. **Time Synchronization**: Keep your system clock accurate (via NTP or Chrony). Mismatched clocks can cause missed proofs.
3. **Hardware Stress Testing**: Sealing is CPU/GPU heavy. Make sure your hardware can handle sustained loads.
4. **Diversify Deals**: Seek out verified clients if possible, since that can boost your block reward chances (due to higher quality-adjusted power).
5. **Community Interaction**: Join Filecoin Slack or other community channels to stay updated on protocol changes and to troubleshoot issues with other providers.

***

## 11. Summary

Becoming a successful Filecoin Storage Provider requires:

1. **Solid Understanding** of Filecoin’s protocol and concepts (sectors, PoSt, deals, collateral, etc.).
2. **Robust Hardware** sized for your planned sealing rate and total storage.
3. **Continuous Maintenance** to ensure you never miss your proofs or run out of collateral.
4. **Economic Planning** to manage rewards, locked funds, and the cost of potential faults.
5. **Attention to Detail** with advanced features like SnapDeals, batching, and retrieval market optimizations.

***

## Next Steps

* **Deep-Dive Documentation**: Explore the [official Filecoin Specification](https://spec.filecoin.io/) for in-depth details of each proof, state machine, and economic mechanism.
* **Pilot Deployment**: Start small to gain hands-on experience with sealing, proof submission, and deal flow.
* **Expand** after building confidence in your setup, hardware reliability, and economic strategy.

By following these principles, you can build and manage a reliable Filecoin storage operation, confidently handle client deals, avoid excessive faults, and steadily grow your provider business over time.
