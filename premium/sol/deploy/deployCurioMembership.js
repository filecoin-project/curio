const { ethers, network } = require("hardhat");

// Hardcoded addresses for Axelar Gateway and Gas Service per network.
// Replace these placeholders with the actual addresses from Axelar documentation.
const AXELAR_ADDRESSES = {
    calibrationnet: {
        gateway: "0x999117D44220F33e0441fbAb2A5aDB8FF485c54D",
        gasService: "0xbE406F0189A0B4cf3A05C286473D23791Dd44Cc6"
    },
    mainnet: {
        gateway: "0xe432150cce91c13a887f7D836923d5597adD8E31",
        gasService: "0x2d5d7d31F671F86C782533cc367F14109a082712"
    }
};

// The signer public key for exchange rate verification.
// You can hardcode it here, or retrieve from somewhere else.
const SIGNER_PUBLIC_KEY = "0xb2abb210c6dca5f0ee9071a4647dce5b5f523282";

async function main() {
    const netName = network.name; // e.g., filecoincalibrationnet, filecoinmainnet

    if (!AXELAR_ADDRESSES[netName]) {
        throw new Error(`Unsupported network: ${netName}`);
    }

    const { gateway, gasService } = AXELAR_ADDRESSES[netName];
    if (!gateway || !gasService) {
        throw new Error(`Axelar addresses not set properly for ${netName}`);
    }

    if (!SIGNER_PUBLIC_KEY) {
        throw new Error("SIGNER_PUBLIC_KEY is not defined");
    }

    const CurioMembership = await ethers.getContractFactory("CurioMembership");
    const contract = await CurioMembership.deploy(SIGNER_PUBLIC_KEY, gateway, gasService);

    await contract.deployed();
    console.log(`CurioMembership deployed to ${contract.address} on ${netName}`);

    // Example:
    // await contract.addAllowedChain("arbitrum", "0xArbitrumDestContractAddressString");
    // console.log("Arbitrum allowed chain added");
}

main().catch((error) => {
    console.error(error);
    process.exit(1);
});
