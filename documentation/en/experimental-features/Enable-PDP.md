---
description: This page explains how to enable PDP in Curio.
---

# Enable PDP

## What is Proof of Data Possession (PDP)?

Proof of Data Possession (PDP) is a new component of the Filecoin ecosystem, designed to work alongside Proof of Replication (PoRep). While PoRep ensures that data is securely stored and uniquely encoded, PDP focuses on verifying that data is accessible at specific points in time. Here’s a breakdown of what PDP does:

* **Periodic Accessibility:** PDP checks that data is available when needed, ensuring it’s stored and accessible at regular intervals.
* **Efficient Retrieval:** Unlike PoRep, PDP operates on unencoded data, making it faster to retrieve without the need for decoding the data.
* **Lightweight Verification:** PDP’s proving process is computationally efficient and doesn’t require heavy resources like GPUs.

PDP is particularly useful for applications where data needs to be accessed frequently, such as in hot storage scenarios. It ensures that storage providers maintain accessible copies of data without the overhead of constant decoding. This makes PDP a valuable tool for improving retrieval efficiency on Filecoin.

It’s important to note that while PDP is a supporting element of efficient retrieval, it doesn’t guarantee retrieval; malicious storage providers could still deny service. Additionally, PDP complements, rather than replaces, PoRep, as it does not guarantee data replication or incompressibility — both essential for storage-based consensus.

