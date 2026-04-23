package pdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePullSourceURL(t *testing.T) {
	// Valid PieceCIDv2
	const validCid = "bafkzcibf6x7poaqtr2pqm6qki6sgetps74xutpclzrwbux5ow6rw4nsfu6tbf2zfnmnq"

	tests := []struct {
		name        string
		url         string
		pieceCid    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid HTTPS URL",
			url:      "https://sp.example.com/piece/" + validCid,
			pieceCid: validCid,
			wantErr:  false,
		},
		{
			name:     "valid HTTPS URL with port",
			url:      "https://sp.example.com:8080/piece/" + validCid,
			pieceCid: validCid,
			wantErr:  false,
		},
		{
			name:     "valid HTTPS URL with path prefix",
			url:      "https://sp.example.com/api/v1/piece/" + validCid,
			pieceCid: validCid,
			wantErr:  false,
		},
		{
			name:        "HTTP not allowed",
			url:         "http://sp.example.com/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "HTTPS",
		},
		{
			name:        "wrong path pattern - missing /piece/",
			url:         "https://sp.example.com/data/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "must end with /piece/",
		},
		{
			name:        "wrong path pattern - /pieces/ instead of /piece/",
			url:         "https://sp.example.com/pieces/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "must end with /piece/",
		},
		{
			name:        "pieceCid mismatch",
			url:         "https://sp.example.com/piece/bafkzcibf6x7poaqtihg2pifeyzwfy3ndaumj3ds6c5ddiqewo2dzfzr7pqlery5dwyba",
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "does not match expected",
		},
		{
			name:        "localhost not allowed",
			url:         "https://localhost/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "localhost",
		},
		{
			name:        "localhost with port not allowed",
			url:         "https://localhost:8080/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "localhost",
		},
		{
			name:        "localhost.localdomain not allowed",
			url:         "https://localhost.localdomain/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "localhost",
		},
		{
			name:        "localhost4 not allowed",
			url:         "https://localhost4/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "localhost",
		},
		{
			name:        "localhost6 not allowed",
			url:         "https://localhost6/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "localhost",
		},
		{
			name:        "ip6-localhost not allowed",
			url:         "https://ip6-localhost/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "localhost",
		},
		{
			name:        "ip6-loopback not allowed",
			url:         "https://ip6-loopback/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "localhost",
		},
		{
			name:        "127.0.0.1 not allowed",
			url:         "https://127.0.0.1/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "loopback", // net.IP.IsLoopback() returns true for 127.x.x.x
		},
		{
			name:        "private IP 10.x not allowed",
			url:         "https://10.0.0.1/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "private",
		},
		{
			name:        "private IP 192.168.x not allowed",
			url:         "https://192.168.1.1/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "private",
		},
		{
			name:        "private IP 172.16.x not allowed",
			url:         "https://172.16.0.1/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "private",
		},
		{
			name:        "link-local not allowed",
			url:         "https://169.254.1.1/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "link-local",
		},
		{
			name:        "IPv6 loopback not allowed",
			url:         "https://[::1]/piece/" + validCid,
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "loopback",
		},
		{
			name:        "invalid URL",
			url:         "not-a-url",
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "HTTPS",
		},
		{
			name:        "empty URL",
			url:         "",
			pieceCid:    validCid,
			wantErr:     true,
			errContains: "HTTPS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePullSourceURL(tt.url, tt.pieceCid)
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
			errContains: "sourceUrl is required",
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
