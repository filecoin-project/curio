# **MinerPeerIDMapping Smart Contract**

## **Overview**

The `MinerPeerIDMapping` contract provides a decentralized way to map Filecoin miner IDs (`uint64`) to their PeerIDs along with signed messages for validation. It ensures only authorized entities (owner or controlling addresses) can add, update, or delete mappings.

---

## **Features**

- **PeerID Management**: Associate PeerIDs with Filecoin miner IDs.
- **Ownership Control**: Only the owner or authorized controlling addresses can manage mappings.
- **Signed Message Storage**: Store and verify signed JSON-encoded messages for miner-to-peer mappings.
- **Ownership Transfers**: Allows the contract owner to transfer ownership to another address.

---

## **Contract Details**

### **State Variables**

- `address public owner`: The contract owner address.
- `mapping(uint64 => PeerData) public minerToPeerData`: A mapping of miner IDs (`uint64`) to `PeerData`.

---

### **Structs**

- **PeerData**
  ```solidity
  struct PeerData {
      string peerID;
      bytes signedMessage;
  }
  ```
    - `peerID`: The PeerID associated with the miner.
    - `signedMessage`: A signed JSON message containing miner and PeerID information.

---

### **Events**

- **PeerDataAdded**
  ```solidity
  event PeerDataAdded(uint64 indexed minerID, string peerID);
  ```
  Triggered when new PeerID data is added.

- **PeerDataUpdated**
  ```solidity
  event PeerDataUpdated(uint64 indexed minerID, string oldPeerID, string newPeerID);
  ```
  Triggered when PeerID data is updated.

- **PeerDataDeleted**
  ```solidity
  event PeerDataDeleted(uint64 indexed minerID);
  ```
  Triggered when PeerID data is deleted.

- **OwnershipTransferred**
  ```solidity
  event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
  ```
  Triggered when ownership is transferred.

---

## **Contract Functions**

### **Peer Data Management**

- **Add Peer Data**
  ```solidity
  function addPeerData(uint64 minerID, string memory newPeerID, bytes memory signedMessage) public;
  ```
  Adds a new PeerID and signed message for the specified miner ID.

- **Update Peer Data**
  ```solidity
  function updatePeerData(uint64 minerID, string memory newPeerID, bytes memory signedMessage) public;
  ```
  Updates the PeerID and signed message for the specified miner ID.

- **Delete Peer Data**
  ```solidity
  function deletePeerData(uint64 minerID) public;
  ```
  Deletes the PeerID and signed message for the specified miner ID.

- **Get Peer Data**
  ```solidity
  function getPeerData(uint64 minerID) public view returns (PeerData memory);
  ```
  Retrieves the PeerID and signed message associated with a miner ID.

---

### **Controlling Address Validation**

- **Validate Controlling Address**
  ```solidity
  function isControllingAddress(address caller, uint64 minerID) internal returns (bool);
  ```
  Validates if the `caller` is a controlling address for the given `minerID` using Filecoin APIs.

---

## **Signed Message Format**

The signed message stored in the contract contains JSON-encoded information:

```json
{
  "miner": <uint64>,
  "peer": "<peerID>"
}
```

This message is signed using the private key associated with the PeerID.

---

## **Usage Example**

### **Add Peer Data**

```javascript
await contract.addPeerData(1000, "peer-id-1", signedMessage);
```

### **Update Peer Data**

```javascript
await contract.updatePeerData(1000, "peer-id-2", newSignedMessage);
```

### **Delete Peer Data**

```javascript
await contract.deletePeerData(1000);
```

### **Fetch Peer Data**

```javascript
const peerData = await contract.getPeerData(1000);
console.log(peerData.peerID);
console.log(peerData.signedMessage);
```

### Verify the signature for auth check

```javascript
import { HttpJsonRpcConnector, LotusClient } from "filecoin.js";
import * as libp2pCrypto from "@libp2p/crypto"; // Ensure this library is installed
import * as multihashes from "multihashes"; // For decoding PeerID

/**
 * Verify the signature of a message.
 * @param {int64} minerID - Miner's actor ID
 * @param {string} peerID - PeerID used for IPNI communication by the miner
 * @param {Buffer} signature - The signature to verify
 */

async function verifyMinerSignature(minerID, peerID, signature) {
  // Step 1: Reconstruct the message that was signed
  const message = JSON.stringify({miner: `f0${minerID}`, peer: peerID})

  // Step 2: Decode PeerID to obtain the public key
  const decodedPeerID = multihashes.decode(Buffer.from(peerID, "base64"));
  const publicKeyBytes = decodedPeerID.digest;

  // Step 3: Generate a Libp2p public key object
  const publicKey = await libp2pCrypto.keys.unmarshalPublicKey(publicKeyBytes);

  // Step 4: Verify the signature
  const isValid = await publicKey.verify(message, signature);

  if (isValid) {
    console.log("Signature is valid.");
    return true;
  } else {
    console.log("Signature is invalid.");
    return false;
  }
}

// example usage

const minerID = 1000
const {peerID, signedMessage} = await contract.getPeerData(minerID);
await verifyMinerSignature(minerID, peerID, signedMessage)
```

