---
description: What is Curio and how is it different from Lotus-Miner?
---

# What is Curio?

## Overview

Curio is the new implementation of Filecoin storage protocol. It aims to simplify the setup and operation of storage providers.

{% hint style="danger" %}
Please note that Curio cluster cannot be shared across different networks.

Example: A single Curio cluster cannot host miners IDs coming from Mainnet and Calibnet together.
{% endhint %}

## Key Features&#x20;

#### High Availability&#x20;

Curio is designed for high availability. You can run multiple instances of Curio nodes to handle similar type of tasks. The distributed scheduler and greedy worker design will ensure that tasks are completed on time despite most partial outages. You can safely update one of your Curio machines without disrupting the operation of the others.

#### Node Heartbeat&#x20;

Each Curio node in a cluster must post a heartbeat message every 10 minutes in HarmonyDB updating its status. If a heartbeat is missed, the node is considered lost and all tasks can now be scheduled on remaining nodes.

#### Task Retry&#x20;

Each task in Curio has a limit on how many times it should be tried before being declared lost. This ensures that Curio does not keep retrying bad tasks indefinitely. This safeguards against lost computation time and storage.

#### Polling&#x20;

Curio avoids overloading nodes with a polling system. Nodes check for tasks they can handle, prioritizing idle nodes for even workload distribution.

#### Simple Configuration Management&#x20;

The configuration is stored in the database in the forms of layers. These layers can be stacked on top of each other create a final configuration. Users can reuse these layers to control the behaviour of multiple machines without needing to maintain the configuration of each node. Start the binary with the appropriate flags to connect with YugabyteDB and specify which configuration layers to use to get desired behaviour.

#### Running Curio with Multiple GPUs

Curio can handle multiple GPUs simultaneously without needing to run multiple instances of the Curio process. Therefore, Curio can be managed as a single systemd service without concerns about GPU allocations.

## Curio vs Lotus Miner&#x20;

| Feature                              | Curio                                                          | Lotus-Miner                                         |
| ------------------------------------ | -------------------------------------------------------------- | --------------------------------------------------- |
| Scheduling                           | Collaborative (Prioritized Greedy)                             | Single point of failure                             |
| High Availability                    | Available                                                      | Single control process                              |
| Redundant Post                       | Available                                                      | Not Available                                       |
| Task Retry Control                   | Task retry with a cutoff limit (per task)                      | Unlimited retry leading to resource exhaustion      |
| Multiple Miner IDs                   | Curio cluster can support multiple Miner IDs                   | Single Miner ID per Lotus-Miner                     |
| Shared Task nodes                    | Curio nodes can handle task for multiple Miner IDS             | Attached workers handle tasks for a single Miner ID |
| Distributed Configuration Management | Configuration stored in the highly-available Yugabyte Database | All configuration in a single File                  |

## Future of Curio&#x20;

The long-term vision for Curio is to eventually replace the current lotus-miner and lotus-worker processes. This is part of an ongoing effort to simplify and streamline the setup and operation of storage providers.

\
