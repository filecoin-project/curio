# Guide for Generating Go Bindings Using `abigen`

This guide explains how to use the `abigen` tool to generate Go bindings for Ethereum smart contracts. These bindings allow you to interact with contracts in Go programs. The smart contract ABIs (Application Binary Interfaces) are retrieved from the source repository and updated after being processed with `make build`.

---

## Prerequisites

1. **Install `abigen`:**
   Install `abigen` from the Go Ethereum (geth) toolset. You can install it via the following command:

   ```bash
   go install github.com/ethereum/go-ethereum/cmd/abigen@latest
   ```

2. **Ensure Forge (`foundry`) is Installed:**
   The `make build` step requires the Forge tool (from Foundry). Install it via:

   ```bash
   curl -L https://foundry.paradigm.xyz | bash
   foundryup
   ```

3. **Clone the Repository:**
   Clone the repository where the smart contract code resides:

   ```bash
   git clone https://github.com/FilOzone/pdp.git
   cd pdp
   ```

---

## Steps to Generate Go Bindings

### Step 1: Build the Contracts using `make build`

In the root of the cloned repository, run:

```bash
make build
```

This command will create the `out/` directory containing the compiled contract artifacts, such as `IPDPProvingSchedule.json` and `PDPVerifier.json`.

---

### Step 2: Extract ABIs from Compiled Artifacts

Navigate to the `out/` directory and extract the ABI from the compiled JSON files for the required contracts. Use the `jq` tool:

#### For `IPDPProvingSchedule` ABI:

Run:

```bash
jq '.abi' out/IPDPProvingSchedule.sol/IPDPProvingSchedule.json > pdp/contract/IPDPProvingSchedule.abi
```

#### For `PDPVerifier` ABI:

Run:

```bash
jq '.abi' out/PDPVerifier.sol/PDPVerifier.json > pdp/contract/PDPVerifier.abi
```

Ensure that the respective `.abi` files are updated in the `pdp/contract/` directory.

---

### Step 3: Generate Go Bindings Using `abigen`

Use the `abigen` command-line tool to generate the Go bindings for the parsed ABIs.

#### For `IPDPProvingSchedule` Contract:

Run:

```bash
abigen --abi pdp/contract/IPDPProvingSchedule.abi --pkg contract --type IPDPProvingSchedule --out pdp/contract/pdp_proving_schedule.go
```

- `--abi`: Path to the `.abi` file for the contract.
- `--pkg`: Package name in the generated Go code (use the relevant package name, e.g., `contract` in this case).
- `--type`: The Go struct type for this contract (use descriptive names like `IPDPProvingSchedule`).
- `--out`: Output file path for the generated Go file (e.g., `pdp_proving_schedule.go`).

---

#### For `PDPVerifier` Contract:

Run:

```bash
abigen --abi pdp/contract/PDPVerifier.abi --pkg contract --type PDPVerifier --out pdp/contract/pdp_verifier.go
```

---

### Step 4: Verify the Outputs

After running the `abigen` commands, the Go files (`pdp_proving_schedule.go` and `pdp_verifier.go`) will be generated in the `pdp/contract/` directory. These files contain the Go bindings that can be used in Go applications to interact with the corresponding smart contracts.

---

### Notes

- **ABI Files:** Ensure that the `.abi` files are correct and up to date by extracting them directly from the compiled JSON artifacts.
- **Code Organization:** Keep both the generated Go files and ABI files in a structured directory layout for easier maintenance (e.g., under `pdp/contract/`).