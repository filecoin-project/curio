require("@nomicfoundation/hardhat-toolbox");
require("hardhat-deploy");
require("hardhat-deploy-ethers");
require('dotenv').config();

// Load environment variables for RPC URLs and Private Key
const PRIVATE_KEY = process.env.PRIVATE_KEY || "";
const FILECOIN_DEVNET_URL = process.env.FILECOIN_DEVNET_URL || "http://127.0.0.1:1234/rpc/v1";
const FILECOIN_CALIB_URL = process.env.FILECOIN_CALIB_URL || "https://api.calibration.node.glif.io/rpc/v1";
const FILECOIN_MAINNET_URL = process.env.FILECOIN_MAINNET_URL || "https://api.node.glif.io";
const ETH_RPC_URL = process.env.ETH_RPC_URL || "https://rpc.ankr.com/eth";
const POLYGON_RPC_URL = process.env.POLYGON_RPC_URL || "https://polygon-mainnet.infura.io/v3/9aa3d95b3bc440fa88ea12eaa4456161";
const ARBITRUM_RPC_URL = process.env.ARBITRUM_RPC_URL || "https://1rpc.io/arb";

console.log("Deploying with private key:", PRIVATE_KEY ? "****" : "NO KEY PROVIDED");

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
    // Local Filecoin devnet
    devnet: {
      chainId: 31415926,
      url: FILECOIN_DEVNET_URL,
      accounts: PRIVATE_KEY ? [PRIVATE_KEY] : [],
    },
    // Hardhat built-in local network
    hardhat: {},
    // Filecoin Calibration Testnet
    calibrationnet: {
      chainId: 314159,
      url: FILECOIN_CALIB_URL,
      accounts: PRIVATE_KEY ? [PRIVATE_KEY] : [],
    },
    // Filecoin Mainnet
    mainnet: {
      chainId: 314,
      url: FILECOIN_MAINNET_URL,
      accounts: PRIVATE_KEY ? [PRIVATE_KEY] : [],
    },
    // Ethereum Mainnet
    ethereum: {
      chainId: 1,
      url: ETH_RPC_URL,
      accounts: PRIVATE_KEY ? [PRIVATE_KEY] : [],
    },
    // Polygon Mainnet
    polygon: {
      chainId: 137,
      url: POLYGON_RPC_URL,
      accounts: PRIVATE_KEY ? [PRIVATE_KEY] : [],
    },
    // Arbitrum Mainnet
    arbitrum: {
      chainId: 42161,
      url: ARBITRUM_RPC_URL,
      accounts: PRIVATE_KEY ? [PRIVATE_KEY] : [],
    },
  },
  namedAccounts: {
    deployer: {
      default: 0, // The first account from the accounts array will be the deployer
    },
  },
  paths: {
    sources: "./contracts",
    tests: "./test",
    cache: "./cache",
    artifacts: "./artifacts",
  },
};
