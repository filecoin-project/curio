---
description: Curio 警报管理器设置和配置
---

# Alert Manager
# 警报管理器

Curio 有一个每小时运行一次的 AlertManager 任务，允许 Curio 集群向用户提醒集群中的某些问题。

目前，Curio 支持以下问题的警报：

1. 钱包余额低于 5 Fil。
2. 如果某个截止日期没有运行 WindowPost 任务。
3. 如果发现孤立块或者没有为任何 epoch 创建 WinningPost 任务。
4. 如果永久存储没有足够的空间来容纳当前正在封装的扇区。

AlertManager 是一个基于插件的模块，允许与任何插件集成。目前，Curio 有 2 个可用的插件。生成的警报可以同时发送到多个插件，以实现更强大的通知机制。

{% hint style="info" %}
欢迎对新的关键警报或与其他警报系统的集成做出贡献。
{% endhint %}

### PagerDuty Plugin
### PagerDuty 插件

Curio 默认集成了 [PagerDuty.com](https://www.pagerduty.com/)，允许向存储提供商发送关键警报。要配置您的 Curio 集群以发送警报，您必须设置一个 PagerDuty 账户。

{% hint style="danger" %}
与此软件开发相关的任何人都与 PagerDuty 没有任何业务关系。提供此集成是为了方便存储提供商选择的警报系统。
{% endhint %}

1. 在[这里](https://www.pagerduty.com/sign-up-free/?type=free)注册一个免费的 PagerDuty 账户。
2. 创建一个新的服务，用于处理来自 Curio 集群的警报。
3. 在创建服务过程中，在"Integration"页面上，选择"Events API V2"。
4. 服务创建完成后，从服务中复制"Integration Key"，并将其粘贴到"base"层配置中的"PagerDutyIntegrationKey"。
5. 在配置层中启用插件。
6. 重启其中一个节点，它现在将每小时生成关键警报。


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


### Prometheus AlertManager
### Prometheus 警报管理器

1. 设置一个 [Prometheus AlertManager](https://prometheus.io/docs/alerting/latest/alertmanager/) 实例。
2. 通过将 `Enabled` 设置为 True 来启用插件。
3. 在配置中粘贴 `AlertManagerURL`。
4. 使用更新后的配置层重启节点。


  [Alerting.PrometheusAlertManager]
    # Enable is a flag to enable or disable the Prometheus AlertManager integration.
    #
    # type: bool
    Enable = true

    # AlertManagerURL is the URL for the Prometheus AlertManager API v2 URL.
    #
    # type: string
    AlertManagerURL = "http://localhost:9093/api/v2/alerts"

