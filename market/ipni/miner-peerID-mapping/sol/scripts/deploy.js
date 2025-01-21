const hre = require("hardhat");

async function main() {
    const [deployer] = await hre.ethers.getSigners();

    console.log("Deployer public address:", deployer.address);

    // Deploy the contract
    const MinerPeerIDMapping = await hre.ethers.getContractFactory("MinerPeerIDMapping");
    const contract = await MinerPeerIDMapping.deploy();

    console.log("Waiting for contract deployment...");
    await contract.deployed();

    console.log("Deployed contract address:", contract.address);
}

main().catch((error) => {
    console.error(error);
    process.exit(1);
});
