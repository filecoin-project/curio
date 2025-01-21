module.exports = async ({ getNamedAccounts, deployments }) => {
    const { deploy } = deployments; // Access the deploy helper
    const { deployer } = await getNamedAccounts(); // Get the deployer account

    console.log("Starting deployment of MinerPeerIDMapping contract...");
    console.log("Deployer address:", deployer);

    try {
        const result = await deploy("MinerPeerIDMapping", {
            from: deployer, // Deployer account
            args: [], // Constructor arguments (none for this contract)
            log: true, // Enable logging of the deployment process
        });

        console.log("MinerPeerIDMapping contract successfully deployed!");
        console.log("Contract address:", result.address);
    } catch (error) {
        console.error("Error occurred during deployment:");
        console.error(error);
    }

    // Deployment details will be automatically saved to `deployments/<network>/MinerPeerIDMapping.json`
};

module.exports.tags = ["MinerPeerIDMapping"];
