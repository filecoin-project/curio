// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// Knowns risks: 
// - This contract fails in 2092 (overflow)
// - Only free and 500 & 2000 (x rate) payments are allowed. 
// - Someone could downgrade someone else by paying 500 for their UUID. 
//  -- We could see event logs to see this having happened from the emits. 
// - A clever person could overwhelm uint16, 
//     but this would be expensive if we only consider it valid if paid. 

contract CurioMembership {
    address public admin;
    address public fundsReceiver;
    uint256 public exchangeRate;
    uint256 public lastUpdateTimestamp;
    address public signerPublicKey; // The public key for verifying signatures

    struct PaymentRecord {
        uint16 daysSince2024AndLevel; // Combines daysSince2024 and level
    }

    // Mapping from UUID to PaymentRecord instead of from address to PaymentRecord
    mapping(uint16 => PaymentRecord) public paymentRecords;
    
    // Define an event to emit the amount and UUID
    event PaymentMade(uint16 indexed uuid, uint256 amount, uint8 level);
    event FundsReceiverChanged(address indexed oldReceiver, address indexed newReceiver);
    event ExchangeRateUpdated(uint256 newRate, uint256 newTimestamp);

    constructor(address inFundsReceiver, address inSignerPublicKey, uint256 inExchangeRate, uint256 inLastUpdateTimestamp) {
        require(inFundsReceiver != address(0), "Invalid funds receiver address");
        require(inSignerPublicKey != address(0), "Invalid signer public key");
        admin = msg.sender;
        fundsReceiver = inFundsReceiver;
        signerPublicKey = inSignerPublicKey;
        
        // For testing purposes, set the exchange rate and last update timestamp.
        exchangeRate = inExchangeRate;
        lastUpdateTimestamp = inLastUpdateTimestamp;
    }

    function changeFundsReceiver(address _newReceiver) public {
        require(msg.sender == admin, "Only admin can perform this action");
        require(_newReceiver != address(0), "New receiver cannot be the zero address");
        emit FundsReceiverChanged(fundsReceiver, _newReceiver);
        fundsReceiver = _newReceiver;
    }

    function setExchangeRate(uint256 newRate, uint256 newTimestamp, bytes memory signature) external {
        require(block.timestamp <= newTimestamp + 35 minutes, "Exchange rate update is too old");

        // Verify the signature directly without the "Ethereum Signed Message" prefix
        bytes32 messageHash = keccak256(abi.encodePacked(newRate, newTimestamp));
        require(recoverSigner(messageHash, signature) == signerPublicKey, "Invalid signature");

        // Update the exchange rate and timestamp if the signature is valid
        exchangeRate = newRate;
        lastUpdateTimestamp = newTimestamp;

        // Emit the ExchangeRateUpdated event
        emit ExchangeRateUpdated(newRate, newTimestamp);
    }

    function pay(uint16 uuid) external payable {
        require(block.timestamp <= lastUpdateTimestamp + 40 minutes, "Exchange rate is outdated");

        uint256 level1Amount = exchangeRate * 500;
        uint256 level2Amount = exchangeRate * 2000;
        uint8 level; // Variable to store the payment level
        if (msg.value == level2Amount) {
            level = 2;
        } else if (msg.value == level1Amount) {
            level = 1;
        } else {
            revert("Incorrect payment amount");
        }

        // Store the payment record, combining daysSince2024 and level
        paymentRecords[uuid] = PaymentRecord({
            daysSince2024AndLevel: uint16(
                ((block.timestamp - 1704067200) / 1 days) << 1 | 
                (msg.value == level2Amount ? 1 : 0)
            )
        });

        // Forward the funds to the fundsReceiver address
        payable(fundsReceiver).transfer(msg.value);

        // Emit the PaymentMade event
        emit PaymentMade(uuid, msg.value, level);
    }

    // Helper function to recover the signer from the signature
    function recoverSigner(bytes32 messageHash, bytes memory signature) internal pure returns (address) {
        (bytes32 r, bytes32 s, uint8 v) = splitSignature(signature);
        return ecrecover(messageHash, v, r, s);
    }

    // Helper function to split the signature into r, s, and v
    function splitSignature(bytes memory sig) internal pure returns (bytes32 r, bytes32 s, uint8 v) {
        require(sig.length == 65, "Invalid signature length");

        assembly {
            r := mload(add(sig, 32))
            s := mload(add(sig, 64))
            v := byte(0, mload(add(sig, 96)))
        }

        return (r, s, v);
    }
}