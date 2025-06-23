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
	lru "github.com/hashicorp/golang-lru/v2"
)

const NonceExpiry = 60 * time.Second
const SkewAllowance = 5 * time.Second
const AddressCacheSize = 1000
const AddressCacheTTL = 5 * time.Minute

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

// AddressCacheEntry holds cached address resolution with TTL
type AddressCacheEntry struct {
	AccountKey address.Address
	ExpiresAt  time.Time
}

// AddressResolver provides cached StateAccountKey lookups
type AddressResolver struct {
	wallet WalletAPI
	cache  *lru.Cache[string, AddressCacheEntry]
	mu     sync.Mutex
}

// NewAddressResolver creates a new cached address resolver
func NewAddressResolver(wallet WalletAPI) (*AddressResolver, error) {
	cache, err := lru.New[string, AddressCacheEntry](AddressCacheSize)
	if err != nil {
		return nil, xerrors.Errorf("failed to create LRU cache: %w", err)
	}

	return &AddressResolver{
		wallet: wallet,
		cache:  cache,
	}, nil
}

// ResolveAccountKey resolves an address to its account key with caching
func (ar *AddressResolver) ResolveAccountKey(ctx context.Context, addr address.Address) (address.Address, error) {
	addrStr := addr.String()
	now := time.Now()

	ar.mu.Lock()
	if entry, ok := ar.cache.Get(addrStr); ok && now.Before(entry.ExpiresAt) {
		ar.mu.Unlock()
		return entry.AccountKey, nil
	}
	ar.mu.Unlock()

	// Cache miss or expired, lookup from state
	accountKey, err := ar.wallet.StateAccountKey(ctx, addr, types.EmptyTSK)
	if err != nil {
		return address.Undef, xerrors.Errorf("failed to lookup account key for %s: %w", addr, err)
	}

	// Cache the result
	ar.mu.Lock()
	ar.cache.Add(addrStr, AddressCacheEntry{
		AccountKey: accountKey,
		ExpiresAt:  now.Add(AddressCacheTTL),
	})
	ar.mu.Unlock()

	return accountKey, nil
}

// NonceKey represents a unique combination of account key and nonce ID
type NonceKey struct {
	AccountKey string // canonical account key string for consistent hashing
	NonceID    uint64
}

// NonceCache provides thread-safe replay protection using rotating time buckets
type NonceCache struct {
	mu          sync.Mutex
	buckets     [2]map[NonceKey]struct{} // Two rotating buckets
	initOnce    sync.Once
	lastCleanup time.Time
}

