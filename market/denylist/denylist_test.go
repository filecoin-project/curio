package denylist

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"

	"github.com/filecoin-project/curio/deps/config"
)

// makeCIDv0 creates a CIDv0 from a string for testing.
func makeCIDv0(data string) cid.Cid {
	hash, _ := mh.Sum([]byte(data), mh.SHA2_256, -1)
	return cid.NewCidV0(hash)
}

// makeCIDv1 creates a CIDv1 from a string for testing.
func makeCIDv1(data string) cid.Cid {
	hash, _ := mh.Sum([]byte(data), mh.SHA2_256, -1)
	return cid.NewCidV1(cid.Raw, hash)
}

func TestCIDToHash(t *testing.T) {
	// Verify that the hash is deterministic and non-empty
	c := makeCIDv0("hello world")
	h1 := CIDToHash(c)
	h2 := CIDToHash(c)

	if h1 != h2 {
		t.Fatalf("expected deterministic hash, got %s and %s", h1, h2)
	}
	if len(h1) != 64 { // SHA256 hex = 64 chars
		t.Fatalf("expected 64 char hex hash, got %d chars: %s", len(h1), h1)
	}

	// Verify all characters are lowercase hex
	for _, ch := range h1 {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			t.Fatalf("expected lowercase hex, got char %c in %s", ch, h1)
		}
	}
}

func TestCIDToHash_V0V1Equivalence(t *testing.T) {
	// CIDv0 and its CIDv1 equivalent (with DagProtobuf codec) should produce the same hash
	hash, _ := mh.Sum([]byte("test data"), mh.SHA2_256, -1)
	v0 := cid.NewCidV0(hash)
	v1 := cid.NewCidV1(cid.DagProtobuf, hash)

	h0 := CIDToHash(v0)
	h1 := CIDToHash(v1)

	if h0 != h1 {
		t.Fatalf("expected CIDv0 and CIDv1 (DagProtobuf) to produce same hash, got %s and %s", h0, h1)
	}
}

func TestCIDToHash_DifferentCIDs(t *testing.T) {
	c1 := makeCIDv0("data1")
	c2 := makeCIDv0("data2")

	h1 := CIDToHash(c1)
	h2 := CIDToHash(c2)

	if h1 == h2 {
		t.Fatal("expected different CIDs to produce different hashes")
	}
}

func TestDecodeDenylist(t *testing.T) {
	input := `[{"anchor":"abc123"},{"anchor":"def456"},{"anchor":""}]`
	hashes, _, err := decodeDenylist(strings.NewReader(input), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hashes) != 2 { // empty anchor should be skipped
		t.Fatalf("expected 2 hashes, got %d", len(hashes))
	}
	if hashes[0] != "abc123" {
		t.Fatalf("expected abc123, got %s", hashes[0])
	}
	if hashes[1] != "def456" {
		t.Fatalf("expected def456, got %s", hashes[1])
	}
}

func TestDecodeDenylist_Empty(t *testing.T) {
	input := `[]`
	hashes, _, err := decodeDenylist(strings.NewReader(input), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hashes) != 0 {
		t.Fatalf("expected 0 hashes, got %d", len(hashes))
	}
}

