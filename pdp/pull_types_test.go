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
			name: "valid request with urls array",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				URLs:      [][]string{{validURL, "https://backup.example.com/piece/" + validCid}},
			},
			wantErr: false,
		},
		{
			name: "valid request with provider host and cids",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Provider:  &PullProvider{Hosts: []string{"sp.example.com"}, CIDs: []string{validCid}},
			},
			wantErr: false,
		},
		{
			name: "valid request combining pieces urls and provider",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURLs: []string{validURL}},
				},
				URLs:     [][]string{{"https://backup.example.com/piece/" + validCid}},
				Provider: &PullProvider{Hosts: []string{"other.example.com"}, CIDs: []string{validCid2}},
			},
			wantErr: false,
		},
		{
			name: "provider host without cids uses pieces",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURLs: []string{validURL}},
				},
				Provider: &PullProvider{Hosts: []string{"sp.example.com"}},
			},
			wantErr: false,
		},
		{
			name: "provider cids without host",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Provider:  &PullProvider{CIDs: []string{validCid}},
			},
			wantErr:     true,
			errContains: "provider.cids requires provider.hosts",
		},
		{
			name: "provider cids without host even with pieces",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				Pieces: []PullPieceRequest{
					{PieceCid: validCid, SourceURLs: []string{validURL}},
				},
				Provider: &PullProvider{CIDs: []string{validCid2}},
			},
			wantErr:     true,
			errContains: "provider.cids requires provider.hosts",
		},
		{
			name: "empty urls entry",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				URLs:      [][]string{{}},
			},
			wantErr:     true,
			errContains: "urls[0] is empty",
		},
		{
			name: "empty url in group",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				URLs:      [][]string{{""}},
			},
			wantErr:     true,
			errContains: "urls[0][0] is empty",
		},
		{
			name: "urls group mixed piece cids",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				URLs: [][]string{{
					"https://sp.example.com/piece/" + validCid,
					"https://sp.example.com/piece/" + validCid2,
				}},
			},
			wantErr:     true,
			errContains: "URLs must refer to the same piece",
		},
		{
			name: "url missing piece path",
			req: PullRequest{
				ExtraData: "0x1234",
				DataSetId: &dataSetId,
				URLs:      [][]string{{"https://sp.example.com/not-a-piece"}},
			},
			wantErr:     true,
			errContains: "/piece/{cid}",
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
	const validCid2 = "bafkzcibf6x7poaqtihg2pifeyzwfy3ndaumj3ds6c5ddiqewo2dzfzr7pqlery5dwyba"
	const v1Cid = "baga6ea4seaqpy7usqklokfx2vxuynmupslkeutzexe2uqurdg5vhtebhxqmpqmy"

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

	t.Run("urls array of arrays extracts cid in order", func(t *testing.T) {
		r := PullRequest{URLs: [][]string{
			{
				"https://a.example/piece/" + validCid,
				"https://backup.example/piece/" + validCid,
			},
			{"https://b.example/piece/" + validCid2},
		}}
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{
			{validCid, "https://a.example/piece/" + validCid},
			{validCid, "https://backup.example/piece/" + validCid},
			{validCid2, "https://b.example/piece/" + validCid2},
		}, got(sources))
		require.Equal(t, []int{0, 1, 0}, []int{sources[0].SourceOrder, sources[1].SourceOrder, sources[2].SourceOrder})
	})

	t.Run("provider host uses piece cids", func(t *testing.T) {
		r := PullRequest{
			Pieces: []PullPieceRequest{
				{PieceCid: validCid, SourceURLs: []string{"https://legacy.example/piece/" + validCid}},
			},
			Provider: &PullProvider{Hosts: []string{"sp.example.com"}},
		}
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{
			{validCid, "https://legacy.example/piece/" + validCid},
			{validCid, "https://sp.example.com/piece/" + validCid},
		}, got(sources))
	})

	t.Run("provider hosts are tried in order", func(t *testing.T) {
		r := PullRequest{Provider: &PullProvider{
			Hosts: []string{"first.example.com", "second.example.com"},
			CIDs:  []string{validCid},
		}}
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{
			{validCid, "https://first.example.com/piece/" + validCid},
			{validCid, "https://second.example.com/piece/" + validCid},
		}, got(sources))
		require.Equal(t, []int{0, 1}, []int{sources[0].SourceOrder, sources[1].SourceOrder})
	})

	t.Run("provider host with port and cids", func(t *testing.T) {
		r := PullRequest{Provider: &PullProvider{Hosts: []string{"sp.example.com:8080"}, CIDs: []string{v1Cid, validCid2}}}
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{
			{v1Cid, "https://sp.example.com:8080/piece/" + v1Cid},
			{validCid2, "https://sp.example.com:8080/piece/" + validCid2},
		}, got(sources))
	})

	t.Run("provider host already has scheme", func(t *testing.T) {
		r := PullRequest{Provider: &PullProvider{Hosts: []string{"https://sp.example.com/pdp"}, CIDs: []string{validCid}}}
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{{validCid, "https://sp.example.com/pdp/piece/" + validCid}}, got(sources))
	})

	t.Run("combines and dedupes all sources", func(t *testing.T) {
		legacy := "https://sp.example.com/piece/" + validCid
		r := PullRequest{
			Pieces: []PullPieceRequest{
				{PieceCid: validCid, SourceURLs: []string{legacy}},
			},
			URLs:     [][]string{{legacy, "https://backup.example.com/piece/" + validCid}},
			Provider: &PullProvider{Hosts: []string{"sp.example.com"}},
		}
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{
			{validCid, "https://sp.example.com/piece/" + validCid},
			{validCid, "https://backup.example.com/piece/" + validCid},
		}, got(sources))
	})

	t.Run("json form", func(t *testing.T) {
		raw := `{
			"extraData": "0x1234",
			"pieces": [{"pieceCid": "` + validCid + `", "sourceUrls": ["https://legacy.example/piece/` + validCid + `"]}],
			"urls": [["https://a.example/piece/` + validCid + `"]],
			"provider": {"hosts": ["sp.example.com"], "cids": ["` + v1Cid + `"]}
		}`
		var r PullRequest
		require.NoError(t, json.Unmarshal([]byte(raw), &r))
		sources, err := r.AssembledSources()
		require.NoError(t, err)
		require.Equal(t, [][2]string{
			{validCid, "https://legacy.example/piece/" + validCid},
			{validCid, "https://a.example/piece/" + validCid},
			{v1Cid, "https://sp.example.com/piece/" + v1Cid},
		}, got(sources))
	})

	t.Run("cids without host", func(t *testing.T) {
		r := PullRequest{Provider: &PullProvider{CIDs: []string{v1Cid}}}
		_, err := r.AssembledSources()
		require.Error(t, err)
		require.Contains(t, err.Error(), "provider.cids requires provider.hosts")
	})

	t.Run("json cids without host", func(t *testing.T) {
		raw := `{
			"pieces": [{"pieceCid": "` + validCid + `", "sourceUrls": ["https://legacy.example/piece/` + validCid + `"]}],
			"provider": {"cids": ["` + v1Cid + `"]}
		}`
		var r PullRequest
		require.NoError(t, json.Unmarshal([]byte(raw), &r))
		_, err := r.AssembledSources()
		require.Error(t, err)
		require.Contains(t, err.Error(), "provider.cids requires provider.hosts")
	})
}

func TestPullRequest_ValidateBatchLimit(t *testing.T) {
	dataSetId := uint64(1)

	pieces := make([]PullPieceRequest, MaxAddPiecesBatchSize+1)
	for i := range pieces {
		cid := fmt.Sprintf("cid-%d", i)
		pieces[i] = PullPieceRequest{
			PieceCid:   cid,
			SourceURLs: []string{fmt.Sprintf("https://sp.example.com/piece/%s", cid)},
		}
	}
	req := PullRequest{ExtraData: "0x1234", DataSetId: &dataSetId, Pieces: pieces}

	err := req.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds the maximum allowed per pull")
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
