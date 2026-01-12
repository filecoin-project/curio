const { ethers } = require("hardhat")

const networkConfig = {
    31415926: {
        name: "2k",
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
