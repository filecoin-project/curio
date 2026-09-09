package pdp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidatePullSourceURL(t *testing.T) {
	// Valid PieceCIDv2
	const validCid = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"

	tests := []struct {
		name        string
		url         string
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid HTTPS URL",
			url:     "https://sp.example.com/piece/" + validCid,
			wantErr: false,
		},
		{
			name:    "valid HTTPS URL with port",
			url:     "https://sp.example.com:8080/piece/" + validCid,
			wantErr: false,
		},
		{
			name:    "valid HTTPS URL with path prefix",
			url:     "https://sp.example.com/api/v1/piece/" + validCid,
			wantErr: false,
		},
		{
			name:    "arbitrary path shape allowed",
			url:     "https://sp.example.com",
			wantErr: false,
		},
		{
			name:        "HTTP not allowed",
			url:         "http://sp.example.com/piece/" + validCid,
			wantErr:     true,
			errContains: "HTTPS",
		},
		{
			name:        "invalid URL",
			url:         "not-a-url",
			wantErr:     true,
			errContains: "HTTPS",
		},
		{
			name:        "empty URL",
			url:         "",
			wantErr:     true,
			errContains: "HTTPS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePullSourceURL(tt.url)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.True(t, strings.Contains(err.Error(), tt.errContains),
						"error %q should contain %q", err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPullRequest_Validate(t *testing.T) {
	// Valid PieceCIDv2 CIDs
	const validCid = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"
	const validCid2 = "bafkzcibf6x7poaqtihg2pifeyzwfy3ndaumj3ds6c5ddiqewo2dzfzr7pqlery5dwyba"
	validURL := "https://sp.example.com/piece/" + validCid
	dataSetId := uint64(1) // Use existing dataset to avoid recordKeeper requirement

	tests := []struct {
		name        string
		req         PullRequest
		wantErr     bool
		errContains string
	}{
		{
			name: "valid request with one piece",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURL: validURL},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with multiple pieces",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURL: validURL},
					{PieceCid: validCid2, SourceURL: "https://sp.example.com/piece/" + validCid2},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with same piece from different sources",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURL: validURL},
					{PieceCid: validCid, SourceURL: "https://backup.example.com/piece/" + validCid},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate piece and source",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURL: validURL},
					{PieceCid: validCid, SourceURL: validURL},
				},
			},
			wantErr:     true,
			errContains: "duplicate pieceCid/sourceUrl",
		},
		{
			name: "missing extraData",
			req: PullRequest{
				ExtraData: "",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURL: validURL},
				},
			},
			wantErr:     true,
			errContains: "extraData is required",
		},
		{
			name: "no pieces",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces:    []PullPieceRequest{},
			},
			wantErr:     true,
			errContains: "at least one piece",
		},
		{
			name: "piece missing pieceCid",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: "", SourceURL: validURL},
				},
			},
			wantErr:     true,
			errContains: "pieceCid is required",
		},
		{
			name: "piece missing sourceUrl",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURL: ""},
				},
			},
			wantErr:     true,
			errContains: "at least one source URL is required",
		},
		{
			name: "valid request with urls array",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, URLs: []string{validURL, "https://backup.example.com/piece/" + validCid}},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with provider host",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, Provider: &PullPieceProvider{Host: "sp.example.com"}},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request combining sourceUrl urls and provider",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{
						PieceCid:  validCid,
						SourceURL: validURL,
						URLs:      []string{"https://backup.example.com/piece/" + validCid},
						Provider:  &PullPieceProvider{Host: "other.example.com", CIDs: []string{validCid2}},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "provider cids without host",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, Provider: &PullPieceProvider{CIDs: []string{validCid}}},
				},
			},
			wantErr:     true,
			errContains: "provider.host is required",
		},
		{
			name: "empty urls entry",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, URLs: []string{""}},
				},
			},
			wantErr:     true,
			errContains: "urls[0] is empty",
		},
		{
			name: "invalid sourceUrl",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURL: "http://localhost/piece/" + validCid},
				},
			},
			wantErr:     true,
			errContains: "HTTPS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.True(t, strings.Contains(err.Error(), tt.errContains),
						"error %q should contain %q", err.Error(), tt.errContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPullPieceRequest_SourceURLs(t *testing.T) {
	const validCid = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"
	const validCid2 = "bafkzcibf6x7poaqtihg2pifeyzwfy3ndaumj3ds6c5ddiqewo2dzfzr7pqlery5dwyba"
	const v1Cid = "baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy"

	t.Run("legacy sourceUrl", func(t *testing.T) {
		p := PullPieceRequest{PieceCid: validCid, SourceURL: "https://sp.example.com/piece/" + validCid}
		urls, err := p.SourceURLs()
		require.NoError(t, err)
		require.Equal(t, []string{"https://sp.example.com/piece/" + validCid}, urls)
	})

	t.Run("urls array", func(t *testing.T) {
		p := PullPieceRequest{PieceCid: validCid, URLs: []string{
			"https://a.example/piece/" + validCid,
			"https://b.example/piece/" + validCid,
		}}
		urls, err := p.SourceURLs()
		require.NoError(t, err)
		require.Equal(t, []string{
			"https://a.example/piece/" + validCid,
			"https://b.example/piece/" + validCid,
		}, urls)
	})

	t.Run("provider host uses pieceCid", func(t *testing.T) {
		p := PullPieceRequest{PieceCid: validCid, Provider: &PullPieceProvider{Host: "sp.example.com"}}
		urls, err := p.SourceURLs()
		require.NoError(t, err)
		require.Equal(t, []string{"https://sp.example.com/piece/" + validCid}, urls)
	})

	t.Run("provider host with port and cids", func(t *testing.T) {
		p := PullPieceRequest{
			PieceCid: validCid,
			Provider: &PullPieceProvider{Host: "sp.example.com:8080", CIDs: []string{v1Cid, validCid2}},
		}
		urls, err := p.SourceURLs()
		require.NoError(t, err)
		require.Equal(t, []string{
			"https://sp.example.com:8080/piece/" + v1Cid,
			"https://sp.example.com:8080/piece/" + validCid2,
		}, urls)
	})

	t.Run("provider host already has scheme", func(t *testing.T) {
		p := PullPieceRequest{
			PieceCid: validCid,
			Provider: &PullPieceProvider{Host: "https://sp.example.com/pdp"},
		}
		urls, err := p.SourceURLs()
		require.NoError(t, err)
		require.Equal(t, []string{"https://sp.example.com/pdp/piece/" + validCid}, urls)
	})

	t.Run("combines and dedupes all sources", func(t *testing.T) {
		legacy := "https://sp.example.com/piece/" + validCid
		p := PullPieceRequest{
			PieceCid:  validCid,
			SourceURL: legacy,
			URLs:      []string{legacy, "https://backup.example.com/piece/" + validCid},
			Provider:  &PullPieceProvider{Host: "sp.example.com"},
		}
		urls, err := p.SourceURLs()
		require.NoError(t, err)
		require.Equal(t, []string{
			"https://sp.example.com/piece/" + validCid,
			"https://backup.example.com/piece/" + validCid,
		}, urls)
	})

	t.Run("json form", func(t *testing.T) {
		raw := `{
			"pieceCid": "` + validCid + `",
			"sourceUrl": "https://legacy.example/piece/` + validCid + `",
			"urls": ["https://a.example/piece/` + validCid + `"],
			"provider": {"host": "sp.example.com", "cids": ["` + v1Cid + `"]}
		}`
		var p PullPieceRequest
		require.NoError(t, json.Unmarshal([]byte(raw), &p))
		urls, err := p.SourceURLs()
		require.NoError(t, err)
		require.Equal(t, []string{
			"https://legacy.example/piece/" + validCid,
			"https://a.example/piece/" + validCid,
			"https://sp.example.com/piece/" + v1Cid,
		}, urls)
	})
}

