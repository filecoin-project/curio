package pdp

import (
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
					{PieceCid: validCid, SourceURLs: []string{validURL}},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with legacy sourceUrl",
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
			name: "sourceUrl prepended to sourceUrls",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{
						PieceCid:   validCid,
						SourceURL:  validURL,
						SourceURLs: []string{"https://backup.example.com/piece/" + validCid},
					},
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
					{PieceCid: validCid, SourceURLs: []string{validURL}},
					{PieceCid: validCid2, SourceURLs: []string{"https://sp.example.com/piece/" + validCid2}},
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
					{PieceCid: validCid, SourceURLs: []string{validURL, "https://backup.example.com/piece/" + validCid}},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate pieceCid",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURLs: []string{validURL}},
					{PieceCid: validCid, SourceURLs: []string{validURL}},
				},
			},
			wantErr:     true,
			errContains: "duplicate pieceCid",
		},
		{
			name: "sourceUrl duplicates sourceUrls",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURL: validURL, SourceURLs: []string{validURL}},
				},
			},
			wantErr:     true,
			errContains: "duplicate sourceUrls",
		},
		{
			name: "duplicate sourceUrls",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURLs: []string{validURL, validURL}},
				},
			},
			wantErr:     true,
			errContains: "duplicate sourceUrls",
		},
		{
			name: "missing extraData",
			req: PullRequest{
				ExtraData: "",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURLs: []string{validURL}},
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
			errContains: "at least one source URL is required",
		},
		{
			name: "piece missing pieceCid",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: "", SourceURLs: []string{validURL}},
				},
			},
			wantErr:     true,
			errContains: "pieceCid is required",
		},
		{
			name: "piece missing sourceUrls",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid},
				},
			},
			wantErr:     true,
			errContains: "sourceUrl or sourceUrls is required",
		},
		{
			name: "empty sourceUrls entry",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURLs: []string{""}},
				},
			},
			wantErr:     true,
			errContains: "sourceUrls[0] is empty",
		},
		{
			name: "invalid sourceUrls",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURLs: []string{"http://localhost/piece/" + validCid}},
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

func TestPullRequest_AssembledSources(t *testing.T) {
	const validCid = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"

	got := func(sources []pullSource) [][2]string {
		out := make([][2]string, len(sources))
		for i, s := range sources {
			out[i] = [2]string{s.PieceCid, s.SourceURL}
		}
		return out
	}

	t.Run("legacy sourceUrl", func(t *testing.T) {
		r := PullRequest{Pieces: []PullPieceRequest{
			{PieceCid: validCid, SourceURL: "https://sp.example.com/piece/" + validCid},
		}}
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{{validCid, "https://sp.example.com/piece/" + validCid}}, got(sources))
		require.Equal(t, 0, sources[0].SourceOrder)
	})

	t.Run("sourceUrl is prepended to sourceUrls", func(t *testing.T) {
		first := "https://first.example/piece/" + validCid
		second := "https://second.example/piece/" + validCid
		r := PullRequest{Pieces: []PullPieceRequest{
			{PieceCid: validCid, SourceURL: first, SourceURLs: []string{second}},
		}}
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{{validCid, first}, {validCid, second}}, got(sources))
		require.Equal(t, []int{0, 1}, []int{sources[0].SourceOrder, sources[1].SourceOrder})
	})

	t.Run("pieces sourceUrls", func(t *testing.T) {
		r := PullRequest{Pieces: []PullPieceRequest{
			{PieceCid: validCid, SourceURLs: []string{"https://sp.example.com/piece/" + validCid}},
		}}
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{{validCid, "https://sp.example.com/piece/" + validCid}}, got(sources))
	})
}

func TestPullRequest_ValidateAllowsLargeBatches(t *testing.T) {
	dataSetId := uint64(1)

	// Piece count is no longer capped; Filecoin message size is enforced later
	// when packing addPieces. Distinct piece CIDs avoid the duplicate-piece check.
	pieces := make([]PullPieceRequest, 2000)
	for i := range pieces {
		cid := fmt.Sprintf("cid-%d", i)
		pieces[i] = PullPieceRequest{
			PieceCid:   cid,
			SourceURLs: []string{fmt.Sprintf("https://sp.example.com/piece/%s", cid)},
		}
	}
	req := PullRequest{ExtraData: "0x1234", DataSetId: &dataSetId, Pieces: pieces}

	require.NoError(t, req.Validate())
}

func TestAggregatePullStatuses(t *testing.T) {
	require.Equal(t, PullStatusPending, aggregatePullStatuses(nil))
	require.Equal(t, PullStatusPending, aggregatePullStatuses([]PullStatus{PullStatusFailed, PullStatusPending}))
	require.Equal(t, PullStatusRetrying, aggregatePullStatuses([]PullStatus{PullStatusFailed, PullStatusRetrying}))
	require.Equal(t, PullStatusInProgress, aggregatePullStatuses([]PullStatus{PullStatusComplete, PullStatusInProgress}))
	require.Equal(t, PullStatusFailed, aggregatePullStatuses([]PullStatus{PullStatusFailed, PullStatusFailed}))
	require.Equal(t, PullStatusComplete, aggregatePullStatuses([]PullStatus{PullStatusFailed, PullStatusComplete}))
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
