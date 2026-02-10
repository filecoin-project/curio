---
description: >-
  This page provides a detailed overview of the core concepts and components
  that make up Curio, including HarmonyDB, HarmonyTask, and more.
---

# Design

## Curio Cluster

The core internal components of Curio are HarmonyDB, HarmonyTask, ChainScheduler and a database abstraction of configuration & today’s storage definitions.

<figure><img src="../.gitbook/assets/curio-node (1).png" alt="Curio Node"><figcaption><p>Curio nodes</p></figcaption></figure>

A Curio cluster is a cluster of multiple Curio nodes connected to a YugabyteDB cluster and market nodes. A single Curio cluster can serve multiple miner ID and share the computation resources between them as required.

<figure><img src="../.gitbook/assets/curio-cluster (1).png" alt="Curio cluster"><figcaption><p>Curio cluster</p></figcaption></figure>

## HarmonyDB&#x20;

HarmonyDB is a simple SQL database abstraction layer used by HarmonyTask and other components of the Curio stack to store and retrieve information from YugabyteDB.

### Key Features:&#x20;

* **Resilience:** Automatically fails over to secondary databases if the primary connection fails.
* **Security:** Protects against SQL injection vulnerabilities.
* **Convenience:** Offers helper functions for common Go + SQL operations.
* **Monitoring:** Provides insights into database behavior through Prometheus stats and error logging. See [Prometheus Metrics](../configuration/prometheus-metrics.md) for setup and available metrics.

### Basic Database Details&#x20;

* The Postgres DB schema is called “curio” and all the harmony DB tables reside under this schema.
* Table `harmony_task` stores a list of pending tasks.
* Table `harmony_task_history` stores completed tasks, retried tasks exceeding limits, and serves as input for triggering follower tasks (potentially on different machines).
* Table `harmony_task_machines` is managed by lib/harmony/resources. This table references registered machines for task distribution. Registration does not imply obligation, but facilitates discovery.

## HarmonyTask&#x20;

The HarmonyTask is pure (no task logic) distributed task manager.

### Design Overview&#x20;

* Task-Centric: HarmonyTask focuses on managing tasks as small units of work, relieving developers from scheduling and management concerns.
* Distributed: Tasks are distributed across machines for efficient execution.
* Greedy Workers: Workers actively claim tasks they can handle.
* Round Robin Assignment: After a Curio node claims a task, HarmonyDB attempts to distribute remaining work among other machines.

<figure><img src="../.gitbook/assets/curio-tasks (1).png" alt="Curio Tasks"><figcaption><p>Harmony tasks</p></figcaption></figure>

### Model&#x20;

* **Blocked Tasks:** Tasks can be blocked due to:
  * Configuration under ‘subsystems’ disabled on the running node
  * Reaching specified maximum task limits
  * Resource exhaustion
  * CanAccept() function (task-specific) rejecting the task
* **Task Initiation:** Tasks can be initiated through:
  * Periodic database reads (every 3 seconds)
  * Addition to the database by the current process
* **Task Addition Methods:**
  * Asynchronous listener tasks (e.g., for blockchains)
  * Follower tasks triggered by task completion (sealing pipeline)
* **Duplicate Task Prevention:**
  * The mechanism for avoiding duplicate tasks is left to the task definition, most likely using a unique key.

## Distributed Scheduling&#x20;

Curio implements a distributed scheduling mechanism co-ordinated via the HarmonyDB. The tasks are picked by the Curio nodes based on what they can handle (type and resources). Nodes are not greedy after taking a task, even if they have enough resources. Other nodes get a turn to claim a task. On 3 second intervals, if resources are available then an additional task will be taken. This ensures a more even scheduling of the tasks.

## Chain Scheduler&#x20;

The `CurioChainSched` or the chain scheduler trigger some call back functions when a new TipSet is applied or removed. This is equivalent to getting the heaviest TipSet on each epoch. These callback function in turn add the new tasks for each type that depends on the changes in the chain. These task types are WindowPost, WinningPost and MessageWatcher.

## Poller&#x20;

Poller is a simple loop that fetches pending tasks periodically according to predefined durations (100ms), or until a graceful exit is initiated by the context. Once the pending tasks are fetched from the database, it attempts to schedule all the tasks on the Curio node. This attempt will result in one of the following results:

