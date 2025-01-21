import { ethers } from "hardhat";
import { expect } from "chai";

describe("MinerPeerIDMapping", function () {
    let owner: any;
    let nonOwner: any;
    let MinerPeerIDMapping: any;
    let contract: any;

    beforeEach(async function () {
        [owner, nonOwner] = await ethers.getSigners();
        const MinerPeerIDMappingFactory = await ethers.getContractFactory("MinerPeerIDMapping");
        contract = await MinerPeerIDMappingFactory.deploy();
    });

    it("should set the deployer as the owner", async function () {
        const contractOwner = await contract.owner();
        expect(contractOwner).to.equal(owner.address);
    });

    it("should allow the owner to transfer ownership", async function () {
        await contract.transferOwnership(nonOwner.address);
        const newOwner = await contract.owner();
        expect(newOwner).to.equal(nonOwner.address);
    });

    it("should not allow non-owners to transfer ownership", async function () {
        await expect(contract.connect(nonOwner).transferOwnership(nonOwner.address)).to.be.revertedWith(
            "Caller is not the owner"
        );
    });

    it("should allow the owner to add peer data", async function () {
        const minerID = 1234; // Updated to uint64
        const newPeerID = "peer-id-1";
        const signedMessage = ethers.hexlify(ethers.randomBytes(32));

        await expect(contract.addPeerData(minerID, newPeerID, signedMessage))
            .to.emit(contract, "PeerDataAdded")
            .withArgs(minerID, newPeerID);

        const peerData = await contract.getPeerData(minerID);
        expect(peerData.peerID).to.equal(newPeerID);
        expect(peerData.signedMessage).to.equal(signedMessage);
    });

    // it("should not allow a non-owner to add peer data without being a controlling address", async function () {
    //     const minerID = 1234; // Updated to uint64
    //     const newPeerID = "peer-id-1";
    //     const signedMessage = ethers.hexlify(ethers.randomBytes(32));
    //
    //     await expect(
    //         contract.connect(nonOwner).addPeerData(minerID, newPeerID, signedMessage)
    //     ).to.be.revertedWith("Caller is not the controlling address");
    // });

    it("should allow the owner to update peer data", async function () {
        const minerID = 1234; // Updated to uint64
        const oldPeerID = "peer-id-1";
        const newPeerID = "peer-id-2";
        const signedMessage = ethers.hexlify(ethers.randomBytes(32));

        // Add initial peer data
        await contract.addPeerData(minerID, oldPeerID, signedMessage);

        // Update peer data
        await expect(contract.updatePeerData(minerID, newPeerID, signedMessage))
            .to.emit(contract, "PeerDataUpdated")
            .withArgs(minerID, oldPeerID, newPeerID);

        const peerData = await contract.getPeerData(minerID);
        expect(peerData.peerID).to.equal(newPeerID);
        expect(peerData.signedMessage).to.equal(signedMessage);
    });

    it("should not allow updating peer data for a non-existent miner ID", async function () {
        const minerID = 1234; // Updated to uint64
        const newPeerID = "peer-id-2";
        const signedMessage = ethers.hexlify(ethers.randomBytes(32));

        await expect(contract.updatePeerData(minerID, newPeerID, signedMessage)).to.be.revertedWith(
            "No peer data exists for this MinerID"
        );
    });

    it("should allow the owner to delete peer data", async function () {
        const minerID = 1234; // Updated to uint64
        const peerID = "peer-id-1";
        const signedMessage = ethers.hexlify(ethers.randomBytes(32));

        // Add initial peer data
        await contract.addPeerData(minerID, peerID, signedMessage);

        // Delete peer data
        await expect(contract.deletePeerData(minerID))
            .to.emit(contract, "PeerDataDeleted")
            .withArgs(minerID);

        const peerData = await contract.getPeerData(minerID);
        expect(peerData.peerID).to.equal("");
        expect(peerData.signedMessage).to.equal("0x");
    });

    it("should not allow deleting peer data for a non-existent miner ID", async function () {
        const minerID = 1234; // Updated to uint64

        await expect(contract.deletePeerData(minerID)).to.be.revertedWith("No peer data exists for this MinerID");
    });

    it("should validate the controlling address for a miner ID (mock test)", async function () {
        const minerID = 1234; // Updated to uint64
        const peerID = "peer-id-1";
        const signedMessage = ethers.hexlify(ethers.randomBytes(32));

        // Add peer data as the owner
        await contract.addPeerData(minerID, peerID, signedMessage);

        // Simulate a call to validate controlling address
        // In a real test, mock the MinerAPI call for controlling address validation
        // Assuming isControllingAddress() reverts as a placeholder for now
        await expect(contract.isControllingAddress(owner.address, minerID)).to.be.reverted;
    });
});
