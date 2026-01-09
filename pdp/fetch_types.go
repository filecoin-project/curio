package pdp

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// allowHTTPFetch allows HTTP (not just HTTPS) for fetch source URLs.
// Set CURIO_ALLOW_HTTP_FETCH=1 for development/testing environments.
var allowHTTPFetch = os.Getenv("CURIO_ALLOW_HTTP_FETCH") == "1"

// FetchStatus represents the status of a fetch operation or piece
type FetchStatus string

const (
	FetchStatusPending    FetchStatus = "pending"
	FetchStatusInProgress FetchStatus = "inProgress"
	FetchStatusRetrying   FetchStatus = "retrying"
	FetchStatusComplete   FetchStatus = "complete"
	FetchStatusFailed     FetchStatus = "failed"
)

// piecePathPattern matches URLs ending with /piece/{cid}
var piecePathPattern = regexp.MustCompile(`/piece/([^/]+)$`)

// ValidateFetchSourceURL validates that a source URL is safe and properly formatted
// for fetching a piece from another SP.
//
// Validation rules:
//   - Must be HTTPS
//   - Path must end with /piece/{pieceCid}
//   - The pieceCid in the URL must match the expected pieceCid
//   - Host must not be localhost, private IP, or link-local address
func ValidateFetchSourceURL(sourceURL string, expectedPieceCid string) error {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS (or HTTP if explicitly allowed for development)
	if parsed.Scheme != "https" && (!allowHTTPFetch || parsed.Scheme != "http") {
		return fmt.Errorf("URL must use HTTPS scheme, got %q", parsed.Scheme)
	}

	// Validate path matches /piece/{cid} pattern
	matches := piecePathPattern.FindStringSubmatch(parsed.Path)
	if matches == nil {
		return fmt.Errorf("URL path must end with /piece/{pieceCid}, got %q", parsed.Path)
	}

	// Extract pieceCid from URL and compare with expected
	urlPieceCid := matches[1]
	if urlPieceCid != expectedPieceCid {
		return fmt.Errorf("pieceCid in URL %q does not match expected %q", urlPieceCid, expectedPieceCid)
	}

	// Validate host is not a private/local address
	if err := validatePublicHost(parsed.Host); err != nil {
		return err
	}

	return nil
}

// validatePublicHost ensures the host is not localhost, a private IP, or link-local address
func validatePublicHost(host string) error {
	// Strip port if present
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	// Strip brackets from IPv6 addresses (e.g., [::1] -> ::1)
	hostname = strings.TrimPrefix(hostname, "[")
	hostname = strings.TrimSuffix(hostname, "]")

	// Block localhost and common aliases
	lower := strings.ToLower(hostname)
	if strings.HasPrefix(lower, "localhost") ||
		lower == "ip6-localhost" ||
		lower == "ip6-loopback" {
		return fmt.Errorf("localhost addresses are not allowed")
	}

	// Try to parse as IP address and validate
	ip := net.ParseIP(hostname)
	if ip != nil {
		if err := validatePublicIP(ip); err != nil {
			return err
		}
	}

	// For hostnames, we can't fully validate without DNS lookup
	// The actual connection will fail if it resolves to a private IP
	// Additional protection could be added at the HTTP client level

	return nil
}

// validatePublicIP checks that an IP address is not private, loopback, or link-local
func validatePublicIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("loopback addresses are not allowed")
	}
	if ip.IsPrivate() {
		return fmt.Errorf("private IP addresses are not allowed")
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local addresses are not allowed")
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified addresses (0.0.0.0, ::) are not allowed")
	}

	return nil
}

// FetchPieceRequest represents a single piece in a fetch request
type FetchPieceRequest struct {
	PieceCid  string `json:"pieceCid"`
	SourceURL string `json:"sourceUrl"`
}

// FetchRequest represents the incoming fetch request body
type FetchRequest struct {
	ExtraData    string              `json:"extraData"`
	DataSetId    *uint64             `json:"dataSetId,omitempty"`    // nil or 0 = create new dataset
	RecordKeeper *string             `json:"recordKeeper,omitempty"` // required when dataSetId is nil/0
	Pieces       []FetchPieceRequest `json:"pieces"`
}

// IsCreateNew returns true if this fetch will create a new dataset (dataSetId is nil or 0)
func (r *FetchRequest) IsCreateNew() bool {
	return r.DataSetId == nil || *r.DataSetId == 0
}

// Validate performs validation on the entire fetch request
func (r *FetchRequest) Validate() error {
	if r.ExtraData == "" {
		return fmt.Errorf("extraData is required")
	}

	// Validate dataSetId/recordKeeper combination
	if r.IsCreateNew() {
		if r.RecordKeeper == nil || *r.RecordKeeper == "" {
			return fmt.Errorf("recordKeeper is required when dataSetId is not provided or is 0")
		}
	}

	if len(r.Pieces) == 0 {
		return fmt.Errorf("at least one piece is required")
	}

	// Validate each piece (CID format validation is done later by ParsePieceCidV2)
	for i, piece := range r.Pieces {
		if piece.PieceCid == "" {
			return fmt.Errorf("piece[%d]: pieceCid is required", i)
		}
		if piece.SourceURL == "" {
			return fmt.Errorf("piece[%d]: sourceUrl is required", i)
		}
		if err := ValidateFetchSourceURL(piece.SourceURL, piece.PieceCid); err != nil {
			return fmt.Errorf("piece[%d]: %w", i, err)
		}
	}

	return nil
}

// FetchPieceStatus represents the status of a single piece
type FetchPieceStatus struct {
	PieceCid string      `json:"pieceCid"`
	Status   FetchStatus `json:"status"`
}

// FetchResponse represents the response from a fetch request
type FetchResponse struct {
	Status FetchStatus        `json:"status"`
	Pieces []FetchPieceStatus `json:"pieces"`
}

// ComputeOverallStatus derives the overall status from individual piece statuses.
// Priority: failed > retrying > inProgress > pending > complete
func (r *FetchResponse) ComputeOverallStatus() {
	if len(r.Pieces) == 0 {
		r.Status = FetchStatusPending
		return
	}

	allComplete := true
	anyFailed := false
	anyRetrying := false
	anyInProgress := false

	for _, p := range r.Pieces {
		if p.Status != FetchStatusComplete {
			allComplete = false
		}
		switch p.Status {
		case FetchStatusFailed:
			anyFailed = true
		case FetchStatusRetrying:
			anyRetrying = true
		case FetchStatusInProgress:
			anyInProgress = true
		}
	}

	if allComplete {
		r.Status = FetchStatusComplete
	} else if anyFailed {
		r.Status = FetchStatusFailed
	} else if anyRetrying {
		r.Status = FetchStatusRetrying
	} else if anyInProgress {
		r.Status = FetchStatusInProgress
	} else {
		r.Status = FetchStatusPending
	}
}
