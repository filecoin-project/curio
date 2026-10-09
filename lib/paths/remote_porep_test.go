//go:build !skiff

package paths

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/storiface"
)

// Only local proof generation and sector discovery are substituted. Requests and
// response-body reads use the production Remote method and real loopback HTTP.
// The payloads are opaque transport fixtures, not valid native proof inputs.
type porepTransportStore struct {
	Store
	proof []byte
	err   error
}

func (s porepTransportStore) GeneratePoRepVanillaProof(context.Context, storiface.SectorRef, cid.Cid, cid.Cid, abi.SealRandomness, abi.InteractiveSealRandomness) ([]byte, error) {
	return s.proof, s.err
}

type porepTransportIndex struct {
	SectorIndex
	urls []string
}

func (s porepTransportIndex) StorageFindSector(_ context.Context, sector abi.SectorID, ft storiface.SectorFileType, size abi.SectorSize, fetch bool) ([]storiface.SectorStorageInfo, error) {
	if sector != (abi.SectorID{Miner: 1000, Number: 1}) || ft != storiface.FTSealed|storiface.FTCache || size != 0 || fetch {
		return nil, fmt.Errorf("unexpected sector lookup: %v %v %v %v", sector, ft, size, fetch)
	}
	return []storiface.SectorStorageInfo{{BaseURLs: s.urls}}, nil
}

func porepTransportRemote(urls ...string) *Remote {
	return &Remote{local: porepTransportStore{err: errPathNotFound}, index: porepTransportIndex{urls: urls}}
}

func callPoRepTransport(ctx context.Context, r *Remote) ([]byte, error) {
	return r.GeneratePoRepVanillaProof(ctx, storiface.SectorRef{
		ID: abi.SectorID{Miner: 1000, Number: 1}, ProofType: abi.RegisteredSealProof_StackedDrg2KiBV1,
	}, cid.Undef, cid.Undef, nil, nil)
}

func partialPoRepServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/vanilla/porep" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "truncated")
	}))
	t.Cleanup(s.Close)
	return s
}

func TestPoRepRemoteRejectsTruncatedBody(t *testing.T) {
	s := partialPoRepServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proof, err := callPoRepTransport(ctx, porepTransportRemote(s.URL))
	require.ErrorIs(t, err, io.ErrUnexpectedEOF, "a partial response must not reach SealCommitPhase2 as a successful proof")
	require.Nil(t, proof)
}

func TestPoRepRemoteTruncatedBodyTriesNextLocation(t *testing.T) {
	first := partialPoRepServer(t)
	var calls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "complete transport fixture")
	}))
	defer second.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proof, err := callPoRepTransport(ctx, porepTransportRemote(first.URL, second.URL))
	require.NoError(t, err)
	require.Equal(t, "complete transport fixture", string(proof))
	require.EqualValues(t, 1, calls.Load())
}

func TestPoRepRemoteAllTruncatedLocationsFail(t *testing.T) {
	first, second := partialPoRepServer(t), partialPoRepServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proof, err := callPoRepTransport(ctx, porepTransportRemote(first.URL, second.URL))
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Nil(t, proof)
}

func TestPoRepRemoteCancelledReadThenRefill(t *testing.T) {
	// Each failed response is followed by an independent successful request.
	// Cancellation is transport-only: this does not assert native work stopped.
	for i := 0; i < 8; i++ {
		func() {
			ready, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) > 1 {
					_, _ = io.WriteString(w, "next request")
					return
				}
				w.Header().Set("Content-Length", "100")
				_, _ = io.WriteString(w, "partial")
				w.(http.Flusher).Flush()
				close(ready)
				<-release
			}))
			defer s.Close()
			defer close(release)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r := porepTransportRemote(s.URL)
			done := make(chan struct{})
			var proof []byte
			var err error
			go func() { defer close(done); proof, err = callPoRepTransport(ctx, r) }()
			// Join the client before the owned HTTP server is cleaned up, including
			// on a failed assertion. The context bounds a stalled response.
			defer func() { cancel(); <-done }()
			select {
			case <-ready:
			case <-ctx.Done():
				t.Fatal("response did not start")
			}
			cancel()
			<-done
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, proof)
			next, cancelNext := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelNext()
			proof, err = callPoRepTransport(next, r)
			require.NoError(t, err)
			require.Equal(t, "next request", string(proof))
		}()
	}
}

func TestPoRepRemoteLocalResultAndErrorRemainAuthoritative(t *testing.T) {
	localErr := errors.New("local native failure")
	for _, err := range []error{nil, localErr} {
		r := &Remote{local: porepTransportStore{proof: []byte("local"), err: err}}
		proof, got := callPoRepTransport(context.Background(), r)
		require.ErrorIs(t, got, err)
		require.Equal(t, []byte("local"), proof)
	}
}
