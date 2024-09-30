---
description: 本指南描述如何在Curio中更新日志记录首选项。
---

# Logging
# 日志记录

## Log file configuration
## 日志文件配置

每个Curio节点生成Go日志，如果您以systemd服务的方式运行Curio，默认情况下这些日志会被定向到`/var/log/curio/curio.log`文件。

### Redirect Go logs to a file
### 将Go日志重定向到文件

默认情况下，如果不作为systemd服务运行，Curio会将所有日志重定向到标准输出。要更改此行为，请将以下变量添加到`.bashrc`文件中，并重启`curio`进程，以开始将所有日志重定向到文件。

```bash
export GOLOG_OUTPUT=FILE >> ~/.bashrc
export GOLOG_FILE="$HOME/curio.log" >> ~/.bashrc && source ~/.bashrc
````

### Redirect Rust logs to a standard output
### 将Rust日志重定向到标准输出

默认情况下，`rust-fil-proof`使用的`fil_logger`库不会记录任何内容。您可以通过将RUST_LOG环境变量设置为另一个级别来更改此设置。这将在stderr上显示日志输出，可以通过systemd或在手动启动`curio`进程时在shell中将其重定向到文件。

使用systemd服务文件：


export RUST_LOG=info >> /etc/curio.env
systemctl restart curio.service


手动运行Curio：


export RUST_LOG=info >> ~/.bashrc && source ~/.bashrc


日志级别可以在5个选项之间选择：

* trace
* debug
* info
* warn
* error

### Change logging verbosity
### 更改日志记录详细程度

可以在不重启服务或进程的情况下更改`curio`日志的详细程度。可以使用以下命令列出`curio`进程中的不同子系统，并更改单个子系统的详细程度，以获得更多/更少的详细日志。


curio cli --machine <Machine IP:Port> log list


要更改详细程度，请运行：


curio cli --machine <Machine IP:Port> log set-level --system chain debug


日志级别可以在4个选项之间选择：

* debug
* info
* warn
* error

您可以指定多个子系统，以一次更改多个子系统的日志级别。


curio cli --machine <Machine IP:Port> log set-level --system chain --system chainxchg debug

