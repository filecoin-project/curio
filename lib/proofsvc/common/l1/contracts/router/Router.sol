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

contract Router is ReentrancyGuard {
    using FilAddresses for CommonTypes.FilAddress;
    using FilAddressIdConverter for address;

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
    
    constructor(CommonTypes.FilActorId _serviceActor) {
        // The service actor is provided as a FilActorId (f0) at deployment.
        require(CommonTypes.FilActorId.unwrap(_serviceActor) > 0, "Invalid service actor");
        serviceActor = _serviceActor;
    }
    
    /// @notice Client deposits FIL into the router.
    /// The caller’s EVM address is normalized (via FilAddressIdConverter) into a Filecoin actor ID.
    function deposit() external payable nonReentrant {
        require(msg.value > 0, "Deposit cannot be zero");
        
        // Convert msg.sender (an EVM address) to a Filecoin actor ID.
        // The helper "mustNormalize" will revert if conversion fails.
        address normalized = msg.sender.mustNormalize();
        (bool idSuccess, uint64 clientID) = FilAddressIdConverter.getActorID(normalized);
        require(idSuccess, "Unable to resolve actor ID");
        
        clientBalance[clientID] += msg.value;
        emit Deposit(clientID, msg.value);
    }
    
    /// @notice Service redeems a client voucher off-chain signed by the client.
    /// @param clientID the client actor ID (f0) from which funds are drawn.
    /// @param cumulativeAmount the cumulative amount authorized (attoFIL)
    /// @param nonce voucher sequence number; must be strictly increasing.
    /// @param signature raw signature bytes over the CBOR-encoded voucher message.
    function redeemClientVoucher(
        uint64 clientID,
        uint256 cumulativeAmount,
        uint64 nonce,
        bytes calldata signature
    ) external nonReentrant {
        // Only service (or a designated caller) can call this.
        // For simplicity, we check that msg.sender normalizes to serviceActor.
        address caller = msg.sender.mustNormalize();
        (bool svcSuccess, uint64 callerID) = FilAddressIdConverter.getActorID(caller);
        require(svcSuccess && callerID == CommonTypes.FilActorId.unwrap(serviceActor), "Only service may redeem vouchers");
        
        require(nonce > clientLastNonce[clientID], "Voucher nonce too low");
        require(cumulativeAmount >= clientVoucherRedeemed[clientID], "Cumulative amount decreased");
        
        uint256 increment = cumulativeAmount - clientVoucherRedeemed[clientID];
        require(clientBalance[clientID] >= increment, "Insufficient client balance");
        
        // Construct the voucher message to authenticate.
        // For example, encode: Router address || clientID || serviceActor || cumulativeAmount || nonce.
        bytes memory message = abi.encodePacked(
            address(this),
            clientID,
            CommonTypes.FilActorId.unwrap(serviceActor),
            cumulativeAmount,
            nonce
        );
        
        // Verify the client signature using AccountAPI.
        AccountTypes.AuthenticateMessageParams memory authParams = AccountTypes.AuthenticateMessageParams({
            signature: signature,
            message: message
        });
        // This call reverts if authentication fails.
        int256 authExit = AccountAPI.authenticateMessage(CommonTypes.FilActorId.wrap(clientID), authParams);
        require(authExit == 0, "Client voucher signature invalid");
        
        // Update client state and move funds.
        clientBalance[clientID] -= increment;
        clientVoucherRedeemed[clientID] = cumulativeAmount;
        clientLastNonce[clientID] = nonce;
        servicePool += increment;
        
        emit ClientVoucherRedeemed(clientID, cumulativeAmount, nonce);
    }
    
    /// @notice Provider redeems a service voucher off-chain signed by the service.
    /// @param providerID the provider actor ID (f0) to which funds will be sent.
    /// @param cumulativeAmount the cumulative amount (attoFIL) authorized by service.
    /// @param nonce voucher sequence number; must be strictly increasing.
    /// @param signature raw signature bytes over the voucher message.
    function redeemProviderVoucher(
        uint64 providerID,
        uint256 cumulativeAmount,
        uint64 nonce,
        bytes calldata signature
    ) external nonReentrant {
        // Anyone may call this function; however, voucher authenticity is checked.
        require(nonce > providerLastNonce[providerID], "Voucher nonce too low");
        require(cumulativeAmount >= providerVoucherRedeemed[providerID], "Cumulative amount decreased");
        
        uint256 increment = cumulativeAmount - providerVoucherRedeemed[providerID];
        require(servicePool >= increment, "Insufficient service pool");
        
        // Construct the voucher message: Router address || serviceActor || providerID || cumulativeAmount || nonce.
        bytes memory message = abi.encodePacked(
            address(this),
            CommonTypes.FilActorId.unwrap(serviceActor),
            providerID,
            cumulativeAmount,
            nonce
        );
        
        // Verify service signature.
        AccountTypes.AuthenticateMessageParams memory authParams = AccountTypes.AuthenticateMessageParams({
            signature: signature,
            message: message
        });
        int256 authExit = AccountAPI.authenticateMessage(serviceActor, authParams);
        require(authExit == 0, "Service voucher signature invalid");
        
        // Update provider state and transfer funds.
        servicePool -= increment;
        providerVoucherRedeemed[providerID] = cumulativeAmount;
        providerLastNonce[providerID] = nonce;
        
        // Use SendAPI to send FIL to the provider's address.
        // The provider's on-chain Filecoin address should be provided in f4 or other format.
        // Here we assume providerID can be converted to a FilAddress via FilAddresses.fromActorID.
        CommonTypes.FilAddress memory providerAddr = FilAddresses.fromActorID(providerID);
        int256 sendExit = SendAPI.send(providerAddr, increment);
        require(sendExit == 0, "Failed to send FIL to provider");
        
        emit ProviderVoucherRedeemed(providerID, cumulativeAmount, nonce);
    }
    
    /// @notice Allows the service to withdraw any residual funds from the service pool.
    function serviceWithdraw(uint256 amount) external nonReentrant {
        address caller = msg.sender.mustNormalize();
        (bool svcSuccess, uint64 callerID) = FilAddressIdConverter.getActorID(caller);
        require(svcSuccess && callerID == CommonTypes.FilActorId.unwrap(serviceActor), "Only service may withdraw");
        require(amount <= servicePool, "Amount exceeds service pool");
        
        servicePool -= amount;
        int256 sendExit = SendAPI.send(FilAddresses.fromActorID(CommonTypes.FilActorId.unwrap(serviceActor)), amount);
        require(sendExit == 0, "Failed to withdraw FIL");
        
        emit ServiceWithdrawal(amount);
    }
}
