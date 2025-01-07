---
description: This page explains Curio's HTTP-based content retrieval system.
---

# Retrievals

## Overview

The Curio HTTP server serves as the primary interface for handling content retrieval requests. In a Curio cluster, multiple nodes within the Curio cluster can host HTTP servers. Users can retrieve data using any of the available URLs, allowing for redundancy and ensuring high availability.

### Key Components:

* **Piece-Based Retrievals**: Data can be fetched by a piece CID, enabling users to retrieve content directly from specific pieces stored within the Curio ecosystem.
* **IPFS-Based Retrievals**: The Curio HTTP server also integrates with IPFS, allowing users to request content using an IPFS CID via the `/ipfs` route.
* **Caching and Immutable Data**: Retrieved content is served with caching headers, ensuring immutability and efficiency for repeat requests.

## Piece-Based Retrievals

The retrieval server allows users to fetch content by providing a piece CID through the `/piece/{cid}` route. Upon receiving such a request, the server looks up the requested piece and serves the content directly from the provider's unsealed sector.

Any errors encountered during retrieval, such as the requested piece not being found, result in appropriate HTTP error responses (404, 500, etc.).

### IPFS Gateway Integration

In addition to supporting piece-based retrievals, the Curio HTTP server also integrates IPFS gateway functionality. The `/ipfs/{cid}` route allows users to request content by CID. This is handled by the `frisbii` server backed by a `blockstore`, which fetches and returns the content corresponding to the requested IPFS CID.

### Caching and Headers

Content served by the Curio retrieval server is immutable. For this reason, the server sets caching headers to allow clients and CDNs to cache content for extended periods. Specifically:

* **ETag**: A unique identifier based on the piece CID ensures that cached content can be validated efficiently.
* **Cache-Control**: Responses are marked as immutable and cacheable for up to one year, reducing the load on the retrieval server for repeat requests.

By leveraging these caching mechanisms, the Curio HTTP server ensures that frequently accessed content is delivered quickly and with minimal resource consumption.
