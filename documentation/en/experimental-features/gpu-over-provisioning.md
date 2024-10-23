---
description: >-
  This page explains how to allow Curio to run more than multiple GPU tasks on 
  a single GPU at the same time
---

# GPU Over Provisioning

## Overview

The `HARMONY_GPU_OVERPROVISION_FACTOR` environment variable enables GPU over-provisioning by allowing each physical GPU to present itself as multiple logical GPUs. When set to a value greater than 1, this feature allows a single GPU to handle multiple independent processes concurrently.

## Usage

### Enabling Over provisioning

Set the `HARMONY_GPU_OVERPROVISION_FACTOR` environment variable to the desired over-provisioning factor.

#### **Example**

```bash
export HARMONY_GPU_OVERPROVISION_FACTOR=2
```

* **Effect**: Each physical GPU is treated as two logical GPUs.
* **Application**: In a snap encode worker, this setting allows each GPU to handle two independent encode processes simultaneously.

#### Example with Service File

**/etc/curio.env File**

```sh
CURIO_LAYERS=gui,post
CURIO_ALL_REMAINING_FIELDS_ARE_OPTIONAL=true
CURIO_DB_HOST=yugabyte1,yugabyte2,yugabyte3
CURIO_DB_USER=yugabyte
CURIO_DB_PASSWORD=yugabyte
CURIO_DB_PORT=5433
CURIO_DB_NAME=yugabyte
CURIO_REPO_PATH=~/.curio
CURIO_NODE_NAME=ChangeMe
FIL_PROOFS_USE_MULTICORE_SDR=1
HARMONY_GPU_OVERPROVISION_FACTOR=2
```

## Considerations

* **Workload Compatibility**: Ideal for workloads that are not heavily memory-bound.
  * **Snap Encode Workloads**: Generally suitable for over-provisioning.
  * **SNARK Workloads**: May encounter memory limitations, especially on GPUs with lower memory capacity.
* **GPU Specifications**: Enterprise GPUs with higher memory are better suited for over-provisioning.
* **Performance Testing**: It's important to test and validate the optimal over-provisioning factor for your specific hardware and workloads.

### Benefits

* **Increased Throughput**: Potentially improves processing capacity per GPU.
* **Enhanced Utilization**: Makes better use of GPU resources that might otherwise be underutilized.

### Limitations

* **Memory Constraints**: Over-provisioning can lead to memory bottlenecks on GPUs with limited memory.
* **Potential Instability**: Running multiple processes on a single GPU may affect system stability and performance.

### Recommendations

* **Start with Lower Values**: Begin with an over-provisioning factor of 2 and monitor system performance.
* **Monitor Resource Usage**: Keep an eye on GPU memory usage, temperatures, and overall system load.
* **Increment Gradually**: Adjust the over-provisioning factor incrementally to find the optimal balance.

### Feedback and Support

As this is an experimental feature, we encourage users to provide feedback on their experience. Your insights are valuable for improving GPU over-provisioning support in future releases.
