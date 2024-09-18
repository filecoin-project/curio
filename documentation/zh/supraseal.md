---
description: 本页面解释了如何在 Curio 中设置 supraseal 批量封装器
---

# Batch Sealing with SupraSeal
# 使用 SupraSeal 进行批量封装

{% hint style="danger" %}
**免责声明：** SupraSeal 批量封装目前处于 **测试阶段**。请谨慎使用，并预期在未来版本中可能出现潜在问题或变更。目前需要一些额外的手动系统配置。
{% endhint %}

SupraSeal 是一个针对 Filecoin 优化的批量封装实现，允许并行封装多个扇区。与单独封装扇区相比，它可以显著提高封装吞吐量。

## Key Features
## 主要特性

* 在单个批次中封装多个扇区（最多 128 个）
  * 核心利用效率提高高达 16 倍
* 优化以高效利用 CPU 和 GPU 资源
* 使用原始 NVMe 设备进行层存储，而不是 RAM

## Requirements
## 要求

* CPU 每个 CCX（AMD）或同等配置至少有 4 个核心
* 具有高 IOPS 的 NVMe 驱动器（建议总 IOPS 为 1000-2000 万）
* 用于 PC2 阶段的 GPU（建议使用 NVIDIA RTX 3090 或更好的）
* 配置 1GB 大页（最少 36 页）
* Ubuntu 22.04 或兼容的 Linux 发行版（需要 gcc-11，不需要系统范围内安装）
* 至少 256GB RAM，所有内存通道都已填满
  * 如果没有填满**所有**内存通道，封装**性能将大幅下降**
* NUMA-Per-Socket (NPS) 设置为 1

## Storage Recommendations
## 存储建议

您需要两组 NVMe 驱动器：

1. 用于层的驱动器：
   * 总计 1000-2000 万 IOPS
   * 容量为 11 x 32G x 批次大小 x 管道数
   * 原始未格式化的块设备（SPDK 将接管它们）
   * 每个驱动器应能持续 ~2GiB/s 的写入速度
     * 这个要求目前还不太清楚，可能较低的写入速率也可以。需要更多测试。
2. 用于 P2 输出的驱动器：
   * 带有文件系统
   * 快速且容量充足（~70G x 批次大小 x 管道数）
   * 如果速度足够快（~500MiB/s/GPU），可以是远程存储

## Hardware Recommendations
## 硬件建议

目前，社区正在努力确定批量封装的最佳硬件配置。一些普遍观察如下：

* 单插槽系统将更容易以全容量使用
* 您需要大量 NVMe 插槽，在 PCIe Gen4 平台上使用大批量大小时，可能会使用 20-24 个 3.84TB NVMe 驱动器
* 通常，您需要确保所有内存通道都已填满
* 您需要 4~8 个物理核心（非线程）用于批处理范围的计算，然后在每个 CCX 上，您将失去 1 个核心作为"协调器"
  * 每个线程计算 2 个扇区
  * 在 zen2 及更早版本上，哈希器每个线程只计算一个扇区
  * 大型（多核心）CCX 通常更好

