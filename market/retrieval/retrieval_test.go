package retrieval

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/ipfs/go-cid"
	unixfstestutil "github.com/ipfs/go-unixfsnode/testutil"
	carv2 "github.com/ipld/go-car/v2"
	unixfsgen "github.com/ipld/go-fixtureplate/generator"
	cidlink "github.com/ipld/go-ipld-prime/linking/cid"
	"github.com/ipld/go-ipld-prime/storage/memstore"
	trustlesstestutil "github.com/ipld/go-trustless-utils/testutil"
	"github.com/stretchr/testify/require"
)

// TestIPFSPathRouting verifies that the IPFS router accepts full paths including nested components
// Note that Frisbii already has a full Trustless Gateway test suite and runs against the gateway
// conformance tests so this test is primarily to verify that our routing setup works as expected.
func TestIPFSPathRouting(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage and LinkSystem
	store := &trustlesstestutil.CorrectedMemStore{
		ParentStore: &memstore.Store{Bag: make(map[string][]byte)},
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.SetReadStorage(store)
	lsys.SetWriteStorage(store)
	lsys.TrustedStorage = true

	// Create a random reader for data generation
	rndSeed := int64(1234) // Fixed seed for reproducibility
	rndReader := io.LimitReader(rand.New(rand.NewSource(rndSeed)), int64(4<<30))

	// Generate a simple file for testing
	entity, err := unixfsgen.Parse("file:100B")
	require.NoError(t, err)
	rootEnt, err := entity.Generate(lsys, rndReader)
	require.NoError(t, err)

	// Create a directory structure with files for path testing
	dirEntity, err := unixfsgen.Parse("dir(file:50B{name:\"test.txt\"},file:75B{name:\"data.json\"})")
	require.NoError(t, err)
	dirRootEnt, err := dirEntity.Generate(lsys, rndReader)
	require.NoError(t, err)

	// Create a Provider with the LinkSystem
	provider := NewRetrievalProviderWithLinkSystem(ctx, lsys)

	// Set up router
	mux := chi.NewMux()
	Router(mux, provider)

	testCases := []struct {
		name         string
		method       string
		path         string
		expectMatch  bool
		expectStatus int
	}{
		{
			name:         "GET simple file",
			method:       http.MethodGet,
			path:         "/ipfs/" + rootEnt.Root.String(),
			expectMatch:  true,
			expectStatus: http.StatusOK,
		},
		{
			name:         "GET directory",
			method:       http.MethodGet,
			path:         "/ipfs/" + dirRootEnt.Root.String(),
			expectMatch:  true,
			expectStatus: http.StatusOK,
		},
		{
			name:         "GET directory with file path",
			method:       http.MethodGet,
			path:         "/ipfs/" + dirRootEnt.Root.String() + "/test.txt",
			expectMatch:  true,
			expectStatus: http.StatusOK,
		},
		{
			name:         "HEAD simple file",
			method:       http.MethodHead,
			path:         "/ipfs/" + rootEnt.Root.String(),
			expectMatch:  true,
			expectStatus: http.StatusOK,
		},
		{
			name:         "HEAD directory with path",
			method:       http.MethodHead,
			path:         "/ipfs/" + dirRootEnt.Root.String() + "/data.json",
			expectMatch:  true,
			expectStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			// Add Accept header for trustless gateway (CAR format)
			req.Header.Set("Accept", "application/vnd.ipld.car")
			w := httptest.NewRecorder()

			// Execute the request
			mux.ServeHTTP(w, req)

			// Verify the route was matched
			if tc.expectMatch {
				// Chi returns "404 page not found" for unmatched routes
				if w.Code == http.StatusNotFound {
					body := w.Body.String()
					require.NotContains(t, body, "404 page not found",
						"Chi router did not match path: %s", tc.path)
				}

				// Verify expected status
				if tc.expectStatus != 0 {
					require.Equal(t, tc.expectStatus, w.Code,
						"Unexpected status for path: %s (body: %s)", tc.path, w.Body.String())
				}
			}
		})
	}
}

