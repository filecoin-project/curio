---
description: How to configure Prometheus to scrape metrics from Curio
---

# Prometheus Metrics

Curio exposes a comprehensive set of metrics via a built-in Prometheus endpoint. These metrics provide visibility into database performance, task execution, resource utilization, and more.

## Metrics Endpoint

Curio exposes metrics at the `/debug/metrics` endpoint on the RPC server:

```
http://<curio-listen-address>/debug/metrics
```

The endpoint is automatically enabled on all Curio nodes—no configuration is required to expose basic metrics.

All metrics are prefixed with the `curio_` namespace.

## Prometheus Configuration

To scrape metrics from Curio, add a job to your Prometheus configuration:

```yaml
scrape_configs:
  - job_name: 'curio'
    static_configs:
      - targets:
        - 'curio-node-1:12300'
        - 'curio-node-2:12300'
        - 'curio-node-3:12300'
    # Optional: adjust scrape interval
    scrape_interval: 15s
```

Replace the targets with your actual Curio node addresses and ports.

### Service Discovery

Curio also provides a service discovery endpoint at `/debug/service-discovery` which can be used with Prometheus file-based service discovery for dynamic node discovery.

## Available Metrics

For a complete list of all exported metrics, see the **[Metrics Reference](metrics-reference.md)**.

Key metric categories include:

- **Database (HarmonyDB)**: Query latency, connection pool, errors, failover tracking
- **Tasks (HarmonyTask)**: Task counts, durations, success/failure rates, resource usage
- **Proof Service**: Proofshare queue, durations, retry counts
- **Storage**: NVMe health, cache stats, slot management
- **HTTP/Retrieval**: Request counts, bytes served, response codes

## Wallet Exporter (Optional)

The Wallet Exporter provides additional metrics about wallet balances, miner power, and message activity.

{% hint style="warning" %}
**Enable the Wallet Exporter on exactly ONE Curio node in the cluster.** Enabling it on multiple nodes causes duplicate metrics and incorrect aggregations.
{% endhint %}

### Enabling the Wallet Exporter

Add to a layer active on exactly one node:

```toml
[CurioSubsystems]
EnableWalletExporter = true
```

Restart the node. Metrics appear within ~30 seconds.

See [Wallet Exporter](../experimental-features/Wallet-Exporter.md) for detailed usage and the full list of wallet metrics.

## Example Prometheus Queries

### Task Performance

```promql
# Average task duration over last hour
rate(curio_harmonytask_task_duration_seconds_sum[1h]) 
  / rate(curio_harmonytask_task_duration_seconds_count[1h])

# Task success rate
sum(rate(curio_harmonytask_tasks_completed[5m])) 
  / sum(rate(curio_harmonytask_tasks_started[5m]))

# Active tasks by type
curio_harmonytask_active_tasks
```

### Database Health

```promql
# Average query latency in milliseconds
rate(curio_db_total_wait[5m]) / rate(curio_db_hits[5m])

# Database error rate
rate(curio_db_errors[5m])

# Connection pool usage
curio_db_open_connections
```

### Resource Utilization

```promql
# Node resource usage
curio_harmonytask_cpu_usage
curio_harmonytask_gpu_usage
curio_harmonytask_ram_usage

# Node uptime
curio_harmonytask_uptime
```

## Grafana Integration

These Prometheus metrics can be visualized using Grafana. Create dashboards to monitor:

- **Cluster Overview**: Node uptime, task throughput, error rates
- **Task Performance**: Duration histograms, success/failure rates by task type
- **Resource Utilization**: CPU, GPU, RAM usage across nodes
- **Database Health**: Query latency, connection pool, failover events
- **Wallet Activity**: Balances, gas usage, message throughput (with Wallet Exporter)

## AlertManager Integration

Curio can send alerts to Prometheus AlertManager. See [Alert Manager](alert-manager.md) for configuration details.

## Troubleshooting

### Metrics not appearing

1. Verify the Curio node is running and accessible
2. Check that you can reach the metrics endpoint: `curl http://<node>:<port>/debug/metrics`
3. Verify your Prometheus configuration targets are correct

### Duplicate metrics

If you see duplicate metric series, ensure the Wallet Exporter is enabled on only one node.

### Missing wallet metrics

Wallet Exporter metrics require:
- `EnableWalletExporter = true` in configuration
- Wallets registered in the `wallet_names` table (via the Wallets page in UI)
- At least one miner ID configured on the node