{% hint style="info" %}
请考虑为 [SupraSeal 硬件示例](https://github.com/filecoin-project/curio/discussions/140) 做出贡献。
{% endhint %}

### Benchmark NVME IOPS
### 基准测试 NVME IOPS

在进行进一步配置之前，请确保对原始 NVME IOPS 进行基准测试，以验证是否满足 IOPS 要求。

```bash
cd extern/supra_seal/deps/spdk-v22.09/

# repeat -b with all devices you plan to use with supra_seal
# 注意：您需要测试所有设备，以便查看系统中是否存在任何瓶颈

./build/examples/perf -b 0000:85:00.0 -b 0000:86:00.0...  -q 64 -o 4096 -w randread -t 10
```

输出应该如下所示


========================================================
                                                                           Latency(us)
Device Information                     :       IOPS      MiB/s    Average        min        max
PCIE (0000:04:00.0) NSID 1 from core  0:  889422.78    3474.31      71.93      10.05    1040.94
PCIE (0000:41:00.0) NSID 1 from core  0:  890028.08    3476.67      71.88      10.69    1063.32
PCIE (0000:42:00.0) NSID 1 from core  0:  890035.08    3476.70      71.88      10.66    1001.86
PCIE (0000:86:00.0) NSID 1 from core  0:  889259.28    3473.67      71.95      10.62    1003.83
PCIE (0000:87:00.0) NSID 1 from core  0:  889179.58    3473.36      71.95      10.55     993.32
PCIE (0000:88:00.0) NSID 1 from core  0:  889272.18    3473.72      71.94      10.38     974.63
PCIE (0000:c1:00.0) NSID 1 from core  0:  889815.08    3475.84      71.90      10.97    1044.70
PCIE (0000:c2:00.0) NSID 1 from core  0:  889691.08    3475.36      71.91      11.04    1036.57
PCIE (0000:c3:00.0) NSID 1 from core  0:  890082.78    3476.89      71.88      10.44    1023.32
========================================================
Total                                  : 8006785.90   31276.51      71.91      10.05    1063.32


理想情况下，所有设备的总 IOPS 应大于 1000 万。

## Setup
## 设置

### Dependencies
### 依赖项

需要 CUDA 12.x，11.x 不能工作。构建过程依赖于系统范围内的 GCC 11.x 或本地安装的 gcc-11/g++-11。

* 在 Arch 上安装 https://aur.archlinux.org/packages/gcc11
* Ubuntu 22.04 默认有 GCC 11.x
* 在较新的 Ubuntu 上安装 `gcc-11` 和 `g++-11` 包


### Building
### 构建

构建支持批处理的 Curio 二进制文件：

```bash
make batch


make calibnet


make batch-calibnet
```

{% hint style="warning" %}
构建应在目标机器上运行。由于不同的 AVX512 支持，二进制文件在 CPU 代之间不可移植。
{% endhint %}

## Configuration
## 配置

* 在目标机器上运行 `curio calc batch-cpu` 以确定您的 CPU 支持的批次大小

<details>

<summary>批量CPU输出示例</summary>

```
# EPYC 7313 16-Core Processor

root@udon:~# ./curio calc batch-cpu
Basic CPU Information

Processor count: 1
Core count: 16
Thread count: 32
Threads per core: 2
Cores per L3 cache (CCX): 4
L3 cache count (CCX count): 4
Hasher Threads per CCX: 6
Sectors per CCX: 12
---------
Batch Size: 16 sectors

Required Threads: 8
Required CCX: 2
Required Cores: 6 hasher (+4 minimum for non-hashers)
Enough cores available for hashers ✔
Non-hasher cores: 10
Enough cores for coordination ✔

pc1 writer: 1
pc1 reader: 2
pc1 orchestrator: 3

pc2 reader: 4
pc2 hasher: 5
pc2 hasher_cpu: 6
pc2 writer: 7
pc2 writer_cores: 3

c1 reader: 7

Unoccupied Cores: 0

{
  sectors = 16;
  coordinators = (
    { core = 10;
      hashers = 2; },
    { core = 12;
      hashers = 6; }
  )
}
---------
Batch Size: 32 sectors

Required Threads: 16
Required CCX: 3
Required Cores: 11 hasher (+4 minimum for non-hashers)
Enough cores available for hashers ✔
Non-hasher cores: 5
Enough cores for coordination ✔
! P2 hasher will share a core with P1 writer, performance may be impacted
! P2 hasher_cpu will share a core with P2 reader, performance may be impacted

pc1 writer: 1
pc1 reader: 2
pc1 orchestrator: 3

pc2 reader: 0
pc2 hasher: 1
pc2 hasher_cpu: 0
pc2 writer: 4
pc2 writer_cores: 1

c1 reader: 0

Unoccupied Cores: 0

{
  sectors = 32;
  coordinators = (
    { core = 5;
      hashers = 4; },
    { core = 8;
      hashers = 6; },
    { core = 12;
      hashers = 6; }
  )
}
---------
Batch Size: 64 sectors

Required Threads: 32
Required CCX: 6
Required Cores: 22 hasher (+4 minimum for non-hashers)
Not enough cores available for hashers ✘
Batch Size: 128 sectors

Required Threads: 64
Required CCX: 11
Required Cores: 43 hasher (+4 minimum for non-hashers)
Not enough cores available for hashers ✘

```

</details>

* 创建一个新的批量封装器层配置，例如：batch-machine1：

```yaml
[Subsystems]
EnableBatchSeal = true

[Seal]
LayerNVMEDevices = [
  "0000:88:00.0",
  "0000:86:00.0", 
  # 添加所有要使用的NVMe设备的PCIe地址
  ]

  # 设置为您想要的批量大小（批量CPU命令所支持的CPU和您拥有的NVMe空间）
  BatchSealBatchSize = 32

  # 管道可以是1或2；2个管道会使存储需求翻倍，但在正确平衡的系统中，使层哈希运行100%的时间，几乎使吞吐量翻倍
  BatchSealPipelines = 2

  # 对于Zen2或更旧的CPU设置为true以确保兼容性
SingleHasherPerThread = false
```
### Configure hugepages
### 配置大页面

这可以通过在 `/etc/default/grub` 中添加以下内容来完成。批量密封器需要36个1G的大页面。

```bash
GRUB_CMDLINE_LINUX_DEFAULT="hugepages=36 default_hugepagesz=1G hugepagesz=1G"
```

然后运行 `sudo update-grub` 并重启机器。

或者在运行时：

```bash
sudo sysctl -w vm.nr_hugepages=36
```


然后检查 /proc/meminfo 以验证大页面是否可用：

```bash
cat /proc/meminfo | grep Huge
```


预期输出如下：


AnonHugePages:         0 kB
ShmemHugePages:        0 kB
FileHugePages:         0 kB
HugePages_Total:      36
HugePages_Free:       36
HugePages_Rsvd:        0
HugePages_Surp:        0
Hugepagesize:    1048576 kB


检查 `HugePages_Free` 是否等于36，内核有时会将一些大页面用于其他目的。

### Setup NVMe devices for SPDK:
### 为SPDK设置NVMe设备：

{% hint style="info" %}
这只在批量密封处于测试阶段时需要，Curio的未来版本将自动处理这个问题。
{% endhint %}

```bash
cd extern/supra_seal/deps/spdk-v22.09/
env NRHUGE=36 ./scripts/setup.sh
```


### PC2 output storage
### PC2输出存储

附加临时存储空间用于PC2输出（批量密封器每个扇区需要约70GB - 32GiB用于密封扇区，36GiB用于包含TreeC/TreeR和辅助文件的缓存目录）

## Usage
## 使用方法

1. 启动带有批量密封层的Curio节点

```bash
curio run --layers batch-machine1
```


2. 添加一批CC扇区：

```bash
curio seal start --now --cc --count 32 --actor f01234 --duration-days 365
```


3. 监控进度 - 你应该在[Curio GUI](curio-gui.md)中看到一个"Batch..."任务正在运行
4. PC1将花费3.5-5小时，之后是GPU上的PC2
5. 批处理完成后，存储将被释放用于下一批处理

## Optimization
## 优化

* 平衡批处理大小、CPU核心和NVMe驱动器，以保持PC1持续运行
* 确保有足够的GPU容量在下一个PC1批处理完成之前完成PC2
* 监控CPU、GPU和NVMe利用率以识别瓶颈
* 监控哈希器核心利用率

## Troubleshooting
## 故障排除

### Node doesn't start / isn't visible in the UI
### 节点无法启动/在UI中不可见

* 确保正确配置了大页面
* 检查NVMe设备的IOPS和容量
  * 如果spdk设置失败，尝试对NVMe设备执行 `wipefs -a`（这将擦除设备上的分区，请小心操作！）

### Performance issues
### 性能问题

你可以通过查看例如 `htop` 中的"hasher"核心利用率来监控性能。

要识别哈希器核心，调用 `curio calc supraseal-config --batch-size 128`（使用正确的批处理大小），并查找 `coordinators`
```go
topology:
...
{
  pc1: {
    writer       = 1;
...
    hashers_per_core = 2;

    sector_configs: (
      {
        sectors = 128;
        coordinators = (
          { core = 59;
            hashers = 8; },
          { core = 64;
            hashers = 14; },
          { core = 72;
            hashers = 14; },
          { core = 80;
            hashers = 14; },
          { core = 88;
            hashers = 14; }
        )
      }

    )
  },

  pc2: {
...
}

```
在此示例中，核心59、64、72、80和88是"协调器"，每个核心有两个哈希器，这意味着：

* 在第一组中，核心59是协调器，核心60-63是哈希器（4个哈希器核心/8个哈希器线程）
* 在第二组中，核心64是协调器，核心65-71是哈希器（7个哈希器核心/14个哈希器线程）
* 以此类推

协调器核心通常会保持100%的利用率，哈希器线程**应该**保持100%的利用率，任何低于这个水平的情况都表明系统存在瓶颈，比如NVMe IOPS不足、内存带宽不足或NUMA设置不正确。

### Performance issues
### 性能问题

## Troubleshooting
## 故障排除

要进行故障排除：

* 仔细阅读本页顶部的要求
* [对NVME IOPS进行基准测试](supraseal.md#benchmark-nvme-iops)
* 如果PC2速度慢，验证GPU设置
* 检查批处理过程中的日志是否有任何错误
