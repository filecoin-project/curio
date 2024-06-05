---
description: How to update default listen address for Curio service
---

# Listen Address

By default, all Curio nodes bind to the address "0.0.0.0" on port "12300," ensuring that the Curio API listens on all interfaces. You can change this behavior by specifying an explicit IP address and a different port for Curio to use.

```bash
echo "CURIO_LISTEN=x.x.x.x:12301" >> /etc/curio.ENV
```

and restart the Curio service

```bash
systemctl restart curio.service
```
