package proofsvc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"sync"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/chain/types"
)

const NonceExpiry = 60 * time.Second

//go:generate cbor-gen-for --map-encoding Signature SignedMsg

// SignedMsg is the signed payload, Sig.Sig = nil for signing bytes
type SignedMsg struct {
	Data []byte

	Signer address.Address
	Sig    Signature
}

func (s *SignedMsg) SigningBytes() []byte {
	scopy := *s
	scopy.Sig.Sig = nil
	var buf bytes.Buffer
	if err := scopy.MarshalCBOR(&buf); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

type Signature struct {
	NonceTime uint64 // unix timestamp
	NonceID   uint64
	Sig       []byte
}

// WalletAPI defines the interface for wallet operations
type WalletAPI interface {
	StateAccountKey(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	WalletSign(context.Context, address.Address, []byte) (*crypto.Signature, error)
	WalletVerify(context.Context, address.Address, []byte, *crypto.Signature) (bool, error)
}

// NonceKey represents a unique combination of signer and nonce ID
type NonceKey struct {
	Signer  string // address string for consistent hashing
	NonceID uint64
}

// NonceCache provides thread-safe replay protection using rotating time buckets
type NonceCache struct {
	mu       sync.Mutex
	buckets  [2]map[NonceKey]struct{} // Two rotating buckets
	initOnce sync.Once
}

// NewNonceCache creates a new NonceCache instance
func NewNonceCache() *NonceCache {
	return &NonceCache{}
}

// ensureInit lazily initializes the cache buckets
func (nc *NonceCache) ensureInit() {
	nc.initOnce.Do(func() {
		nc.buckets[0] = make(map[NonceKey]struct{})
		nc.buckets[1] = make(map[NonceKey]struct{})
	})
}

// getBucketIndex returns the current bucket index based on time
func (nc *NonceCache) getBucketIndex(t time.Time) int {
	return int(t.Unix()/int64(NonceExpiry.Seconds())) % 2
}

// AddNonce adds a nonce to the current time bucket, returns false if already exists or invalid
func (nc *NonceCache) AddNonce(signer address.Address, nonceID uint64, nonceTime time.Time) bool {
	nc.ensureInit()
	nc.mu.Lock()
	defer nc.mu.Unlock()

	// Check if nonce time is still valid
	if time.Since(nonceTime) > NonceExpiry {
		return false // Expired
	}

	key := NonceKey{
		Signer:  signer.String(),
		NonceID: nonceID,
	}

	currentBucket := nc.getBucketIndex(nonceTime)
	oldBucket := 1 - currentBucket

	// Check if nonce already exists in either bucket (race protection)
	if _, exists := nc.buckets[currentBucket][key]; exists {
		return false // Already exists
	}
	if _, exists := nc.buckets[oldBucket][key]; exists {
		return false // Already exists
	}

	// Clear old bucket if we've moved to a new time window
	now := time.Now()
	if nc.getBucketIndex(now) != nc.getBucketIndex(now.Add(-NonceExpiry)) {
		// We're in a new time window, clear the old bucket
		nc.buckets[oldBucket] = make(map[NonceKey]struct{})
	}

	// Add to current bucket
	nc.buckets[currentBucket][key] = struct{}{}
	return true
}

// generateRandomNonce generates a random 64-bit nonce ID
func generateRandomNonce() (uint64, error) {
	max := new(big.Int)
	max.SetUint64(^uint64(0)) // max uint64

	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return 0, xerrors.Errorf("failed to generate random nonce: %w", err)
	}

	return n.Uint64(), nil
}

// Sign creates a signature for the given data using the provided wallet API
func Sign(ctx context.Context, wallet WalletAPI, signer address.Address, data []byte, nonceTime time.Time) (*Signature, error) {
	// Lookup the account key for the signer
	accountKey, err := wallet.StateAccountKey(ctx, signer, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("failed to lookup account key for %s: %w", signer, err)
	}

	// Generate random nonce ID
	nonceID, err := generateRandomNonce()
	if err != nil {
		return nil, xerrors.Errorf("failed to generate nonce ID: %w", err)
	}

	// Create the signed message structure
	signedMsg := &SignedMsg{
		Data:   data,
		Signer: accountKey,
		Sig: Signature{
			NonceTime: uint64(nonceTime.Unix()),
			NonceID:   nonceID,
			Sig:       nil, // Will be filled after signing
		},
	}

	// Get the bytes to sign using SigningBytes method
	signingBytes := signedMsg.SigningBytes()

	// Sign the data using the account key
	cryptoSig, err := wallet.WalletSign(ctx, accountKey, signingBytes)
	if err != nil {
		return nil, xerrors.Errorf("failed to sign data with key %s: %w", accountKey, err)
	}

	// Store the signature bytes (including type prefix)
	signedMsg.Sig.Sig = append([]byte{byte(cryptoSig.Type)}, cryptoSig.Data...)

	return &signedMsg.Sig, nil
}

// Verify verifies a signature against the given data using the provided wallet API
func Verify(ctx context.Context, wallet WalletAPI, nonceCache *NonceCache, signer address.Address, data []byte, sig *Signature) (bool, error) {
	if sig == nil {
		return false, xerrors.New("signature is nil")
	}

	// Lookup the account key for the signer
	accountKey, err := wallet.StateAccountKey(ctx, signer, types.EmptyTSK)
	if err != nil {
		return false, xerrors.Errorf("failed to lookup account key for %s: %w", signer, err)
	}

	// Check if signature has expired
	sigTime := time.Unix(int64(sig.NonceTime), 0)
	if time.Since(sigTime) > NonceExpiry {
		return false, xerrors.New("signature has expired")
	}

	// Create the signed message structure for verification
	signedMsg := &SignedMsg{
		Data:   data,
		Signer: accountKey,
		Sig:    *sig, // Copy the signature
	}

	// Get the bytes that were signed using SigningBytes method
	signingBytes := signedMsg.SigningBytes()

	// Extract crypto.Signature from our signature
	if len(sig.Sig) < 1 {
		return false, xerrors.New("invalid signature: too short")
	}

	cryptoSig := &crypto.Signature{
		Type: crypto.SigType(sig.Sig[0]),
		Data: sig.Sig[1:],
	}

	// Verify using the account key
	valid, err := wallet.WalletVerify(ctx, accountKey, signingBytes, cryptoSig)
	if err != nil {
		return false, xerrors.Errorf("failed to verify signature with key %s: %w", accountKey, err)
	}

	// If signature is valid, try to add nonce to cache to prevent replay
	// AddNonce will return false if nonce already exists (replay) or is expired
	if valid {
		if !nonceCache.AddNonce(signer, sig.NonceID, sigTime) {
			return false, xerrors.New("nonce already used or expired (replay attack)")
		}
	}

	return valid, nil
}

// VerifyHexSig deserializes a hex-encoded signature and verifies it
func VerifyHexSig(ctx context.Context, wallet WalletAPI, nonceCache *NonceCache, signer address.Address, data []byte, hexSig string) (bool, error) {
	// Decode hex string to bytes
	sigBytes, err := hex.DecodeString(hexSig)
	if err != nil {
		return false, xerrors.Errorf("failed to decode hex signature: %w", err)
	}

	// Deserialize signature from CBOR bytes
	var sig Signature
	if err := sig.UnmarshalCBOR(bytes.NewReader(sigBytes)); err != nil {
		return false, xerrors.Errorf("failed to unmarshal signature: %w", err)
	}

	// Use the existing Verify function
	return Verify(ctx, wallet, nonceCache, signer, data, &sig)
}
