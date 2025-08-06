# Wallet Exporter

The **Wallet Exporter** is an optional telemetry component that periodically exposes
comprehensive statistics about Curio wallets, miners and the messages they send.
The metrics are exported via the built-in Prometheus endpoint and can be scraped
by any Prometheus compatible monitoring stack.

> ⚠️  IMPORTANT: **Enable the exporter on exactly _one_ Curio node in the
> cluster.** Enabling it on multiple nodes would cause duplicated metric series
> which leads to incorrect dashboards and aggregated values.

---

## Why would I enable it?

* Track wallet balances for every on-chain address that appears in the
  `wallet_names` table. Those are wallets with a name on the Wallets page.
* Observe storage-provider balances and power in real-time.
* Monitor message throughput, gas usage and fees broken down by sender,
  receiver and reason.
* Build Grafana dashboards that correlate wallet activity with other Curio
  subsystems.

## How does it work?

1. Every 30 seconds (`WalletExporterInterval`) Curio executes one exporter
   cycle.
2. During a cycle Curio gathers the following information:

   * Wallet balances
   * Miner available balance and power (for the SP IDs configured on
     this node)
   * Newly sent messages that are waiting to land
   * Messages that have already landed (executed) and their execution result
3. For every data-point the exporter records an *OpenCensus measure* which is
   immediately made available as a Prometheus metric.

## Enabling the exporter

Add the following to one layer which is active on _exactly one_ node:

```toml
[CurioSubsystems]
EnableWalletExporter = true
```

Restart the node. The new metrics will appear at the standard metrics endpoint within ~30 seconds.

## Exported metrics

| Metric name                         | Type      | Description |
|------------------------------------|-----------|-------------|
| `wallet_balance_nfil`              | gauge     | Wallet or miner balance in **NanoFIL** |
| `wallet_power`                     | gauge     | Storage-provider power in **bytes** (label `type` = `raw` or `qap`) |
| `wallet_message_sent`              | counter   | Number of messages that have been **submitted** |
| `wallet_message_landed`            | counter   | Number of messages that have **executed** on-chain |
| `wallet_gas_units_requested`       | counter   | Gas units requested by submitted messages |
| `wallet_gas_units_used`            | counter   | Gas units used by executed messages |
| `wallet_sent_nfil`                 | counter   | NanoFIL **value** transferred by executed messages |
| `wallet_gas_paid_nfil`             | counter   | NanoFIL **gas fee** paid by executed messages |
| `wallet_message_land_duration_seconds` | histogram | Distribution of time (in seconds) between message submission and execution |
