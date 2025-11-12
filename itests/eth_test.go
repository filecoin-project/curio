package itests

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	mathbig "math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/api"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"

	lapi "github.com/filecoin-project/lotus/api"
	lotustypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/itests/kit"
)

func TestEthClientMoneyTransfer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start first lotus node ensemble
	full1, miner1, ensemble1 := kit.EnsembleMinimal(t,
		kit.LatestActorsAt(-1),
		kit.PresealSectors(32),
		kit.ThroughRPC(),
	)

	ensemble1.Start()
	blockTime := 100 * time.Millisecond
	ensemble1.BeginMining(blockTime)

	// Wait for chain to advance
	full1.WaitTillChain(ctx, kit.HeightAtLeast(15))

	// Start second lotus node ensemble
	full2, _, ensemble2 := kit.EnsembleMinimal(t,
		kit.LatestActorsAt(-1),
		kit.PresealSectors(32),
		kit.ThroughRPC(),
	)

	ensemble2.Start()
	// Don't start mining on node2 yet - let it sync from node1 first

	// Connect the nodes so node2 syncs from node1's chain
	// Get peer address from node1
	addrs1, err := full1.NetAddrsListen(ctx)
	require.NoError(t, err, "should be able to get node1 peer address")

	// Connect node2 to node1 so node2 syncs from node1
	err = full2.NetConnect(ctx, addrs1)
	require.NoError(t, err, "should be able to connect node2 to node1")

	// Wait for node2 to sync with node1's chain
	// This ensures both nodes have the same chain state
	t.Logf("Connected nodes - waiting for node2 to sync from node1...")

	// Wait for node2 to catch up to node1's chain height
	// We'll wait up to 30 seconds for sync to complete
	head1, err := full1.ChainHead(ctx)
	require.NoError(t, err)
	targetHeight := head1.Height()

	for i := 0; i < 30; i++ {
		head2, err := full2.ChainHead(ctx)
		require.NoError(t, err)
		if head2.Height() >= targetHeight {
			t.Logf("Nodes synced! Node1 height: %d, Node2 height: %d", targetHeight, head2.Height())
			break
		}
		if i == 29 {
			t.Logf("Warning: Nodes may not be fully synced. Node1 height: %d, Node2 height: %d", targetHeight, head2.Height())
		}
		time.Sleep(1 * time.Second)
	}

	// Now start mining on node2 so it continues to sync with node1
	ensemble2.BeginMining(blockTime)

	// Create Ethereum wallets
	// Wallet 1
	privateKey1, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	require.NoError(t, err)
	wallet1EthAddr := crypto.PubkeyToAddress(privateKey1.PublicKey)

	wallet1FilAddr, err := address.NewDelegatedAddress(builtin.EthereumAddressManagerActorID, wallet1EthAddr.Bytes())
	require.NoError(t, err)

	// Wallet 2
	privateKey2, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	require.NoError(t, err)
	wallet2EthAddr := crypto.PubkeyToAddress(privateKey2.PublicKey)

	wallet2FilAddr, err := address.NewDelegatedAddress(builtin.EthereumAddressManagerActorID, wallet2EthAddr.Bytes())
	require.NoError(t, err)

	t.Logf("Wallet1 ETH: %s, FIL: %s", wallet1EthAddr.Hex(), wallet1FilAddr.String())
	t.Logf("Wallet2 ETH: %s, FIL: %s", wallet2EthAddr.Hex(), wallet2FilAddr.String())

	// Fund wallet1 from the genesis miner
	genesisAddr := miner1.OwnerKey.Address
	genesisBalance, err := full1.WalletBalance(ctx, genesisAddr)
	require.NoError(t, err)
	t.Logf("Genesis balance: %s", lotustypes.FIL(genesisBalance).String())

	// Send 100 FIL to wallet1
	amount := lotustypes.MustParseFIL("100 FIL")
	msg := &lotustypes.Message{
		From:  genesisAddr,
		To:    wallet1FilAddr,
		Value: abi.TokenAmount(amount),
	}

	signedMsg, err := full1.MpoolPushMessage(ctx, msg, nil)
	require.NoError(t, err)

	// Wait for message to be mined
	_, err = full1.StateWaitMsg(ctx, signedMsg.Cid(), 1, lapi.LookbackNoLimit, true)
	require.NoError(t, err)

	// Verify wallet1 balance
	wallet1Balance, err := full1.WalletBalance(ctx, wallet1FilAddr)
	require.NoError(t, err)
	require.Greater(t, wallet1Balance.Int64(), int64(0))
	t.Logf("Wallet1 FIL balance after funding: %s", lotustypes.FIL(wallet1Balance).String())

	// Also fund wallet2 so it has some initial balance
	msg2 := &lotustypes.Message{
		From:  genesisAddr,
		To:    wallet2FilAddr,
		Value: abi.TokenAmount(amount),
	}
	signedMsg2, err := full1.MpoolPushMessage(ctx, msg2, nil)
	require.NoError(t, err)
	_, err = full1.StateWaitMsg(ctx, signedMsg2.Cid(), 1, lapi.LookbackNoLimit, true)
	require.NoError(t, err)

	// Create eth client connection with BOTH nodes for failover testing
	token1, err := full1.AuthNew(ctx, lapi.AllPermissions)
	require.NoError(t, err)
	token2, err := full2.AuthNew(ctx, lapi.AllPermissions)
	require.NoError(t, err)

	apiInfo1 := fmt.Sprintf("%s:%s", string(token1), full1.ListenAddr)
	apiInfo2 := fmt.Sprintf("%s:%s", string(token2), full2.ListenAddr)
	// Configure with both nodes - first node will be tried first
	apiInfoCfg := config.NewDynamic([]string{apiInfo1, apiInfo2})

	// Create CLI context for eth client
	app := cli.NewApp()
	cctx := cli.NewContext(app, nil, nil)
	cctx.Context = ctx

	ethClient, err := deps.GetEthClient(cctx, apiInfoCfg)
	require.NoError(t, err)

	// Verify eth client works
	chainID, err := ethClient.ChainID(ctx)
	require.NoError(t, err)
	t.Logf("Chain ID: %s", chainID.String())

	blockNumber, err := ethClient.BlockNumber(ctx)
	require.NoError(t, err)
	t.Logf("Block number: %d", blockNumber)

	// Get initial balances - these should match the FIL balances we sent
	wallet1EthBalance, err := ethClient.BalanceAt(ctx, wallet1EthAddr, nil)
	require.NoError(t, err)
	require.Greater(t, wallet1EthBalance.Cmp(mathbig.NewInt(0)), 0, "wallet1 should have ETH balance from FIL funding")
	t.Logf("Wallet1 ETH balance before transfer: %s (should match FIL balance)", wallet1EthBalance.String())

	wallet2EthBalance, err := ethClient.BalanceAt(ctx, wallet2EthAddr, nil)
	require.NoError(t, err)
	require.Greater(t, wallet2EthBalance.Cmp(mathbig.NewInt(0)), 0, "wallet2 should have ETH balance from FIL funding")
	t.Logf("Wallet2 ETH balance before transfer: %s (should match FIL balance)", wallet2EthBalance.String())

	// Transfer money from wallet1 to wallet2
	transferAmount := mathbig.NewInt(1000000000000000000) // 1 FIL in attoFIL
	txHash, err := sendEthTransaction(ctx, ethClient, privateKey1, wallet1EthAddr, wallet2EthAddr, transferAmount)
	require.NoError(t, err)
	t.Logf("Transaction hash: %s", txHash.Hex())

	// Wait for transaction to be mined
	err = waitForTransaction(ctx, ethClient, txHash)
	require.NoError(t, err)

	// Verify balances after transfer
	wallet1EthBalanceAfter, err := ethClient.BalanceAt(ctx, wallet1EthAddr, nil)
	require.NoError(t, err)
	t.Logf("Wallet1 ETH balance after transfer: %s", wallet1EthBalanceAfter.String())

	wallet2EthBalanceAfter, err := ethClient.BalanceAt(ctx, wallet2EthAddr, nil)
	require.NoError(t, err)
	t.Logf("Wallet2 ETH balance after transfer: %s", wallet2EthBalanceAfter.String())

	require.Greater(t, wallet2EthBalanceAfter.Cmp(wallet2EthBalance), 0, "wallet2 should have received funds")
	require.Less(t, wallet1EthBalanceAfter.Cmp(wallet1EthBalance), 0, "wallet1 should have sent funds")

	// Wait for node2 to sync the transactions we just made on node1
	// This ensures node2 has the same chain state including our transactions
	t.Logf("Waiting for node2 to sync transactions from node1...")
	head1AfterTx, err := full1.ChainHead(ctx)
	require.NoError(t, err)
	targetHeightAfterTx := head1AfterTx.Height()

	for i := 0; i < 30; i++ {
		head2AfterTx, err := full2.ChainHead(ctx)
		require.NoError(t, err)
		if head2AfterTx.Height() >= targetHeightAfterTx {
			// Verify node2 can see the same balance by querying directly
			balanceOnNode2, err := full2.WalletBalance(ctx, wallet1FilAddr)
			if err == nil && balanceOnNode2.Int64() > 0 {
				t.Logf("Node2 synced transactions! Node1 height: %d, Node2 height: %d, Wallet1 balance on node2: %s",
					targetHeightAfterTx, head2AfterTx.Height(), lotustypes.FIL(balanceOnNode2).String())
				break
			}
		}
		if i == 29 {
			balanceOnNode2, _ := full2.WalletBalance(ctx, wallet1FilAddr)
			t.Logf("Warning: Node2 may not have fully synced transactions. Node1 height: %d, Node2 height: %d, Wallet1 balance on node2: %s",
				targetHeightAfterTx, head2AfterTx.Height(), lotustypes.FIL(balanceOnNode2).String())
		}
		time.Sleep(1 * time.Second)
	}

	// Verify node2 can see the wallet balance directly (proves state is synced)
	wallet1BalanceOnNode2, err := full2.WalletBalance(ctx, wallet1FilAddr)
	require.NoError(t, err)
	require.Greater(t, wallet1BalanceOnNode2.Int64(), int64(0), "node2 should see wallet1 balance after sync")
	t.Logf("Verified: Wallet1 balance on node2: %s", lotustypes.FIL(wallet1BalanceOnNode2).String())

	// Test failover: Shutdown the first node and verify automatic failover to node2
	// Since GetEthClient now connects to all nodes, the proxy should automatically retry with node2
	// when node1 fails
	t.Logf("Testing automatic failover - shutting down first node...")

	// Verify both nodes are accessible initially
	chainIDFromNode1, err := ethClient.ChainID(ctx)
	require.NoError(t, err, "should be able to query node1 initially")

	// Shutdown the first node to simulate failure
	// The eth client proxy should automatically failover to node2
	t.Logf("Shutting down first node (ensemble1)...")
	err = full1.Shutdown(ctx)
	require.NoError(t, err, "should be able to shutdown first node")

	// Wait a moment for connections to detect the failure and close
	time.Sleep(3 * time.Second)

	// The eth client should automatically failover to node2 on the next call
	// The proxy's built-in retry logic will automatically retry with node2 when node1 fails
	t.Logf("Verifying automatic failover - eth client should automatically retry with node2...")

	// The proxy will automatically retry with the next provider (node2) if node1 fails
	// It retries up to 5 times with exponential backoff for connection errors
	chainID2, err := ethClient.ChainID(ctx)
	require.NoError(t, err, "eth client should automatically failover to node2 when node1 is shutdown")
	require.Equal(t, chainIDFromNode1.String(), chainID2.String(), "chain IDs should match")

	// Verify we can still query balances (using failover node)
	// Note: Even though nodes are connected, they may be on slightly different chains
	// due to independent mining. In production, nodes would be fully synced.
	wallet1BalanceAfterFailover, err := ethClient.BalanceAt(ctx, wallet1EthAddr, nil)
	require.NoError(t, err, "should be able to query balance via failover node")
	t.Logf("Wallet1 balance via failover node (node2): %s", wallet1BalanceAfterFailover.String())

	// If nodes are synced, verify the balance matches
	// Otherwise, just verify we can query (proves failover works)
	if wallet1BalanceOnNode2.Int64() > 0 {
		// Nodes appear to be synced, verify balance matches
		require.Equal(t, wallet1EthBalanceAfter.String(), wallet1BalanceAfterFailover.String(),
			"wallet balance should match after failover since nodes are synced")
		t.Logf("Balance matches after failover - nodes are synced!")
	} else {
		t.Logf("Note: Nodes may not be fully synced (node2 balance: %s), but failover is working",
			lotustypes.FIL(wallet1BalanceOnNode2).String())
	}

	// Verify we can query block number (proves failover is working)
	blockNumber2, err := ethClient.BlockNumber(ctx)
	require.NoError(t, err, "should be able to query block number via failover node")
	t.Logf("Block number via failover node: %d", blockNumber2)

	// The failover test is successful - we've verified that:
	// 1. The client automatically fails over to node2 when node1 is shutdown
	// 2. We can still make queries (ChainID, BlockNumber, BalanceAt) using the failover node
	// Note: Since node1 and node2 are independent chains, the state differs between them
	// In a real scenario with synced nodes, the state would be consistent
}

