---
description: 如何更新 Curio 服务的默认监听地址
---

# Listen Address
# 监听地址

默认情况下，所有 Curio 节点都绑定到地址 "0.0.0.0" 和端口 "12300"，确保 Curio API 在所有接口上监听。您可以通过指定一个明确的 IP 地址和不同的端口来改变这种行为。

```bash
echo "CURIO_LISTEN=x.x.x.x:12301" >> /etc/curio.env
```


然后重启 Curio 服务

```bash
systemctl restart curio.service
```

