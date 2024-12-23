const { ethers, network } = require("hardhat");

// Hardcoded Axelar Gateway addresses for Ethereum, Polygon, and Arbitrum
// Replace with the actual addresses.
const AXELAR_ADDRESSES = {
    ethereumsepolia: {
        gateway: "0xe432150cce91c13a887f7D836923d5597adD8E31"
    },
    ethereum: {
        gateway: "0x4F4495243837681061C4743b74B3eEdf548D56A5"
    },
    polygonsepolia: {
        gateway: "0xe432150cce91c13a887f7D836923d5597adD8E31"
    },
    polygon: {
        gateway: "0x6f015F16De9fC8791b234eF68D486d2bF203FBA8"
    },
    arbitrumsepolia: {
        gateway: "0xe1cE95479C84e9809269227C7F8524aE051Ae77a"
    },
    arbitrum: {
        gateway: "0xe432150cce91c13a887f7D836923d5597adD8E31"
    }
};

// The Filecoin Curio Membership address string recognized by Axelar.
// This must match what you set in the Filecoin contract's allowedChains mapping.
const FILECOIN_CURIO_MEMBERSHIP_ADDRESS_STR = process.env.CurioMembershipContract || "";

async function main() {
    const netName = network.name; // e.g., ethereum, polygon, arbitrum

    if (!AXELAR_ADDRESSES[netName]) {
        throw new Error(`Unsupported network: ${netName}`);
    }

    const { gateway } = AXELAR_ADDRESSES[netName];
    if (!gateway) {
        throw new Error(`Axelar gateway not set for ${netName}`);
    }

    if ((!FILECOIN_CURIO_MEMBERSHIP_ADDRESS_STR) || (FILECOIN_CURIO_MEMBERSHIP_ADDRESS_STR === "")) {
        throw new Error("FILECOIN_CURIO_MEMBERSHIP_ADDRESS_STR is not defined");
    }

    const CurioMembershipStableCoin = await ethers.getContractFactory("CurioMembershipStableCoin");
    const contract = await CurioMembershipStableCoin.deploy(gateway, FILECOIN_CURIO_MEMBERSHIP_ADDRESS_STR);

    await contract.deployed();
    console.log(`CurioMembershipStableCoin deployed to ${contract.address} on ${netName}`);

    // Example: after deployment, add a token
    // await contract.allowToken("0xUSDCaddressOnThisChain");
    // console.log("USDC token allowed on", netName);
}

main().catch((error) => {
    console.error(error);
    process.exit(1);
});