func TestPullRequest_ValidateBatchLimit(t *testing.T) {
	const validCid = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"
	dataSetId := uint64(1)

	// Distinct source URLs so the batch-size check is isolated from the
	// duplicate and per-piece checks that run afterwards.
	pieces := make([]PullPieceRequest, MaxAddPiecesBatchSize+1)
	for i := range pieces {
		pieces[i] = PullPieceRequest{
			PieceCid:  validCid,
			SourceURL: fmt.Sprintf("https://sp.example.com/piece/%s?n=%d", validCid, i),
		}
	}
	req := PullRequest{ExtraData: "0x1234", DataSetId: &dataSetId, Pieces: pieces}

	err := req.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds the maximum allowed per pull")
}

func TestPullRetryAfter(t *testing.T) {
	tests := []struct {
		name     string
		pending  int
		limit    int
		expected time.Duration
	}{
		{name: "at limit", pending: 120, limit: 120, expected: time.Minute},
		{name: "slightly over", pending: 121, limit: 120, expected: time.Minute},
		{name: "second bucket", pending: 131, limit: 120, expected: 2 * time.Minute},
		{name: "capped", pending: 200, limit: 120, expected: 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, pullRetryAfter(tt.pending, tt.limit))
		})
	}
}

func TestPullEffectiveClientPendingLimit(t *testing.T) {
	tests := []struct {
		name         string
		otherPending int
		expected     int
	}{
		{name: "empty queue", otherPending: 0, expected: 108},
		{name: "some other work", otherPending: 20, expected: 88},
		{name: "reserve mostly used", otherPending: 98, expected: 10},
		{name: "never below normal cap", otherPending: 120, expected: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, pullEffectiveClientPendingLimit(tt.otherPending))
		})
	}
}