* Task is accepted
* Task is not scheduled as machine is busy
* Task is not accepted as the node’s CanAccept (defined by the task) elects not to handle the specified Task

If the task is accepted during a polling cycle, the wait time before the next cycle is equal to 100ms. But if the task is not scheduled for any reason the poller will retry after 3 seconds.

## Task Decision Logic&#x20;

For each task type a machine can handle, it first checks if the machine has enough capacity to execute the said task. Then it queries the database for tasks with no `owner_id` and the same name as the task type. If such tasks are present, it attempts to accept their work. It returns true if any work has been accepted and false otherwise. The decision-making logic to accept each task is following:

1. Checks if there are any tasks to do. If there are none, returns true.
2. Checks if the maximum number of tasks of this type is reached. If the number of running tasks meets or exceeds the maximum limit, log a message and returns false.
3. Checks if the machine has enough resources to handle the task. This includes checking the CPU, RAM, GPU capacity, and available storage. If the machine does not have enough resources, log a message and return false.
4. Checks if the task can be accepted by calling the `CanAccept` method. If it cannot be accepted, log a message and return false.
5. If the task requires storage space, the machine attempts to claim it. If the claim fails, log a message and releases the claimed storage space, then return false.
6. If the task source is `recover` i.e. the machine was performing this task before shutdown then increase the task count by one and begin processing the task in a separate goroutine.
7. If the task source is `poller` i.e. new pending task, attempt to claim the task for the current hostname. If unsuccessful, release the claimed storage and attempt to consider the next task.
8. If successful, increase the task count by one and begin processing the task in a separate goroutine.
9. This goroutine also updates the task status in the task history and depending on whether the task was successful or not, either deletes the task or updates the task in the tasks table.
10. Returns true, indicating that the work was accepted and will be processed.

## GPU Management in Curio

### **Historical Issues with Lotus-Miner Scheduler**

Historically, the Lotus-Miner scheduler has encountered difficulties efficiently utilizing GPUs when more than one GPU is available to the lotus-worker process. These issues primarily arise from the underlying proofs library, which handles all GPU-related tasks and manages GPU assignments. This has led to problems such as:

* A single task being assigned to multiple GPUs.
* Multiple tasks being assigned to a single GPU.

### **Solution with Curio: The GPU Picker Library "ffiselect"**

To address these issues in Curio, we have implemented a GPU picker library called "ffiselect". This library ensures that each task requiring a GPU is assigned one individually. The process works as follows:

1. **Task Assignment**: Each GPU-requiring task is assigned a specific GPU.
2. **Subprocess Creation**: A new subprocess is spawned for each task, with the dedicated GPU allocated to it.
3. **Proofs Library Call**: The subprocess calls the Proofs library with a single GPU and the specific task.

<figure><img src="../.gitbook/assets/2024-06-04-040735_1470x522_scrot (1).png" alt=""><figcaption><p>Curio FFISelect in action</p></figcaption></figure>

This approach ensures efficient and conflict-free GPU usage, with each task being handled by a dedicated GPU, thus resolving the historical issues observed with the `lotus-miner` scheduler.

# Security Boundary 

This is what Curio expects an SP to secure in order to have a safe experience. 
Curio is cluster software which coordinates directly and through the database. It also communicates to the public through chain providers (Lotus) and the market node. To secure this properly, ensure that only trusted people & services have access to:
- logs: (these include inputs to failing processes)
- physical machines, 
- virtual machine access (ssh) for Curio, Lotus, or Yugabyte
- Curio or Lotus' or Yugabyte's open ports (with exceptions noted by Lotus, and the Curio market node)
 -- This includes the admin web ui for Curio which exposes numerous capabilities beyond viewing.

Safe to share with untrusted parties: (will not receive private information)
- Prometheus output 
- alerts can be sent to untrusted receivers
- CuView (at your own risk) has modes for light investigation. 

Curio team recommends a network (VPN) containing all the pieces to have limited access. 
Logs are mostly clean except for errors which try to be as specific as possible, so partial redaction may be best here if sharing with untrusted parties. 