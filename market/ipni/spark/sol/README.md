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

### **Ownership Management**

- **Transfer Ownership**
  ```solidity
  function transferOwnership(address newOwner) public onlyOwner;
  ```
  Allows the owner to transfer contract ownership to a new address.

---

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

This message is signed using the private key associated with the on-chain PeerID.

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
 * Fetch miner's PeerID and verify the signature of a message.
 * @param {string} minerAddress - Filecoin miner address
 * @param {Buffer} message - The message that was signed
 * @param {Buffer} signature - The signature to verify
 */
async function verifyMinerSignature(minerAddress, message, signature) {
  try {
    // Set up the Filecoin RPC client
    const rpcUrl = "http://127.0.0.1:1234/rpc/v0"; // Replace with your Filecoin Lotus RPC URL
    const connector = new HttpJsonRpcConnector({ url: rpcUrl });
    const lotusClient = new LotusClient(connector);

    // Step 1: Fetch miner info
    const minerInfo = await lotusClient.state.minerInfo(minerAddress);

    if (!minerInfo || !minerInfo.PeerId) {
      throw new Error("Could not retrieve PeerID from miner info.");
    }

    // Extract PeerID
    const { PeerId } = minerInfo;
    console.log("Retrieved PeerID:", PeerId);

    // Step 2: Decode PeerID to obtain the public key
    const decodedPeerID = multihashes.decode(Buffer.from(PeerId, "base64"));
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
  } catch (error) {
    console.error("Error during verification:", error.message);
    throw error;
  }
}
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

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type PeerData struct {
	PeerID        string `json:"peerID"`
	SignedMessage []byte `json:"signedMessage"`
}

func main() {
	client, err := ethclient.Dial("<RPC_URL>")
	if err != nil {
		log.Fatalf("Failed to connect to Ethereum client: %v", err)
	}

	contractAddress := common.HexToAddress("<CONTRACT_ADDRESS>")
	parsedABI, err := abi.JSON(strings.NewReader(<ABI_JSON>))
	if err != nil {
		log.Fatalf("Failed to parse contract ABI: %v", err)
	}

	callOpts := &bind.CallOpts{
		Pending: false,
	}

	minerID := uint64(1234)
	result, err := parsedABI.Pack("getPeerData", minerID)
	if err != nil {
		log.Fatalf("Failed to pack data: %v", err)
	}

	response := make([]interface{}, 2) // Matching the return type
	err = client.CallContract(callOpts, &response, contractAddress, result)
	if err != nil {
		log.Fatalf("Failed to call contract: %v", err)
	}

	peerData := PeerData{
		PeerID:        response[0].(string),
		SignedMessage: response[1].([]byte),
	}

	output, _ := json.MarshalIndent(peerData, "", "  ")
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