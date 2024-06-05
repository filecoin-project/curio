---
description: >-
  This page describe how to access the Curio GUI and what information is
  available there.
---

# Curio GUI

## Accessing Curio GUI

The default port is `4701` to access the Curio GUI. To enable GUI on a Curio node user must start a Curio node with `gui` layer. This is a pre-built layer shipped with Curio binaries.

### Changing default GUI port

You can change the default GUI port by setting a different IP address and port in the "base" layer of the configuration. We highly recommend not specifying the GUI address in other layers to avoid confusion.

```
curio config edit base
```

This will open the "base" layer in your default text editor.

```
  # The address that should listen for Web GUI requests.
  #
  # type: string
  #GuiAddress = "0.0.0.0:4701"

should be changed to below

  # The address that should listen for Web GUI requests.
  #
  # type: string
  GuiAddress = "127.0.0.1:4702"
```

Save the configuration and restart the Curio service on the node running GUI layer to access the GUI on the new address and port.

## GUI menu and dashboards

{% hint style="danger" %}
Curio web UI is currently under development. Some UI pages might change over time and could be different from the screenshots and description below.
{% endhint %}

