---
description: This page explains how to setup a systemd service for Curio
---

# Curio Service

Curio can handle multiple GPUs simultaneously without needing to run multiple instances of the Curio process. Therefore, Curio can be managed as a single systemd service without concerns about GPU allocations.

## Systemd Service Configuration

The service file for Curio is included in the Debian package and is named `curio.service`. If you have built Curio from source, you can create the service file manually as described below.

### **Service File for Curio**

To create the `curio.service` file manually, use the following content:

```ini
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

The service file requires an `/etc/curio.env` file to be present. This file contains all the necessary environment variables to connect to the database. The `env` file should be created automatically during the Debian package installation. If you are running Curio built from source, you can create the `env` file manually with the following content:

**/etc/curio.env File**

```sh
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

Ensure all variables are correctly set according to your environment. Additionally you can also export the following variable for cache location.

```sh
FIL_PROOFS_PARAMETER_CACHE=/path/to/folder/in/fast/disk
FIL_PROOFS_PARENT_CACHE=/path/to/folder/in/fast/disk2
```

## Starting the Curio Service

Once all the variables are correctly updated, create the log directory:

```sh
mkdir -p /var/log/curio
```

Now, you can start the systemd service with the following command:

```sh
sudo systemctl start curio.service
```

Verify that process started successfully by monitoring `systemctl status curio.service`

Once Curio service is running, you can proceed to [attaching storage for sealing or permanent storage to the Curio node](storage-configuration.md) or [setting up next Curio node in the cluster](scaling-curio-cluster.md).
