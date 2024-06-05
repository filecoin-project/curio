---
description: This guide describes how to update logging preferences in Curio.
---

# Logging

## Log file configuration&#x20;

Each Curio node generates Go logs which are directed to `/var/log/curio/curio.log` file by default if you are running Curio as a systemd service.

### Redirect Go logs to a file&#x20;

By default, Curio redirect all logs to the standard output if not running as a systemd service. To change this behaviour, add the following variable to the `.bashrc` file and restart the `curio` process to start redirecting all logs to the file.

```shell
export GOLOG_OUTPUT=FILE >> ~/.bashrc
export GOLOG_FILE="$HOME/curio.log" >> ~/.bashrc && source ~/.bashrc
```

### Redirect Rust logs to a standard output&#x20;

By default the `fil_logger` library used by `rust-fil-proof` doesn’t log anything. You can change this by setting the RUST\_LOG environment variable to another level. This will show log output on stderr which can be redirected to a file either by systemd or in the shell while launching the `curio` process manually.

Using systemd service file:

```bash
export RUST_LOG=info >> /etc/curio.ENV
systemctl restart curio.service
```

Running Curio manually:

```shell
export RUST_LOG=info >> ~/.bashrc && source ~/.bashrc
```

The log-level can be chosen between 5 options:

* trace
* debug
* info
* warn
* error

### Change logging verbosity&#x20;

The verbosity of the `curio` logs can be changed without restarting the service or process. The following command can be used to list different subsystems within the `curio` process and change the verbosity of individual subsystem to get more/less detailed logs.

```shell
curio cli --machine <Machine IP:Port> log list
```

To change the verbosity, please run:

```shell
curio cli --machine <Machine IP:Port> log set-level --system chain debug
```

The log-level can be chosen between 4 options:

* debug
* info
* warn
* error

You can specify multiple subsystems to change the log level of multiple subsystems at once.

```shell
curio cli --machine <Machine IP:Port> log set-level --system chain --system chainxchg debug
```

