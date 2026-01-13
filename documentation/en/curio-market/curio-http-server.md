---
description: >-
  This page provides an overview of the Curio HTTP Server's key features,
  including HTTPS support, security, middleware, routing capabilities, and
  instructions for attaching custom service routes.
---

# Curio HTTP Server

The Curio HTTP Server is a secure, flexible, and high-performance HTTP server designed for use with Curio cluster. It comes with built-in support for HTTPS using Let's Encrypt certificates, advanced middleware features like logging and compression, and the ability to integrate various Curio services, including IPNI, retrieval providers, and LibP2P.

## Key Features

### 1. **HTTPS with Let's Encrypt**

* Automatic certificate management using Let's Encrypt, ensuring that all traffic is encrypted.
* Supports automatic renewal of certificates and domain validation through the `autocert.Manager`.

### 2. **Security-First Design**

* **Strict Security Headers**: Adds essential security headers like Strict-Transport-Security, Content-Security-Policy, X-Frame-Options, and XSS protection to mitigate common vulnerabilities.
* Protects from clickjacking, content-type sniffing attacks, and more by enforcing best practices via HTTP headers.

### 3. **Flexible Routing with Chi**

* Uses the `chi` router for lightweight, flexible routing. Easily extend routes or handle custom paths by attaching new service modules.
* Out-of-the-box support for path handling and easy extension for future service needs.

### 4. **Built-in Middleware**

* **Compression**: Utilizes the `httpcompression` package to support GZIP, Brotli, and Deflate, optimizing bandwidth usage based on configurable compression levels.
* **Logging**: Logs every incoming request with details such as request method, path, and duration, aiding in easier debugging and monitoring.
* **CORS Support**: Conditional CORS support based on configuration for handling cross-origin requests securely.

### 5. **WebSocket and LibP2P Support**

* Specialized handling for WebSocket upgrade requests, with support for forwarding them to the `/libp2p` endpoint.
* Facilitates smooth communication between nodes and services in distributed Curio cluster running a single LibP2P.

### 6. **Health Check Endpoint**

* A built-in `/health` endpoint provides an easy way to check the status of the HTTP server, ensuring it is running smoothly.

## Performance and Scalability

* The server is optimized for handling large numbers of concurrent requests efficiently with appropriate timeouts (read, write, idle) to prevent overload.
* Integrated compression ensures minimal bandwidth usage, even for large data exchanges.

## Database Integration for TLS Certificate Cache

The server stores Let's Encrypt certificates and cache information in `Harmonydb`. This ensures persistence and fast access to TLS certificates in a Curio cluster with multiple HTTP servers.

### Database Operations

* **Get**: Retrieves TLS certificates from the `autocert_cache` table.
* **Put**: Inserts or updates certificates in the database.
* **Delete**: Removes expired or invalid certificates from the cache.

## Attaching Routes to Extend Server Functionality

Curio HTTP Server is designed to allow easy integration of various services by attaching custom routes. The `attachRouters` function enables the attachment of specific service routes to the Chi router. Examples include:

1. **Retrieval Provider**: Attaches routes to handle data retrieval using the `retrieval` module.
   * Creates a `RetrievalProvider` and registers the necessary HTTP endpoints.
2. **IPNI (Interplanetary Network Indexer)**: Integrates IPNI-specific routes.
   * The IPNI provider is instantiated, and routes are attached for IPNI services to handle data advertisement publishing.
3. **LibP2P Redirector**: Handles WebSocket connections for LibP2P communication.
   * Redirects WebSocket upgrade requests from `/` to `/libp2p` for seamless peer-to-peer communication.

Here’s how the routes are attached:

