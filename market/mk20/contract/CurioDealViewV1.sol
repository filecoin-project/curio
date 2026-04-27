// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

interface ICurioDealViewV1 {
    // Must return this error when curio tries to verify a non existing deal
    error DealNotFound(uint256 dealId);

    // These deal states must exist and are used by Curio to determine deal state
    // while sealing
    enum DealState {
        Open,       // Can be sent to SP for sealing
        Active,     // SP has sealed the deal in a sector
        Finalized   // Deal has been terminated. Termination can be due to non proving, client, reaching duration end etc
    }

    // Input argument Curio will send to verification function
    struct CurioDealView {
        uint256 dealId;
        DealState state;
        uint256 providerActorId;// Miner's actor ID
        bytes clientId;         // Client address bytes
        bytes pieceCidV2;       // Piece CID v2 bytes
        uint256 startEpoch;     // Optional Start Epoch, must be 0 when not used
        uint256 duration;       // Duration of deal in Filecoin epochs
        uint256 allocationId;   // Optional allocation ID, must be 0 when not used
        uint256 finalizedEpoch; // Must be 0 when deal state is Open, Active. Must be non zero deal termination epoch
    }

    function version() external pure returns (uint256);

    // Verify deal details provided by Curio based on deal received from client
    function verifyDeal(CurioDealView calldata deal) external view returns (bool);

    // Provide current deal state. This method is used for housekeeping by Curio
    function getDealState(uint256 dealId) external view returns (DealState);
}
