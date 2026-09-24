---
description: >-
  Optional nginx reverse-proxy example that offloads TLS from Curio, keeps Curio
  hosts private, and adds a place to filter traffic. Curio already serves HTTPS
  via Let's Encrypt when HTTP.DomainName is set.
---

# Optional TLS offloading and filtering

Curio's HTTP server already terminates TLS. When you set `HTTP.DomainName` to a real public hostname and leave `DelegateTLS = false`, Curio obtains and renews a Let's Encrypt certificate and serves HTTPS on the market HTTP endpoint (including PDP routes on that same server). You do **not** need a reverse proxy to get HTTPS.

This page is an **optional reverse-proxy example**. Put nginx (or another proxy) in front of Curio when you want to:

* **Offload TLS** — certificates and HTTPS live on the proxy, not on every Curio node.
* **Keep Curio boxes otherwise private** — only the proxy is on the public internet; Curio listens on the LAN and is not reachable from outside.
* **Increase configurability, including filtering** — IP allow/deny lists, rate limits, path rules, and similar controls belong on the proxy, where they are easier to change than in Curio itself.

Set `DelegateTLS = true` in this mode so Curio serves plain HTTP on `ListenAddress` and does not try to issue its own certificate.

The nginx config below is **deliberately minimal**: large-file/streaming settings plus forwarding headers only. Tuning directives such as timeouts and `ssl_session_cache` are omitted on purpose; add them if you need them. Filtering is also add-on-demand — see [Filtering](#filtering-add-on-demand).

This walkthrough uses **Ubuntu 22.04**, Certbot, and the placeholder domains `pdp.example.com` and `pdp2.example.com`. Replace hostnames, internal IPs, and paths with your own.

{% hint style="warning" %}
**This setup is specific to one example environment.** You must adjust hostnames, internal IP addresses, and paths to match your own system and network configuration.
{% endhint %}

{% hint style="info" %}
**Before making any configuration changes,** back up your existing nginx configuration and validate every change with `sudo nginx -t` before reloading. This catches syntax errors before they take down the service.
{% endhint %}

For the two TLS modes (`DelegateTLS` false vs true), see [Curio HTTP Server](curio-http-server.md).

***

## 🗺️ Overview

This setup uses nginx as a reverse proxy with TLS termination:

* Nginx is public-facing on ports 80 and 443 and manages certificates.
* Backend Curio services listen on internal IPs only, serving HTTP (`DelegateTLS = true`).
* Communication between nginx and Curio is unencrypted HTTP over a trusted LAN. Do not expose that hop to the internet.

***

## 🚀 Prerequisites

* Root or sudo access.
* Domain name(s) pointing to the reverse-proxy server's public IP (not to the Curio node).
* Curio HTTP server running on the internal network. For PDP, see [Enable PDP](../experimental-features/Enable-PDP.md).
* Ports `80` and `443` open on the **proxy** firewall. Curio itself should not need public 80/443.

***

## 1️⃣ Install Nginx

```sh
sudo apt update
sudo apt install nginx
```

Verify the installation:

```sh
nginx -v
```

Start and enable nginx:

```sh
sudo systemctl start nginx
sudo systemctl enable nginx
```

***

## 2️⃣ Install Certbot for SSL Certificates

```sh
sudo apt install certbot python3-certbot-nginx
```

Verify the installation:

```sh
certbot --version
```

***

## 3️⃣ Configure the Virtual Host

Create a configuration file for your domain. Replace `pdp.example.com` with your domain and `192.168.1.160` with your Curio service IP.

Create the file:

```sh
sudo nano /etc/nginx/sites-available/pdp.example.com
```

Add this initial configuration (before SSL setup):

```nginx
server {
    listen 80;
    server_name pdp.example.com;

    location / {
        return 200 "Server is ready for certbot";
    }
}
```

Enable the site:

```sh
sudo ln -s /etc/nginx/sites-available/pdp.example.com /etc/nginx/sites-enabled/
```

Test the configuration and reload nginx:

```sh
sudo nginx -t
sudo systemctl reload nginx
```

***

## 4️⃣ Obtain an SSL Certificate

Run Certbot with the nginx plugin:

```sh
sudo certbot --nginx -d pdp.example.com
```

Follow the prompts:

* Enter your email address.
* Agree to the terms of service.
* Choose whether to redirect HTTP to HTTPS (recommended: **yes**).

Certbot will automatically:

* Obtain the certificate from Let's Encrypt.
* Configure nginx to use the certificate.
* Set up automatic renewal.

***

## 5️⃣ Configure the Reverse Proxy

After Certbot completes, edit your site configuration:

```sh
sudo nano /etc/nginx/sites-available/pdp.example.com
```

Replace the contents with the following. Substitute `YOUR_DOMAIN` and `YOUR_CURIO_IP` with your own values. `YOUR_CURIO_IP` must be a **private** address; Curio's `ListenAddress` should bind there, not on a public interface.

```nginx
# HTTP server - redirect to HTTPS
server {
    listen 80;
    server_name YOUR_DOMAIN;

    # Let's Encrypt challenge location
    location /.well-known/acme-challenge/ {
        root /var/www/html;
    }

    # Redirect everything else to HTTPS
    location / {
        return 301 https://$server_name$request_uri;
    }
}

# HTTPS server - proxy to Curio
server {
    listen 443 ssl;
    server_name YOUR_DOMAIN;

    # Let's Encrypt certificates
    ssl_certificate /etc/letsencrypt/live/YOUR_DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/YOUR_DOMAIN/privkey.pem;
    include /etc/letsencrypt/options-ssl-nginx.conf;
    ssl_dhparam /etc/letsencrypt/ssl-dhparams.pem;

    # Logging
    access_log /var/log/nginx/YOUR_DOMAIN.access.log;
    error_log /var/log/nginx/YOUR_DOMAIN.error.log;

    # Large file upload/download settings
    client_max_body_size 0;
    proxy_request_buffering off;
    proxy_buffering off;
    gzip off;

    # Proxy everything to Curio (HTTP with DelegateTLS)
    location / {
        proxy_pass http://YOUR_CURIO_IP:443;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

{% hint style="info" %}
This example is **deliberately minimal**: unlimited body size, unbuffered streaming, gzip off, and forwarding headers. It does **not** set proxy timeouts, `ssl_session_cache`, worker tuning, or request filters. Add those only if you need them.
{% endhint %}

Test and reload:

```sh
sudo nginx -t
sudo systemctl reload nginx
```

### ⚙️ Configuration breakdown

**HTTP server block (port 80)**

* `location /.well-known/acme-challenge/`: allows Certbot to renew certificates.
* `location /`: redirects all other HTTP traffic to HTTPS.

**HTTPS server block (port 443)**

* `listen 443 ssl`: listen on the HTTPS port with SSL.
* `ssl_certificate` / `ssl_certificate_key`: paths to your SSL certificate and private key.
* `include /etc/letsencrypt/options-ssl-nginx.conf`: Certbot's SSL settings.
* `ssl_dhparam`: Diffie-Hellman parameters for security.

**Large file settings (needed for piece/PDP uploads)**

* `client_max_body_size 0`: no limit on upload size.
* `proxy_request_buffering off`: stream uploads directly to the backend (reduces memory usage).
* `proxy_buffering off`: stream downloads directly to the client (reduces memory usage).
* `gzip off`: don't compress binary data (saves CPU; piece data doesn't compress).

**Proxy settings**

* `proxy_pass http://YOUR_CURIO_IP:443`: forward to Curio over plain HTTP on the LAN. With `DelegateTLS = true`, Curio does not terminate TLS. Keep this hop private. If Curio's `ListenAddress` is not port 443, change the port in `proxy_pass` to match.
* `proxy_set_header` directives: preserve client information (original host, real IP, forwarded chain, and scheme). Curio honours `X-Forwarded-For` for rate limiting when the proxy connects over loopback; forwarding headers from non-loopback peers are ignored. See [Curio HTTP Server](curio-http-server.md).

***

## Filtering (add on demand)

The example above proxies all paths. The reason to run a reverse proxy — beyond TLS offload — is that you can add filtering here without changing Curio.

Examples you might add later (not included in the minimal config):

* IP allow/deny lists (`allow` / `deny`) so only known clients, partners, or your LAN reach certain paths.
* Rate limiting (`limit_req`) in front of expensive upload or API routes.
* Path rules that expose `/piece` and PDP routes publicly while restricting other paths.

Add filters only for the traffic you actually need to constrain. Over-filtering can break Let's Encrypt renewal (`/.well-known/acme-challenge/`) and client uploads.

***

## 6️⃣ Configure Curio for DelegateTLS

On your Curio machine (for example, `192.168.1.160`), set **DelegateTLS** in the **HTTP** section of the layer that enables the HTTP server. Curio then serves HTTP on `ListenAddress` and expects the reverse proxy to terminate TLS.

Bind `ListenAddress` to an internal address. Do not publish Curio's HTTP port on the public internet in this mode.

```toml
DelegateTLS = true
```

{% hint style="warning" %}
Restart Curio after making configuration changes.
{% endhint %}

***

## 7️⃣ Test Your Setup

Test the HTTPS connection:

```sh
curl -I https://YOUR_DOMAIN
```

You should see a response from your Curio service through nginx.

Check the SSL certificate:

```sh
openssl s_client -connect YOUR_DOMAIN:443 -servername YOUR_DOMAIN
```

Confirm that Curio is **not** reachable from the public internet on its `ListenAddress` (the proxy should be the only public HTTPS entry point).

***

## 📊 Monitoring and Logs

View nginx access logs:

```sh
sudo tail -f /var/log/nginx/YOUR_DOMAIN.access.log
```

View nginx error logs:

```sh
sudo tail -f /var/log/nginx/YOUR_DOMAIN.error.log
```

Check nginx status:

```sh
sudo systemctl status nginx
```

***

## 🛠️ Troubleshooting

**Certificate renewal fails**

* Ensure port 80 is accessible from the internet **on the proxy**.
* Check that the `/.well-known/acme-challenge/` location is configured in the HTTP block.
* Verify the `/var/www/html` directory exists.

**Proxy connection fails**

* Verify the Curio service is running on the internal IP.
* Check that Curio is configured with `DelegateTLS = true`.
* Ensure Curio is listening on the port in `proxy_pass`.
* Verify network connectivity between the nginx and Curio machines.

**502 Bad Gateway**

* The Curio service is down or not responding.
* Wrong IP address or port in the `proxy_pass` directive.
* Curio not configured for DelegateTLS mode.

**Configuration test fails**

```sh
sudo nginx -t
```

This will show specific syntax errors in your configuration.

***

## 🎉 Summary

* Curio already provides HTTPS on the market HTTP endpoint via Let's Encrypt when `DomainName` is set. This reverse proxy is optional.
* Use it to offload TLS, keep Curio hosts private, and add filtering or other proxy policy.
* The nginx example is minimal (streaming + forwarding headers). Timeouts, `ssl_session_cache`, and filters are add-on-demand.
* `DelegateTLS = true` makes Curio serve HTTP on the LAN while nginx is the public TLS terminator.
* Multiple domains are supported (one per Curio HTTP instance, each with its own server block).