// TestRouterSetup verifies that all expected routes are registered
func TestRouterSetup(t *testing.T) {
	ctx := context.Background()

	// Create minimal LinkSystem for router setup
	store := &trustlesstestutil.CorrectedMemStore{
		ParentStore: &memstore.Store{Bag: make(map[string][]byte)},
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.SetReadStorage(store)
	lsys.SetWriteStorage(store)

	provider := NewRetrievalProviderWithLinkSystem(ctx, lsys)

	mux := chi.NewMux()
	Router(mux, provider)

	// Test that non-IPFS routes work
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "Version")
}

// TestInfoEndpointVersion verifies that /info returns actual build version, not hardcoded "0.0.0"
func TestInfoEndpointVersion(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	w := httptest.NewRecorder()
	handleInfo(w, req)

	body := w.Body.String()
	require.NotContains(t, body, "0.0.0", "version should not be hardcoded 0.0.0")
}

// TestTrustlessGatewaySentinel tests the special sentinel CID (bafkqaaa - identity empty CID)
// which is used as a probe for trustless gateway conformance
func TestTrustlessGatewaySentinel(t *testing.T) {
	ctx := context.Background()

	// Create minimal LinkSystem
	store := &trustlesstestutil.CorrectedMemStore{
		ParentStore: &memstore.Store{Bag: make(map[string][]byte)},
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.SetReadStorage(store)
	lsys.SetWriteStorage(store)
	lsys.TrustedStorage = true

	provider := NewRetrievalProviderWithLinkSystem(ctx, lsys)
	mux := chi.NewMux()
	Router(mux, provider)

	testCases := []struct {
		name   string
		method string
		accept string
	}{
		{
			name:   "GET sentinel with CAR accept",
			method: http.MethodGet,
			accept: "application/vnd.ipld.car",
		},
		{
			name:   "GET sentinel with raw accept",
			method: http.MethodGet,
			accept: "application/vnd.ipld.raw",
		},
		{
			name:   "HEAD sentinel with CAR accept",
			method: http.MethodHead,
			accept: "application/vnd.ipld.car",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/ipfs/bafkqaaa", nil)
			req.Header.Set("Accept", tc.accept)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			// Sentinel should return 200 OK
			require.Equal(t, http.StatusOK, w.Code,
				"Sentinel bafkqaaa should return 200 OK")
		})
	}
}

// TestTrustlessGatewayHeaders validates that responses include required trustless gateway headers
func TestTrustlessGatewayHeaders(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage and LinkSystem
	store := &trustlesstestutil.CorrectedMemStore{
		ParentStore: &memstore.Store{Bag: make(map[string][]byte)},
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.SetReadStorage(store)
	lsys.SetWriteStorage(store)
	lsys.TrustedStorage = true

	// Generate a simple test file
	rndSeed := int64(5678)
	rndReader := io.LimitReader(rand.New(rand.NewSource(rndSeed)), int64(4<<30))
	entity, err := unixfsgen.Parse("file:100B")
	require.NoError(t, err)
	rootEnt, err := entity.Generate(lsys, rndReader)
	require.NoError(t, err)

	provider := NewRetrievalProviderWithLinkSystem(ctx, lsys)
	mux := chi.NewMux()
	Router(mux, provider)

	t.Run("CAR response headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipfs/"+rootEnt.Root.String(), nil)
		req.Header.Set("Accept", "application/vnd.ipld.car")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		// Verify required headers
		contentType := w.Header().Get("Content-Type")
		require.NotEmpty(t, contentType, "Content-Type header is required")
		require.Contains(t, contentType, "application/vnd.ipld.car",
			"Content-Type should indicate CAR format")

		contentDisposition := w.Header().Get("Content-Disposition")
		require.NotEmpty(t, contentDisposition, "Content-Disposition header is required")
		require.Contains(t, contentDisposition, "attachment",
			"Content-Disposition should be 'attachment'")

		etag := w.Header().Get("Etag")
		require.NotEmpty(t, etag, "Etag header is required")
	})
}

