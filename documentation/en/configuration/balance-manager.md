---
description: How to configure balance manager in Curio cluster
---

# Balance Manager

## Overview
The **Balance Manager** is a subsystem within Curio responsible for **automatically managing balances** for certain actors. Currently, it is used to **top up deal collateral** when the available balance falls below a defined threshold. In future iterations, the Balance Manager will be extended to **handle control addresses and other balance-related operations.**

## Functionality
The Balance Manager operates on a periodic task cycle and ensures that a miner's **market balance** remains within an acceptable range by:
1. **Monitoring market balance**: Checks the miner's market escrow balance at regular intervals.
2. **Comparing against thresholds**:
    - If the balance drops **below** `CollateralLowThreshold`, additional funds are added.
    - The balance is topped **up to** `CollateralHighThreshold`.
3. **Sending transactions**: Issues an `AddBalance` message to the **market actor (`f05`)** from the **configured wallet**.
4. **Ensuring sufficient funds**: Verifies that the deal collateral wallet has enough balance before sending funds.
5. In the future, this functionality will be expanded to include maintaining control address and other operational balances.

The Balance Manager runs every **5 minutes** by default (`BalanceCheckInterval`).

## Configuration
The **Balance Manager configuration** should be defined at the **base layer** within Curio. It is part of the **`BalanceManagerConfig`** section inside the `CurioConfig`.

### **Enabling Balance Manager**
To enable the Balance Manager, modify the **`EnableBalanceManager`** setting under `Subsystems`:

```toml
[Subsystems]
EnableBalanceManager = true  # Set to true to activate Balance Manager
```

### **Balance Manager Settings**
The Balance Manager's configuration is located under `Fees`:

```toml
[Fees.BalanceManager.MK12Collateral]
DealCollateralWallet = "t3xyz..."  # The wallet used to fund market balance
CollateralLowThreshold = "5 FIL"    # If market balance drops below this, a top-up is triggered
CollateralHighThreshold = "20 FIL"   # The target balance after a top-up
```

#### Configuration Parameters
| Parameter                 | Description |
|---------------------------|-------------|
| `DealCollateralWallet`    | The wallet used to top up the miner's market balance. |
| `CollateralLowThreshold`  | When the balance falls below this threshold, the Balance Manager triggers a top-up. |
| `CollateralHighThreshold` | The target balance after a top-up operation is performed. |

### Example Configuration
```toml
[Subsystems]
  EnableBalanceManager = true

[Fees]
  [Fees.BalanceManager]
    [Fees.BalanceManager.MK12Collateral]
      DealCollateralWallet = "t3xyz..."
      CollateralLowThreshold = "5 FIL"
      CollateralHighThreshold = "20 FIL"
```

## Future Enhancements
Currently, the Balance Manager is only used for deal collateral top-ups, but it will be extended to:
- Manage control addresses balances.
- Monitor and maintain additional operation-specific balances.

By enabling the Balance Manager, miners can **automate their collateral management**, reducing the risk of **deal failures due to insufficient balance** while optimizing operational efficiency.
```