You can read more about PDP [here](https://medium.com/@filoz/the-pdp-journey-an-in-depth-look-at-how-pdp-works-4b6079f4baad).

## PDP Provider Setup

### Existing Curio cluster (Filecoin PoRep)

In one of the configuration layer used by Market nodes, please enable the following tasks and restart the node.

```toml
[Subsystems]
  EnableCommP = true
  EnableMoveStorage = true
  EnablePDP = true
  EnableParkPiece = true
```

Once done, please proceed with [PDP owner](Enable-PDP.md#pdp-owner) and [PDP service](Enable-PDP.md#pdp-service) configuration.

### New Curio cluster exclusively for PDP

1. Get familiar with [Curio's design](../design/).
2. [Get started](../getting-started.md) with a new Curio cluster.
3.  Since, this cluster will be dedicated to PDP, you can install Curio using the [debian packages](../installation.md#debian-package-installation) when PDP feature is available as part of a release. Till then, you need to manually [build the binaries](../installation.md#linux-build-from-source).\


    ```bash
    git clone https://github.com/filecoin-project/curio.git
    cd curio/
    ```


4.  Once Curio is installed, proceed to [Curio setup](../setup.md) and [initiating a new Curio cluster](../setup.md#initiating-a-new-curio-cluster). This new minerID will not be used by PDP but we need to initiate it as Curio is designed to function with at least one miner ID. As part of initiating a new cluster, you will start the Curio process and the [proceed with service configuration](../curio-service.md) from there. Update the `/etc/curio.env`file and remove the post layers.



    ```bash
    CURIO_LAYERS=gui
    ```


5. After the service configuration is complete, please start the Curio node using the `systemd` and verify that GUI is accessible.
6. Go to Curio UI at `https://<curio node IP>:4701` and click on "Configurations" in the menu. Click on the layer named "base" and update the following configurations.
   1. **Set a Domain Name**:
      * Ensure that a valid `DomainName` is specified in the `HTTPConfig`. This is mandatory for proper HTTP server functionality and essential for enabling TLS. The domain name cannot be an IP address.
      * In case `DelegateTLS`  is `False` , the domain name must point to the public IP address your curio node is listening on. The purpose of setting this field is to allow lets encrypt ACME protocol to automatically issue a certificate to use TLS for encrypting access to the curio api. For let's encrypt policy reasons this will only work if curio listens on port 443.
      * Domain name should be specified in the base layer
   2. **HTTP Configuration Details**:
      * If TLS is managed by a reverse proxy, enable `DelegateTLS` in the `HTTPConfig` to allow the HTTP server to run without handling TLS directly.
      * Configure additional parameters such as `ReadTimeout`, `IdleTimeout`, and `CompressionLevels` to ensure the server operates efficiently.
   3. Save the layer with the changes.
7. Add a new layer with name "pdp" and update the following configuration.
   1. **Enable the Deal Market**:
      * Set `EnableDealMarket` to `true` in the `CurioSubsystemsConfig` for at least one node. This enables deal-making capabilities on the node.
   2. **Enable CommP**:
      * Set `EnableCommP` to `true`. This allows the node to compute piece commitments (CommP) before publishing storage deal messages.
   3. **Enable PDP**:
      * Set `EnablePDP` to `true`. This will allow the nodes to run PDP related tasks.
   4. **Enable ParkPiece:**
      1. Set `EnableParkPiece`to `true`. This will enable the nodes to download the remote data and save it locally.
   5. Save the layer with changes.
8. Add a new layer called "http".
   1. **Enable HTTP**:&#x20;
      * To enable HTTP, set the `Enable` flag in the `HTTPConfig` to `true` and specify the `ListenAddress` for the HTTP server.
9. If you have more than one node in the cluster, you should enable HTTP server only on one of those nodes unless you plan to load balance with a reverse proxy.
10. Now, we have all the required configuration layers in place, we can start the nodes with required configuration. Update the `/etc/curio.env`file and add the PDP layers.\


    ```bash
    CURIO_LAYERS=gui,pdp,http
    ```



    {% hint style="danger" %}
    **The layer "http" should only be added to one node if cluster has multiple nodes and TLS is not delegated.**
    {% endhint %}
11. Start the Curio nodes and [attach the long-term storage](../storage-configuration.md#adding-long-term-storage-location) to store the PDP files.

### PDP owner

To interact with the PDP smart contracts the Curio SP must import a private key owning their PDP data sets.

1. Obtain private key bytes
2. In the Curio web UI (localhost:4701) navigate to PDP and click "Import Key"
3. Paste key bytes

There are many ways to obtain key bytes. Any method of exporting an ETH private key will work. Filecoin native address private keys can be extracted with

```bash
lotus wallet export <f1 address> | xxd -r -p | jq -r '.PrivateKey' | base64 -d | xxd -p -c 32
```

{% hint style="info" %}
PDP providers can use an existing wallet or create a new dedicated wallet for Proving.
{% endhint %}

### PDP Service

To interact with the Curio PDP api clients need to have authentication to access a PDP API Service. PDP SPs must create PDP Services for clients to use their API. Here is the creation flow.

1. Client creates a local authentication token offline, they then send PEM public key to curio PDP SP
2. In the Curio web UI (localhost:4701) navigate to PDP and click "Add Service"
3. Fill out service name (i.e. clientX) and paste their public key

## PDP Client

Curio provides a client utilty which can be used by clients to onboards the data with PDP providers.

To build the client binary, clone the Curio repo and build the PDP client.&#x20;

```bash
git clone https://github.com/filecoin-project/curio.git
cd curio/
make pdptool
```

The `pdptool` command contains utilities for uploading files to a curio node and interacting with the pdp contract to create data sets, and add and remove pieces from data sets.

### Create service secret

To get started using `pdptool` to access the curio API as a client you will need to create a service authentication secret for accessing the API. To do this run `pdptool create-service-secret`. This will display the public key for sending to the curio node for [registering the PDP service](Enable-PDP.md#pdp-service). Additionally it will write a file `pdpservice.json` that contains a PEM encoded private key for API authentication. `pdptool` can be used without any extra steps if run in the same directory as this file.

### Connect to service

Once the curio node has imported the service public key you can check that you are able to connect with

```
pdptool ping --service-url <curio.domain.name.com> --service-name <pdp-service-name>
```

Upon success you can start proving files on PDP.