func TestPullResponse_ComputeOverallStatus(t *testing.T) {
	tests := []struct {
		name           string
		pieces         []PullPieceStatus
		expectedStatus PullStatus
	}{
		{
			name:           "empty pieces",
			pieces:         []PullPieceStatus{},
			expectedStatus: PullStatusPending,
		},
		{
			name: "all complete",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusComplete},
				{PieceCid: "cid2", Status: PullStatusComplete},
			},
			expectedStatus: PullStatusComplete,
		},
		{
			name: "all pending",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusPending},
				{PieceCid: "cid2", Status: PullStatusPending},
			},
			expectedStatus: PullStatusPending,
		},
		{
			name: "mixed with inProgress",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusComplete},
				{PieceCid: "cid2", Status: PullStatusInProgress},
			},
			expectedStatus: PullStatusInProgress,
		},
		{
			name: "some complete some pending",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusComplete},
				{PieceCid: "cid2", Status: PullStatusPending},
			},
			expectedStatus: PullStatusPending,
		},
		{
			name: "some complete some failed is complete",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusComplete},
				{PieceCid: "cid2", Status: PullStatusFailed},
			},
			expectedStatus: PullStatusComplete,
		},
		{
			name: "failed with inProgress is inProgress",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusFailed},
				{PieceCid: "cid2", Status: PullStatusInProgress},
			},
			expectedStatus: PullStatusInProgress,
		},
		{
			name: "failed with pending is pending",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusFailed},
				{PieceCid: "cid2", Status: PullStatusPending},
			},
			expectedStatus: PullStatusPending,
		},
		{
			name: "failed with retrying is retrying",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusFailed},
				{PieceCid: "cid2", Status: PullStatusRetrying},
			},
			expectedStatus: PullStatusRetrying,
		},
		{
			name: "all failed",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusFailed},
				{PieceCid: "cid2", Status: PullStatusFailed},
			},
			expectedStatus: PullStatusFailed,
		},
		{
			name: "single complete",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusComplete},
			},
			expectedStatus: PullStatusComplete,
		},
		{
			name: "single inProgress",
			pieces: []PullPieceStatus{
				{PieceCid: "cid1", Status: PullStatusInProgress},
			},
			expectedStatus: PullStatusInProgress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &PullResponse{Pieces: tt.pieces}
			resp.ComputeOverallStatus()
			require.Equal(t, tt.expectedStatus, resp.Status)
		})
	}
}
