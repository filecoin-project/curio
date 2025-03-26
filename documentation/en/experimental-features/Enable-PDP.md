---
description: This page explains how to setup a Curio PDP SP
---

# Curio PDP

This guide is focused on running a PDP-only SP.  Most of the same instructions will apply for running a curio node providing both PoRep and PDP storage though you will likely need to modify the configuration defaults specified here to keep the sealing pipeline running smoothly.

## Setup curio 

The first step to running a curio PDP SP is to install and setup curio.

Some important callouts departing from the ordinary curio setup instructions
* You MUST check out `feat/pdp` branch and build curio from source on this branch. PDP code is not yet on `main`
* The `feat/pdp` branch has not been updated to go v1.24.x so running PDP SP requires building with go v1.23.x
* Follow the guidelines under `Initiating a new Curio cluster` including the `guided-setup` command.  This means that you need to create a miner actor onchain (i.e. PoRep infrastructure). While unnecessary for PDP this is the quickest and easiest way to get PDP working on curio.


With these points in mind proceed to the [curio setup instructions](setup.md#setup) to setup curio.

## Curio configuration


### Config 

Curio PDP requires specific configuration options. Briefly you will need to modify your config to look like this: 

```toml
[HTTP]
  DomainName = "your.domain.name.com"
  Enable = true
  IdleTimeout = 7200000000000
  ListenAddress = "0.0.0.0:443"
  ReadHeaderTimeout = 7200000000000
  ReadTimeout = 7200000000000

[Subsystems]
  EnableCommP = true
  EnableMoveStorage = true
  EnablePDP = true
  EnableParkPiece = true
```

### Explanation

The domain name must point to the IP address your curio node is listening on. The purpose of setting this field is to allow lets encrypt ACME protocol to automatically issue a certificate to use TLS for encrypting access to the curio api. For let's encrypt policy reasons this will only work if curio listens on port 443.  

The maxed out timeouts are a suggestion to maximally avoid data transfer timeout issues.

The curio CommP, MoveStorage, PDP and ParkPiece subsystems are all necessary for pdp to function properly and must be enabled.

### Setting config

Copy the above toml to a file `pdp-layer` and then run `curio config create pdp-layer`.

Then start up curio `./curio run --layers gui,pdp-layer`.

Alternatively, you can start curio with `./curio run --layers gui`, navigate to localhost:4701 -> Configurations -> Add Layer (pdp-layer) and then manually update the necessary subsystems and HTTP options.  Then restart with the new config layer applied `./curio run --layers gui,pdp-layer`

To set your curio configuration values you will need to create a new pdp specific configuration layer.

## Storage attach

Running a PDP SP means storing data. Curio requires manually adding data storage paths to actually store data. Run `./curio cli storage attach --init --store /path/to/storage ` to setup a storage path. Now your curio node can store PDP files.

## PDP Service

To interact with the curio PDP api clients need to have authentication to access a PDP API Service. PDP SPs must create PDP Services for clients to use their API. Here is the creation flow.

1. Client creates a local authentication token offline, they then send PEM public key to curio PDP SP
2. In the Curio web UI (localhost:4701) navigate to PDP and click "Add Service" 
3. Fill out service name (i.e. clientX) and paste their public key

## PDP owner

To interact with the PDP smart contracts the curio SP must import a private key owning their PDP proof sets.  

1. Obtain private key bytes
2. In the Curio web UI (localhost:4701) navigate to PDP and click "Import Key"
3. Paste key bytes

There are many ways to obtain key bytes. Any method of exporting an ETH private key will work. Filecoin native address private keys can be extracted with
```
lotus wallet export <f1 address> | xxd -r -p | jq -r '.PrivateKey' | base64 -d | xxd -p -c 32
```

## pdp-tool 

Go to curio/cmd/pdp-tool and run `go build .`

The `pdptool` command contains utilities for uploading files to a curio node and interacting with the pdp contract to create proofsets, and add and remove roots from proofsets.

### Create service secret

To get started using `pdptool` to access the curio API as a client you will need to create a service authentication secret for accessing the API.  To do this run `pdptool create-service-secret`.  This will display the public key for sending to the curio node for [registering the PDP service](#pdp-service).  Additionally it will write a file pdpservice.json that contains a PEM encoded private key for API authentication.  `pdptool` can be used without any extra steps if run in the same directory as this file

### Connect to service

Once the curio node has imported the service public key you can check that you are able to connect with

```
pdptool ping --service-url <curio.domain.name.com> --service-name <pdp-service-name>
```

Upon success you can start proving files on PDP.