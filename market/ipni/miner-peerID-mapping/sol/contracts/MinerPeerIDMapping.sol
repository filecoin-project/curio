// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@zondax/filecoin-solidity/contracts/v0.8/MinerAPI.sol";
import "@zondax/filecoin-solidity/contracts/v0.8/types/CommonTypes.sol";

contract MinerPeerIDMapping {
    struct PeerData {
        string peerID;
        bytes signedMessage;
    }

    // State variables
    address public owner;
    mapping(uint64 => PeerData) public minerToPeerData;

    // Events
    event PeerDataAdded(uint64 indexed minerID, string peerID);
    event PeerDataUpdated(uint64 indexed minerID, string oldPeerID, string newPeerID);
    event PeerDataDeleted(uint64 indexed minerID);
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    // Modifier to restrict access to the owner
    modifier onlyOwner() {
        require(msg.sender == owner, "Caller is not the owner");
        _;
    }

    // Constructor: Set the deployer as the initial owner
    constructor() {
        owner = msg.sender;
        emit OwnershipTransferred(address(0), msg.sender);
    }

    /**
     * @notice Transfer ownership to a new address.
     * @param newOwner The address of the new owner.
     */
    function transferOwnership(address newOwner) public onlyOwner {
        require(newOwner != address(0), "New owner is the zero address");
        emit OwnershipTransferred(owner, newOwner);
        owner = newOwner;
    }

    /**
     * @notice Add a new PeerID and signed message for a MinerID in the contract.
     * @param minerID The MinerID to associate with the new PeerID.
     * @param newPeerID The new PeerID to bind to the MinerID.
     * @param signedMessage The signed message to store.
     */
    function addPeerData(
        uint64 minerID,
        string memory newPeerID,
        bytes memory signedMessage
    ) public {
        require(bytes(minerToPeerData[minerID].peerID).length == 0, "Peer data already exists for this MinerID");

        if (msg.sender != owner) {
            require(isControllingAddress(msg.sender, minerID), "Caller is not the controlling address");
        }

        minerToPeerData[minerID] = PeerData(newPeerID, signedMessage);

        emit PeerDataAdded(minerID, newPeerID);
    }

    /**
     * @notice Update an existing PeerID and signed message for a MinerID in the contract.
     * @param minerID The MinerID whose PeerID will be updated.
     * @param newPeerID The new PeerID to bind to the MinerID.
     * @param signedMessage The new signed message to store.
     */
    function updatePeerData(
        uint64 minerID,
        string memory newPeerID,
        bytes memory signedMessage
    ) public {
        require(bytes(minerToPeerData[minerID].peerID).length > 0, "No peer data exists for this MinerID");

        if (msg.sender != owner) {
            require(isControllingAddress(msg.sender, minerID), "Caller is not the controlling address");
        }

        string memory oldPeerID = minerToPeerData[minerID].peerID;
        minerToPeerData[minerID] = PeerData(newPeerID, signedMessage);

        emit PeerDataUpdated(minerID, oldPeerID, newPeerID);
    }

    /**
     * @notice Delete an existing PeerID and signed message for a MinerID in the contract.
     * @param minerID The MinerID whose peer data will be deleted.
     */
    function deletePeerData(uint64 minerID) public {
        require(bytes(minerToPeerData[minerID].peerID).length > 0, "No peer data exists for this MinerID");

        if (msg.sender != owner) {
            require(isControllingAddress(msg.sender, minerID), "Caller is not the controlling address");
        }

        delete minerToPeerData[minerID];

        emit PeerDataDeleted(minerID);
    }

    /**
     * @notice Fetch the PeerID and signed message associated with a MinerID.
     * @param minerID The MinerID to query.
     * @return The PeerID and signed message associated with the MinerID.
     */
    function getPeerData(uint64 minerID) public view returns (PeerData memory) {
        return minerToPeerData[minerID];
    }

    /**
     * @notice Check if the caller is the controlling address for the given MinerID.
     * @param caller The address of the caller.
     * @param minerID The MinerID to check.
     * @return True if the caller is the controlling address, false otherwise.
     */
    function isControllingAddress(address caller, uint64 minerID) internal returns (bool) {
        // Wrap the uint64 miner ID into a FilActorId
        CommonTypes.FilActorId minerActorID = CommonTypes.FilActorId.wrap(minerID);

        // Create a FilAddress for the caller
        CommonTypes.FilAddress memory callerAddress = CommonTypes.FilAddress({data: abi.encodePacked(caller)});

        // Call the MinerAPI function
        bool isControlling = MinerAPI.isControllingAddress(minerActorID, callerAddress);

        return isControlling;
    }
}
