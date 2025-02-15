// SPDX-License-Identifier: MIT
pragma solidity ^0.8.17;

import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";

interface IAxelarGateway {
    function callContract(
        string calldata destinationChain,
        string calldata destinationAddress,
        bytes calldata payload
    ) external;
}

interface IAxelarGasService {
    function payNativeGasForContractCall(
        address sourceAddress,
        string calldata destinationChain,
        string calldata destinationAddress,
        bytes calldata payload,
        address refundAddress
    ) external payable;
}

abstract contract AxelarExecutable {
    IAxelarGateway public gateway;

    constructor(address _gateway) {
        require(_gateway != address(0), "Invalid gateway");
        gateway = IAxelarGateway(_gateway);
    }

    function _execute(bytes calldata sourceAddress, string calldata sourceChain, bytes calldata payload) internal virtual;

    function execute(
        string calldata sourceChain,
        string calldata sourceAddress,
        bytes calldata payload
    ) external {
        require(msg.sender == address(gateway), "Not Axelar gateway");
        _execute(bytes(sourceAddress), sourceChain, payload);
    }
}

contract CurioMembership is AxelarExecutable {
    using ECDSA for bytes32;

    /*------------------------------------------------------------
    | FIL Payment Logic
    ------------------------------------------------------------*/
    address public serviceProvider;
    uint256 public exchangeRateGLOBAL;
    uint256 public updatedTimeGLOBAL;
    address public signerPublicKeyGLOBAL;
    uint256 public filPaymentCounter;
    uint256 public stableCoinPaymentCounter;

    struct FilPaymentRecord {
        string machineId;
        address payer;
        uint256 timestamp;
        uint256 amount;
    }

    // filPaymentCounter -> FilPaymentRecord
    mapping(uint256 => FilPaymentRecord) public FilPaymentRecords;

    event PaymentMade(string indexed machineId, address payer, uint256 amount);
    event FundsReceiverChanged(address indexed oldReceiver, address indexed newReceiver);
    event ExchangeRateUpdated(uint256 newRate, uint256 newTimestamp);

    /*------------------------------------------------------------
    | Cross-Chain Data & Logic
    ------------------------------------------------------------*/

    // Request structure for cross-chain instructions
    struct StableCoinPaymentRecord {
        string machineId;
        uint256 timestamp;
        address payer;
        address token;
        uint256 amount;
        string destinationChain;
        bool executed;
    }

    mapping(uint256 => StableCoinPaymentRecord) public StableCoinPaymentRecords;

    // Whitelist chains and their destination contract addresses
    mapping(string => string) public allowedChains; // chainName -> contractAddress(string). This is address of StableCurioPayment contract.

    address public axelarGasService;

    event CrossChainInstructionSent(
        uint256 indexed requestId,
        string machineId,
        address indexed user,
        address token,
        uint256 amount,
        string destinationChain
    );

    event CrossChainRequestExecuted(uint256 indexed requestId, bool success);

    //inSignerPublicKey:
    //A valid Filecoin address that matches the public key your off-chain signer uses.
    //
    //_axelarGateway:
    //The Axelar Gateway contract address on the Filecoin network. Check Axelar’s documentation for the correct address. This ensures your contract can call callContract() to send messages to other chains.
    //
    //_axelarGasService:
    //The Axelar Gas Service contract address on the Filecoin network. Also found in Axelar’s documentation. This is required to pay gas fees for cross-chain execution.
    constructor(
        address inSignerPublicKey,
        address _axelarGateway,
        address _axelarGasService
    ) AxelarExecutable(_axelarGateway) {
        require(inSignerPublicKey != address(0), "Invalid signer public key");
        require(_axelarGasService != address(0), "Invalid gas service");

        serviceProvider = msg.sender;
        signerPublicKeyGLOBAL = inSignerPublicKey;
        axelarGasService = _axelarGasService;
    }

    /*------------------------------------------------------------
    | Ownership and Rates
    ------------------------------------------------------------*/
    modifier onlyServiceProvider() {
        require(msg.sender == serviceProvider, "Not service provider");
        _;
    }

    function setExchangeRate(uint256 rateAndTimestamp, bytes memory signature) public {
        uint256 newTimestamp = rateAndTimestamp & 0xFFFFFFFFFFFFFFFF;
        require(block.timestamp <= newTimestamp + 35 minutes, "Exchange rate update too old");

        bytes32 hashedMessage = getEthSignedMessageHash(keccak256(abi.encodePacked(rateAndTimestamp)));
        require(hashedMessage.recover(signature) == signerPublicKeyGLOBAL, "Invalid signature");

        exchangeRateGLOBAL = rateAndTimestamp >> 64;
        updatedTimeGLOBAL = newTimestamp;

        emit ExchangeRateUpdated(exchangeRateGLOBAL, newTimestamp);
    }

    function getEthSignedMessageHash(bytes32 _messageHash) public pure returns (bytes32) {
        return keccak256(
            abi.encodePacked("\x19Ethereum Signed Message:\n32", _messageHash)
        );
    }

    // User pays FIL according to the exchange rate
    function payWithFil(
        string memory machineId,
        uint256 rateAndTimestamp,
        bytes memory signature
    ) external payable {
        setExchangeRate(rateAndTimestamp, signature);

        // Example calculation: 1000 * exchangeRateGLOBAL = required FIL
        uint256 amount = exchangeRateGLOBAL * 1000; // TODO: Update this if we want to take quarterly payments
        require(msg.value == amount, "Incorrect payment amount");

        updateRecord(machineId, msg.sender, msg.value);

        emit PaymentMade(machineId, msg.sender, msg.value);
    }

    function updateRecord(string memory machineId, address wallet, uint256 amount) internal {
        filPaymentCounter++;
        FilPaymentRecords[filPaymentCounter] = FilPaymentRecord({
            machineId: machineId,
            payer: wallet,
            timestamp: block.timestamp,
            amount: amount
        });
    }

    /*------------------------------------------------------------
    | Allowed Chains Management
    ------------------------------------------------------------*/
    function addAllowedChain(string memory chainName, string memory contractAddressStr) external onlyServiceProvider {
        allowedChains[chainName] = contractAddressStr;
    }

    function removeAllowedChain(string memory chainName) external onlyServiceProvider {
        delete allowedChains[chainName];
    }

    /*------------------------------------------------------------
    | Cross-Chain Message Sending
    ------------------------------------------------------------*/

    // Before calling this, ensure:
    // 1. The user has already approved the token to the destination contract on that chain.

    // Anyone can call this now to trigger cross-chain instruction after they've done necessary steps.
    function payWithStableCoin(
        string memory machineId,
        address user,
        address token,
        uint256 amount,
        string memory destinationChain
    ) external payable {
        require(bytes(allowedChains[destinationChain]).length > 0, "Destination not allowed");

        stableCoinPaymentCounter++;
        StableCoinPaymentRecords[stableCoinPaymentCounter] = StableCoinPaymentRecord({
            machineId: machineId,
            timestamp: block.timestamp,
            payer: user,
            token: token,
            amount: amount,
            destinationChain: destinationChain,
            executed: false
        });

        // Encode payload: (uint256 requestId, string machineId, address user, address token, uint256 amount)
        bytes memory payload = abi.encode(stableCoinPaymentCounter, machineId, user, token, amount);

        // The caller provides FIL to pay Axelar’s gas service
        IAxelarGasService(axelarGasService).payNativeGasForContractCall{value: msg.value}(
            address(this),
            destinationChain,
            allowedChains[destinationChain],
            payload,
            msg.sender
        );

        // At this point, Axelar gas should be paid separately by calling payAxelarGasForInstruction
        // If not paid, the call may fail or never execute on destination.
        gateway.callContract(destinationChain, allowedChains[destinationChain], payload);

        emit CrossChainInstructionSent(stableCoinPaymentCounter, machineId, user, token, amount, destinationChain);
    }

    /*------------------------------------------------------------
    | Handle Callback from Destination Chain
    ------------------------------------------------------------*/
    // Destination chains will call back this contract once they've done transferFrom
    // Payload: (uint256 requestId, bool success)
    function _execute(bytes calldata sourceAddress, string calldata sourceChain, bytes calldata payload) internal override {
        // Decode callback
        (uint256 requestId, bool success) = abi.decode(payload, (uint256, bool));

        StableCoinPaymentRecord storage req = StableCoinPaymentRecords[requestId];
        require(!req.executed, "Already executed");
        // Optionally verify sourceChain and sourceAddress are what we expect (the contract we sent to)
        // For security:
        // Convert bytes(sourceAddress) back to string to verify if matches allowedChains[sourceChain]
        string memory sourceAddrStr = string(sourceAddress);
        require(keccak256(abi.encodePacked(sourceAddrStr)) == keccak256(abi.encodePacked(allowedChains[sourceChain])), "Invalid source callback");

        req.executed = success;
        emit CrossChainRequestExecuted(requestId, success);
    }

    // Transfer ownership if needed
    function transferOwnership(address newOwner) external onlyServiceProvider {
        require(newOwner != address(0), "Invalid new owner");
        serviceProvider = newOwner;
    }
}
