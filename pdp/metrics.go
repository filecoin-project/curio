package pdp

import (
	"context"
	"net"
	"net/http"
	"net/url"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var (
	// Tag keys for retrieval metrics
	domainTagKey, _    = tag.NewKey("domain")
	pieceSizeTagKey, _ = tag.NewKey("piece_size_category")
	pieceCIDTagKey, _  = tag.NewKey("piece_cid")
	statusTagKey, _    = tag.NewKey("status")

	pre = "curio_pdp_"

	// Duration buckets for response times (in milliseconds)
	durationBuckets = []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000}

	// Size buckets for piece sizes (in MB)
	sizeBuckets = []float64{0.1, 1, 10, 50, 100, 500, 1000, 5000, 10000}
)

var (
	// Measures for PDP piece access tracking (when PDP pieces are retrieved via /piece/{cid})
	PDPPieceAccess       = stats.Int64(pre+"piece_access", "Number of times PDP pieces are accessed via main retrieval endpoint", stats.UnitDimensionless)
	PDPPieceAccessDomain = stats.Int64(pre+"piece_access_by_domain", "Number of PDP piece accesses by domain", stats.UnitDimensionless)
	PDPPieceBytesServed  = stats.Int64(pre+"piece_bytes_served", "Total bytes served for PDP pieces", stats.UnitBytes)
	PDPPieceSize         = stats.Int64(pre+"piece_size", "Size distribution of PDP pieces accessed", stats.UnitBytes)
	PDPPieceTTFB         = stats.Float64(pre+"piece_ttfb", "Time to first byte for PDP piece retrieval", stats.UnitMilliseconds)
	PDPPieceRetrievals   = stats.Int64(pre+"piece_retrievals", "Number of PDP piece retrieval attempts by status", stats.UnitDimensionless)
)

func init() {
	err := view.Register(
		&view.View{
			Measure:     PDPPieceAccess,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{},
		},
		&view.View{
			Measure:     PDPPieceAccessDomain,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{domainTagKey, pieceCIDTagKey},
		},
		&view.View{
			Measure:     PDPPieceBytesServed,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{domainTagKey, pieceCIDTagKey},
		},
		&view.View{
			Measure:     PDPPieceSize,
			Aggregation: view.Distribution(sizeBuckets...),
			TagKeys:     []tag.Key{pieceSizeTagKey, pieceCIDTagKey},
		},
		&view.View{
			Measure:     PDPPieceTTFB,
			Aggregation: view.Distribution(durationBuckets...),
			TagKeys:     []tag.Key{domainTagKey, pieceSizeTagKey, pieceCIDTagKey},
		},
		&view.View{
			Measure:     PDPPieceRetrievals,
			Aggregation: view.Count(),
			TagKeys:     []tag.Key{statusTagKey, domainTagKey, pieceCIDTagKey},
		},
	)
	if err != nil {
		panic(err)
	}
}

// categorizeSize returns a category string for the given size in bytes
func categorizeSize(sizeBytes int64) string {
	sizeMB := float64(sizeBytes) / (1024 * 1024)

	switch {
	case sizeMB < 1:
		return "small"
	case sizeMB < 100:
		return "medium"
	case sizeMB < 1000:
		return "large"
	default:
		return "xlarge"
	}
}

// extractDomain extracts the domain from the request for metrics
func extractDomain(r *http.Request) string {
	// Try to get domain from Referer header first
	if referer := r.Header.Get("Referer"); referer != "" {
		if u, err := url.Parse(referer); err == nil && u.Host != "" {
			return u.Host
		}
	}

	// Try to get domain from Host header
	if host := r.Header.Get("Host"); host != "" {
		return host
	}

	// Try to get from X-Forwarded-Host header
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		return forwardedHost
	}

	// Fall back to remote address
	if remoteAddr := r.RemoteAddr; remoteAddr != "" {
		if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
			return host
		}
		return remoteAddr
	}

	return "unknown"
}

// RecordPDPPieceRetrieval records metrics for PDP piece retrieval attempts (both success and failure)
// This function should be called for all PDP piece retrieval attempts
func RecordPDPPieceRetrieval(ctx context.Context, r *http.Request, pieceCID string, status string) {
	domain := extractDomain(r)

	// Record retrieval attempt with status
	if retrievalCtx, err := tag.New(ctx, tag.Insert(statusTagKey, status), tag.Insert(domainTagKey, domain), tag.Insert(pieceCIDTagKey, pieceCID)); err == nil {
		stats.Record(retrievalCtx, PDPPieceRetrievals.M(1))
	}
}

// RecordPDPPieceAccess records metrics when a PDP piece is successfully accessed via the main retrieval endpoint
// This function should be called from the retrieval system when successfully serving PDP pieces
func RecordPDPPieceAccess(ctx context.Context, r *http.Request, pieceCID string, pieceSize int64, ttfbMs float64) {
	domain := extractDomain(r)
	sizeCategory := categorizeSize(pieceSize)

	// Record basic access
	stats.Record(ctx, PDPPieceAccess.M(1))

	// Record access by domain and piece CID
	if domainCtx, err := tag.New(ctx, tag.Insert(domainTagKey, domain), tag.Insert(pieceCIDTagKey, pieceCID)); err == nil {
		stats.Record(domainCtx, PDPPieceAccessDomain.M(1))
		stats.Record(domainCtx, PDPPieceBytesServed.M(pieceSize))
	}

	// Record piece size distribution
	if sizeCtx, err := tag.New(ctx, tag.Insert(pieceSizeTagKey, sizeCategory), tag.Insert(pieceCIDTagKey, pieceCID)); err == nil {
		stats.Record(sizeCtx, PDPPieceSize.M(pieceSize))
	}

	// Record TTFB with domain, size category, and piece CID tags
	if ttfbCtx, err := tag.New(ctx, tag.Insert(domainTagKey, domain), tag.Insert(pieceSizeTagKey, sizeCategory), tag.Insert(pieceCIDTagKey, pieceCID)); err == nil {
		stats.Record(ttfbCtx, PDPPieceTTFB.M(ttfbMs))
	}
}

// IsPDPPiece checks if a piece CID corresponds to a PDP piece by checking the database
// This function can be used by the retrieval system to identify PDP pieces
func IsPDPPiece(ctx context.Context, db interface{}, pieceCID string) bool {
	hdb, ok := db.(*harmonydb.DB)
	if !ok {
		return false
	}

	var count int
	err := hdb.QueryRow(ctx, `
		SELECT 1 FROM pdp_piecerefs 
		WHERE piece_cid = $1
		LIMIT 1
	`, pieceCID).Scan(&count)

	return err == nil && count == 1
}
