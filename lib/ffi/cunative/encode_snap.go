//go:build cunative

package cunative

/*
#cgo CFLAGS: -I${SRCDIR}/../../../extern/supraseal/deps/blst/bindings
#cgo LDFLAGS: -L${SRCDIR}/../../../extern/supraseal/deps/blst -lblst
#include <stdint.h>
#include <stdlib.h>
#include "blst.h"

void snap_encode_loop(const uint8_t *key, const uint8_t *data, const uint8_t *rhos, uint8_t *out, size_t node_count, size_t node_size) {
    blst_fr key_fr, data_fr, rho_fr, tmp_fr, out_fr;

    for (size_t i = 0; i < node_count; i++) {
        // Load inputs
        blst_fr_from_uint64(&key_fr, (const uint64_t*)(key + i * node_size));
        blst_fr_from_uint64(&data_fr, (const uint64_t*)(data + i * node_size));
        blst_fr_from_uint64(&rho_fr, (const uint64_t*)(rhos + i * 32));

        // tmp = data * rho
        blst_fr_mul(&tmp_fr, &data_fr, &rho_fr);

        // out = key + tmp
        blst_fr_add(&out_fr, &key_fr, &tmp_fr);

        // Store
        blst_uint64_from_fr((uint64_t*)(out + i * node_size), &out_fr);
    }
}
*/
import "C"

import (
	"io"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ipfs/go-cid"
	pool "github.com/libp2p/go-buffer-pool"
	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proof"
)

// New generates rho values (not inverted) for the whole sector
func New(phi [32]byte, h uint64, nodesCount uint64) (*Rhos, error) {
	return NewRange(phi, h, nodesCount, 0, nodesCount)
}

// NewRange generates rho values (not inverted) for the specified range
func NewRange(phi [32]byte, h uint64, nodesCount, offset, num uint64) (*Rhos, error) {
	bitsShr := calcBitsShr(h, nodesCount)
	highRange := calcHighRange(offset, num, bitsShr)

	rhos := make(map[uint64]B32le)
	for high := highRange.Start; high <= highRange.End; high++ {
		rhoVal, err := rho(phi, uint32(high))
		if err != nil {
			return nil, err
		}

		rhos[high] = ffElementBytesLE(rhoVal)
	}

	return &Rhos{
		rhos:    rhos,
		bitsShr: bitsShr,
	}, nil
}

// EncodeSnap encodes deal data into an existing sector replica key according to FIP-0019
// out[i] = key[i] + data[i] * rho(i)
func EncodeSnap(spt abi.RegisteredSealProof, commD, commK cid.Cid, key, data io.Reader, out io.Writer) error {
	ssize, err := spt.SectorSize()
	if err != nil {
		return xerrors.Errorf("failed to get sector size: %w", err)
	}

	nodesCount := uint64(ssize / proof.NODE_SIZE)

	commDNew, err := commcid.CIDToDataCommitmentV1(commD)
	if err != nil {
		return xerrors.Errorf("failed to convert commD to CID: %w", err)
	}

	commROld, err := commcid.CIDToReplicaCommitmentV1(commK)
	if err != nil {
		return xerrors.Errorf("failed to convert commK to replica commitment: %w", err)
	}

	// Calculate phi
	phi, err := Phi(commDNew, commROld)
	if err != nil {
		return xerrors.Errorf("failed to calculate phi: %w", err)
	}

	// Precompute all rho values
	h := hDefault(nodesCount)
	rhos, err := New(phi, h, nodesCount)
	if err != nil {
		return xerrors.Errorf("failed to compute rhos: %w", err)
	}

	workers := nWorkers
	if runtime.NumCPU() < workers {
		workers = runtime.NumCPU()
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 1)
	jobChan := make(chan jobEnc, workers)
	resultChan := make(chan resultEnc, workers*ResultBufDepth)

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go workerEnc(&wg, jobChan, resultChan, rhos)
	}

	// Start a goroutine to close the job channel when all reading is done
	go func() {
		defer close(jobChan)
		chunkID := int64(0)
		for {
			kbuf := pool.Get(bufSz)
			dbuf := pool.Get(bufSz)

			// Read key
			kn, err := io.ReadFull(key, kbuf)
			if err != nil && err != io.ErrUnexpectedEOF {
				if err == io.EOF {
					return
				}
				errChan <- err
				return
			}

			// Read data
			dn, err := io.ReadFull(data, dbuf[:kn])
			if err != nil && err != io.ErrUnexpectedEOF {
				errChan <- err
				return
			}

			if dn != kn {
				errChan <- io.ErrUnexpectedEOF
				return
			}

			// worker will release kbuf and dbuf, so get len here
			kblen := len(kbuf)

			jobChan <- jobEnc{kbuf[:kn], dbuf[:dn], kn, chunkID}
			chunkID++

			if kn < kblen {
				return
			}
		}
	}()

	// Start a goroutine to close the result channel when all jobs are done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Write results in order
	var writeErr error
	expectedChunkID := int64(0)
	resultBuffer := make(map[int64]resultEnc)

	for r := range resultChan {
		for {
			if r.chunkID == expectedChunkID {
				_, err := out.Write(r.data)
				pool.Put(r.data)
				if err != nil && writeErr == nil {
					writeErr = err
				}
				expectedChunkID++

				// Check if we have buffered results that can now be written
				if nextResult, ok := resultBuffer[expectedChunkID]; ok {
					r = nextResult
					delete(resultBuffer, expectedChunkID)
					continue
				}
				break
			} else {
				// Buffer this result for later
				resultBuffer[r.chunkID] = r
				break
			}
		}
	}

	close(errChan)

	// Check for any errors
	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return writeErr
}

type jobEnc struct {
	kbuf    []byte
	dbuf    []byte
	size    int
	chunkID int64
}

type resultEnc struct {
	data    []byte
	size    int
	chunkID int64
}

func workerEnc(wg *sync.WaitGroup, jobs <-chan jobEnc, results chan<- resultEnc, rhos *Rhos) {
	defer wg.Done()
	for j := range jobs {
		obuf := pool.Get(j.size)

		// Calculate the starting node index for this chunk
		startNode := uint64(j.chunkID) * uint64(bufSz) / proof.NODE_SIZE
		nodeCount := uint64(j.size) / proof.NODE_SIZE

		// Build rhos byte slice for this chunk
		rhoBytes := pool.Get(int(nodeCount * 32))
		for i := uint64(0); i < nodeCount; i++ {
			rhoVal := rhos.Get(startNode + i)
			copy(rhoBytes[i*32:(i+1)*32], rhoVal[:])
		}

		C.snap_encode_loop(
			(*C.uint8_t)(unsafe.Pointer(&j.kbuf[0])),
			(*C.uint8_t)(unsafe.Pointer(&j.dbuf[0])),
			(*C.uint8_t)(unsafe.Pointer(&rhoBytes[0])),
			(*C.uint8_t)(unsafe.Pointer(&obuf[0])),
			(C.size_t)(nodeCount),
			(C.size_t)(proof.NODE_SIZE),
		)

		pool.Put(rhoBytes)

		pool.Put(j.kbuf)
		pool.Put(j.dbuf)

		results <- resultEnc{obuf, j.size, j.chunkID}
	}
}
