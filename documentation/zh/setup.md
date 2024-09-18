---
description: >-
   本指南将向您展示如何设置新的Curio集群或从Lotus-Miner迁移到Curio
---

# Setup
# 设置

## Setup YugabyteDB
## 设置YugabyteDB

{% hint style="warning" %}
如果您已经为Boost设置了YugabyteDB，那么您可以为Curio重用相同的YugabyteDB实例。您必须确保YugabyteDB是多节点集群以实现高可用性。您可以直接跳到[从Lotus-Miner迁移到Curio](setup.md#migrating-from-lotus-miner-to-curio)或[初始化新的Curio矿工](setup.md#initiating-a-new-curio-cluster)。
{% endhint %}

在本指南中，我们将设置一个单节点YugaByteDB。但是，您必须在集群中设置多个YugaByteDB实例以实现高可用性。

在安装和设置YugabyteDB之前，请确保您具备以下条件：

{% hint style="danger" %}
**不要使用ZFS作为YugabyteDB的后备驱动器，因为目前无法使用高级文件系统命令。**
{% endhint %}

1. 以下操作系统之一：

    * CentOS 7或更高版本
    * Ubuntu 16.04或更高版本

    对于其他操作系统，Docker或Kubernetes。请查看[YugabyteDB文档](https://docs.yugabyte.com/preview/quick-start/)。

2. **Python 3。** 要检查版本，请执行以下命令：

```bash
python --version
```


Python 3.7.3


如果遇到`Command 'python' not found`错误，您可能没有未版本化的系统范围python命令。

* 从Ubuntu 20.04开始，python不再可用。要解决此问题，请运行`sudo apt install python-is-python3`。
* 对于CentOS 8，通过运行`sudo alternatives --set python /usr/bin/python3`将`python3`设置为python的替代方案。

安装这些依赖项后，我们可以运行安装脚本：


wget https://downloads.yugabyte.com/releases/2.21.0.1/yugabyte-2.21.0.1-b1-linux-x86_64.tar.gz
tar xvfz yugabyte-2.21.0.1-b1-linux-x86_64.tar.gz && cd yugabyte-2.21.0.1/
./bin/post_install.sh
./bin/yugabyted start --advertise_address 127.0.0.1  --master_flags rpc_bind_addresses=127.0.0.1 --tserver_flags rpc_bind_addresses=127.0.0.1



+----------------------------------------------------------------------------------------------------------+
|                                                yugabyted                                                 |
+----------------------------------------------------------------------------------------------------------+
| Status              :                                                                                    |
| Replication Factor  : None                                                                               |
| YugabyteDB UI       : http://127.0.0.1:15433                                                             |
| JDBC                : jdbc:postgresql://127.0.0.1:5433/yugabyte?user=yugabyte&password=yugabyte          |
| YSQL                : bin/ysqlsh   -U yugabyte -d yugabyte                                               |
| YCQL                : bin/ycqlsh   -u cassandra                                                          |
| Data Dir            : /root/var/data                                                                     |
| Log Dir             : /root/var/logs                                                                     |
| Universe UUID       : 411422ee-4c17-4f33-996e-ced847d10f5c                                               |
+----------------------------------------------------------------------------------------------------------+


您可以根据自己的配置和需求调整`--advertise_address`、`--rpc_bind_addresses`和`--tserver_flags`。

## Migrating from Lotus-miner to Curio
## 从Lotus-miner迁移到Curio

Curio为用户提供了快速上手的工具。请在您的`lotus-miner`节点上运行以下命令，并按照屏幕上的说明操作。它支持英语（en）、中文（zh）和韩语（ko）。


curio guided-setup


迁移完成后，您可以关闭所有工作节点和矿工进程。您可以启动`curio`进程，使用正确的[配置层](configuration/#configuration-layers)来替换它们。

### Testing the setup
### 测试设置

您可以通过运行WindowPoSt测试计算来确认`curio`进程能够调度和计算WindowPoSt：


curio test window-post task


从输出中，我们可以确认WindowPoSt被插入到数据库中，并被运行_wdpost_配置层的Curio进程拾取。

测试成功后，请继续[curio服务配置](curio-service.md)。

## Initiating a new Curio cluster
## 初始化新的Curio集群

要创建新的Curio集群，需要一个[Lotus守护节点](https://bafybeib7hujkpoqohpby6dqabdea2t6ehcysics3ejoh4jrgtuke4rmolu.on.fleek.co/lotus/install/prerequisites/)。

{% hint style="warning" %}
Lotus守护节点必须与正在设置的Curio属于同一网络。

例如：`calibration`网络守护节点不能与`mainnet` Curio集群一起使用。
{% endhint %}

### Wallet setup
### 钱包设置

在Filecoin网络上初始化新的矿工ID需要所有者地址、工作者地址和发送者地址。这些地址可以相同或不同，取决于用户的选择。用户必须在运行Curio命令之前在Lotus节点上创建这些钱包。

```bash
lotus wallet new bls
lotus wallet new bls
```

创建新钱包后，我们必须向它们发送一些资金。

```bash
lotus send <WALLET 1> 5
lotus send <WALLET 2> 5
```

### Creating new miner ID
### 创建新的矿工ID

Curio为用户提供了快速上手的工具。请在新的Curio节点上运行以下命令，选择"创建新矿工"选项，并按照屏幕上的说明操作。它支持英语（en）、中文（zh）和韩语（ko）。

1. 启动引导设置。

```bash
curio guided-setup
```

2. 选择"创建新矿工"选项。


默认使用英语。如果您需要其他语言支持，请联系Curio团队。
使用箭头键导航：↓ ↑ → ←
? 我想要：
从现有的Lotus-Miner迁移
▸ 创建新矿工


3. 输入您的YugabyteDB详细信息。


此过程部分是幂等的。一旦创建了新的矿工参与者并且后续步骤失败，用户需要运行'curio config new-cluster < miner ID >'来完成配置。

使用箭头键导航：↓ ↑ → ←
? 输入连接到您的Yugabyte数据库安装的信息（https://download.yugabyte.com/）：
▸ 主机：127.0.0.1
端口：5433
用户名：yugabyte
密码：yugabyte
数据库：yugabyte
继续连接并更新架构。

✔ 步骤完成：预初始化步骤完成


4. 输入用于"创建矿工"消息的钱包详细信息。


初始化新的矿工参与者。
使用箭头键导航：↓ ↑ → ←
? 输入创建新矿工的信息：
▸ 所有者地址：<空>  <------ 在此处输入钱包1
工作者地址：<空>  <------ 在此处输入钱包2
发送者地址：<空>  <------ 在此处输入钱包1
扇区大小：0  <--------------- 扇区大小（32 G/GiB/GB）
置信纪元：0
继续验证地址并创建新的矿工参与者。



初始化新的矿工参与者。
✔ 所有者地址：<空>
输入所有者地址：t3weiymrx3iyivzeuub5l232gb62ocu7zbjtztudiipm6wkkmsehdydrdddm6cdrflxir26cmrz4xui6t5gruq
✔ 工作者地址：<空>
输入工作者地址：t3xhmgfxurecrusgubzdgme4t2ecxbiyny5uanfzvcrrihzhia654f6gp2ynugpiyp5xe7ibg6fqly76kowfva
✔ 发送者地址：<空>
输入发送者地址：t3weiymrx3iyivzeuub5l232gb62ocu7zbjtztudiipm6wkkmsehdydrdddm6cdrflxir26cmrz4xui6t5gruq
✔ 扇区大小：0
输入扇区大小：8 MiB
✔ 置信纪元：0
置信纪元：0
推送CreateMiner消息：bafy2bzacebu3mhaj6chnz5frjo2sbxduebnh4e7e37fwm3jd7xhvhla7t6ylu
等待确认


5. 等待新的矿工参与者创建完成。


新矿工的地址是：t01004 (t2cmgqvicpcil5zlp6bqsffmjjfz7ix66k4zaojay)
✔ 步骤完成：矿工t01004创建成功

✔ 步骤完成：配置'base'已更新以包含此矿工的地址


6. 我们请求您与我们分享有关您的矿工的基本数据，以帮助我们改进Curio。


Curio团队希望改进您使用的软件。告诉团队您正在使用`curio`。
使用箭头键导航：↓ ↑ → ←
? 选择您想与Curio团队分享的内容：
▸ 个人数据：矿工ID、Curio版本、链（主网或校准网）。已签名。
聚合匿名：版本、链和矿工算力（分桶）。
提示：我是在某个链上运行Curio的人。
什么都不分享。


7. 完成初始化。


✔ 步骤完成：新矿工初始化完成。

尝试使用curio run --layers=gui运行Web界面，以获得进一步的引导改进。


8. 如果在步骤3中输入了非默认值，请在运行Curio命令之前导出相关详细信息。

| 环境变量 | 用途 |
| ------------------- | ------------------------- |
| CURIO_DB_HOST | YugabyteDB SQL IP |
| CURIO_DB_NAME | YugabyteDB名称 |
| CURIO_DB_USER | 连接的DB用户 |
| CURIO_DB_PASSWORD | 用户密码 |
| CURIO_DB_PORT | YugabyteDB的SQL端口 |
| CURIO_REPO_PATH | Curio的默认仓库路径 |

9. 首先尝试仅使用`GUI`运行Curio。

```bash
curio run --layers gui
```

10. 如果`curio`进程成功启动，请继续使用GUI并验证您可以访问所有页面。验证完成后，请继续[curio服务配置](curio-service.md)。
