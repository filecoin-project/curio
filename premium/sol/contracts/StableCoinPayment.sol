// SPDX-License-Identifier: MIT
pragma solidity ^0.8.17;

import "@openzeppelin/contracts/token/ERC20/extensions/IERC20Metadata.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/utils/structs/EnumerableSet.sol";

interface IAxelarGatewayDest {
    function callContract(
        string calldata destinationChain,
        string calldata destinationAddress,
        bytes calldata payload
    ) external;
}

abstract contract AxelarExecutableDest {
    IAxelarGatewayDest public gateway;

    constructor(address _gateway) {
        require(_gateway != address(0), "Invalid gateway");
        gateway = IAxelarGatewayDest(_gateway);
    }

    function _execute(
        bytes calldata sourceAddress,
        string calldata sourceChain,
        bytes calldata payload
    ) internal virtual;

    function execute(
        string calldata sourceChain,
        string calldata sourceAddress,
        bytes calldata payload
    ) external {
        require(msg.sender == address(gateway), "Not Axelar gateway");
        _execute(bytes(sourceAddress), sourceChain, payload);
    }
}

contract CurioMembershipStableCoin is AxelarExecutableDest {
    using EnumerableSet for EnumerableSet.AddressSet;

    address public serviceProvider;
    EnumerableSet.AddressSet private allowedTokens;

    event TokenAllowed(address token);
    event TokenRemoved(address token);
    event FundsWithdrawn(address indexed serviceProvider, address token, uint256 amount);

    // Filecoin controller info
    string public filecoinCurioMembershipAddress;  // The address string of the FilecoinController
    // This is the contract address known to Axelar on Filecoin

    // _gateway: This is the Axelar gateway addres of the chain where this contract is deployed
    // __filecoinCurioMembershipAddress: The address of CurioMembership contract deployed on Filecoin
    constructor(
        address _gateway,
        string memory _filecoinCurioMembershipAddress
    ) AxelarExecutableDest(_gateway) {
        serviceProvider = msg.sender;
        filecoinCurioMembershipAddress = _filecoinCurioMembershipAddress;
    }

    modifier onlyServiceProvider() {
        require(msg.sender == serviceProvider, "Not service provider");
        _;
    }

    function allowToken(address token) external onlyServiceProvider {
        require(token != address(0), "Invalid token");
        // Attempt to fetch decimals; revert if token does not implement it.
        try IERC20Metadata(token).decimals() returns (uint8) {
            // decimals() call succeeded; do nothing else here
        } catch {
            // If the call failed, token likely doesn't implement IERC20Metadata
            revert("Token doesn't implement decimals()");
        }
        allowedTokens.add(token);
        emit TokenAllowed(token);
    }

    function removeToken(address token) external onlyServiceProvider {
        allowedTokens.remove(token);
        emit TokenRemoved(token);
    }

    function isTokenAllowed(address token) public view returns (bool) {
        return allowedTokens.contains(token);
    }

    function updateFilecoinCurioMembershipAddress(string memory contractAddress) external onlyServiceProvider {
        filecoinCurioMembershipAddress = contractAddress;
    }

    function _execute(
        bytes calldata /*sourceAddress*/,
        string calldata /*sourceChain*/,
        bytes calldata payload
    ) internal override {
        // Payload: (uint256 requestId, string machineId, address user, address token, uint256 amount)
        (uint256 requestId, /*string memory machineId*/, address user, address token, uint256 amount)
        = abi.decode(payload, (uint256, string, address, address, uint256));

        require(isTokenAllowed(token), "Token not allowed");

        uint8 tokenDecimals = IERC20Metadata(token).decimals();

        // Convert from "humanAmount" (e.g., 1000) to base units for this token
        uint256 baseUnits = amount * (10 ** uint256(tokenDecimals));

        bool success = IERC20(token).transferFrom(user, address(this), baseUnits);

        // After attempt, call back to Filecoin to confirm success or failure
        bytes memory callbackPayload = abi.encode(requestId, success);

        // Call back to Filecoin controller
        gateway.callContract(
            "filecoin",
            filecoinCurioMembershipAddress,
            callbackPayload
        );
    }

    // Withdraw funds by service provider
    function withdrawFunds(address token, uint256 amount) external onlyServiceProvider {
        require(isTokenAllowed(token), "Token not allowed");
        uint256 balance = IERC20(token).balanceOf(address(this));
        require(amount <= balance, "Insufficient balance");

        bool success = IERC20(token).transfer(serviceProvider, amount);
        require(success, "Withdraw transfer failed");

        emit FundsWithdrawn(serviceProvider, token, amount);
    }

    // Transfer ownership if needed
    function transferOwnership(address newOwner) external onlyServiceProvider {
        require(newOwner != address(0), "Invalid new owner");
        serviceProvider = newOwner;
    }
}
