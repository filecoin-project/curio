---
description: 本页面解释了如何为 Curio 设置 systemd 服务

---

# Curio Service

Curio 服务

Curio 可以同时处理多个 GPU，而无需运行多个 Curio 进程实例。因此，Curio 可以作为单个 systemd 服务进行管理，而无需担心 GPU 分配问题。
Curio 可以同时处理多个 GPU，而无需运行多个 Curio 进程实例。因此，Curio 可以作为单个 systemd 服务进行管理，而无需担心 GPU 分配问题。

## Systemd Service Configuration

## Systemd 服务配置

Curio 的服务文件包含在 Debian 包中，名为 `curio.service`。如果您是从源代码构建 Curio 的，可以按照下面的描述手动创建服务文件。

### **Service File for Curio**

### **Curio 的服务文件**

要手动创建 `curio.service` 文件，请使用以下内容：

```yaml
[Unit]
Description=Curio
After=network.target

[Service]
ExecStart=/usr/local/bin/curio run
Environment=GOLOG_FILE="/var/log/curio/curio.log"
Environment=GOLOG_LOG_FMT="json"
LimitNOFILE=1000000
Restart=always
RestartSec=10
EnvironmentFile=/etc/curio.env

[Install]
WantedBy=multi-user.target

```
### Environment Variables Configuration

### 环境变量配置

服务文件需要存在一个 `/etc/curio.env` 文件。该文件包含连接数据库所需的所有环境变量。`env` 文件应在 Debian 包安装期间自动创建。如果您运行的是从源代码构建的 Curio，可以使用以下内容手动创建 `env` 文件：

#### /etc/curio.env 文件
```bash
CURIO_LAYERS=gui,post
CURIO_ALL_REMAINING_FIELDS_ARE_OPTIONAL=true
CURIO_DB_HOST=yugabyte1,yugabyte2,yugabyte3
CURIO_DB_USER=yugabyte
CURIO_DB_PASSWORD=yugabyte
CURIO_DB_PORT=5433
CURIO_DB_NAME=yugabyte
CURIO_REPO_PATH=~/.curio
CURIO_NODE_NAME=ChangeMe
FIL_PROOFS_USE_MULTICORE_SDR=1
```
确保所有变量根据您的环境正确设置。

## Starting the Curio Service

## 启动 Curio 服务

一旦所有变量都正确更新，创建日志目录：

```bash
mkdir -p /var/log/curio
```

现在，您可以使用以下命令启动 systemd 服务：

```bash
sudo systemctl start curio.service
```

通过监控 `systemctl status curio.service` 验证进程是否成功启动

一旦 Curio 服务运行，您可以继续 [为 Curio 节点附加存储以进行封装或永久存储](storage-configuration.md) 或 [在集群中设置下一个 Curio 节点](scaling-curio-cluster.md)。
