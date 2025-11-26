# Localnet Setup Guide

This guide explains how to build and configure Curio for use with a local Filecoin network (localnet).

## Overview

By default, Curio is built for either mainnet or calibnet. For development and testing purposes, you can build Curio with localnet support, which allows configuration via environment variables.

## Building for Localnet

To build Curio with localnet support, use the `localnet` build tag:

```bash
go build -tags localnet -o curio ./cmd/curio
```

Or using make:

```bash
make clean
go build -tags localnet -o curio ./cmd/curio
```

This will produce a Curio binary configured for localnet with the version suffix `+localnet`.

## Required Environment Variables

When running Curio built for localnet, you must configure certain contract addresses via environment variables:

### FEVM Contract Addresses

- **`CURIO_LOCALNET_MULTICALL_ADDRESS`** - FEVM multicall contract address (required if using multicall functionality)
  - Example: `0xcA11bde05977b3631167028862bE2a173976CA11`

- **`CURIO_LOCALNET_PAYMENT_CONTRACT`** - FEVM payment contract address (required if using payment contracts)
  - Example: `0x09a0fDc2723fAd1A7b8e3e00eE5DF73841df55a0`

## Optional Environment Variables

### Network Parameters

- **`CURIO_LOCALNET_BLOCK_DELAY`** - Block delay in seconds (default: `4`)
  - Controls the time between blocks in the network

- **`CURIO_LOCALNET_EQUIVOCATION_DELAY`** - Equivocation delay in seconds (default: `0`)
  - Time delay for equivocation checks

- **`CURIO_LOCALNET_ADDRESS_NETWORK`** - Address network type (default: `testnet`)
  - Valid values: `mainnet` or `testnet`
  - Determines the address format used by the network

### Propagation Delay

The propagation delay is hardcoded to `1` second for localnet builds.

## Example Configuration

Here's a complete example of setting up environment variables for localnet:

```bash
# Contract addresses (required)
export CURIO_LOCALNET_MULTICALL_ADDRESS="0xcA11bde05977b3631167028862bE2a173976CA11"
export CURIO_LOCALNET_PAYMENT_CONTRACT="0x23b1e018F08BB982348b15a86ee926eEBf7F4DAa"

# Network parameters (optional)
export CURIO_LOCALNET_BLOCK_DELAY=4
export CURIO_LOCALNET_EQUIVOCATION_DELAY=0
export CURIO_LOCALNET_ADDRESS_NETWORK="testnet"

# Run Curio
./curio run
```

## Network Name Validation

When connecting to a Lotus node, Curio validates that the network name matches the build type:

- For **localnet** builds: The node's network name must start with `"local"` (e.g., `localnet`, `local-dev`, etc.)
- For **mainnet** builds: The node must be on `mainnet`
- For **calibnet** builds: The node must be on `calibnet` or `calibrationnet`

This ensures you don't accidentally connect a localnet binary to a production network.

## Network Protocol Differences

Localnet builds automatically use different networking protocols compared to production networks:

- **Mainnet/Calibnet**: Uses WSS (WebSocket Secure) on port 443
- **Localnet/Devnets**: Uses plain WS (WebSocket) without TLS encryption

This simplifies local development by removing the need for TLS certificates.

## Troubleshooting

### Missing Contract Addresses

If you see errors like:
```
multicall address not configured for localnet - set CURIO_LOCALNET_MULTICALL_ADDRESS env var
```

Make sure you've set the required environment variables before starting Curio.

### Network Mismatch

If you see warnings like:
```
Network mismatch for node: binary built for localnet but node is on mainnet
```

You're trying to connect a localnet-built Curio to a non-local network. Rebuild Curio without the `localnet` tag for production networks.

### Wrong Build Type

To verify your Curio binary is built for localnet, check the version:

```bash
./curio version
```

You should see `+localnet` in the version string, e.g., `1.27.0+localnetabcd1234`.

## Build Tags Reference

Curio supports several build tags:

- **No tag** (default) - Mainnet configuration
- **`-tags calibnet`** - Calibration testnet configuration
- **`-tags localnet`** - Local network configuration (this guide)
- **`-tags 2k`** - 2048 byte sector size for testing
- **`-tags debug`** - Debug build

## Additional Resources

- [Curio Documentation](https://docs.curiostorage.org/)
- [Filecoin Local Network Setup](https://lotus.filecoin.io/tutorials/lotus/local-network/)
- [Curio Configuration Guide](https://docs.curiostorage.org/configuration/)

## Contributing

If you encounter issues with localnet support or have suggestions for improvements, please:

1. Check existing issues: [GitHub Issues](https://github.com/filecoin-project/curio/issues)
2. Join the discussion: [#fil-curio-help on Slack](https://filecoinproject.slack.com/archives/C06LF5YP8S3)
3. Submit a pull request with your improvements
