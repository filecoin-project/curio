// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "filecoin-solidity-api/contracts/v0.8/MinerAPI.sol";
import "filecoin-solidity-api/contracts/v0.8/types/CommonTypes.sol";
import "filecoin-solidity-api/contracts/v0.8/utils/FilAddressIdConverter.sol";
import "filecoin-solidity-api/contracts/v0.8/utils/FilAddresses.sol";

contract MinerPeerIDMapping {
    struct PeerData {
        string peerID;
        bytes signedMessage;
    }

    mapping(uint64 => PeerData) public minerToPeerData;

    // Events
    event PeerDataAdded(uint64 indexed minerID, string peerID);
    event PeerDataUpdated(uint64 indexed minerID, string oldPeerID, string newPeerID);
    event PeerDataDeleted(uint64 indexed minerID);

    constructor() {
        // The contract is immutable from the owner now, no ownership transfer is possible
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

        require(isControllingAddress(msg.sender, minerID), "Caller is not the controlling address");

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

        require(isControllingAddress(msg.sender, minerID), "Caller is not the controlling address");

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

        require(isControllingAddress(msg.sender, minerID), "Caller is not the controlling address");

        delete minerToPeerData[minerID];

        emit PeerDataDeleted(minerID);
    }

    /**
     * @notice Fetch the PeerData struct associated with a MinerID.
     * @param minerID The MinerID to query.
     * @return The PeerData struct associated with the MinerID.
     */
    function getPeerData(uint64 minerID) public view returns (PeerData memory) {
        PeerData memory data = minerToPeerData[minerID];

        // If no data exists for the minerID, return default values
        if (bytes(data.peerID).length == 0) {
            return PeerData("", bytes(""));
        }

        return data;
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
        (bool ok, uint64 addr) = FilAddressIdConverter.getActorID(caller);
        require(ok = true, "Failed to covert called to actor ID");
        CommonTypes.FilAddress memory callerAddress = FilAddresses.fromActorID(addr);

        // Call the MinerAPI function
        (int256 exitCode, bool isControlling) = MinerAPI.isControllingAddress(minerActorID, callerAddress);
        require(exitCode == 0, "MinerAPI call failed");

        return isControlling;
    }
}