// TestTrustlessGatewayAcceptHeader tests content negotiation via Accept header
func TestTrustlessGatewayAcceptHeader(t *testing.T) {
	ctx := context.Background()

	// Create minimal LinkSystem
	store := &trustlesstestutil.CorrectedMemStore{
		ParentStore: &memstore.Store{Bag: make(map[string][]byte)},
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.SetReadStorage(store)
	lsys.SetWriteStorage(store)
	lsys.TrustedStorage = true

	// Generate a simple test file
	rndSeed := int64(9012)
	rndReader := io.LimitReader(rand.New(rand.NewSource(rndSeed)), int64(4<<30))
	entity, err := unixfsgen.Parse("file:50B")
	require.NoError(t, err)
	rootEnt, err := entity.Generate(lsys, rndReader)
	require.NoError(t, err)

	provider := NewRetrievalProviderWithLinkSystem(ctx, lsys)
	mux := chi.NewMux()
	Router(mux, provider)

	t.Run("Missing Accept header returns 400", func(t *testing.T) {
		// Note that Frisbii will be forgiving about a wildcard Accept header,
		// so we specifically omit the header entirely to test the strict case.

		req := httptest.NewRequest(http.MethodGet, "/ipfs/"+rootEnt.Root.String(), nil)
		// Intentionally omit Accept header
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		// Trustless gateways should return 400 when Accept header is missing
		require.Equal(t, http.StatusBadRequest, w.Code,
			"Missing Accept header should return 400 Bad Request")
	})

	t.Run("Valid Accept header returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipfs/"+rootEnt.Root.String(), nil)
		req.Header.Set("Accept", "application/vnd.ipld.car")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code,
			"Valid Accept header should return 200 OK")
	})
}

// TestTrustlessGatewayQueryParameters tests dag-scope and other query parameters
func TestTrustlessGatewayQueryParameters(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	store := &trustlesstestutil.CorrectedMemStore{
		ParentStore: &memstore.Store{Bag: make(map[string][]byte)},
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.SetReadStorage(store)
	lsys.SetWriteStorage(store)
	lsys.TrustedStorage = true

	// Generate a directory with files
	rndSeed := int64(3456)
	rndReader := io.LimitReader(rand.New(rand.NewSource(rndSeed)), int64(4<<30))
	dirEntity, err := unixfsgen.Parse("dir(file:50B{name:\"a.txt\"},file:75B{name:\"b.txt\"})")
	require.NoError(t, err)
	dirRootEnt, err := dirEntity.Generate(lsys, rndReader)
	require.NoError(t, err)

	provider := NewRetrievalProviderWithLinkSystem(ctx, lsys)
	mux := chi.NewMux()
	Router(mux, provider)

	testCases := []struct {
		name       string
		queryPath  string
		expectCode int
	}{
		{
			name:       "dag-scope=all",
			queryPath:  "/ipfs/" + dirRootEnt.Root.String() + "?dag-scope=all",
			expectCode: http.StatusOK,
		},
		{
			name:       "dag-scope=entity",
			queryPath:  "/ipfs/" + dirRootEnt.Root.String() + "?dag-scope=entity",
			expectCode: http.StatusOK,
		},
		{
			name:       "dag-scope=block",
			queryPath:  "/ipfs/" + dirRootEnt.Root.String() + "?dag-scope=block",
			expectCode: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.queryPath, nil)
			req.Header.Set("Accept", "application/vnd.ipld.car")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			require.Equal(t, tc.expectCode, w.Code,
				"Query parameter test %s failed", tc.name)
		})
	}
}

