// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

interface ICurioDealViewV1 {
    error DealNotFound(uint256 dealId);

    enum DealState {
        Open,
        Active,
        Finalized
    }

    struct DealView {
        DealState state;
        uint256 providerActorId;
        bytes clientId;
        bytes pieceCidV2;
        uint256 startEpoch;
        uint256 duration;
        uint256 allocationId;
        uint256 finalizedEpoch;
    }

    function version() external pure returns (uint256);

    function getDeal(uint256 dealId) external view returns (DealView memory);
}
