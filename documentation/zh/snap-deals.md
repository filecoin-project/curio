---
description: 本指南解释了如何在Curio中启用snap-deals。
---

# Snap Deals
# 快照交易

## Simplified explanation
## 简化解释

快照交易允许存储提供商接受用户的交易，并将用户的数据放入已经提交的存储块中。这听起来有点复杂，所以让我们这样想象一下。

想象有一个城镇，里面有一个很长的架子。这个城镇的任何人都可以在这个架子上存储任何东西。当一个镇民想要存储某样东西时，他们把那个"东西"交给存储提供商。存储提供商制作一个木箱，把镇民的东西放进箱子里，然后把箱子放在架子上。

<figure><img src=".gitbook/assets/shelf.png" alt="代表Filecoin网络的架子。"><figcaption><p>扇区作为一个架子</p></figcaption></figure>

一些箱子里装有有用的东西，比如照片、音乐或视频。但有时，存储提供商没有镇民排队要把有用的东西放进箱子里。所以他们就把包装花生放进箱子里，然后把它放在架子上。这意味着有很多箱子被制作出来只是为了装包装花生。制作箱子需要很长时间，也需要存储提供商付出大量的工作。

<figure><img src=".gitbook/assets/data-types.png" alt="Filecoin扇区中的数据类型。"><figcaption><p>数据箱</p></figcaption></figure>

与其每次有人想存储东西时都创建一个新箱子，不如我们直接用有用的东西替换包装花生！因为没有人在乎包装花生，所以把它们扔掉也不会有人不高兴。而且存储提供商可以在架子上放置有用的东西，而不必创建新的箱子！对镇民来说也更好，因为他们不必等待存储提供商创建新的箱子！

<figure><img src=".gitbook/assets/emptying-boxes.png" alt="清空扇区中的虚拟数据，用真实数据填充。"><figcaption><p>替换数据</p></figcaption></figure>

这是快照交易工作方式的简化视图。存储提供商不需要创建一个全新的扇区来存储客户的数据，而是可以将客户的数据放入已提交容量的扇区中。数据变得更快可用，对存储提供商来说成本更低，而且网络的存储容量得到了更多的利用！

## How to enable snap-deals
## 如何启用快照交易

要在Curio集群中启用快照交易管道，用户需要在具有GPU资源的机器上启用特定于快照交易的任务。除此之外，还需要更新交易接收管道，以将交易传递给快照交易管道，而不是PoRep封装管道。

{% hint style="warning" %}
数据可以在任何给定时间使用快照交易管道或PoRep管道进行接收，但不能同时使用两者。
{% endhint %}

## FastSnap（SnapDeals UpdateEncode 加速）

Curio 的 SnapDeals `UpdateEncode` 支持 **快速模式**（“fastsnap”）：使用 Supraseal 加速 TreeR 生成，并使用 Curio 原生的 snap 编码实现。

- **能力检查**：

```bash
curio test supra system-info
```

查看 **“Can run fast TreeR: yes”**。

- **回退模式**：如果主机缺少 AVX-512（AMD64v4）或没有可用 CUDA GPU，会自动回退到 CPU 的 TreeR 生成路径。
- **故障排查 / 强制回退**：

```bash
export DISABLE_SUPRA_TREE_R=1
```

这会强制使用 CPU 回退的 TreeR 路径（用于隔离 Supraseal/CUDA/工具链问题）。

### Enable snap tasks
### 启用快照任务

1. 将Curio已经附带的`upgrade`层添加到具有GPU资源的Curio节点上的`/etc/curio.env`文件中。\
    

```bash
CURIO_LAYERS=gui,seal,post,upgrade <----- 添加"upgrade"层
CURIO_ALL_REMAINING_FIELDS_ARE_OPTIONAL=true
CURIO_DB_HOST=yugabyte1,yugabyte2,yugabyte3
CURIO_DB_USER=yugabyte
CURIO_DB_PASSWORD=yugabyte
CURIO_DB_PORT=5433
CURIO_DB_NAME=yugabyte
CURIO_REPO_PATH=~/.curio
CURIO_NODE_NAME=ChangeMe
FIL_PROOFS_USE_MULTICORE_SDR=1
```

\

2. 重启节点上的Curio服务。\
    

```bash
systemctl restart curio
```

### Update the Curio market adapter
### 更新Curio市场适配器

1. 为您希望使用快照交易管道的minerID创建或更新市场层（[如果已经创建了一个](enabling-market.md#enable-market-adapter-in-curio)）。\

```bash
curio config add --title mt01000
```

 \
添加一个类似这样的条目：\
 

```yaml
  [Subsystems]
  EnableParkPiece = true
  BoostAdapters = ["t10000:127.0.0.1:32100"]
  
  [Ingest]
  DoSnap = true
```

\
按`ctrl + D`保存并退出。\
或编辑现有层。\


curio config edit mt01000


 \
为接收启用快照交易：\

```yaml
  [Subsystems]
  EnableParkPiece = true
  BoostAdapters = ["t10000:127.0.0.1:32100"]
  
  [Ingest]
  DoSnap = true
```

\
保存层并退出。

 
2. 根据[最佳实践](best-practices.md)将新的市场配置层添加到适当的节点。
3. 重启Curio服务。
