---
description: >-
  This page outlines the main features of the Libp2p server, including its
  configuration and components.
---

# Curio libp2p Server

## Overview

The Curio Libp2p server facilitates network communication between nodes and clients using the Libp2p protocol. It primarily handles deal proposals, deal status updates, and miner information in the Filecoin network. This server is tightly integrated with Curio's HTTP server and ensures the network operates smoothly for Filecoin deal processing.

### Key Components:

* **Host Setup**: The libp2p host is initialized with necessary configuration details, including listening addresses, identity keys, and a peerstore for managing peer connections.
* **Deal Handling**: The libp2p server manages deal proposals and responses using the MK12 market protocol. It processes incoming streams for deal proposals, status checks, and query requests.
* **WebSocket Proxy**: WebSocket connections from clients are proxied through the Curio HTTP server at the `/libp2p` path, directing traffic to the libp2p node's listening address for secure P2P communication.
* **Miner Information Updates**: The libp2p provider also ensures that miner information, such as peer IDs and multi-addresses, is kept up-to-date on-chain, allowing for smooth communication during deal negotiations.

## libp2p Host

A **Libp2p host** is initialized with randomized ports for listening to incoming connections. Unlike other networks where the listen address might be broadcasted, in Curio's implementation, the Libp2p node listens on a random port and does not advertise its address to other peers.

The setup involves creating a peer identity, configuring listen addresses, and ensuring that the node is ready to handle connections and requests. Once the node is operational, its local listen address is stored in the database, which the WebSocket proxy uses for connecting clients.

## Network Deal Management

Curio libp2p server plays a critical role in handling incoming deal proposals over the network. Deals are initiated using the `DealProtocolv120ID` and `DealProtocolv121ID` for handling storage market deals, and the `DealStatusV12ProtocolID` for tracking deal statuses.

The deal provider continuously listens for deal requests, processes them, and sends responses back to the clients.

### Deal Proposal Handling

When a deal proposal is received, the libp2p handler send the request to MK12(INSERT LINK HERE) provider to validates the request, checks miner permissions, and processes the deal. If the deal is valid, MK12 provider moves forward with execution, handling the storage and sealing process on the storage provider's side.

### Deal Status Tracking

The libp2p server supports querying the status of a deal using the `DealStatusV12ProtocolID`. This allows clients to monitor the current state of their proposals, such as whether the deal is sealed, indexed, or still in progress.

## WebSocket Proxy

The Curio libp2p server leverages the HTTP server's `/libp2p` path to proxy WebSocket connections between the client and the libp2p node. This mechanism ensures secure and direct P2P communication through WebSocket channels. By connecting via the WebSocket proxy, peers can securely exchange deal information, status updates, and more.

This proxy system enables seamless data flow between Curio nodes and external clients or peers, facilitating decentralized communication across the network without exposing local node details directly to clients.

### Failover and Node Coordination

To ensure proper coordination between multiple nodes, the libp2p server periodically updates the database with the node's status, such as the `running_on` field, and performs keep-alive checks. If the node fails to update its status in time, another node may take over, ensuring that there is no interruption in the decentralized network.