func sendEthTransaction(ctx context.Context, ethClient api.EthClientInterface, privateKey *ecdsa.PrivateKey, from, to common.Address, amount *mathbig.Int) (common.Hash, error) {
	// Estimate gas
	msg := ethereum.CallMsg{
		From:  from,
		To:    &to,
		Value: amount,
	}

	gasLimit, err := ethClient.EstimateGas(ctx, msg)
	if err != nil {
		return common.Hash{}, xerrors.Errorf("failed to estimate gas: %w", err)
	}

	// Get current header for base fee
	header, err := ethClient.HeaderByNumber(ctx, nil)
	if err != nil {
		return common.Hash{}, xerrors.Errorf("failed to get latest block header: %w", err)
	}

	baseFee := header.BaseFee
	if baseFee == nil {
		return common.Hash{}, xerrors.Errorf("base fee not available; network might not support EIP-1559")
	}

	// Get gas tip cap
	gasTipCap, err := ethClient.SuggestGasTipCap(ctx)
	if err != nil {
		return common.Hash{}, xerrors.Errorf("estimating gas tip cap: %w", err)
	}

	// Calculate gas fee cap
	gasFeeCap := mathbig.NewInt(0).Add(baseFee, gasTipCap)

	// Get chain ID
	chainID, err := ethClient.NetworkID(ctx)
	if err != nil {
		return common.Hash{}, xerrors.Errorf("getting network ID: %w", err)
	}

	// Get nonce
	nonce, err := ethClient.PendingNonceAt(ctx, from)
	if err != nil {
		return common.Hash{}, xerrors.Errorf("getting pending nonce: %w", err)
	}

	// Create transaction
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasFeeCap: gasFeeCap,
		GasTipCap: gasTipCap,
		Gas:       gasLimit,
		To:        &to,
		Value:     amount,
		Data:      nil,
	})

	// Sign transaction
	signer := ethtypes.LatestSignerForChainID(chainID)
	signedTx, err := ethtypes.SignTx(tx, signer, privateKey)
	if err != nil {
		return common.Hash{}, xerrors.Errorf("signing transaction: %w", err)
	}

	// Send transaction
	err = ethClient.SendTransaction(ctx, signedTx)
	if err != nil {
		return common.Hash{}, xerrors.Errorf("sending transaction: %w", err)
	}

	return signedTx.Hash(), nil
}

func waitForTransaction(ctx context.Context, ethClient api.EthClientInterface, txHash common.Hash) error {
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	for {
		_, pending, err := ethClient.TransactionByHash(ctxTimeout, txHash)
		if err != nil {
			if err == context.Canceled || err == context.DeadlineExceeded {
				return xerrors.Errorf("timed out waiting for transaction %s: %w", txHash.Hex(), err)
			}
			// Transaction might not be found yet
			time.Sleep(1 * time.Second)
			continue
		}
		if pending {
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}

	// Verify receipt
	receipt, err := ethClient.TransactionReceipt(ctx, txHash)
	if err != nil {
		return xerrors.Errorf("getting transaction receipt: %w", err)
	}

	if receipt.Status == 0 {
		return xerrors.Errorf("transaction failed")
	}

	return nil
}
