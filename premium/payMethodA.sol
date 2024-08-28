// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// Knowns risks: 
// - This contract fails in 2092 (overflow)
// - Only free and 500 & 2000 (x rate) payments are allowed. 
//   -- As the admin key is automatically used to set exchange rates, it should have limits. 
// - A wallet can only pay for 1 account UUID. 

contract PaymentContract {
    address public admin;
    address public fundsReceiver;
    uint256 public exchangeRate;
    uint256 public lastUpdateTimestamp;
    address public signerPublicKey; // The public key for verifying signatures

    struct PaymentRecord {
        uint16 uuid;
        uint16 daysSince2024AndLevel; // Combined daysSince2024 and level in a single uint16
    }

    mapping(address => PaymentRecord) public paymentRecords;

    constructor(address inFundsReceiver, address inSignerPublicKey) {
        require(inFundsReceiver != address(0), "Invalid funds receiver address");
        require(inSignerPublicKey != address(0), "Invalid signer public key");
        admin = msg.sender;
        fundsReceiver = inFundsReceiver;
        signerPublicKey = inSignerPublicKey;
    }

    function setExchangeRate(uint256 newRate, uint256 newTimestamp, bytes memory signature) external {
        require(block.timestamp <= newTimestamp + 60 minutes, "Exchange rate update is too old");

        // Verify the signature directly without the "Ethereum Signed Message" prefix
        bytes32 messageHash = keccak256(abi.encodePacked(newRate, newTimestamp));
        require(recoverSigner(messageHash, signature) == signerPublicKey, "Invalid signature");

        // Update the exchange rate and timestamp if the signature is valid
        exchangeRate = newRate;
        lastUpdateTimestamp = newTimestamp;
    }

    function pay(uint16 uuid) external payable {
        require(block.timestamp <= lastUpdateTimestamp + 90 minutes, "Exchange rate is outdated");

        uint256 level2Amount = exchangeRate * 2000;
        require(
            msg.value == level2Amount / 4 || msg.value == level2Amount,
            "Incorrect payment amount"
        );

        // Store the payment record, combining daysSince2024 and level
        paymentRecords[msg.sender] = PaymentRecord({
            uuid: uuid,
            daysSince2024AndLevel: uint16(
                ((block.timestamp - 1704067200) / 1 days) << 1 | 
                (msg.value == level2Amount ? 1 : 0)
            )
        });

        // Forward the funds to the fundsReceiver address
        payable(fundsReceiver).transfer(msg.value);
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