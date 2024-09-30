---
description: >-
  本页面详细概述了构成 Curio 的核心概念和组件，包括 HarmonyDB、HarmonyTask 等。
---

# Design
# 设计

## Curio Cluster
## Curio 集群

Curio 的核心内部组件包括 HarmonyDB、HarmonyTask、ChainScheduler 以及配置和当前存储定义的数据库抽象。

<figure><img src="../.gitbook/assets/curio-node.png" alt="Curio Node"><figcaption><p>Curio nodes</p></figcaption></figure>

Curio 集群是由多个连接到 YugabyteDB 集群和市场节点的 Curio 节点组成的集群。单个 Curio 集群可以根据需要为多个矿工 ID 提供服务，并在它们之间共享计算资源。

<figure><img src="../.gitbook/assets/curio-cluster.png" alt="Curio cluster"><figcaption><p>Curio cluster</p></figcaption></figure>

## HarmonyDB 

HarmonyDB 是一个简单的 SQL 数据库抽象层，由 HarmonyTask 和 Curio 堆栈的其他组件使用，用于存储和检索 YugabyteDB 中的信息。

### Key Features: 
### 关键特性：

* **弹性:** 如果主连接失败，自动切换到备用数据库。
* **安全性:** 防止 SQL 注入漏洞。
* **便利性:** 提供常见 Go + SQL 操作的辅助函数。
* **监控:** 通过 Prometheus 统计和错误日志提供数据库行为的洞察。

### Basic Database Details 
### 基本数据库详情

* Postgres 数据库模式称为 “curio”，所有的 harmony 数据库表都在这个模式下。
* 表 `harmony_task` 存储待处理任务列表。
* 表 `harmony_task_history` 存储已完成的任务、超过限制的重试任务，并作为触发后续任务（可能在不同机器上）的输入。
* 表 `harmony_task_machines` 由 lib/harmony/resources 管理。此表引用注册的机器用于任务分配。注册不意味着义务，但有助于发现。

## HarmonyTask 

HarmonyTask 是纯粹的（无任务逻辑）分布式任务管理器。

### Design Overview 
### 设计概述

* 任务为中心：HarmonyTask 专注于将任务管理为小型工作单元，减轻开发人员的调度和管理负担。
* 分布式：任务分布在各个机器上以实现高效执行。
* 贪婪工人：工人主动认领他们可以处理的任务。
* 轮询分配：在 Curio 节点认领任务后，HarmonyDB 尝试将剩余工作分配给其他机器。

<figure><img src="../.gitbook/assets/curio-tasks.png" alt="Curio Tasks"><figcaption><p>Harmony tasks</p></figcaption></figure>

### Model 
### 模型

* **被阻止的任务:** 任务可能因以下原因被阻止：
  * 运行节点上的‘子系统’配置被禁用
  * 达到指定的最大任务限制
  * 资源耗尽
  * CanAccept() 函数（任务特定）拒绝任务
* **任务启动:** 任务可以通过以下方式启动：
  * 定期数据库读取（每 3 秒）
  * 当前进程添加到数据库
* **任务添加方法：**
  * 异步监听任务（例如，用于区块链）
  * 由任务完成触发的后续任务（封装流水线）
* **防止重复任务：**
  * 避免重复任务的机制由任务定义决定，最有可能使用唯一键。

## Distributed Scheduling 
## 分布式调度

Curio 实现了一种通过 HarmonyDB 协调的分布式调度机制。Curio 节点根据它们可以处理的任务类型和资源来选择任务。节点在接受任务后不会贪婪，即使它们有足够的资源。其他节点轮流认领任务。每隔 3 秒，如果有可用资源，则会接受额外的任务。这确保了任务的更均匀调度。

## Chain Scheduler 
## 链调度器

`CurioChainSched` 或链调度器在应用或移除新的 TipSet 时触发一些回调函数。这相当于在每个 epoch 获取最重的 TipSet。这些回调函数依次为每种依赖链变化的类型添加新任务。这些任务类型包括 WindowPost、WinningPost 和 MessageWatcher。

