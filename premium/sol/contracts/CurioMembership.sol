// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";

// Knowns risks: 
// - This contract fails in 2092 (overflow)
// - Only free and 500 & 2000 (x rate) payments are allowed. 
// - Someone could downgrade someone else by paying 500 for their UUID. 
//  -- We could see event logs to see this having happened from the emits. 
// - Tiny chance of colllision in UUIDs, mitigated by different addresses if needed.

contract CurioMembership {
    address public admin;
    address public fundsReceiver;
    uint256 public exchangeRate;
    uint256 public lastUpdateTimestamp;
    address public signerPublicKey; // The public key for verifying signatures

    struct PaymentRecord {
        uint16 daysSince2024AndLevel; // Combines daysSince2024 and level
        address payer;
    }

    // Mapping from UUID to PaymentRecord instead of from address to PaymentRecord
    mapping(uint256 => PaymentRecord) public paymentRecords;
    
    // Define an event to emit the amount and UUID
    event PaymentMade(uint256 indexed uuid, address payer, uint256 amount, uint8 level);
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

    function setExchangeRate(bytes memory rateAndTimestampBytes, bytes memory signature) external {
        uint256 rateAndTimestamp;
        assembly {
            rateAndTimestamp := mload(add(rateAndTimestampBytes, 32))
        }
        uint256 newTimestamp = rateAndTimestamp & 0xFFFFFFFFFFFFFFFF;
        require(block.timestamp <= newTimestamp + 35 minutes, "Exchange rate update is too old");

        bytes32 hashedMessage = getEthSignedMessageHash(keccak256(abi.encodePacked(rateAndTimestampBytes)));
        require(ECDSA.recover(hashedMessage, signature) == signerPublicKey, "Invalid signature");

        // Update the exchange rate and timestamp if the signature is valid
        exchangeRate = rateAndTimestamp >> 64;
        lastUpdateTimestamp = newTimestamp;

        // Emit the ExchangeRateUpdated event
        emit ExchangeRateUpdated(exchangeRate, newTimestamp);
    }

    function getEthSignedMessageHash(bytes32 _messageHash) public pure returns (bytes32) {
        // This replicates the behavior of ECDSA.toEthSignedMessageHash
        return keccak256(
            abi.encodePacked("\x19Ethereum Signed Message:\n32", _messageHash)
        );
    }

    function adminUpdateMapping(uint256 uuid, uint8 level, address payer) external {
        require(msg.sender == admin, "Only admin can perform this action");
        updateRecord(uuid, level, payer);
        emit PaymentMade(uuid, msg.sender, 0, level);
    }

    function pay(uint256 uuid) external payable {
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

        updateRecord(uuid, level, msg.sender);

        // Forward the funds to the fundsReceiver address
        payable(fundsReceiver).transfer(msg.value);

        // Emit the PaymentMade event
        emit PaymentMade(uuid, msg.sender, msg.value, level);
    }

    function updateRecord(uint256 uuid, uint8 level, address wallet) internal {
        // Store the payment record, combining daysSince2024 and level
        paymentRecords[uuid] = PaymentRecord({
            daysSince2024AndLevel: uint16(
                ((block.timestamp - 1704067200) / 1 days) << 1 | 
                level
            ),
            payer: wallet
        });
    }
}