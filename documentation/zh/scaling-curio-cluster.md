---
description: >-
  本页描述如何向Curio集群添加额外的节点或矿工ID
---

# Scaling Curio cluster
# 扩展Curio集群

## Migrating additional lotus-miner to Curio
## 将额外的lotus-miner迁移到Curio

要将第二个或之后的`lotus-miner`迁移到现有的Curio集群，您需要遵循与之前相同的步骤。唯一的例外是不需要安装新的YugabyteDB集群。在`lotus-miner`节点上安装`curio`二进制文件后，您可以运行`curio guided-setup`来开始迁移。

## Initialising additional Miner IDs in existing Curio cluster
## 在现有Curio集群中初始化额外的矿工ID

在网络上启动新的矿工ID的过程与[使用新矿工ID初始化新的Curio集群](setup.md#initiating-a-new-curio-cluster|使用新矿工ID初始化新的Curio集群)时相同。唯一的例外是不需要安装新的YugabyteDB集群。

## Migrating lotus-worker to Curio cluster
## 将lotus-worker迁移到Curio集群

一旦您将矿工ID迁移到Curio集群，您需要将所有附加到已迁移矿工ID的`lotus-worker`节点重新用作Curio节点。

1. 在`lotus-worker`节点上[安装](installation.md|安装)`curio`二进制文件。
2. 使用正确的详细信息[配置服务ENV文件](curio-service.md#environment-variables-configuration|配置服务ENV文件)。
3. 启动新的`curio`节点，并在GUI中验证新节点现在是集群的一部分。
4. [将现有存储附加](storage-configuration.md#attach-existing-storage-to-curio|将现有存储附加到Curio)到Curio节点。
5. 对其余的worker重复此过程。

## Adding nodes to Curio cluster
## 向Curio集群添加节点

要向现有的Curio集群添加新节点，请按照以下流程操作。

* [安装](installation.md|安装)`curio`二进制文件。
* 使用正确的详细信息[配置服务ENV文件](curio-service.md#environment-variables-configuration|配置服务ENV文件)。
* 启动新的`curio`节点，并在GUI中验证新节点现在是集群的一部分。
* 如果需要，[附加任何新的或现有的存储](storage-configuration.md|附加存储)。
* 对任何需要附加的额外节点重复此过程。
