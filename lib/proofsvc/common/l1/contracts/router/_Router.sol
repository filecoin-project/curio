// SPDX-License-Identifier: MIT
pragma solidity ^0.8.17;

// Filecoin-Solidity high-level APIs and types
import "filecoin-solidity-api/contracts/v0.8/AccountAPI.sol";
import "filecoin-solidity-api/contracts/v0.8/SendAPI.sol";
import "filecoin-solidity-api/contracts/v0.8/PrecompilesAPI.sol";
import "filecoin-solidity-api/contracts/v0.8/types/CommonTypes.sol";
import "filecoin-solidity-api/contracts/v0.8/utils/FilAddresses.sol";
import "filecoin-solidity-api/contracts/v0.8/utils/FilAddressIdConverter.sol";

// OpenZeppelin reentrancy protection
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

contract _Router is ReentrancyGuard {
    using FilAddresses for CommonTypes.FilAddress;
    using FilAddressIdConverter for address;

    // --- Constants ---
    uint256 public constant DST_CLIENT_VOUCHER = 0xc896443a8cbf4cb49ec50beef800b5b2a3764c14b7c5454e8248e1f195b1c000;
    uint256 public constant DST_PROVIDER_VOUCHER = 0xc896443a8cbf4cb49ec50beef800b5b2a3764c14b7c5454e8248e1f195b1c001;

    // Withdraw window (4 hours)
    uint32 public constant WITHDRAW_WINDOW = 10 minutes;

    // --- Roles ---
    // service actor is fixed at deployment (as an ID)
    CommonTypes.FilActorId public serviceActor;
    
    // mapping from client actor IDs to their deposited balance (in attoFIL)
    mapping(uint64 => uint256) public clientBalance;
    // mapping from client actor ID to highest redeemed voucher amount (cumulative)
    mapping(uint64 => uint256) public clientVoucherRedeemed;
    // mapping from client actor ID to last voucher nonce redeemed
    mapping(uint64 => uint64) public clientLastNonce;
    
    // service pool balance aggregated from clients
    uint256 public servicePool;
    
    // mapping from provider actor IDs to highest redeemed voucher amount from service (cumulative)
    mapping(uint64 => uint256) public providerVoucherRedeemed;
    // mapping from provider actor ID to last voucher nonce redeemed
    mapping(uint64 => uint64) public providerLastNonce;
    
    // --- Withdrawal Request Structs ---
    struct WithdrawRequest {
        uint256 amount;
        uint256 timestamp;
    }
    // Clients may have a pending withdrawal request.
    mapping(uint64 => WithdrawRequest) public clientWithdrawRequests;
    // The service has at most one pending withdrawal request.
    WithdrawRequest public serviceWithdrawRequest;

    // --- Events ---
    event Deposit(uint64 indexed clientID, uint256 amount);
    event ClientVoucherRedeemed(uint64 indexed clientID, uint256 cumulativeAmount, uint64 nonce);
    event ProviderVoucherRedeemed(uint64 indexed providerID, uint256 cumulativeAmount, uint64 nonce);
    event ServiceDeposit(uint256 amount);

    // New events for the withdrawal process:
    event ClientWithdrawalInitiated(uint64 indexed clientID, uint256 amount, uint256 readyTime);
    event ClientWithdrawalCompleted(uint64 indexed clientID, uint256 amount);
    event ClientWithdrawalCanceled(uint64 indexed clientID, uint256 amount);

    event ServiceWithdrawalInitiated(uint256 amount, uint256 readyTime);
    event ServiceWithdrawalCompleted(uint256 amount);
    event ServiceWithdrawalCanceled(uint256 amount);

    constructor(CommonTypes.FilActorId _serviceActor) {
        require(CommonTypes.FilActorId.unwrap(_serviceActor) > 0, "Invalid service actor");
        serviceActor = _serviceActor;
    }
    
    /// @notice Client deposits FIL into the router.
    function deposit() external payable nonReentrant {
        require(msg.value > 0, "Deposit cannot be zero");
        
        // Resolve caller’s Filecoin actor ID.
        (bool idSuccess, uint64 clientID) = FilAddressIdConverter.getActorID(msg.sender);
        require(idSuccess, "Unable to resolve actor ID");
        
        clientBalance[clientID] += msg.value;
        emit Deposit(clientID, msg.value);
    }
    
    /// @notice Creates the voucher bytes for a client voucher.
    function createClientVoucher(
        uint64 clientID,
        uint256 cumulativeAmount,
        uint64 nonce
    ) external view returns (bytes memory) {
        return abi.encodePacked(
            DST_CLIENT_VOUCHER,
            address(this),
            clientID,
            CommonTypes.FilActorId.unwrap(serviceActor),
            cumulativeAmount,
            nonce
        );
    }

    /// @notice Creates the voucher bytes for a provider voucher.
    function createProviderVoucher(
        uint64 providerID,
        uint256 cumulativeAmount,
        uint64 nonce
    ) external view returns (bytes memory) {
        return abi.encodePacked(
            DST_PROVIDER_VOUCHER,
            address(this),
            CommonTypes.FilActorId.unwrap(serviceActor),
            providerID,
            cumulativeAmount,
            nonce
        );
    }

    /// @notice Validates a client voucher signature given its inputs.
    function validateClientVoucher(
        uint64 clientID,
        uint256 cumulativeAmount,
        uint64 nonce,
        bytes calldata signature
    ) external view returns (bool) {
        bytes memory voucher = this.createClientVoucher(clientID, cumulativeAmount, nonce);
        AccountTypes.AuthenticateMessageParams memory authParams = AccountTypes.AuthenticateMessageParams({
            signature: signature,
            message: voucher
        });
        int256 authExit = AccountAPI.authenticateMessage(CommonTypes.FilActorId.wrap(clientID), authParams);
        return authExit == 0;
    }

    /// @notice Validates a provider voucher signature given its inputs.
    function validateProviderVoucher(
        uint64 providerID,
        uint256 cumulativeAmount,
        uint64 nonce,
        bytes calldata signature
    ) external view returns (bool) {
        bytes memory voucher = this.createProviderVoucher(providerID, cumulativeAmount, nonce);
        AccountTypes.AuthenticateMessageParams memory authParams = AccountTypes.AuthenticateMessageParams({
            signature: signature,
            message: voucher
        });
        int256 authExit = AccountAPI.authenticateMessage(serviceActor, authParams);
        return authExit == 0;
    }

    /// @notice Service redeems a client voucher off-chain signed by the client.
    function redeemClientVoucher(
        uint64 clientID,
        uint256 cumulativeAmount,
        uint64 nonce,
        bytes calldata signature
    ) external nonReentrant {
        (bool svcSuccess, uint64 callerID) = FilAddressIdConverter.getActorID(msg.sender);
        require(svcSuccess && callerID == CommonTypes.FilActorId.unwrap(serviceActor), "Only service may redeem vouchers");
        
        require(nonce > clientLastNonce[clientID], "Voucher nonce too low");
        require(cumulativeAmount >= clientVoucherRedeemed[clientID], "Cumulative amount decreased");
        
        uint256 increment = cumulativeAmount - clientVoucherRedeemed[clientID];
        require(clientBalance[clientID] >= increment, "Insufficient client balance");
        
        // Use the external voucher creation method.
        bytes memory voucherBytes = this.createClientVoucher(clientID, cumulativeAmount, nonce);
        
        AccountTypes.AuthenticateMessageParams memory authParams = AccountTypes.AuthenticateMessageParams({
            signature: signature,
            message: voucherBytes
        });
        int256 authExit = AccountAPI.authenticateMessage(CommonTypes.FilActorId.wrap(clientID), authParams);
        require(authExit == 0, "Client voucher signature invalid");
        
        clientBalance[clientID] -= increment;
        clientVoucherRedeemed[clientID] = cumulativeAmount;
        clientLastNonce[clientID] = nonce;
        servicePool += increment;
        
        emit ClientVoucherRedeemed(clientID, cumulativeAmount, nonce);
    }
    
    /// @notice Provider redeems a service voucher off-chain signed by the service.
    function redeemProviderVoucher(
        uint64 providerID,
        uint256 cumulativeAmount,
        uint64 nonce,
        bytes calldata signature
    ) external nonReentrant {
        require(nonce > providerLastNonce[providerID], "Voucher nonce too low");
        require(cumulativeAmount >= providerVoucherRedeemed[providerID], "Cumulative amount decreased");
        
        uint256 increment = cumulativeAmount - providerVoucherRedeemed[providerID];
        require(servicePool >= increment, "Insufficient service pool amount");
        
        // Use the external voucher creation method.
        bytes memory voucherBytes = this.createProviderVoucher(providerID, cumulativeAmount, nonce);
        
        AccountTypes.AuthenticateMessageParams memory authParams = AccountTypes.AuthenticateMessageParams({
            signature: signature,
            message: voucherBytes
        });
        int256 authExit = AccountAPI.authenticateMessage(serviceActor, authParams);
        require(authExit == 0, "Service voucher signature invalid");
        
        servicePool -= increment;
        providerVoucherRedeemed[providerID] = cumulativeAmount;
        providerLastNonce[providerID] = nonce;
        
        CommonTypes.FilAddress memory providerAddr = FilAddresses.fromActorID(providerID);
        int256 sendExit = SendAPI.send(providerAddr, increment);
        require(sendExit == 0, "Failed to send FIL to provider");
        
        emit ProviderVoucherRedeemed(providerID, cumulativeAmount, nonce);
    }
    
    /// @notice Service deposits funds into the service pool.
    function serviceDeposit() external payable nonReentrant {
        (bool svcSuccess, uint64 callerID) = FilAddressIdConverter.getActorID(msg.sender);
        require(svcSuccess && callerID == CommonTypes.FilActorId.unwrap(serviceActor), "Only service may deposit");
        require(msg.value > 0, "Must deposit non-zero amount");
        
        servicePool += msg.value;
        
        emit ServiceDeposit(msg.value);
    }
    
    // ================================================================
    // New functions for withdrawal with a time delay (withdraw window)
    // ================================================================

    // --- Client Withdrawal ---
    /// @notice Initiate a withdrawal request from the client’s deposit.
    /// @param amount The amount to withdraw.
    function initiateClientWithdrawal(uint256 amount) external nonReentrant {
        (bool idSuccess, uint64 clientID) = FilAddressIdConverter.getActorID(msg.sender);
        require(idSuccess, "Unable to resolve actor ID");
        require(amount > 0, "Withdrawal amount must be > 0");
        require(amount <= clientBalance[clientID], "Insufficient client balance");

        clientWithdrawRequests[clientID] = WithdrawRequest({
            amount: amount,
            timestamp: block.timestamp
        });
        emit ClientWithdrawalInitiated(clientID, amount, block.timestamp + WITHDRAW_WINDOW);
    }

    /// @notice Complete a previously initiated client withdrawal after the withdraw window has elapsed.
    function completeClientWithdrawal() external nonReentrant {
        (bool idSuccess, uint64 clientID) = FilAddressIdConverter.getActorID(msg.sender);
        require(idSuccess, "Unable to resolve actor ID");

        WithdrawRequest memory request = clientWithdrawRequests[clientID];
        require(request.amount > 0, "No pending withdrawal request");
        require(block.timestamp >= request.timestamp + WITHDRAW_WINDOW, "Withdraw window not yet elapsed");
        require(clientBalance[clientID] >= request.amount, "Client balance insufficient");

        // Update state and send funds.
        clientBalance[clientID] -= request.amount;
        delete clientWithdrawRequests[clientID];

        CommonTypes.FilAddress memory clientAddr = FilAddresses.fromActorID(clientID);
        int256 sendExit = SendAPI.send(clientAddr, request.amount);
        require(sendExit == 0, "Failed to send FIL to client");

        emit ClientWithdrawalCompleted(clientID, request.amount);
    }

    /// @notice Cancel a pending client withdrawal request.
    function cancelClientWithdrawal() external nonReentrant {
        (bool idSuccess, uint64 clientID) = FilAddressIdConverter.getActorID(msg.sender);
        require(idSuccess, "Unable to resolve actor ID");

        WithdrawRequest memory request = clientWithdrawRequests[clientID];
        require(request.amount > 0, "No pending withdrawal request");

        delete clientWithdrawRequests[clientID];

        emit ClientWithdrawalCanceled(clientID, request.amount);
    }

    // --- Service Withdrawal ---
    /// @notice Initiate a withdrawal request from the service pool.
    /// @param amount The amount to withdraw.
    function initiateServiceWithdrawal(uint256 amount) external nonReentrant {
        (bool svcSuccess, uint64 callerID) = FilAddressIdConverter.getActorID(msg.sender);
        require(svcSuccess && callerID == CommonTypes.FilActorId.unwrap(serviceActor), "Only service may initiate withdrawal");
        require(amount > 0, "Withdrawal amount must be > 0");
        require(amount <= servicePool, "Amount exceeds service pool");

        serviceWithdrawRequest = WithdrawRequest({
            amount: amount,
            timestamp: block.timestamp
        });
        emit ServiceWithdrawalInitiated(amount, block.timestamp + WITHDRAW_WINDOW);
    }

    /// @notice Complete a previously initiated service withdrawal after the withdraw window has elapsed.
    function completeServiceWithdrawal() external nonReentrant {
        (bool svcSuccess, uint64 callerID) = FilAddressIdConverter.getActorID(msg.sender);
        require(svcSuccess && callerID == CommonTypes.FilActorId.unwrap(serviceActor), "Only service may complete withdrawal");

        WithdrawRequest memory request = serviceWithdrawRequest;
        require(request.amount > 0, "No pending withdrawal request");
        require(block.timestamp >= request.timestamp + WITHDRAW_WINDOW, "Withdraw window not yet elapsed");
        require(servicePool >= request.amount, "Service pool balance insufficient");

        // Update state and send funds.
        servicePool -= request.amount;
        delete serviceWithdrawRequest;

        CommonTypes.FilAddress memory svcAddr = FilAddresses.fromActorID(CommonTypes.FilActorId.unwrap(serviceActor));
        int256 sendExit = SendAPI.send(svcAddr, request.amount);
        require(sendExit == 0, "Failed to send FIL to service");

        emit ServiceWithdrawalCompleted(request.amount);
    }

    /// @notice Cancel a pending service withdrawal request.
    function cancelServiceWithdrawal() external nonReentrant {
        (bool svcSuccess, uint64 callerID) = FilAddressIdConverter.getActorID(msg.sender);
        require(svcSuccess && callerID == CommonTypes.FilActorId.unwrap(serviceActor), "Only service may cancel withdrawal");

        WithdrawRequest memory request = serviceWithdrawRequest;
        require(request.amount > 0, "No pending withdrawal request");

        delete serviceWithdrawRequest;

        emit ServiceWithdrawalCanceled(request.amount);
    }
    
    /// @notice Returns the state for a given client.
    function getClientState(uint64 clientID) external view returns (uint256 balance, uint256 voucherRedeemed, uint64 lastNonce) {
        return (clientBalance[clientID], clientVoucherRedeemed[clientID], clientLastNonce[clientID]);
    }
    
    /// @notice Returns the state for a given provider.
    function getProviderState(uint64 providerID) external view returns (uint256 voucherRedeemed, uint64 lastNonce) {
        return (providerVoucherRedeemed[providerID], providerLastNonce[providerID]);
    }
    
    /// @notice Returns the service state.
    function getServiceState() external view returns (CommonTypes.FilActorId, uint256) {
        return (serviceActor, servicePool);
    }
}