// NewNonceCache creates a new NonceCache instance
func NewNonceCache() *NonceCache {
	return &NonceCache{
		lastCleanup: time.Now(),
	}
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
func (nc *NonceCache) AddNonce(accountKey address.Address, nonceID uint64, nonceTime time.Time) bool {
	nc.ensureInit()
	nc.mu.Lock()
	defer nc.mu.Unlock()

	now := time.Now()

	// Reject future timestamps beyond skew allowance
	if nonceTime.After(now.Add(SkewAllowance)) {
		return false // Future timestamp
	}

	// Check if nonce time is still valid (not too old)
	if now.Sub(nonceTime) > NonceExpiry {
		return false // Expired
	}

	key := NonceKey{
		AccountKey: accountKey.String(), // Use canonical account key
		NonceID:    nonceID,
	}

	// Use current time for bucket index to prevent malicious timestamp attacks
	currentBucket := nc.getBucketIndex(now)
	oldBucket := 1 - currentBucket

	// Check if nonce already exists in either bucket (race protection)
	if _, exists := nc.buckets[currentBucket][key]; exists {
		return false // Already exists
	}
	if _, exists := nc.buckets[oldBucket][key]; exists {
		return false // Already exists
	}

	// Clear old bucket if we've moved to a new time window (based on actual time)
	if nc.getBucketIndex(now) != nc.getBucketIndex(nc.lastCleanup) {
		// We're in a new time window, clear the old bucket
		nc.buckets[oldBucket] = make(map[NonceKey]struct{})
		nc.lastCleanup = now
	}

	// Add to current bucket (based on actual time, not nonce time)
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

// Sign creates a signature for the given data using the provided wallet API and returns it as hex
func Sign(ctx context.Context, resolver *AddressResolver, signer address.Address, dst string, data []byte, nonceTime time.Time) (string, error) {
	// Lookup the account key for the signer
	accountKey, err := resolver.ResolveAccountKey(ctx, signer)
	if err != nil {
		return "", err
	}

	// Generate random nonce ID
	nonceID, err := generateRandomNonce()
	if err != nil {
		return "", xerrors.Errorf("failed to generate nonce ID: %w", err)
	}

	// Create the signature structure
	sig := &Signature{
		NonceTime: uint64(nonceTime.Unix()),
		NonceID:   nonceID,
		Sig:       nil, // Will be filled after signing
	}

	// Create the signed message structure
	signedMsg := &SignedMsg{
		Data:   append([]byte(dst), data...),
		Signer: accountKey,
		Sig:    *sig,
	}

	// Get the bytes to sign using SigningBytes method
	signingBytes := signedMsg.SigningBytes()

	// Sign the data using the account key
	cryptoSig, err := resolver.wallet.WalletSign(ctx, accountKey, signingBytes)
	if err != nil {
		return "", xerrors.Errorf("failed to sign data with key %s: %w", accountKey, err)
	}

	// Store the signature bytes (including type prefix)
	sig.Sig = append([]byte{byte(cryptoSig.Type)}, cryptoSig.Data...)

	// Serialize to CBOR and return as hex
	var buf bytes.Buffer
	if err := sig.MarshalCBOR(&buf); err != nil {
		return "", xerrors.Errorf("failed to marshal signature: %w", err)
	}

	return hex.EncodeToString(buf.Bytes()), nil
}

// Verify verifies a signature against the given data using the provided resolver and nonce cache
func Verify(ctx context.Context, resolver *AddressResolver, nonceCache *NonceCache, signer address.Address, data []byte, sig *Signature) (bool, error) {
	if sig == nil {
		return false, xerrors.New("signature is nil")
	}

	// Lookup the canonical account key for the signer
	accountKey, err := resolver.ResolveAccountKey(ctx, signer)
	if err != nil {
		return false, err
	}

	// Validate signature timestamp
	sigTime := time.Unix(int64(sig.NonceTime), 0)
	now := time.Now()

	// Reject future timestamps beyond skew allowance
	if sigTime.After(now.Add(SkewAllowance)) {
		return false, xerrors.New("signature timestamp too far in future")
	}

	// Check if signature has expired
	if now.Sub(sigTime) > NonceExpiry {
		return false, xerrors.New("signature has expired")
	}

	// Create the signed message structure for verification
	signedMsg := &SignedMsg{
		Data:   data,
		Signer: accountKey, // Use canonical account key
		Sig:    *sig,       // Copy the signature
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
	valid, err := resolver.wallet.WalletVerify(ctx, accountKey, signingBytes, cryptoSig)
	if err != nil {
		return false, xerrors.Errorf("failed to verify signature with key %s: %w", accountKey, err)
	}

	// If signature is valid, try to add nonce to cache to prevent replay
	// AddNonce will return false if nonce already exists (replay) or is expired
	if valid {
		if !nonceCache.AddNonce(accountKey, sig.NonceID, sigTime) {
			return false, xerrors.New("nonce already used or expired (replay attack)")
		}
	}

	return valid, nil
}

// VerifyHexSig deserializes a hex-encoded signature and verifies it
func VerifyHexSig(ctx context.Context, resolver *AddressResolver, nonceCache *NonceCache, signer address.Address, dst string, data []byte, hexSig string) error {
	// Decode hex string to bytes
	sigBytes, err := hex.DecodeString(hexSig)
	if err != nil {
		return xerrors.Errorf("failed to decode hex signature: %w", err)
	}

	// Deserialize signature from CBOR bytes
	var sig Signature
	if err := sig.UnmarshalCBOR(bytes.NewReader(sigBytes)); err != nil {
		return xerrors.Errorf("failed to unmarshal signature: %w", err)
	}

	// Use the existing Verify function
	valid, err := Verify(ctx, resolver, nonceCache, signer, append([]byte(dst), data...), &sig)
	if err != nil {
		return err
	}
	if !valid {
		return xerrors.New("signature verification failed")
	}
	return nil
}
