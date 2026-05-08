---
description: 本页面介绍如何将 Curio Snark Market 配置为消费方（节省 GPU，实验性功能）。
---

# Snark Market (Consumer)
# Snark 市场（消费方）

> ⚠️ **实验性功能，正在测试中**\
> 此功能目前仍处于实验和活跃测试阶段，界面、行为和要求**都可能在没有通知的情况下发生变化**。

***

## 什么是 Snark Market（消费方）？

Snark Market 允许存储提供商从市场中**购买**证明计算，而不是在本地运行 GPU。你可以把 PoRep 和 Snap 的证明工作外包给市场中的提供方，并用 FIL 支付费用。这对于想节省 GPU 资源、或者根本没有本地 GPU 的集群尤其有用。

如果你想出售证明算力，请参见 [Snark Market（提供方）](Snark-Market.md)。

***

## 前置条件

在节点上启用 Snark Market 消费方之前，请确保：

* 你运行的是带有**封装流水线**的 Curio 节点（PoRep 或 Snap 任务）。
* 你可以正常访问 Curio Web UI。
* 你已经安装 Lotus，并且 Filecoin 主网已同步完成。\
  可参考 Lotus 文档：\
  [https://lotus.filecoin.io/lotus/install/linux/](https://lotus.filecoin.io/lotus/install/linux/)
* 你已经安装 **YugabyteDB**。\
  👉 可参考官方文档：\
  [https://docs.curiostorage.org/setup#setup-yugabytedb](https://docs.curiostorage.org/setup#setup-yugabytedb)

***

## 系统要求

* **不需要 GPU**，因为证明是从市场购买的
* 常规封装节点所需的内存和存储
* Curio **v1.27.0 或更新版本**
* 主网上有可用的 FIL 余额（用于支付证明费用）

***

## 设置步骤

### 1. 在配置中启用 Remote Proofs

1. 进入 `Overview` → `Configuration`，选择你的矿工层或封装层
2. 找到 **Subsystems** 部分
3. 将 `EnableRemoteProofs` 设置为 `true`
4. 保存并重启节点

***

### 2. 添加并充值 Client Wallets

在侧边栏进入 `Snark Market`，然后查看 **Client Wallets**：

* **Add Wallet**：点击 **Add Wallet**，输入一个你控制的 `f1` 地址。你可以直接使用已有钱包（例如 worker、collateral 等），也可以专门新建一个独立钱包。多数 SP 已经有可用的钱包，因此通常不需要专门再创建一个。
* **Deposit**：点击 **Deposit**，把该钱包链上的 FIL 转入支付路由器。只有路由器里有可用余额，系统才能购买证明。

***

### 3. 配置 Client Settings

在 **Client Settings**（页面右侧）中：

1. 根据提示接受 **Client Terms of Service**
2. 点击 **Add SP**，输入你的 SP 地址以及用于付款的客户端钱包地址（该钱包应当已经在 Client Wallets 中添加）
3. 对每一行 SP 设置：
   * 勾选 **Enabled**
   * 将 **Wallet** 设为用于付款的 `f1` 地址
   * 设置 **buy_delay_secs**，表示在把任务外包出去前等待多长时间，以便本地 GPU（如果有）先尝试处理
   * 根据需要启用 **do_porep** 和/或 **do_snap**
   * 设置 **FIL/P**，表示你愿意接受的最高价格
4. 点击 **Save**

***

## 价格说明

**FIL/P** 表示你愿意为一个 **P** 支付的最高价格。一个 **P** 可以理解为一个“证明价格单位”，大致对应 **一个 32 GiB C2（PoRep）证明** 的价格基准。

| 证明类型 | 倍数 | 费用公式 | 当 `FIL/P = 0.005` 时的示例 |
| --- | --- | --- | --- |
| **32 GiB C2**（PoRep） | 1× | `1 × FIL/P` | **0.005 FIL** |
| **32 GiB Snap**（UpdateEncode/更新证明） | 1.6× | `1.6 × FIL/P` | **0.008 FIL** |

例如，如果你把 **FIL/P** 设为 `0.005`，并且当前市场价格不高于这个值，那么一个 32 GiB C2 大约会花费 `0.005 FIL`，一个 32 GiB Snap 大约会花费 `0.008 FIL`。

你的 **FIL/P** 是你愿意支付的**上限**。只有当当前市场价格低于或等于该上限时，系统才会购买证明。

你可以查看公共仪表盘 [https://mainnet.snass.fsp.sh/ui/](https://mainnet.snass.fsp.sh/ui/) 中的 **Min price**，并据此设置自己的 `FIL/P`。

***

## Balance Manager（可选）

如果你希望自动补充客户端钱包余额：

1. 进入 **Wallet** → **Balance Manager**
2. 点击 **Add SnarkMarket Client Rule**
3. 将 **Subject** 设为你的客户端钱包地址
4. 设置 **Low** 和 **High** 水位（单位：FIL）
5. 保存

当支付路由器中的可用余额低于低水位时，Balance Manager 会自动把链上余额补充到路由器中。

***

## 验证

* **View Requests**：可查看某个 SP 当前和历史的证明请求
* **Client Messages**：可查看充值、提现等客户端消息状态

***

## 说明

* `buy_delay_secs` 可以让本地 GPU（如果有）先尝试接手任务；若设为 `0`，则会更快地把任务送往市场
* 只有当当前市场价格低于或等于你设置的 `FIL/P` 时，任务才会被发送到市场
* 请确保客户端钱包持续有余额，否则无法继续购买证明

***

如果你正在测试，欢迎在 Slack 的 `#fil-curio-help` 频道反馈，我们会持续关注。
