---
description: 如何编辑和管理Curio集群的配置
---

# Configuration
# 配置

Curio的配置存储在HarmonyDB的一个名为`harmony_config`的表中。当启动Curio节点时，会提供一个或多个层名称以获取该节点所需的配置。

## Configuration Layers 
## 配置层 

配置层提供了一组决定系统如何运行的配置参数。这些层可以在不同级别定义，意味着更高层可以覆盖较低层，系统将根据最终堆叠的输出进行行为。

配置层可以按层次结构排列，通常从`base`（最通用）到最具体。`base`层定义默认配置值。更具体的层会用更有针对性的配置覆盖这些默认值。

例如，在一个简单的两层配置系统中，层可以按以下顺序组织：基础层 - 这是最通用的层。它总是被包含，所以在这里添加任何对默认值的修改。如果你在这里包含所有的矿工ID（在地址部分定义），所有硬件将用于所有矿工需求。任务层 - 这一层启用SDR任务。考虑包含的层（如下）。

如果Curio节点以上述2层启动，那么它将为所有矿工ID执行SDR任务，并使用任何其他配置参数的默认值。

层的使用示例：

```bash
curio run --layers=post
```


{% hint style="warning" %}
当Curio节点启动时，`base`层总是默认应用。
{% endhint %}

### Advantages of Configuration Layers 
### 配置层的优势 

灵活性：配置层允许系统的不同部分或不同用户根据预定义的设置表现不同。
可扩展性：通过分离关注点并允许特定配置，系统在扩展时变得更容易管理。
可维护性：可以在适当的层上进行配置更改，而不影响整个系统。

### Layer Stacking 
### 层堆叠 

配置层按提供的顺序堆叠。`base`层总是默认应用，所以可以跳过。

例如，如果Curio节点以以下层启动：

```
--layers miner1,sdr,wdPost,pricing
```


这些层将相互堆叠，创建节点的最终配置。堆叠顺序将是base > miner1 > sdr > wdPost > pricing。如果一个配置参数在多个层中定义，则将使用最终层的值。

### Working with layers 
### 使用层 

Curio允许您使用层来管理节点配置。每个层可以独立应用或修改，其中'base'层在启动时是必需的。

#### **Print default configuration** 
#### **打印默认配置** 

默认配置在base层中默认使用。

```bash
curio config default
```


#### **Adding a New Layer** 
#### **添加新层** 

要添加新的配置层或更新现有的层，您可以提供文件名或通过stdin直接输入。

```bash
curio config set --title <stdin/Filename>
```


#### **List all layers** 
#### **列出所有层** 

列出数据库中存在的所有配置层。

```bash
curio config ls
```


#### **Editing a Layer** 
#### **编辑层** 

直接编辑配置层。

* 使用`vim`编辑器编辑


```bash
curio config edit --editor vim <layer name>
```


* 使用其他编辑器如nano编辑


```bash
curio config edit --editor nano <layer name>
```

#### **Interpreting Stacked Layers** 
#### **解释堆叠的层** 

解释并查看所有应用的配置层的组合效果，包括系统生成的注释。

```bash
curio config view --layers [layer1,layers2]
```


#### **Removing a Layer** 
#### **移除层** 

通过名称移除特定的配置层。


```bash
curio config rm <layer name>
```


### Pre-built Layers 
### 预构建层 

当初始化第一个Curio矿工或将第一个Lotus-Miner迁移到Curio时，该过程默认为用户创建一些层。这些层主要定义特定任务是否应该被机器选择。

```toml
#### **post** 



[Subsystems]
EnableWindowPost = true
EnableWinningPost = true
```


```bash
#### **sdr** 


[Subsystems]
EnableSealSDR = true
```


```toml
#### **seal** 


[Subsystems]
EnableSealSDR = true
EnableSealSDRTrees = true
EnableSendPrecommitMsg = true
EnablePoRepProof = true
EnableSendCommitMsg = true
EnableMoveStorage = true
```


```toml
#### **seal-gpu** 


[Subsystems]
EnableSealSDRTrees = true
EnableSendPrecommitMsg = true
```


```toml
#### **seal-snark** 


[Subsystems]
EnablePoRepProof = true
EnableSendCommitMsg = true
```


```toml
#### **gui** 


[Subsystems]
EnableWebGui = true
```


## Configuration management in UI 
## UI中的配置管理 

Curio GUI为管理配置提供了一个用户友好的界面。要访问此功能，请从UI菜单导航到"Configurations"页面。在此页面上，列出了数据库中所有可用的层。用户可以通过点击每个层来编辑它。

<figure><img src="../.gitbook/assets/config.png" alt="Configurations"><figcaption><p>Curio GUI配置页面</p></figcaption></figure>

要更新配置字段，用户必须首先通过勾选相应的框来启用它。启用后，可以填充字段值。要注释掉该字段，只需取消勾选该框。

<figure><img src="../.gitbook/assets/config-edit.png" alt="Configuration edit"><figcaption><p>Curio GUI配置编辑器</p></figcaption></figure>
