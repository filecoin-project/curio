# Sample Hardhat Project

This project demonstrates a basic Hardhat use case. It comes with a sample contract, a test for that contract, and a Hardhat Ignition module that deploys that contract.

Try running some of the following tasks:

```shell
npx hardhat help
npx hardhat test
REPORT_GAS=true npx hardhat test
npx hardhat node
npx hardhat ignition deploy ./ignition/modules/Lock.js
```

## Deploying the contract
1. Mainnet
    ```shell
   npx hardhat deploy --network mainnet
   ```
   
2. Calibnet
    ```shell
   npx hardhat deploy --network calibnet
   ```
   
3. Devnet
    ```shell
   npx hardhat deploy --network devnet
   ```

Once the contract is deployed, we must update the value of constant `contractABI` in file `curio/cmd/sptool/premium.go` with content of file `curio/premium/sol/artifacts/contracts/CurioMembership.sol/CurioMembership.json`
