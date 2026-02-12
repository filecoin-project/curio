---
description: This page explains how to set up Curio batch sealing (extern/supraseal)
---

# Batch Sealing

{% hint style="danger" %}
**Disclaimer:** Batch sealing is currently in **BETA**. Use with caution and expect potential issues or changes in future versions. Some additional manual system configuration is required.

Batch sealing only supports **CC sectors** at the moment.

If you enable batch sealing on a node but do **not** enable SnapDeals in the cluster, deals may be routed into the CC/batch pipeline which will seal empty sectors (discarding deal data). Make sure SnapDeals are enabled if you intend to onboard real data deals.
{% endhint %}

## CC Scheduler (batch sealing only)

Curio includes a **CC Scheduler** UI and DB table (`sectors_cc_scheduler`) used to decide how many CC sectors to queue for batch sealing per SP.

- The **CC Scheduler is only used for batch sealing**.
- Do not enable or rely on it for deals/SnapDeals.

Curio’s web UI exposes this as the **CC Scheduler** page/tab.

## Key Features

* Seals multiple sectors (up to 128) in a single batch
  * Up to 16x better core utilisation efficiency
* Optimized to utilize CPU and GPU resources efficiently
* Uses raw NVMe devices for layer storage instead of RAM

## Requirements

* CPU with at least 4 cores per CCX (AMD) or equivalent
* NVMe drives with high IOPS (10-20M total IOPS recommended)
* GPU for PC2 phase (NVIDIA RTX 3090 or better recommended)
* 1GB hugepages configured (minimum 36 pages)
* Ubuntu or compatible Linux distribution (**gcc-13 required**, doesn't need to be system-wide)
* At least 256GB RAM, ALL MEMORY CHANNELS POPULATED
  * Without **all** memory channels populated sealing **performance will suffer drastically**
* NUMA-Per-Socket (NPS) set to 1

## Storage Recommendations

You need 2 sets of NVMe drives:

1. Drives for layers:
   * Total 10-20M IOPS
   * Capacity for 11 x 32G x batchSize x pipelines
   * Raw unformatted block devices (SPDK will take them over)
   * Each drive should be able to sustain \~2GiB/s of writes
     * This requirement isn't understood well yet, it's possible that lower write rates are fine. More testing is needed.
2. Drives for P2 output:
   * With a filesystem
   * Fast with sufficient capacity (\~70G x batchSize x pipelines)
   * Can be remote storage if fast enough (\~500MiB/s/GPU)

The following table shows the number of NVMe drives required for different batch sizes. The drive count column indicates `N + M` where `N` is the number of drives for layer data (SPDK) and `M` is the number of drives for P2 output (filesystem). The iops/drive column shows the minimum iops **per drive** required for the batch size. Batch size indicated with `2x` means dual-pipeline drive setup. IOPS requirements are calculated simply by dividing total target 10M IOPS by the number of drives. In reality, depending on CPU core speed this may be too low or higher than necessary. When ordering a system with barely enough IOPS plan to have free drive slots in case you need to add more drives later.

| Batch Size   | 3.84TB | 7.68TB | 12.8TB | 15.36TB | 30.72TB |
| ------------ | ------ | ------ | ------ | ------- | ------- |
| 32           | 4 + 1  | 2 + 1  | 1 + 1  | 1 + 1   | 1 + 1   |
| ^ iops/drive | 2500K  | 5000K  | 10000K | 10000K  | 10000K  |
| 64 (2x 32)   | 7 + 2  | 4 + 1  | 2 + 1  | 2 + 1   | 1 + 1   |
| ^ iops/drive | 1429K  | 2500K  | 5000K  | 5000K   | 10000K  |
| 128 (2x 64)  | 13 + 3 | 7 + 2  | 4 + 1  | 4 + 1   | 2 + 1   |
| ^ iops/drive | 770K   | 1429K  | 2500K  | 2500K   | 5000K   |
| 2x 128       | 26 + 6 | 13 + 3 | 8 + 2  | 7 + 2   | 4 + 1   |
| ^ iops/drive | 385K   | 770K   | 1250K  | 1429K   | 2500K   |

## Hardware Recommendations

Currently, the community is trying to determine the best hardware configurations for batch sealing. Some general observations are:

* Single socket systems will be easier to use at full capacity
* You want a lot of NVMe slots, on PCIe Gen4 platforms with large batch sizes you may use 20-24 3.84TB NVMe drives
* In general you'll want to make sure all memory channels are populated
* You need 4\~8 physical cores (not threads) for batch-wide compute, then on each CCX you'll lose 1 core for a "coordinator"
  * Each thread computes 2 sectors
  * On zen2 and earlier hashers compute only one sector per thread
  * Large (many-core) CCX-es are typically better

{% hint style="info" %}
Please consider contributing to the [batch sealing hardware examples](https://github.com/filecoin-project/curio/discussions/140).
{% endhint %}

## Setup

### Check NUMA setup:

```bash
numactl --hardware
```

You should expect to see `available: 1 nodes (0)`. If you see more than one node you need to go into your UEFI and set `NUMA Per Socket` (or a similar setting) to 1.

### Configure hugepages:

This can be done by adding the following to `/etc/default/grub`. You need 36 1G hugepages for the batch sealer.

```bash
GRUB_CMDLINE_LINUX_DEFAULT="hugepages=36 default_hugepagesz=1G hugepagesz=1G"
```

Then run `sudo update-grub` and reboot the machine.

Or at runtime:

```bash
sudo sysctl -w vm.nr_hugepages=36
```

Then check /proc/meminfo to verify the hugepages are available:

```bash
cat /proc/meminfo | grep Huge
```

Expect output like:

```
AnonHugePages:         0 kB
ShmemHugePages:        0 kB
FileHugePages:         0 kB
HugePages_Total:      36
HugePages_Free:       36
HugePages_Rsvd:        0
HugePages_Surp:        0
Hugepagesize:    1048576 kB
```

Check that `HugePages_Free` is equal to 36, the kernel can sometimes use some of the hugepages for other purposes.

### Dependencies

CUDA 12.x is required, 11.x won't work. The build process depends on GCC 13.x system-wide or `gcc-13`/`g++-13` installed locally.

* On Arch install GCC 13 via your distro/AUR as appropriate
* On Ubuntu/Debian install `gcc-13` and `g++-13` packages
    ```shell
    sudo apt install gcc-13 g++-13
    ```
* In addtion to general build dependencies (listed on the [installation page](installation.md)), you need `libgmp-dev` and `libconfig++-dev`
    ```shell
    sudo apt install libgmp-dev libconfig++-dev
    ```

{% hint style="info" %}
For SnapDeals “fastsnap” troubleshooting (fast TreeR path), you can check CPU/CUDA prerequisites with:

```bash
curio test supra system-info
```
{% endhint %}

### Building

Build and install the batch-capable Curio binary:

```bash
make curio
make sptool
```

```shell
make install
```

For calibnet

```bash
make calibnet
make calibnet-sptool
```

```shell
make install
```

{% hint style="warning" %}
The build should be run on the target machine. Binaries won't be portable between CPU generations due to different AVX512 support.
{% endhint %}

### Setup NVMe devices for SPDK:

{% hint style="success" %}
SPDK setup can be done automatically using the Curio CLI command:
{% endhint %}

```bash
sudo curio batch setup
```

This command will:
- Download SPDK if not already available
- Configure 1GB hugepages (36 pages by default)
- Bind NVMe devices for use with batch sealing

You can customize the number of hugepages:

```bash
sudo curio batch setup --hugepages 36 --min-pages 36
```

Alternatively, if you need to manually check SPDK status or unbind devices, you can use:

```bash
cd extern/supraseal/deps/spdk-v24.05/
# Check status
sudo ./scripts/setup.sh status
# Manually run setup (not normally needed)
sudo env NRHUGE=36 ./scripts/setup.sh
```

### Benchmark NVME IOPS

Please make sure to benchmark the raw NVME IOPS before proceeding with further configuration to verify that IOPS requirements are fulfilled.

```bash
cd extern/supraseal/deps/spdk-v24.05/

# repeat -b with all devices you plan to use with supraseal
# NOTE: You want to test with ALL devices so that you can see if there are any bottlenecks in the system
./build/examples/perf -b 0000:85:00.0 -b 0000:86:00.0...  -q 64 -o 4096 -w randread -t 10
```

The output should look like below

```
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
```

With ideally >10M IOPS total for all devices.

### PC2 output storage

Attach scratch space storage for PC2 output (batch sealer needs \~70GB per sector in batch - 32GiB for the sealed sector, and 36GiB for the cache directory with TreeC/TreeR and aux files)

## Usage

1. Start the Curio node with the batch sealer layer

```bash
curio run --layers batch-machine1
```

2. Add a batch of CC sectors:

```bash
curio seal start --now --cc --count 32 --actor f01234 --duration-days 365
```

3. Monitor progress - you should see a "Batch..." task running in the [Curio GUI](curio-gui.md)
4. PC1 will take 3.5-5 hours, followed by PC2 on GPU
5. After batch completion, the storage will be released for the next batch

## Configuration

* Run `curio calc batch-cpu` on the target machine to determine supported batch sizes for your CPU

<details>

<summary>Example batch-cpu output</summary>

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

* Create a new layer configuration for the batch sealer, e.g. batch-machine1:

```toml
[Subsystems]
EnableBatchSeal = true

[Seal]
# This field is optional. In most setups, NVMe devices can be inferred automatically if this configuration is omitted.
LayerNVMEDevices = [
  "0000:88:00.0",
  "0000:86:00.0", 
  # Add PCIe addresses for all NVMe devices to use
]

# Set to your desired batch size (what the batch-cpu command says your CPU supports AND what you have nvme space for)
BatchSealBatchSize = 32

# pipelines can be either 1 or 2; 2 pipelines double storage requirements but in correctly balanced systems makes
# layer hashing run 100% of the time, nearly doubling throughput
BatchSealPipelines = 2

# Set to true for Zen2 or older CPUs for compatibility
SingleHasherPerThread = false
```

### Environment Variables

| Variable | Description |
| -------- | ----------- |
| `DISABLE_SPDK_SETUP=1` | When set, disables automatic SPDK setup (hugepage configuration and NVMe device binding) during supraseal initialization. Useful for advanced users who want to manually manage SPDK configuration, map drives, or control hugepage-numa assignment. |

## Optimization

* Balance batch size, CPU cores, and NVMe drives to keep PC1 running constantly
* Ensure sufficient GPU capacity to complete PC2 before next PC1 batch finishes
* Monitor CPU, GPU and NVMe utilization to identify bottlenecks
* Monitor hasher core utilisation

## Troubleshooting

### Node doesn't start / isn't visible in the UI

* Ensure hugepages are configured correctly
* Check NVMe device IOPS and capacity
  * If spdk setup fails, try to `wipefs -a` the NVMe devices (this will wipe partitions from the devices, be careful!)

### Performance issues

You can monitor performance by looking at "hasher" core utilisation in e.g. `htop`.

To identify hasher cores, call `curio calc supraseal-config --batch-size 128` (with the correct batch size), and look for `coordinators`

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

In this example, cores 59, 64, 72, 80, and 88 are "coordinators", with two hashers per core, meaning that

* In first group core 59 is a coordinator, cores 60-63 are hashers (4 hasher cores / 8 hasher threads)
* In second group core 64 is a coordinator, cores 65-71 are hashers (7 hasher cores / 14 hasher threads)
* And so on

Coordinator cores will usually sit at 100% utilisation, hasher threads **SHOULD** sit at 100% utilisation, anything less indicates a bottleneck in the system, like not enough NVMe IOPS, not enough Memory bandwidth, or incorrect NUMA setup.

To troubleshoot:

* Read the requirements at the top of this page very carefully
* [Benchmark NVME IOPS](supraseal.md#benchmark-nvme-iops)
* Validate GPU setup if PC2 is slow
* Review logs for any errors during batch processing

### Slower than expeted NVMe speed

If the [NVME Benchmark](supraseal.md#benchmark-nvme-iops) shows lower than expected IOPS, you can try formatting the NVMe devices with SPDK:

```bash
cd extern/supraseal/deps/spdk-v24.05/
./build/examples/nvme_manage
```

Go through the menus like this

```
NVMe Management Options
	[1: list controllers]
	[2: create namespace]
	[3: delete namespace]
	[4: attach namespace to controller]
	[5: detach namespace from controller]
	[6: format namespace or controller]
	[7: firmware update]
	[8: opal]
	[9: quit]
6

0000:04:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829861           6 
0000:41:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829862           6 
0000:42:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829860           6 
0000:86:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829794           6 
0000:87:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829798           6 
0000:88:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829837           6 
0000:c1:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829795           6 
0000:c2:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829836           6 
0000:c3:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829797           6 
0000:c4:00.00 SAMSUNG MZQL23T8HCLS-00A07               S64HNG0W829850           6 
Please Input PCI Address(domain:bus:dev.func):
0000:c4:00.00
Please Input Namespace ID (1 - 32):
1                                                          ## Select 1

Please Input Secure Erase Setting:
	0: No secure erase operation requested
	1: User data erase
	2: Cryptographic erase
0

Supported LBA formats:
 0: 512 data bytes
 1: 4096 data bytes
Please input LBA format index (0 - 1):
1                                                          ## Select 4096 data bytes

Warning: use this utility at your own risk.
This command will format your namespace and all data will be lost.
This command may take several minutes to complete,
so do not interrupt the utility until it completes.
Press 'Y' to continue with the format operation.
y
```

Then you might see a difference in performance like this:

```
                                                                           Latency(us)
Device Information                     :       IOPS      MiB/s    Average        min        max
PCIE (0000:c1:00.0) NSID 1 from core  0:  721383.71    2817.91      88.68      11.20     591.51  ## before
PCIE (0000:86:00.0) NSID 1 from core  0: 1205271.62    4708.09      53.07      11.87     446.84  ## after
```


---

## Troubleshooting batch commit / all-or-nothing failures

Support frequently sees failures of the form:
- `all-or-nothing: Batch successes 67/68, Batch failing: [code=.. at idx=..] ...`

What it means:
- Some batch operations are **atomic**: a single failing entry can cause the whole batch to fail.

How to debug (operator workflow):
1) In the Curio UI, locate the failing **CommitBatch / PreCommitBatch** task and open its details.
2) Correlate the batch failure with the per-sector tasks immediately before it (often the underlying failure is earlier).
3) Check chain/message errors: many failures are actually wallet funding, fee, or chain state errors.
4) Collect:
   - `sp_id`
   - the task ID(s)
   - the full error message including the `idx=...`

How to recover safely:
- If you suspect one “bad” sector is poisoning the batch, temporarily reduce batching (batch size / concurrency) to isolate.
- Avoid manual DB edits unless you know exactly what you are doing (take a backup first).

If you’re stuck:
- follow `documentation/en/troubleshooting/collect-debug-info.md` and include the task IDs.
