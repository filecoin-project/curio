---
description: >-
  本指南描述了如何为Curio节点附加和配置密封和永久存储
---

# Storage Configuration
# 存储配置

每个Curio节点在 `~/.curio/storage.json`（或 `$CURIO_REPO_PATH/storage.json`）中跟踪定义的存储位置，并使用 `~/.curio` 路径作为默认值。

初始化存储位置时，会创建一个 `<path-to-storage>/sectorstorage.json` 文件，其中包含分配给该位置的UUID，以及是否可用于密封或存储。

## Adding sealing storage location 
## 添加密封存储位置 

在添加密封存储位置之前，您需要考虑密封任务将在哪里执行。此命令必须从您想要附加存储的Curio节点本地运行。

```bash
curio cli --machine <Machine IP:Port> storage attach --init --seal <PATH_FOR_SEALING_STORAGE>
```

## Adding long-term storage location 
## 添加长期存储位置 

**自定义存储位置：** 密封过程完成后，密封的扇区会被移动到存储位置，可以按以下方式指定：

```bash
curio cli --machine <Machine IP:Port> storage attach --init --store <PATH_FOR_LONG_TERM_STORAGE>
```

此命令必须从您想要附加存储的Curio节点本地运行。这个位置可以由大容量但较慢的旋转硬盘组成。

## Attach existing storage to Curio
## 将现有存储附加到Curio

`lotus-miner` 或 `lotus-worker` 使用的存储位置可以被Curio集群重复使用。一旦迁移的 `lotus-miner` 或 `lotus-worker` 已经运行Curio服务，就可以附加它。

```bash
curio cli --machine <Machine IP:Port> storage attach <PATH_FOR_LONG_TERM_STORAGE>
```

## Filter sector types <a href="#filter-sector-types" id="filter-sector-types"></a>
## 过滤扇区类型 <a href="#filter-sector-types" id="filter-sector-types"></a>

您可以通过调整 `<path-to-storage>/sectorstorage.json` 中的配置文件来过滤每个密封路径中允许的扇区类型。

```json
{
  "ID": "1626519a-5e05-493b-aa7a-0af71612010b",
  "Weight": 10,
  "CanSeal": false,
  "CanStore": true,
  "MaxStorage": 0,
  "Groups": [],
  "AllowTo": [],
  "AllowTypes": null,
  "DenyTypes": null
}
```

`AllowTypes` 和 `DenyTypes` 的有效值是：


"unsealed"
"sealed"
"cache"
"update"
"update-cache"


这些值必须放在数组中才有效（例如 `"AllowTypes": ["unsealed", "update-cache"]`），任何其他值都会在 `Curio` 启动时生成错误。还需要重启附加了此存储的 `Curio` 节点，以使更改生效。

## Separate sealed and unsealed 
## 分离密封和未密封扇区 

一个非常基本的设置，您可以通过以下方式分离未密封和密封的扇区：

* 在您想要存储密封扇区的长期存储路径中添加 `"DenyTypes": ["unsealed"]`。
* 在您想要存储未密封扇区的长期存储路径中添加 `"AllowTypes": ["unsealed"]`。

仅为 `AllowTypes` 设置 `unsealed` 仍然允许 `cache` 和 `update-cache` 文件放置在此存储路径中。如果您想完全拒绝此路径中的所有其他类型的扇区，可以在 `"DenyTypes"` 字段中添加其他有效值。

{% hint style="info" %}
如果存储路径中存在不允许类型的现有文件，这些文件仍然可以用于PoSt/检索。因此，在存储路径配置错误的情况下，最坏的情况是密封任务会卡住，等待存储变得可用。
{% endhint %}

## Segregating long-term storage per miner 
## 按矿工隔离长期存储 

用户可以通过在 `sectorstore.json` 文件中指定矿工地址字符串来为特定矿工ID分配长期存储。这种配置允许精确控制哪些矿工可以使用存储。

* 要允许特定矿工，请在AllowMiners数组中包含他们的地址：

  ```yaml 
    "AllowMiners": ["t01000", "t01002"]
  ```

    这种配置只允许列出的矿工（t01000和t01002）访问存储。

* 同样，要拒绝特定矿工访问存储，请在DenyMiners数组中包含他们的地址：

  ```yaml
    "DenyMiners": ["t01003", "t01004"]
  ```

    在这个例子中，地址为t01003和t01004的矿工被明确拒绝访问存储。

这种双重配置方法允许基于矿工ID灵活和安全地管理存储访问。
