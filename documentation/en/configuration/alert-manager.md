---
description: Curio alert manager setup and configuration
---

# Alert Manager

Curio comes with a default integration with [PagerDuty.com](https://www.pagerduty.com/), allowing the sending of critical alerts to storage providers. To configure your Curio cluster to send alerts, you must set up a PagerDuty account.

{% hint style="danger" %}
Nobody associated with the development of this software has any business relationship with PagerDuty. This integration is provided as a convenient gateway to the storage provider’s alert system of choice.
{% endhint %}

1. Sign up for a free PagerDuty account [here](https://www.pagerduty.com/sign-up-free/?type=free).
2. Create a new service that will handle the alerts from the Curio cluster.
3. During the service creation, on the “Integration” page, choose “Events API V2”.
4. Once the service creation is complete, copy the “Integration Key” from the service and paste it in the “base” layer configuration for “PagerDutyIntegrationKey”.
5. Restart one of the nodes, and it will now generate critical alerts every hour.

{% hint style="info" %}
Contributions to new critical alerts or integrations with other alerting systems are welcome.
{% endhint %}
