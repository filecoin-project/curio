---
description: Curio alert manager setup and configuration
---

# Alert Manager

Curio has an AlertManager task which runs every 1 hour and allows Curio cluster to alert users about certain issues in the cluster.

Currently, Curio supports the alert for following issues:

1. Wallet balance is below 5 Fil.
2. If not WindowPost task is run for a deadline.
3. If an orphan block is found or if no WinningPost task is not created for any epoch.
4. If permanent storage does not have enough space to accommodate the sectors currently being sealed.

The AlertManager is a plugin based module and allows integration with any plugin. As of now, Curio has 2 plugins available. Alerts generated can be send to multiple plugins at the same time to allow for a more robust notification mechanism.

{% hint style="info" %}
Contributions to new critical alerts or integrations with other alerting systems are welcome.
{% endhint %}

### PagerDuty Plugin

Curio comes with a default integration with [PagerDuty.com](https://www.pagerduty.com/), allowing the sending of critical alerts to storage providers. To configure your Curio cluster to send alerts, you must set up a PagerDuty account.

{% hint style="danger" %}
Nobody associated with the development of this software has any business relationship with PagerDuty. This integration is provided as a convenient gateway to the storage provider’s alert system of choice.
{% endhint %}

1. Sign up for a free PagerDuty account [here](https://www.pagerduty.com/sign-up-free/?type=free).
2. Create a new service that will handle the alerts from the Curio cluster.
3. During the service creation, on the “Integration” page, choose “Events API V2”.
4. Once the service creation is complete, copy the “Integration Key” from the service and paste it in the “base” layer configuration for “PagerDutyIntegrationKey”.
5. Enable the plugin in config layer.
6. Restart one of the nodes, and it will now generate critical alerts every hour.

```
[Alerting]
  # MinimumWalletBalance is the minimum balance all active wallets. If the balance is below this value, an
  # alerts will be triggered for the wallet
  #
  # type: types.FIL
  #MinimumWalletBalance = "5 FIL"

  [Alerting.PagerDuty]
    # Enable is a flag to enable or disable the PagerDuty integration.
    #
    # type: bool
    Enable = true

    # PagerDutyEventURL is URL for PagerDuty.com Events API v2 URL. Events sent to this API URL are ultimately
    # routed to a PagerDuty.com service and processed.
    # The default is sufficient for integration with the stock commercial PagerDuty.com company's service.
    #
    # type: string
    PagerDutyEventURL = "https://events.pagerduty.com/v2/enqueue"

    # PageDutyIntegrationKey is the integration key for a PagerDuty.com service. You can find this unique service
    # identifier in the integration page for the service.
    #
    # type: string
    PageDutyIntegrationKey = ""
```

### Prometheus AlertManager

1. Setup a [Prometheus AlertManager](https://prometheus.io/docs/alerting/latest/alertmanager/) instance.
2. Enable the plugin but setting the `Enabled` to True.
3. Paste the `AlertManagerURL` in the configguration.
4. Restart the node with the updated config layer.

```
  [Alerting.PrometheusAlertManager]
    # Enable is a flag to enable or disable the Prometheus AlertManager integration.
    #
    # type: bool
    Enable = true

    # AlertManagerURL is the URL for the Prometheus AlertManager API v2 URL.
    #
    # type: string
    AlertManagerURL = "http://localhost:9093/api/v2/alerts"
```
