---
description: How to enable the market submodule and connect to Boost
---

# Enabling market

## Introduction

Curio offers a market submodule that enables seamless integration with Boost without requiring any changes to the Boost code base. This document will guide you through enabling the market in Curio, configuring PiecePark, and setting up both existing and new Boost instances.

## Enable Market adapter in Curio

Edit Market Config

```shell
curio config add --title mt01000
```

&#x20;Add an entry like:&#x20;

```
  [Subsystems]
  EnableParkPiece = true
  BoostAdapters = ["t10000:127.0.0.1:32100"]
```

Press `ctrl + D` to save and exit.

{% hint style="info" %}
You should run only one Curio market adapter node per minerID. Boost can only talk to one adapter at a time.
{% endhint %}

Edit the `/etc/curio.env` file and update the `CURIO_LAYERS` variable to include the new market layer as well.

```
CURIO_LAYERS=seal,mt01000
CURIO_ALL_REMAINING_FIELDS_ARE_OPTIONAL=true
CURIO_DB_HOST=yugabyte
CURIO_DB_USER=yugabyte
CURIO_DB_PASSWORD=yugabyte
```

Restart the Curio service for the changes to take effect.

{% hint style="info" %}
Node running the market is the node that Boost will interact with. It proxies deal data, so it is best to run it either next to Boost or on the same node running TreeD (and TreeRC) tasks. TreeD is where we add the deal data to the sectors.
{% endhint %}

## Connecting with Boost

### Get Market RPC Info

```shell
curio market rpc-info --layers mt01000
```

{% hint style="info" %}
If the output of rpc-info is empty then you have likely not included the correct layer containing the `BoostAdapters` configuration. Please recheck your --layers flag.
{% endhint %}

### Connect with existing Boost after migration

Use the market rpc-info string to replace the `SealerApiInfo` and `SectorIndexApiInfo` string in the Boost config and restart Boost.

### Initialising New Boost

Follow the [Boost Setup Instructions](https://boost.filecoin.io/new-boost-setup) with additional change of replacing `MINER_API_INFO` with the market rpc-info string.

### Updating PeerID and On-Chain Address

Make sure that the correct _peer id_ and _multiaddr_ for your SP is set on chain, given that `boost init` generates a new identity. Use the following commands to update the values on chain:

Find the PeerID

```
boostd net id
```

Find the address

```
boostd net listen
```

Set on chain

```
sptool --actor <miner id> actor set-addrs <MULTIADDR>
sptool --actor <miner id> actor set-peer-id <PEER_ID>
```