---

## **Integration with Go**

The contract can be interacted with using Go by encoding ABI calls. Below is an example to fetch Peer Data:

### **Go Example**

```go
package main

import (
  "encoding/json"
  "fmt"
  "log"
  "strings"

  "github.com/ethereum/go-ethereum/accounts/abi"
  "github.com/filecoin-project/go-state-types/builtin"
  "github.com/filecoin-project/lotus/chain/types"
  "github.com/filecoin-project/lotus/chain/types/ethtypes"
  "github.com/filecoin-project/lotus/cli"
)

// Define a struct that represents the tuple from the ABI
type PeerData struct {
  PeerID        string `abi:"peerID"`
  SignedMessage []byte `abi:"signedMessage"`
}

// Define a wrapper struct for proper ABI decoding
type WrappedPeerData struct {
  Result PeerData `abi:""`
}

func main() {
  // Setup Lotus CLI context
  // Get FullNodeAPI from Lotus CLI
  fullNode, closer, err := cli.GetFullNodeAPI(cctx)
  if err != nil {
    log.Fatalf("Failed to connect to Lotus node: %v", err)
  }
  defer closer()

  ctx := cctx.Context

  // ABI definition for the function getPeerData(uint64) -> tuple(string,bytes)
  const abiJSON = `[{"name":"getPeerData","type":"function","inputs":[{"name":"minerID","type":"uint64"}],"outputs":[{"name":"","type":"tuple","components":[{"name":"peerID","type":"string"},{"name":"signedMessage","type":"bytes"}]}]}]`

  // Parse ABI properly
  parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
  if err != nil {
    log.Fatalf("Failed to parse contract ABI: %v", err)
  }

  // Miner ID to query
  minerID := uint64(1234)

  // Encode the function call with the Miner ID
  callData, err := parsedABI.Pack("getPeerData", minerID)
  if err != nil {
    log.Fatalf("Failed to pack function call data: %v", err)
  }

  // Convert contract address to Filecoin format
  contractID := "<CONTRACT_ETH_ADDRESS>" // Replace with actual contract address

  to, err := ethtypes.ParseEthAddress(contractID)
  if err != nil {
    fmt.Println("failed to parse contract address: %w", err)
    return
  }

  toAddr, err := to.ToFilecoinAddress()
  if err != nil {
    fmt.Println("failed to convert Eth address to Filecoin address: %w", err)
    return
  }

  // Construct the Filecoin message
  msg := &types.Message{
    To:     toAddr,
    From:   toAddr, // from address doesn't matter
    Value:  types.NewInt(0),
    Method: builtin.MethodsEVM.InvokeContract,
    Params: callData,
  }

  // Execute the contract call on Lotus
  res, err := fullNode.StateCall(ctx, msg, types.EmptyTSK)
  if err != nil {
    log.Fatalf("StateCall failed: %v", err)
  }

  // Check if execution failed
  if res.MsgRct.ExitCode.IsError() {
    log.Fatalf("Smart contract call failed with exit code: %s", res.MsgRct.ExitCode.String())
  }

  // Decode the response correctly (tuple unpacking)
  var result WrappedPeerData
  err = parsedABI.UnpackIntoInterface(&result, "getPeerData", res.MsgRct.Return)
  if err != nil {
    log.Fatalf("Failed to unpack ABI data: %v", err)
  }

  // Print the decoded output
  output, _ := json.MarshalIndent(result.Result, "", "  ")
  fmt.Println(string(output))

}
```

## Deploying the contract
1. Mainnet
    ```shell
   npx hardhat deploy --network mainnet
   ```

2. Calibnet
    ```shell
   npx hardhat deploy --network calibnet
   ```

3. Devnet
    ```shell
   npx hardhat deploy --network devnet
   ```
   
## Testing
The contract has some builtin tests
```shell
make test
```