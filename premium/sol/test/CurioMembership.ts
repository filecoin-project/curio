import { ethers } from "hardhat";
import { Contract } from "ethers";
import { loadFixture } from "@nomicfoundation/hardhat-toolbox/network-helpers";
import { expectEvent } from "@openzeppelin/test-helpers";

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
      var rate = 2000;
      var {signature, sig, packedMessage, messageHash} = await sign(rate, now);
      
      //ethers.Signature.getNormalizedV(sig.v);
      //const recoveredAddress = ethers.Signature.verify(messageHash, signature);
      //expect(recoveredAddress).to.equal(rateChangeSignerAddress);

      // Pack the rate and timestamp like PHP's pack("Q", $rate) . pack("Q", $timestamp)
      //const packedData = coder.encode(['uint64', 'uint64'], [rate, now]);
      await expect(contract.connect(owner).setExchangeRate(packedMessage, signature)).
        //to.emit(contract, "DebugRate").withArgs(rate).
        //to.emit(contract, "DebugTimestamp").withArgs(now).
        //to.emit(contract, "DebugPackedData").withArgs(packedData).
        //to.emit(contract, "DebugMessageHash").withArgs(ethers.keccak256(packedData)).
        //to.emit(contract, "DebugExpectedSigner").withArgs('0x410EeD40c00cab9EE850bec3EE8336f6f21B54C3'). // it's right, but caps are off
        //to.emit(contract, "DebugSplitSig").withArgs(sig.r, sig.s, sig.v).
        //to.emit(contract, "DebugRecoveredSigner").withArgs(rateChangeSignerAddress). // <-- here's the failure
        to.emit(contract, "ExchangeRateUpdated").withArgs(rate, now);

      expect(await contract.exchangeRate()).to.equal(rate);
    });

    it("Should forbid a rate change with a bad signature", async function () {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);
      var now = (+new Date() / 1000)|0;
      var rate = 2000;
      var {signature, sig, packedMessage, messageHash} = await sign(rate, now);
      var fn = await expect(contract.connect(owner).getFunction('setExchangeRate')(packedMessage, atob("SGVsbG8sIHdvcmxk")));
      fn.to.be.revertedWith("Exchange rate update is too old");
    });

    it("Should forbid a rate change from the distant past", async function () {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);
      var now = (+new Date("2005-01-01") / 1000)|0;
      var rate = 2000;
      var {signature, sig, packedMessage, messageHash} = await sign(rate, now);
      var fn = await expect(contract.connect(owner).getFunction('setExchangeRate')(packedMessage, atob("SGVsbG8sIHdvcmxk")));
      fn.to.be.revertedWith("Exchange rate update is too old");
    });
  });
  describe("Pay", function() {
    it("Should allow pay() happy path for 500 with emit and updated pay record", async function() {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);

      var {signature, sig, packedMessage, messageHash} = await sign(1000, (+new Date() / 1000)|0);
      var setup = await expect(contract.connect(owner).setExchangeRate(packedMessage, signature));
      setup.to.emit(contract, "ExchangeRateUpdated").withArgs(1000, (+new Date() / 1000)|0);

      var tx = await contract.connect(owner).getFunction('pay')(1234, {value: 5_000_000});
      var receipt = await tx.wait();
      expectEvent(receipt, "PaymentMade", {
        memberId: 1234,
        amount: 5_000_000,
        paymentId: 1
      });
      expect(await contract.paymentRecords(1234)).to.equal(5_000_000);
    });

    it("Should allow pay() happy path for 2000 with emit and updated pay record", async function() {
      const { contract, owner, otherAccount, fundsDestWallet } = await loadFixture(deployCurioMembershipFixture);

      var {signature, sig, packedMessage, messageHash} = await sign(1000, (+new Date() / 1000)|0);
      var setup = await expect(contract.connect(owner).setExchangeRate(packedMessage, signature));
      setup.to.emit(contract, "ExchangeRateUpdated").withArgs(1000, (+new Date() / 1000)|0);

      var tx = await contract.connect(owner).getFunction('pay')(5678, {value: 20_000_000});
      var receipt = await tx.wait();
      expectEvent(receipt, "PaymentMade", {
        memberId: 5678,
        amount: 20_000_000,
        paymentId: 1
      });
      expect(await contract.paymentRecords(5678)).to.equal(20_000_000);
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

// Function to pack a 64-bit unsigned integer (equivalent to PHP's pack("Q", ...))
function packUint64(value:number) {
  const buffer = Buffer.alloc(8);
  buffer.writeBigUInt64BE(BigInt(value));
  return buffer;
}

function packUint256(value:number) {
  const buffer = Buffer.alloc(32);
  buffer.writeBigUInt64BE(BigInt(value), 24);
  return buffer;
}

async function sign(rate:number, timestamp:number) {
  // Import the necessary libraries
  const EC = require('elliptic').ec;
  const keccak256 = require('ethereumjs-util').keccak256;
  const ec = new EC('secp256k1');

  // Use the private key generated in PHP
  const privateKey = '0x01a1f7cd43fc707d4012705d11b34758ec91d7b5fc3c733aa89e4e9a75f3c1d7';

  // Convert the private key to a key pair
  const wallet = new ethers.Wallet(privateKey);

  //const coder = new ethers.AbiCoder();
  // Pack the rate and timestamp like PHP's pack("Q", $rate) . pack("Q", $timestamp)
  //const packedData = coder.encode(['uint64', 'uint64'], [rate, timestamp]);
  const packedMessage = new Uint8Array(
    [...packUint64(0), ...packUint64(0), ...packUint64(rate), ...packUint64(timestamp)]);

  // Hash the packed data using keccak256 (Ethereum's hash function)
  const messageHash = ethers.keccak256(packedMessage);

  // Sign the message hash
  const signature = await wallet.signMessage(ethers.getBytes(messageHash));
  const sig = ethers.Signature.from(signature);
  return {signature, sig, packedMessage, messageHash};//Buffer.from(signature, 'hex');
}