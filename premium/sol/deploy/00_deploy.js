require("hardhat-deploy");
require("hardhat-deploy-ethers");

module.exports = async ({ getNamedAccounts, deployments}) => {
    const { deploy } = deployments;
    const { deployer } = await getNamedAccounts();

    // Define the constructor arguments
    const inFundsReceiver = "0x5134C19C59613A5243d39084Aa539998EEcD2396"; // Replace with actual address
    const inSignerPublicKey = "0xb2abb210c6dca5f0ee9071a4647dce5b5f523282";

    console.log("Deployer address:", deployer);
    console.log("Deploying CurioMembership with:");
    console.log("Funds Receiver:", inFundsReceiver);
    console.log("Signer Public Key:", inSignerPublicKey);

    // Deploy the contract
    const curioMembership = await deploy("CurioMembership", {
        from: deployer,
        args: [inFundsReceiver, inSignerPublicKey],
        log: true,
    });

    console.log("CurioMembership deployed at:", curioMembership.address);
};