func TestDecodeDenylist_InvalidJSON(t *testing.T) {
	_, _, err := decodeDenylist(strings.NewReader(`not json`), "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFilter_NotReadyBeforeLoad(t *testing.T) {
	f := &Filter{}
	// Before loading, hashes is nil
	if f.IsReady() {
		t.Fatal("expected filter to not be ready before loading")
	}

	c := makeCIDv0("test")
	denied, ready := f.IsDenied(c)
	if ready {
		t.Fatal("expected not ready")
	}
	if denied {
		t.Fatal("expected not denied when not ready")
	}
}

func TestFilter_IsDenied(t *testing.T) {
	c := makeCIDv0("blocked content")
	h := CIDToHash(c)

	f := &Filter{}
	m := map[string]struct{}{h: {}}
	f.hashes.Store(&m)

	denied, ready := f.IsDenied(c)
	if !ready {
		t.Fatal("expected ready")
	}
	if !denied {
		t.Fatal("expected CID to be denied")
	}

	// Different CID should not be denied
	c2 := makeCIDv0("allowed content")
	denied2, ready2 := f.IsDenied(c2)
	if !ready2 {
		t.Fatal("expected ready")
	}
	if denied2 {
		t.Fatal("expected CID to not be denied")
	}
}

func TestFilter_IsDenied_PieceCIDv2MatchesPieceCIDv1Hash(t *testing.T) {
	commD := sha256.Sum256([]byte("piece-v2-denylist-equivalence"))

	pieceCIDv1, err := commcid.DataCommitmentV1ToCID(commD[:])
	if err != nil {
		t.Fatalf("failed to create PieceCIDv1: %v", err)
	}
	pieceCIDv2, err := commcid.PieceCidV2FromV1(pieceCIDv1, 127)
	if err != nil {
		t.Fatalf("failed to create PieceCIDv2: %v", err)
	}

	h := CIDToHash(pieceCIDv1)
	f := &Filter{}
	m := map[string]struct{}{h: {}}
	f.hashes.Store(&m)

	denied, ready := f.IsDenied(pieceCIDv2)
	if !ready {
		t.Fatal("expected ready")
	}
	if !denied {
		t.Fatal("expected PieceCIDv2 to be denied when PieceCIDv1 hash is denylisted")
	}
}

func TestNewFilter_WithHTTPServer(t *testing.T) {
	// Create a test denylist
	c := makeCIDv1("blocked-item")
	h := CIDToHash(c)

	entries := []denylistEntry{{Anchor: h}, {Anchor: "otherhash123"}}
	data, _ := json.Marshal(entries)

	// Serve the denylist over HTTP
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f := NewFilterForTest(ctx, []string{ts.URL})

	// Wait for the filter to become ready
	deadline := time.Now().Add(5 * time.Second)
	for !f.IsReady() {
		if time.Now().After(deadline) {
			t.Fatal("filter did not become ready in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify the blocked CID is denied
	denied, ready := f.IsDenied(c)
	if !ready {
		t.Fatal("expected ready")
	}
	if !denied {
		t.Fatal("expected CID to be denied")
	}
}

func TestMiddleware_NotReady(t *testing.T) {
	f := &Filter{} // not loaded yet

	c := makeCIDv0("test")
	handler := Middleware(f)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/ipfs/%s", c.String()), nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}

func TestMiddleware_Denied(t *testing.T) {
	c := makeCIDv0("blocked")
	h := CIDToHash(c)

	f := &Filter{}
	m := map[string]struct{}{h: {}}
	f.hashes.Store(&m)

	handler := Middleware(f)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/ipfs/%s", c.String()), nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnavailableForLegalReasons {
		t.Fatalf("expected 451, got %d", rr.Code)
	}
}

func TestMiddleware_Allowed(t *testing.T) {
	c := makeCIDv0("allowed")

	f := &Filter{}
	m := map[string]struct{}{} // empty denylist
	f.hashes.Store(&m)

	handler := Middleware(f)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/ipfs/%s", c.String()), nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestMiddleware_PiecePath(t *testing.T) {
	c := makeCIDv1("blocked-piece")
	h := CIDToHash(c)

	f := &Filter{}
	m := map[string]struct{}{h: {}}
	f.hashes.Store(&m)

	handler := Middleware(f)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/piece/%s", c.String()), nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnavailableForLegalReasons {
		t.Fatalf("expected 451, got %d", rr.Code)
	}
}

func TestMiddleware_NoCIDPath(t *testing.T) {
	f := &Filter{} // not loaded - but non-CID paths should pass through

	handler := Middleware(f)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestExtractCID(t *testing.T) {
	c := makeCIDv1("test")
	cidStr := c.String()

	tests := []struct {
		path    string
		wantOK  bool
		wantCID cid.Cid
	}{
		{"/ipfs/" + cidStr, true, c},
		{"/ipfs/" + cidStr + "/path/to/file", true, c},
		{"/piece/" + cidStr, true, c},
		{"/info", false, cid.Undef},
		{"/ipfs/", false, cid.Undef},
		{"/piece/", false, cid.Undef},
		{"/ipfs/notacid", false, cid.Undef},
		{"/other/" + cidStr, false, cid.Undef},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := extractCID(tt.path)
			if ok != tt.wantOK {
				t.Fatalf("extractCID(%q) ok=%v, want %v", tt.path, ok, tt.wantOK)
			}
			if ok && got != tt.wantCID {
				t.Fatalf("extractCID(%q) = %s, want %s", tt.path, got, tt.wantCID)
			}
		})
	}
}

func TestNewFilter_FailedServer(t *testing.T) {
	// Filter should still become ready (with empty denylist) even if server fails
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f := NewFilterForTest(ctx, []string{ts.URL})

	deadline := time.Now().Add(5 * time.Second)
	for !f.IsReady() {
		if time.Now().After(deadline) {
			t.Fatal("filter did not become ready in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Should be ready with empty denylist - nothing blocked
	c := makeCIDv0("anything")
	denied, ready := f.IsDenied(c)
	if !ready {
		t.Fatal("expected ready")
	}
	if denied {
		t.Fatal("expected not denied with empty denylist")
	}
}

func TestNewFilter_DynamicReload(t *testing.T) {
	c := makeCIDv1("will-be-blocked")
	h := CIDToHash(c)

	// First server: empty denylist
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}))
	defer ts1.Close()

	// Second server: denylist with the blocked CID
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		entries := []denylistEntry{{Anchor: h}}
		data, _ := json.Marshal(entries)
		_, _ = w.Write(data)
	}))
	defer ts2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start with the empty-denylist server
	dynServers := config.NewDynamic([]string{ts1.URL})

	f := NewFilter(ctx, dynServers)

	// Wait for initial load
	deadline := time.Now().Add(5 * time.Second)
	for !f.IsReady() {
		if time.Now().After(deadline) {
			t.Fatal("filter did not become ready in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Initially the CID should not be denied
	denied, ready := f.IsDenied(c)
	if !ready {
		t.Fatal("expected ready")
	}
	if denied {
		t.Fatal("expected CID to not be denied initially")
	}

	// Switch to the server that has the blocked entry — this is a real value change
	// so Dynamic's change detection will fire the OnChange callback
	dynServers.Set([]string{ts2.URL})

	// Wait for the OnChange callback to reload
	deadline = time.Now().Add(5 * time.Second)
	for {
		denied, _ = f.IsDenied(c)
		if denied {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("CID was not denied after dynamic config reload")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLoadDenylists_PeriodicUnchangedPreservesHashesAndSkipsGet(t *testing.T) {
	c := makeCIDv1("already-blocked")
	h := CIDToHash(c)
	const etag = "\"bafy-unchanged\""

	var getCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("etag", etag)
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodGet {
			getCount.Add(1)
			_, _ = w.Write([]byte("[]"))
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer ts.Close()

	f := &Filter{}
	current := map[string]struct{}{h: {}}
	f.hashes.Store(&current)
	servers := map[string]string{ts.URL: etag}
	f.servers.Store(&servers)

	f.loadDenylists(context.Background(), servers, false)

	if getCount.Load() != 0 {
		t.Fatalf("expected no GET on unchanged etag, got %d", getCount.Load())
	}
	denied, ready := f.IsDenied(c)
	if !ready {
		t.Fatal("expected ready")
	}
	if !denied {
		t.Fatal("expected existing hash to remain denylisted on unchanged etag")
	}
}

func TestLoadDenylists_PeriodicChangedEmptyListUpdatesEtag(t *testing.T) {
	const oldEtag = "\"bafy-old\""
	const newEtag = "\"bafy-new\""

	var getCount atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("etag", newEtag)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			getCount.Add(1)
			w.Header().Set("etag", newEtag)
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer ts.Close()

	f := &Filter{}
	servers := map[string]string{ts.URL: oldEtag}
	f.servers.Store(&servers)

	f.loadDenylists(context.Background(), servers, false)

	if getCount.Load() != 1 {
		t.Fatalf("expected 1 GET on changed etag, got %d", getCount.Load())
	}
	updated := f.servers.Load()
	if updated == nil {
		t.Fatal("expected servers map to be stored")
	}
	if (*updated)[ts.URL] != newEtag {
		t.Fatalf("expected etag to update to %s, got %s", newEtag, (*updated)[ts.URL])
	}
}
