package cunative

/*
#cgo CFLAGS: -I${SRCDIR}/../../../extern/supra_seal/deps/blst/bindings
#cgo LDFLAGS: -L${SRCDIR}/../../../extern/supra_seal/deps/blst -lblst
#include <stdint.h>
#include <stdlib.h>
#include "blst.h"

// Decode function using blst_fr_sub
void curio_blst_decode(const uint8_t *replica, const uint8_t *key, uint8_t *out, size_t len) {
    blst_fr value, k, result;

    for (size_t i = 0; i < len; i += 32) {
        // Read 32 bytes (256 bits) from replica and key
        blst_fr_from_uint64(&value, (const uint64_t*)(replica + i));
        blst_fr_from_uint64(&k, (const uint64_t*)(key + i));

        // Perform the decoding operation using blst_fr_sub
        blst_fr_sub(&result, &value, &k);

        // Write the result to the output
        blst_uint64_from_fr((uint64_t*)(out + i), &result);
    }
}
*/
import "C"
import (
	pool "github.com/libp2p/go-buffer-pool"
	"io"
	"runtime"
	"sync"
	"unsafe"
)

/*

Simple Sequential implementation for reference:

func Decode(replica, key io.Reader, out io.Writer) error {
	const bufSz = 1 << 20

	var rbuf, kbuf [bufSz]byte
	var obuf [bufSz]byte

	for {
		// Read replica
		rn, err := io.ReadFull(replica, rbuf[:])
		if err != nil && err != io.ErrUnexpectedEOF {
			if err == io.EOF {
				return nil
			}
			return err
		}

		// Read key
		kn, err := io.ReadFull(key, kbuf[:rn])
		if err != nil && err != io.ErrUnexpectedEOF {
			return err
		}

		if kn != rn {
			return io.ErrUnexpectedEOF
		}

		// Decode the chunk using blst_decode
		C.curio_blst_decode(
			(*C.uint8_t)(unsafe.Pointer(&rbuf[0])),
			(*C.uint8_t)(unsafe.Pointer(&kbuf[0])),
			(*C.uint8_t)(unsafe.Pointer(&obuf[0])),
			C.size_t(rn),
		)

		// Write the chunk
		_, err = out.Write(obuf[:rn])
		if err != nil {
			return err
		}

		if rn < len(rbuf) {
			return nil
		}
	}
}

*/

const (
	bufSz    = 4 << 20
	nWorkers = 24
)

func Decode(replica, key io.Reader, out io.Writer) error {
	workers := nWorkers
	if runtime.NumCPU() < workers {
		workers = runtime.NumCPU()
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 1)
	jobChan := make(chan job, workers)
	resultChan := make(chan result, workers)

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker(&wg, jobChan, resultChan)
	}

	// Start a goroutine to close the job channel when all reading is done
	go func() {
		defer close(jobChan)
		chunkID := int64(0)
		for {
			rbuf := pool.Get(bufSz)
			kbuf := pool.Get(bufSz)

			// Read replica
			rn, err := io.ReadFull(replica, rbuf)
			if err != nil && err != io.ErrUnexpectedEOF {
				if err == io.EOF {
					return
				}
				errChan <- err
				return
			}

			// Read key
			kn, err := io.ReadFull(key, kbuf[:rn])
			if err != nil && err != io.ErrUnexpectedEOF {
				errChan <- err
				return
			}

			if kn != rn {
				errChan <- io.ErrUnexpectedEOF
				return
			}

			// worker will release rbuff and kbuf, so get len here
			rblen := len(rbuf)

			jobChan <- job{rbuf[:rn], kbuf[:rn], rn, chunkID}
			chunkID++

			if rn < rblen {
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
	resultBuffer := make(map[int64]result)

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

type job struct {
	rbuf    []byte
	kbuf    []byte
	size    int
	chunkID int64
}

type result struct {
	data    []byte
	size    int
	chunkID int64
}

func worker(wg *sync.WaitGroup, jobs <-chan job, results chan<- result) {
	defer wg.Done()
	for j := range jobs {
		obuf := pool.Get(j.size)
		C.curio_blst_decode(
			(*C.uint8_t)(unsafe.Pointer(&j.rbuf[0])),
			(*C.uint8_t)(unsafe.Pointer(&j.kbuf[0])),
			(*C.uint8_t)(unsafe.Pointer(&obuf[0])),
			C.size_t(j.size),
		)

		pool.Put(j.rbuf)
		pool.Put(j.kbuf)

		results <- result{obuf, j.size, j.chunkID}
	}
}