// collectAllCIDs recursively collects all unique CIDs from a DirEntry structure in traversal order
func collectAllCIDs(entry unixfstestutil.DirEntry) []cid.Cid {
	seen := make(map[cid.Cid]bool)
	var cids []cid.Cid

	var collect func(e unixfstestutil.DirEntry)
	collect = func(e unixfstestutil.DirEntry) {
		// Add root if not seen
		if !seen[e.Root] {
			cids = append(cids, e.Root)
			seen[e.Root] = true
		}

		// Add self CIDs if not seen
		for _, c := range e.SelfCids {
			if !seen[c] {
				cids = append(cids, c)
				seen[c] = true
			}
		}

		// Recurse into children
		for _, child := range e.Children {
			collect(child)
		}
	}

	collect(entry)
	return cids
}

// TestRawBlockRetrieval tests retrieving individual blocks with application/vnd.ipld.raw
// This is currently the primary use case for the retrieval endpoint by the Boxo/Kubo stack
func TestRawBlockRetrieval(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	store := &trustlesstestutil.CorrectedMemStore{
		ParentStore: &memstore.Store{Bag: make(map[string][]byte)},
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.SetReadStorage(store)
	lsys.SetWriteStorage(store)
	lsys.TrustedStorage = true

	// Generate a directory structure with multiple files to get multiple blocks
	rndSeed := int64(7890)
	rndReader := io.LimitReader(rand.New(rand.NewSource(rndSeed)), int64(4<<30))
	dirEntity, err := unixfsgen.Parse("dir(file:100B{name:\"a.txt\"},file:150B{name:\"b.json\"},file:200B{name:\"c.dat\"})")
	require.NoError(t, err)
	dirRootEnt, err := dirEntity.Generate(lsys, rndReader)
	require.NoError(t, err)

	// Collect all CIDs from the generated structure
	allCIDs := collectAllCIDs(dirRootEnt)
	require.NotEmpty(t, allCIDs, "Should have generated multiple CIDs")

	provider := NewRetrievalProviderWithLinkSystem(ctx, lsys)
	mux := chi.NewMux()
	Router(mux, provider)

	// Test retrieval of each individual block
	for i, c := range allCIDs {
		t.Run("GET raw block "+c.String()[:16], func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ipfs/"+c.String(), nil)
			req.Header.Set("Accept", "application/vnd.ipld.raw")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code,
				"Block %d (CID: %s) should be retrievable", i, c.String())

			// Verify Content-Type for raw block
			contentType := w.Header().Get("Content-Type")
			require.Contains(t, contentType, "application/vnd.ipld.raw",
				"Content-Type should indicate raw block format")

			// Verify we got data back
			require.NotEmpty(t, w.Body.Bytes(),
				"Response should contain block data")
		})
	}

	// Test HEAD requests for raw blocks
	t.Run("HEAD raw block", func(t *testing.T) {
		if len(allCIDs) > 0 {
			c := allCIDs[0]
			req := httptest.NewRequest(http.MethodHead, "/ipfs/"+c.String(), nil)
			req.Header.Set("Accept", "application/vnd.ipld.raw")
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code,
				"HEAD request should succeed for raw block")

			contentType := w.Header().Get("Content-Type")
			require.Contains(t, contentType, "application/vnd.ipld.raw",
				"Content-Type should indicate raw block format")
		}
	})
}

