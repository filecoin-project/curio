<p align="center">
  <a href="https://lotus.filecoin.io/storage-providers/curio/overview/" title="Curio Docs">
    <img src="https://github.com/filecoin-project/curio/assets/63351350/a42a9baf-9091-4d3e-bb4b-088765ed8727" alt="Curio Logo" width="244" />
  </a>
</p>

<h1 align="center">Curio Storage - 储存</h1>

<p align="center">
  <a href="https://app.circleci.com/pipelines/github/filecoin-project/curio"><img src="https://circleci.com/gh/filecoin-project/curio.svg?style=svg" alt="CircleCI"></a>
  <a href="https://goreportcard.com/report/github.com/filecoin-project/curio"><img src="https://goreportcard.com/badge/github.com/filecoin-project/curio" /></a>
  <a href=""><img src="https://img.shields.io/badge/golang-%3E%3D1.22.3-blue.svg" /></a>
  <br>
</p>

## Documentation

For detailed documentation and usage instructions, please refer to the [official documentation](https://docs.curiostorage.org/).

## Overview

Curio Storage is an advanced platform designed to simplify the setup and operation of storage providers within the Filecoin ecosystem. Building on the foundation of `lotus-miner`, **Curio Storage** offers enhanced redundancy, scalability, and fault tolerance, ensuring efficient and reliable data storage solutions.

## Key Features

- **Redundancy**: Curio Storage employs multiple daemons, worker types, and database nodes to eliminate single points of failure and ensure maximum uptime.
- **Simplicity**: Consolidates all functions into a single binary, reducing the complexity of operations and minimizing the need for separate components.
- **Scalability**: Utilizes a greedy task management system for superior scaling capabilities, allowing operations to grow from 1 machine to thousands seamlessly.
- **Fault Tolerance**: Features robust mechanisms for handling disruptions, including remote failure recovery, automatic work balancing, and fallback-capable components.
- **Flexible Configuration**: Offers fine-grained configuration options to tailor the system to specific needs, enabling various subsystems like sealing and storage.
- **Multiple Miner IDs**: Efficiently manages multiple Miner IDs on the same hardware, optimizing resource utilization.
- **User Interface (GUI)**: Includes a comprehensive web-based dashboard for real-time monitoring and management of mining operations.

## Key Benefits for Storage Providers

Curio Storage is designed to deliver significant improvements and new features for storage providers, including:

- **High Availability / Zero-Downtime Capable**: Ensures continuous operation without downtime.
- **Efficient Resource Utilization**: Better use of machines by stacking work tightly, reducing waste.
- **Scalability**: No single point of stress, allowing for seamless expansion from a single machine to thousands.
- **Multi-Miner-ID Support**: Saves hardware resources by enabling multi-proving.
- **Rich Filtering**: Allows detailed filtering at storage locations by ID, purpose, etc.

## Future Developments

Curio Storage is continuously evolving, with future plans including:

- **New Snap Sectors & Unsealing**: Completing the feature set of lotus-miner.
- **Built-In Markets (like Boost) with Automation**: Enhancing market capabilities with automation.
- **Staking, Resource Sharing, & Deals 2.0**: Introducing advanced features for resource management.
- **Efficiency Gains**: Achieving significant cost savings for storage providers of all sizes.
- **Non-Interactive Proof of Replication (NI-PoRep)**: Unlocking the potential for non-sealing storage providers.
- **Two-Hour-Per-Month Admin Goal**: Streamlining operations to require minimal administrative effort.

## **General Availability**

**Curio Storage is now GA.** We encourage you to migrate your lotus-miner to Curio. For testing, you can deploy or migrate your miner on the calibrationnet. Your feedback is invaluable and will help us improve Curio.

## Getting Started

To get started with Curio Storage, follow these steps:

1. **Clone the Repository**:
    ```sh
    https://github.com/filecoin-project/curio
    cd curio
    ```

2. **Build the Project**:
    ```sh
    make clean all
    ```

3. **Run the Guided Setup**:
    ```sh
    curio guided-setup
    ```

## Community and Support

Join our community discussions and seek support via:

- **Slack**: [#fil-curio-help](https://filecoinproject.slack.com/archives/C06LF5YP8S3)
- **GitHub Issues**: [Submit an Issue](https://github.com/filecoin-project/curio/issues/new)

## License

Dual-licensed under [MIT](https://github.com/filecoin-project/curio/blob/master/LICENSE-MIT) + [Apache 2.0](https://github.com/filecoin-project/curio/blob/master/LICENSE-APACHE)

# Contributing

We welcome contributions from the community! Please see our [Contributing Guide](CONTRIBUTING.md) for more details.

---

**Curio Storage** is a testament to our commitment to enhancing the Filecoin ecosystem, providing a robust and user-friendly platform for all storage providers. Join us on this journey and contribute to the future of decentralized storage. 💙
