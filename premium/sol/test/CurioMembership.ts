import { ethers } from "hardhat";
import { Contract } from "ethers";
import { loadFixture } from "@nomicfoundation/hardhat-toolbox/network-helpers";
import { expect } from "chai";

/* From the keymaker: (a different keymaker output is used in production)
The private key in hexadecimal format
Private Key: 0x01a1f7cd43fc707d4012705d11b34758ec91d7b5fc3c733aa89e4e9a75f3c1d7
public key in compessed format
Public Key (uncompressed): 0x047741f0fb096ea752f0a3bf5eeb7c8c827313449441d9343818dd470d3253db1de0438d4b5e3ca697058edba123450729e440554b2988aa7e2398879b65ce2bab
The public key in compressed format (as Ethereum uses)
Public Key (compressed): 0x037741f0fb096ea752f0a3bf5eeb7c8c827313449441d9343818dd470d3253db1d
Derive the Ethereum address from the public key
Ethereum Address: 0x410eed40c00cab9ee850bec3ee8336f6f21b54c3
*/
const rateChangeSignerAddress = "0x410eed40c00cab9ee850bec3ee8336f6f21b54c3";


describe("CurioMembership", function () {
  // We define a fixture to reuse the same setup in every test.
  // We use loadFixture to run this setup once, snapshot that state,
  // and reset Hardhat Network to that snapshot in every test.
  async function deployCurioMembershipFixture() {
    const ONE_YEAR_IN_SECS = 365 * 24 * 60 * 60;
    const ONE_GWEI = 1_000_000_000;

    // Contracts are deployed using the first signer/account by default
    const [owner, otherAccount] = await ethers.getSigners();

    const fundsDestWallet = await ethers.Wallet.createRandom();
    const myContractType = await ethers.getContractFactory("CurioMembership");
    const contract = await myContractType.deploy(fundsDestWallet.address, rateChangeSignerAddress, 10, 0);
 
    console.log(contract);
    return { contract, owner, fundsDestWallet, otherAccount };
  }
  describe("Deployment", function () {
    it("Should know its admin", async function () {
      const { contract, owner } = await loadFixture(deployCurioMembershipFixture);
      expect(await contract.admin()).to.equal(owner);
    });

    it("Should know its funds Dest", async function () {
      const { contract, owner, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);
      expect(await contract.fundsReceiver()).to.equal(fundsDestWallet.address);
    });
  });

  describe("Changes", function () {
    it("Should allow fundsDest change by admin", async function () {
      const { contract, owner } = await loadFixture(deployCurioMembershipFixture);
      // Create a new wallet to receive funds
      const otherAccount = await ethers.Wallet.createRandom();

      await contract.connect(owner).getFunction('changeFundsReceiver')(otherAccount.address);
      expect(await contract.fundsReceiver()).to.equal(otherAccount.address);
    });

    it("Should forbid fundsDest change by non-admin", async function () {
      const { contract, owner, otherAccount } = await loadFixture(deployCurioMembershipFixture);
      await expect(contract.connect(otherAccount).getFunction('changeFundsReceiver')(otherAccount.address)).to.be.revertedWith("Only admin can perform this action");
    });

    it("Should allow a signed rate change", async function () {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);
      var now = (+new Date() / 1000)|0;
      var rate = 1000;
      var signature = sign(rate, now);
      const binaryBuffer = Buffer.from(signature.slice(2), 'hex') // turns hex 0x..... into a buffer of bytes.
      var fn = await contract.connect(owner).getFunction('setExchangeRate')(rate, now, signature);
      fn.to.emit(contract, "ExchangeRateUpdated").withArgs(rate, now, signature);
      expect(await contract.exchangeRate).to.equal(rate);
    });

    it("Should forbid a rate change with a bad signature", async function () {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);
      var fn = await expect(contract.connect(owner).getFunction('setExchangeRate')(1000, +new Date()/1000, atob("SGVsbG8sIHdvcmxk")));
      fn.to.be.revertedWith("Exchange rate update is too old");
    });

    it("Should forbid a rate change from the distant past", async function () {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);

      var fn = await expect(contract.connect(owner).getFunction('setExchangeRate')(1000, 100_000, atob("SGVsbG8sIHdvcmxk")));
      fn.to.be.revertedWith("Exchange rate update is too old");
    });
  });
  describe("Pay", function() {
    it("Should allow pay() happy path for 500 with emit and updated pay record", async function() {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);
      var fn = await contract.connect(owner).getFunction('pay')(1234, {value: 5_000});
      fn.to.emit(contract, "PaymentMade").withArgs(1234, 5_000, 1);
      expect(await contract.paymentRecords(1234)).to.equal(5_000);
    });

    it("Should allow pay() happy path for 2000 with emit and updated pay record", async function() {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);
      var fn = await contract.connect(owner).getFunction('pay')(5678, {value: 20_000});
      fn.to.emit(contract, "PaymentMade").withArgs(1234, 20_000, 1);
      expect(await contract.paymentRecords(5678)).to.equal(20_000);
      // TODO verify event log entry
    });

    it("Should forbid pay() for 100", async function() {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);
      var fn = await expect(contract.connect(owner).getFunction('pay')(100));
      fn.to.be.revertedWith("Incorrect payment amount");
    });

    it("Should forbid pay() with outdated rate", async function() {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);
      var fn = await expect(contract.connect(owner).getFunction('pay')(500));
      fn.to.be.revertedWith("Exchange rate is outdated");
    });
  })
});

// Function to pack a 256-bit unsigned integer (equivalent to PHP's pack("Q", ...))
function abiEncodePackedUint256(value1:bigint, value2:bigint) {
  // Allocate a buffer of 64 bytes (32 bytes for each uint256 value)
  const buffer = Buffer.alloc(64);

  // Write the first uint256 value to the first 32 bytes (big-endian format)
  buffer.writeBigUInt64BE(BigInt(value1 >> BigInt(64)), 0); // High 64 bits
  buffer.writeBigUInt64BE(BigInt(value1 & BigInt(0xFFFFFFFFFFFFFFFF)), 8); // Low 64 bits

  // Write the second uint256 value to the next 32 bytes (big-endian format)
  buffer.writeBigUInt64BE(BigInt(value2 >> BigInt(64)), 32); // High 64 bits
  buffer.writeBigUInt64BE(BigInt(value2 & BigInt(0xFFFFFFFFFFFFFFFF)), 40); // Low 64 bits

  return buffer;
}
function sign(rate:number, timestamp:number) {
  // Import the necessary libraries
  const EC = require('elliptic').ec;
  const keccak256 = require('ethereumjs-util').keccak256;
  const ec = new EC('secp256k1');

  // Use the private key generated in PHP
  const privateKey = '01a1f7cd43fc707d4012705d11b34758ec91d7b5fc3c733aa89e4e9a75f3c1d7';

  // Convert the private key to a key pair
  const keyPair = ec.keyFromPrivate(privateKey);

  // Pack the rate and timestamp like PHP's pack("Q", $rate) . pack("Q", $timestamp)
  const packedData = abiEncodePackedUint256(BigInt(rate), BigInt(timestamp));

  // Hash the packed data using keccak256 (Ethereum's hash function)
  const messageHash = keccak256(packedData);

  // Sign the message hash
  const signature = keyPair.sign(messageHash);

  // Get the signature in hex format
  const r = signature.r.toString('hex');
  const s = signature.s.toString('hex');
  const v = signature.recoveryParam + 27; // Ethereum specific

  return `0x${r}${s}${v}`;
}