// TestMissingBlockReturns404 tests that missing root blocks return 404 as per trustless gateway spec
func TestMissingBlockReturns404(t *testing.T) {
	ctx := context.Background()

	// Create minimal empty LinkSystem
	store := &trustlesstestutil.CorrectedMemStore{
		ParentStore: &memstore.Store{Bag: make(map[string][]byte)},
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.SetReadStorage(store)
	lsys.SetWriteStorage(store)
	lsys.TrustedStorage = true

	provider := NewRetrievalProviderWithLinkSystem(ctx, lsys)
	mux := chi.NewMux()
	Router(mux, provider)

	// Create a CID that doesn't exist in the store
	nonExistentCID := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"

	testCases := []struct {
		name   string
		method string
		accept string
	}{
		{
			name:   "GET missing block with raw accept",
			method: http.MethodGet,
			accept: "application/vnd.ipld.raw",
		},
		{
			name:   "GET missing block with CAR accept",
			method: http.MethodGet,
			accept: "application/vnd.ipld.car",
		},
		{
			name:   "HEAD missing block with raw accept",
			method: http.MethodHead,
			accept: "application/vnd.ipld.raw",
		},
		{
			name:   "HEAD missing block with CAR accept",
			method: http.MethodHead,
			accept: "application/vnd.ipld.car",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/ipfs/"+nonExistentCID, nil)
			req.Header.Set("Accept", tc.accept)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			// Per trustless gateway spec, missing root blocks MUST return 404
			require.Equal(t, http.StatusNotFound, w.Code,
				"Missing root block should return 404 Not Found")
		})
	}
}

// TestDagScopeCAR validates that dag-scope parameter returns correct CAR contents
func TestDagScopeCAR(t *testing.T) {
	ctx := context.Background()

	// Create in-memory storage
	store := &trustlesstestutil.CorrectedMemStore{
		ParentStore: &memstore.Store{Bag: make(map[string][]byte)},
	}
	lsys := cidlink.DefaultLinkSystem()
	lsys.SetReadStorage(store)
	lsys.SetWriteStorage(store)
	lsys.TrustedStorage = true

	// Generate a directory structure with multiple files
	rndSeed := int64(1111)
	rndReader := io.LimitReader(rand.New(rand.NewSource(rndSeed)), int64(4<<30))
	dirEntity, err := unixfsgen.Parse("dir(file:100B{name:\"a.txt\"},file:150B{name:\"b.txt\"})")
	require.NoError(t, err)
	dirRootEnt, err := dirEntity.Generate(lsys, rndReader)
	require.NoError(t, err)

	// Collect all CIDs from the structure
	allCIDs := collectAllCIDs(dirRootEnt)
	require.NotEmpty(t, allCIDs, "Should have generated multiple CIDs")

	provider := NewRetrievalProviderWithLinkSystem(ctx, lsys)
	mux := chi.NewMux()
	Router(mux, provider)

	t.Run("dag-scope=block returns single block CAR", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipfs/"+dirRootEnt.Root.String()+"?dag-scope=block", nil)
		req.Header.Set("Accept", "application/vnd.ipld.car")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		// Parse the CAR response
		carReader, err := carv2.NewBlockReader(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err, "Should be able to parse CAR response")

		// Count blocks in CAR
		blockCount := 0
		for {
			block, err := carReader.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			blockCount++

			// For dag-scope=block, should only have the root block
			if blockCount == 1 {
				require.Equal(t, dirRootEnt.Root, block.Cid(),
					"First block should be the root CID")
			}
		}

		require.Equal(t, 1, blockCount,
			"dag-scope=block should return exactly 1 block")
	})

	t.Run("dag-scope=all returns all blocks CAR in correct order", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ipfs/"+dirRootEnt.Root.String()+"?dag-scope=all", nil)
		req.Header.Set("Accept", "application/vnd.ipld.car")
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		// Parse the CAR response
		carReader, err := carv2.NewBlockReader(bytes.NewReader(w.Body.Bytes()))
		require.NoError(t, err, "Should be able to parse CAR response")

		// Collect CIDs from CAR in order
		var carCIDs []cid.Cid
		for {
			block, err := carReader.Next()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			carCIDs = append(carCIDs, block.Cid())
		}

		// dag-scope=all should return all blocks from the fixture
		require.Len(t, carCIDs, len(allCIDs),
			"CAR should contain all blocks from the fixture")

		// Verify blocks are returned in traversal order
		require.Equal(t, allCIDs, carCIDs,
			"CAR blocks should be in traversal order matching fixture")
	})
}
