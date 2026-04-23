---
description: 本页面介绍如何将 Curio Snark Market 配置为提供方（实验性功能）。
---

# Snark Market
# Snark 市场（提供方）

> ⚠️ **实验性功能，正在测试中**\
> 此功能目前仍处于实验和活跃测试阶段，界面、行为和要求**都可能在没有通知的情况下发生变化**。

***

## 什么是 Snark Market？

Snark Market 允许 Curio 节点出售或购买证明计算能力，以换取 FIL。对于拥有空闲封装或 GPU 资源的存储提供商来说，它提供了一个去中心化的证明计算市场。

存储提供商也可以作为**消费方**使用 Snark Market，从市场中购买证明计算以节省本地 GPU 资源。相关设置请参见 [Snark Market（消费方）](Snark-Market-Consumer.md)。

本指南将介绍如何：

* 在 GPU 节点上启用证明出售
* 配置价格和钱包
* 在界面中查看活动与结算信息

***

## 前置条件

在节点上启用 Snark Market 之前，请确保：

* 你运行的是具备 GPU 能力的 Curio 节点。
* 你可以正常访问 Curio Web UI。
* 你已经安装 Lotus，并且 Filecoin 主网已同步完成。\
  可参考 Lotus 文档：\
  [https://lotus.filecoin.io/lotus/install/linux/](https://lotus.filecoin.io/lotus/install/linux/)
* 你已经安装 **YugabyteDB**。\
  👉 可参考官方文档：\
  [https://docs.curiostorage.org/setup#setup-yugabytedb](https://docs.curiostorage.org/setup#setup-yugabytedb)

***

## 系统要求

* 现代 **NVIDIA GPU**（建议 12GB 及以上显存）
* 基础系统内存 70GB，加上每张 GPU 约 220GB
  * 内存更低也能运行，但无法使用更快的 CUDA C2 批量封装工具链，单次证明时间可能从约 2 分钟增加到约 10 分钟
* Curio **v1.27.0 或更新版本**
* 主网上有可用的 FIL 余额

***

## 设置步骤

### 1. 在配置层中启用 Market

请确保你**不是在 WindowPoSt 节点上启用该功能**。该功能仅适用于基于 GPU 的 PoRep 或 Snap 节点。

在 Web UI 中：

1. 进入 `Overview` → `Configuration`，选择你的 `snark-provider` 配置层
2. 找到 **Subsystems** 部分
3. 将 `EnableProofShare` 设置为 `true`
4. 保存并重启节点

***

### 2. 配置 Provider Settings

在侧边栏进入 `Snark Market`，在 **Provider Settings** 中：

* 勾选启用
* **创建一个新的 `f1` 钱包**（不要复用已有钱包）\
  可在命令行使用 `lotus wallet new secp256k1`\
  ⚠️ 后续虽然可以修改此钱包，但操作会比较麻烦
* 将 **Price (FIL/P)** 设置为合适值，例如测试时可先用 `0.005`
* 点击 **Update Settings**

***

### 3. 验证节点状态

完成配置后：

* 节点会自动开始排队获取证明任务
* 仪表盘中会看到：
  * Active Asks
  * SNARK Queue
  * Payment Summaries
  * Recent Settlements
* 你也可以查看公共仪表盘：[https://mainnet.snass.fsp.sh/ui/](https://mainnet.snass.fsp.sh/ui/)

***

## 钱包说明

* 请创建一个**新的 `f1` 地址**并为其充值（例如 `0.1 FIL`）
* 该钱包用于接收 SNARK 证明收益
* 请确保该钱包保持**已解锁**状态

***

## 价格与结算

* 价格按约 **130M constraints** 为一个基准单位设置
* 测试时可先从 `0.005 FIL` 起步
* 当结算 gas 费用低于待结算余额的 0.2% 时，系统会自动结算

***

## 说明

* 提供方需要先完成 **50 个 challenge proofs** 才能获得一个工作槽位
* 每个工作槽位可用于在市场中挂出一个 ask，或处理一个已分配的证明任务
* 工作槽位数量 = 已完成的 challenge proofs / 50
* 若未能在截止时间前完成任务，会损失 **50 个** challenge proofs
* 如果为了调价而撤销 ask，会损失 **1 个** challenge proof
* challenge proofs 只会通过完成未付费的 challenge 工作获得
* 证明任务必须在 **45 分钟** 内完成，否则需要重新积累信誉
* 系统具备故障重试能力，可自动重试失败任务
* 你可以通过增加 GPU 工作节点来进行水平扩展

***

如果你正在测试，欢迎在 Slack 的 `#fil-curio-help` 频道反馈，我们会持续关注。
