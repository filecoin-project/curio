package denylist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/multiformats/go-multibase"

	"github.com/filecoin-project/curio/deps/config"
)

var log = logging.Logger("denylist")

// refreshInterval is how often the denylist is re-fetched from servers.
const refreshInterval = 5 * time.Minute

// Filter manages denylist state and provides CID filtering.
// It fetches denylists from configured servers on startup and stores
// blocked hashes in an atomically-swapped in-memory map.
// Denylists are refreshed every 5 minutes and also when the dynamic
// config changes.
type Filter struct {
	// hashes holds the set of blocked SHA256 hex hashes.
	// nil means denylists have not been loaded yet (server not ready).
	hashes atomic.Pointer[map[string]struct{}]

	// servers holds the current server list for periodic refresh.
	servers atomic.Pointer[[]string]

	ctx context.Context
}

// denylistEntry represents a single entry in a denylist JSON array.
type denylistEntry struct {
	Anchor string `json:"anchor"`
}

// NewFilter creates a new denylist Filter and starts fetching denylists
// from the provided dynamic server list. The filter will reject all requests
// until at least one successful load completes. When the dynamic config
// changes, denylists are automatically re-fetched.
func NewFilter(ctx context.Context, servers *config.Dynamic[[]string]) *Filter {
	f := &Filter{ctx: ctx}
	// hashes starts as nil (not loaded yet)

	s := servers.Get()
	f.servers.Store(&s)
	go f.loadDenylists(ctx, s)

	// Re-fetch denylists when the server list changes at runtime
	servers.OnChange(func() {
		log.Infow("denylist servers config changed, reloading")
		s := servers.Get()
		f.servers.Store(&s)
		f.loadDenylists(ctx, s)
	})

	// Periodically refresh the denylist
	go f.refreshLoop(ctx)

	return f
}

// NewFilterForTest creates a Filter from a static server list (no dynamic config).
// This is intended for use in tests only.
func NewFilterForTest(ctx context.Context, servers []string) *Filter {
	f := &Filter{ctx: ctx}
	f.servers.Store(&servers)
	go f.loadDenylists(ctx, servers)
	return f
}

// loadDenylists fetches all configured denylist servers and merges
// their entries into a single set, then atomically stores it.
func (f *Filter) loadDenylists(ctx context.Context, servers []string) {
	merged := make(map[string]struct{})

	for _, serverURL := range servers {
		entries, err := fetchDenylist(ctx, serverURL)
		if err != nil {
			log.Errorw("failed to fetch denylist", "url", serverURL, "error", err)
			continue
		}
		for _, h := range entries {
			merged[h] = struct{}{}
		}
		log.Infow("loaded denylist", "url", serverURL, "entries", len(entries))
	}

	f.hashes.Store(&merged)
	log.Infow("denylist filter ready", "totalEntries", len(merged))
}

// refreshLoop periodically re-fetches the denylists at refreshInterval.
func (f *Filter) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s := f.servers.Load()
			if s == nil {
				continue
			}
			log.Debugw("periodic denylist refresh")
			f.loadDenylists(ctx, *s)
		}
	}
}

// fetchDenylist fetches a denylist JSON file from the given URL using
// streaming JSON decoding to handle large datasets efficiently.
func fetchDenylist(ctx context.Context, url string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching denylist: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return decodeDenylist(resp.Body)
}

// decodeDenylist uses streaming JSON decoding to parse a denylist from
// a reader. The format is a JSON array of objects with an "anchor" field.
func decodeDenylist(r io.Reader) ([]string, error) {
	dec := json.NewDecoder(r)

	// Read opening bracket
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("reading opening token: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, fmt.Errorf("expected opening '[', got %v", tok)
	}

	var hashes []string
	for dec.More() {
		var entry denylistEntry
		if err := dec.Decode(&entry); err != nil {
			return nil, fmt.Errorf("decoding entry: %w", err)
		}
		if entry.Anchor != "" {
			hashes = append(hashes, entry.Anchor)
		}
	}

	// Read closing bracket
	tok, err = dec.Token()
	if err != nil {
		return nil, fmt.Errorf("reading closing token: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != ']' {
		return nil, fmt.Errorf("expected closing ']', got %v", tok)
	}

	return hashes, nil
}

// IsReady returns true if the denylists have been loaded.
func (f *Filter) IsReady() bool {
	return f.hashes.Load() != nil
}

// IsDenied checks whether the given CID is in the denylist.
// Returns (denied, ready) where ready=false means the denylist
// has not been loaded yet.
func (f *Filter) IsDenied(c cid.Cid) (denied bool, ready bool) {
	hashesPtr := f.hashes.Load()
	if hashesPtr == nil {
		return false, false
	}

	h := CIDToHash(c)
	_, found := (*hashesPtr)[h]
	return found, true
}

// CIDToHash normalizes a CID and produces its denylist hash.
//
// The pipeline:
//  1. Convert CIDv0 to CIDv1 (using DagProtobuf codec)
//  2. Encode as base32 string
//  3. Append "/" to create a path-like representation
//  4. SHA256 hash the resulting bytes
//  5. Return lowercase hex encoding of the hash
func CIDToHash(c cid.Cid) string {
	// Convert CIDv0 to CIDv1
	if c.Version() == 0 {
		c = cid.NewCidV1(cid.DagProtobuf, c.Hash())
	}

	// Get base32 string representation
	cidStr, _ := c.StringOfBase(multibase.Base32)

	// Append "/" to create path-like string
	cidStr += "/"

	// SHA256 hash the bytes
	shaBytes := sha256.Sum256([]byte(cidStr))

	// Return lowercase hex encoding
	return hex.EncodeToString(shaBytes[:])
}
