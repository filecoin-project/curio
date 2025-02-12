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

    uint256 public constant DST_CLIENT_VOUCHER = 0xc896443a8cbf4cb49ec50beef800b5b2a3764c14b7c5454e8248e1f195b1c000;
    uint256 public constant DST_PROVIDER_VOUCHER = 0xc896443a8cbf4cb49ec50beef800b5b2a3764c14b7c5454e8248e1f195b1c001;

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
    
    // --- Events ---
    event Deposit(uint64 indexed clientID, uint256 amount);
    event ClientVoucherRedeemed(uint64 indexed clientID, uint256 cumulativeAmount, uint64 nonce);
    event ProviderVoucherRedeemed(uint64 indexed providerID, uint256 cumulativeAmount, uint64 nonce);
    event ServiceWithdrawal(uint256 amount);
    event ServiceDeposit(uint256 amount);
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
            address(this),
            clientID,
            CommonTypes.FilActorId.unwrap(serviceActor),
            cumulativeAmount,
            nonce,
            DST_CLIENT_VOUCHER
        );
    }

    /// @notice Creates the voucher bytes for a provider voucher.
    function createProviderVoucher(
        uint64 providerID,
        uint256 cumulativeAmount,
        uint64 nonce
    ) external view returns (bytes memory) {
        return abi.encodePacked(
            address(this),
            CommonTypes.FilActorId.unwrap(serviceActor),
            providerID,
            cumulativeAmount,
            nonce,
            DST_PROVIDER_VOUCHER
        );
    }

    /// @notice Validates a client voucher signature given its inputs.
    function validateClientVoucher(
        uint64 clientID,
        uint256 cumulativeAmount,
        uint64 nonce,
        bytes calldata signature
    ) external view returns (bool) {
        bytes memory voucher = abi.encodePacked(
            address(this),
            clientID,
            CommonTypes.FilActorId.unwrap(serviceActor),
            cumulativeAmount,
            nonce
        );
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
        bytes memory voucher = abi.encodePacked(
            address(this),
            CommonTypes.FilActorId.unwrap(serviceActor),
            providerID,
            cumulativeAmount,
            nonce
        );
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
    
    /// @notice Allows the service to withdraw any residual funds from the service pool.
    function serviceWithdraw(uint256 amount) external nonReentrant {
        (bool svcSuccess, uint64 callerID) = FilAddressIdConverter.getActorID(msg.sender);
        require(svcSuccess && callerID == CommonTypes.FilActorId.unwrap(serviceActor), "Only service may withdraw");
        require(amount <= servicePool, "Amount exceeds service pool");
        
        servicePool -= amount;
        int256 sendExit = SendAPI.send(FilAddresses.fromActorID(CommonTypes.FilActorId.unwrap(serviceActor)), amount);
        require(sendExit == 0, "Failed to withdraw FIL");
        
        emit ServiceWithdrawal(amount);
    }

    /// @notice Allows the service to deposit funds into the service pool.
    function serviceDeposit() external payable nonReentrant {
        (bool svcSuccess, uint64 callerID) = FilAddressIdConverter.getActorID(msg.sender);
        require(svcSuccess && callerID == CommonTypes.FilActorId.unwrap(serviceActor), "Only service may deposit");
        require(msg.value > 0, "Must deposit non-zero amount");
        
        servicePool += msg.value;
        
        emit ServiceDeposit(msg.value);
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
