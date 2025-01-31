import { HardhatUserConfig } from "hardhat/config";
import "@nomicfoundation/hardhat-toolbox";
import "@nomicfoundation/hardhat-ethers";
import "dotenv/config"; // For .env variables
import "hardhat-deploy";

const PRIVATE_KEY = process.env.PRIVATE_KEY;

if (!PRIVATE_KEY) {
    throw new Error("Please set your PRIVATE_KEY in a .env file");
}

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
    solidity: {
        version: "0.8.23",
        settings: {
            optimizer: {
                enabled: true,
                runs: 1000,
                details: { yul: false },
            },
        },
    },
    defaultNetwork: "hardhat",
    networks: {
        hardhat: {
            chainId: 1337,
            allowUnlimitedContractSize: true, // Ensure compatibility with large contracts if needed
        },
        devnet: {
            chainId: 31415926,
            url: "http://127.0.0.1:1234/rpc/v1",
            accounts: [PRIVATE_KEY],
        },
        calibrationnet: {
            chainId: 314159,
            url: "https://api.calibration.node.glif.io/rpc/v1",
            accounts: [PRIVATE_KEY],
        },
        mainnet: {
            chainId: 314,
            url: "https://api.node.glif.io",
            accounts: [PRIVATE_KEY],
        },
    },
    namedAccounts: {
        deployer: {
            default: 0, // The first account from the accounts array will be the deployer
        },
    },
    paths: {
        sources: "./contracts", // Contract source files
        cache: "./cache", // Cache folder
        artifacts: "./artifacts", // Compiled artifacts
        deployments: "./deployments", // Deployment files
        tests: "./test"
    },
    mocha: {
        timeout: 200000, // Extend timeout for tests if necessary
    },
};
