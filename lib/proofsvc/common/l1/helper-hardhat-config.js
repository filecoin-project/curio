const { ethers } = require("hardhat")

const networkConfig = {
    31415926: {
	name: "localnet",
    },
    314159: {
        name: "calibrationnet",
    },
    314: {
        name: "filecoinmainnet",
    },
}

// const developmentChains = ["hardhat", "localhost"]

module.exports = {
    networkConfig,
    // developmentChains,
}
