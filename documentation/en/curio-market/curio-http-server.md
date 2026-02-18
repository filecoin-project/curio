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

The list below is aligned with the actual `HTTPConfig` struct in `deps/config/types.go`.

* **Enable**: Enables/disables the HTTP server on this node.
* **DomainName**: DNS name used for requests and (when Curio terminates TLS) certificate issuance. Must be a real domain (not an IP).
  Default: `""`.
* **ListenAddress**: IP:port to bind.
  Default: `"0.0.0.0:12310"`.
* **DelegateTLS**: When `true`, Curio serves **plain HTTP** on `ListenAddress` and expects a reverse proxy to terminate TLS.
* **ReadTimeout**: Max time to read request body.
  Default: `10s`.
* **IdleTimeout**: Max keep-alive idle time.
  Default: `1h`.
* **ReadHeaderTimeout**: Max time to read headers.
  Default: `5s`.
* **CORSOrigins**: Allowed origins; empty disables CORS.
  Default: `[]`.
* **CSP**: Content Security Policy mode for `/piece/` content. Values: `off`, `self`, `inline`.
  Default: `inline`.
* **CompressionLevels**: Response compression tuning.
  Defaults: gzip=6, brotli=4, deflate=6.

### **Impact of Compression Levels**

The compression levels directly affect server performance and bandwidth usage:

* Higher compression levels (e.g., `GzipLevel 9`, `BrotliLevel 11`) reduce the size of responses, which can save bandwidth but require more CPU processing time, especially for large responses.
* Lower levels (e.g., `GzipLevel 1`) are faster but provide less compression, meaning higher bandwidth usage but reduced server load.

For most applications, the default values of `6` for GZIP and Deflate, and `4` for Brotli provide a good trade-off between compression efficiency and CPU load, especially for responses that contain text or JSON.


---

## HTTPS setup (Let’s Encrypt / autocert) — operational guide

Curio can terminate TLS itself (autocert / Let’s Encrypt) or you can terminate TLS in a reverse proxy.

### Prerequisites (both modes)

- You must use a real **domain name** (not an IP) in `HTTPConfig.DomainName`.
- DNS must point your domain to the host that will receive traffic.
- Inbound access must be allowed for:
  - **80/tcp** (ACME HTTP-01) and
  - **443/tcp** (HTTPS)

If ports 80/443 are blocked (cloud firewall, NAT, or corporate network), Let’s Encrypt cannot validate your domain.

### Mode A: Curio terminates TLS (DelegateTLS = false)

Use this when you want Curio to handle certificates directly.

Checklist:
- `DomainName` is set and resolvable publicly.
- Curio must be reachable from the internet on 80/443.

Operational notes:
- If your OS restricts binding to privileged ports, you may need one of:
  - run the Curio HTTP service with permissions to bind 80/443, or
  - terminate TLS in a reverse proxy (Mode B), or
  - forward 80/443 to Curio’s listen port via firewall/NAT.

### Mode B: Reverse proxy terminates TLS (DelegateTLS = true)

Use this when you already run Nginx/Caddy/Traefik or cannot (or do not want to) expose Curio directly.

In this mode:
- Curio serves **plain HTTP** internally.
- Your reverse proxy handles TLS + Let’s Encrypt.

Minimal Nginx example:

```nginx
server {
  listen 443 ssl;
  server_name example.com;

  # TLS config here (certbot/caddy/managed)

  location / {
    proxy_pass http://127.0.0.1:12310;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto https;
    proxy_set_header X-Forwarded-For $remote_addr;
  }
}
```

### Troubleshooting HTTPS

If `curl https://<domain>` fails:
- Confirm DNS A/AAAA is correct.
- Confirm ports 80/443 are reachable externally.
- Confirm `DomainName` matches the hostname you’re curling.
- Confirm you didn’t enable `DelegateTLS=true` without actually configuring a reverse proxy.
- Check Curio logs around certificate issuance / autocert.