```go
attachRouters(ctx context.Context, r *chi.Mux, d *deps.Deps) (*chi.Mux, error) {
   // Attach retrievals
   rp := retrieval.NewRetrievalProvider(ctx, d.DB, d.IndexStore, d.CachedPieceReader)
   retrieval.Router(r, rp)

   // Attach IPNI
   ipp, err := ipni_provider.NewProvider(d)
   if err != nil {
      return nil, xerrors.Errorf("failed to create new IPNI provider: %w", err)
   }
   ipni_provider.Routes(r, ipp)

   go ipp.StartPublishing(ctx)

   // Attach LibP2P redirector
   rd := libp2p.NewRedirector(d.DB)
   libp2p.Router(r, rd)

   return r, nil
}
```

This flexibility allows the server to be easily extended with new services without modifying the core server logic.

## Configuration

The Curio HTTP Server can be customized using the `HTTPConfig` structure, which allows you to configure timeouts, compression levels, and security settings to suit your application. Below are the key configuration options along with their default values and explanations of their impact.

### **HTTPConfig**

* **Enable**: This boolean flag enables or disables the HTTP server. Default: `true`.
* **DomainName**: The domain name used by the server to handle HTTP requests. It must be a valid domain name and cannot be an IP address. A correct `DomainName` is required for Let's Encrypt to issue SSL certificates, enabling HTTPS.\
  Default: `""` (empty), meaning no domain is specified. This needs to be set for production environments using HTTPS.
* **ListenAddress**: Defines the IP address and port on which the server listens for incoming requests.\
  Default: `"0.0.0.0:12310"` — This binds the server to all available network interfaces on port 12310.
* **DelegateTLS: It** allows the server to delegate TLS to a reverse proxy. When enabled the listen address will serve HTTP and the reverse proxy will handle TLS termination.
* **ReadTimeout**: This sets the maximum duration to read a full request from the client, including the request body.\
  Default: `10 seconds` — A reasonable timeout for handling most requests. Reducing this value can prevent slow clients from hanging the server, while increasing it might help in cases where the request size is larger or the connection is slower.
* **WriteTimeout**: The maximum time allowed for writing the response to the client.\
  Default: `10 seconds` — Ensures that the server does not take too long to send responses, especially useful for API calls or small files. For large files or slower clients, you may need to increase this value.
* **IdleTimeout**: The duration after which an idle connection is closed if no activity is detected.\
  Default: `2 minutes` — Prevents resources from being consumed by idle connections. If your application expects longer periods of inactivity, such as in long polling or WebSocket connections, this value should be adjusted accordingly.
* **ReadHeaderTimeout**: The time allowed to read the request headers from the client.\
  Default: `5 seconds` — Prevents slow clients from keeping connections open without sending complete headers. For standard web traffic, this value is sufficient, but it may need adjustment for certain client environments.
* **CORSOrigins**: Specifies the allowed origins for CORS requests. If empty, CORS is disabled.\
  Default: `[]` (empty array) — This disables CORS by default for security. To enable CORS, specify the allowed origins (e.g., `["https://example.com", "https://app.example.com"]`). This is required for third-party UI servers.
* **CompressionLevels**: Defines the compression levels for GZIP, Brotli, and Deflate, which are used to optimize the response size. The defaults balance performance and bandwidth savings:
  * **GzipLevel**: Default: `6` — A moderate compression level that balances speed and compression ratio, suitable for general-purpose use.
  * **BrotliLevel**: Default: `4` — A moderate Brotli compression level, which provides better compression than GZIP but is more CPU-intensive. This level is good for text-heavy responses like HTML or JSON.
  * **DeflateLevel**: Default: `6` — Similar to GZIP in terms of performance and compression, this default level provides a good balance for most responses.

### **Impact of Compression Levels**

The compression levels directly affect server performance and bandwidth usage:

* Higher compression levels (e.g., `GzipLevel 9`, `BrotliLevel 11`) reduce the size of responses, which can save bandwidth but require more CPU processing time, especially for large responses.
* Lower levels (e.g., `GzipLevel 1`) are faster but provide less compression, meaning higher bandwidth usage but reduced server load.

For most applications, the default values of `6` for GZIP and Deflate, and `4` for Brotli provide a good trade-off between compression efficiency and CPU load, especially for responses that contain text or JSON.