## Poller 
## 轮询器

轮询器是一个简单的循环，根据预定义的时间间隔（100 毫秒）定期获取待处理任务，或直到上下文发起优雅退出。一旦从数据库中获取到待处理任务，它会尝试在 Curio 节点上调度所有任务。此尝试将导致以下结果之一：

* 任务被接受
* 任务未被调度，因为机器繁忙
* 任务未被接受，因为节点的 CanAccept（由任务定义）选择不处理指定任务

如果任务在轮询周期内被接受，则下一个周期前的等待时间为 100 毫秒。但如果任务因任何原因未被调度，轮询器将在 3 秒后重试。

## Task Decision Logic 
## 任务决策逻辑

对于机器可以处理的每种任务类型，它首先检查机器是否有足够的能力执行所述任务。然后它查询数据库中没有 `owner_id` 且与任务类型同名的任务。如果存在这样的任务，它会尝试接受它们的工作。如果任何工作被接受，它返回 true，否则返回 false。接受每个任务的决策逻辑如下：

1. 检查是否有任何任务要做。如果没有，返回 true。
2. 检查是否达到此类型任务的最大数量。如果运行任务的数量达到或超过最大限制，记录一条消息并返回 false。
3. 检查机器是否有足够的资源来处理任务。这包括检查 CPU、RAM、GPU 容量和可用存储。如果机器没有足够的资源，记录一条消息并返回 false。
4. 通过调用 `CanAccept` 方法检查任务是否可以被接受。如果不能接受，记录一条消息并返回 false。
5. 如果任务需要存储空间，机器尝试声明它。如果声明失败，记录一条消息并释放已声明的存储空间，然后返回 false。
6. 如果任务来源是 `recover`，即机器在关机前正在执行此任务，则任务计数增加一个，并在单独的 goroutine 中开始处理任务。
7. 如果任务来源是 `poller`，即新的待处理任务，尝试为当前主机名声明任务。如果不成功，释放已声明的存储并尝试考虑下一个任务。
8. 如果成功，任务计数增加一个，并在单独的 goroutine 中开始处理任务。
9. 这个 goroutine 还会更新任务历史中的任务状态，并根据任务是否成功，删除任务或更新任务表中的任务。
10. 返回 true，表示工作已被接受并将被处理。

## GPU Management in Curio
## Curio 中的 GPU 管理

### **Historical Issues with Lotus-Miner Scheduler**
### **Lotus-Miner 调度器的历史问题**

历史上，当 lotus-worker 进程有多个 GPU 可用时，Lotus-Miner 调度器在高效利用 GPU 方面遇到了困难。这些问题主要源于底层的 proofs 库，该库处理所有与 GPU 相关的任务并管理 GPU 分配。这导致了以下问题：

* 单个任务被分配到多个 GPU。
* 多个任务被分配到单个 GPU。

### **Solution with Curio: The GPU Picker Library "ffiselect"**
### **Curio 的解决方案：GPU 选择库 "ffiselect"**

为了解决 Curio 中的这些问题，我们实现了一个名为 "ffiselect" 的 GPU 选择库。该库确保每个需要 GPU 的任务都单独分配一个。过程如下：

1. **任务分配**：每个需要 GPU 的任务都分配一个特定的 GPU。
2. **子进程创建**：为每个任务生成一个新的子进程，并为其分配专用 GPU。
3. **Proofs 库调用**：子进程使用单个 GPU 和特定任务调用 Proofs 库。

<figure><img src="../.gitbook/assets/2024-06-04-040735_1470x522_scrot.png" alt=""><figcaption><p>Curio FFISelect in action</p></figcaption></figure>

这种方法确保了高效且无冲突的 GPU 使用，每个任务都由专用 GPU 处理，从而解决了 `lotus-miner` 调度器观察到的历史问题。
