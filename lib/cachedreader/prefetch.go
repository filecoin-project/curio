package cachedreader

import (
	"io"
	"sync/atomic"
	"time"

	pool "github.com/libp2p/go-buffer-pool"
)

const (
	defaultSleepDuration = 1 * time.Millisecond
	minBufferDepth       = 1024
)

type PrefetchReader struct {
	source     io.Reader
	buffer     []byte
	readPtr    atomic.Uint64
	writePtr   atomic.Uint64
	bufferSize uint64
	done       chan struct{}
	workerDone chan struct{} // Signal that the worker has finished
	err        atomic.Pointer[error]
}

func New(source io.Reader, bufferDepth int) *PrefetchReader {
	if bufferDepth < minBufferDepth {
		bufferDepth = minBufferDepth
	}

	pr := &PrefetchReader{
		source:     source,
		buffer:     pool.Get(bufferDepth),
		bufferSize: uint64(bufferDepth),
		done:       make(chan struct{}),
		workerDone: make(chan struct{}),
	}

	pr.readPtr.Store(0)
	pr.writePtr.Store(0)

	go pr.prefetchWorker()

	return pr
}

func (pr *PrefetchReader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	// Check for errors
	if errPtr := pr.err.Load(); errPtr != nil {
		return 0, *errPtr
	}

	readPos := pr.readPtr.Load()
	writePos := pr.writePtr.Load()

	// If no data available, wait
	for readPos == writePos {
		// Check if worker is done
		select {
		case <-pr.workerDone:
			if errPtr := pr.err.Load(); errPtr != nil {
				return 0, *errPtr
			}
			return 0, io.EOF
		default:
			// Check for errors again
			if errPtr := pr.err.Load(); errPtr != nil {
				return 0, *errPtr
			}
		}

		time.Sleep(defaultSleepDuration)
		writePos = pr.writePtr.Load()
	}

	// Calculate available bytes
	available := writePos - readPos
	if available > uint64(len(p)) {
		available = uint64(len(p))
	}

	// Copy data from ring buffer
	for i := uint64(0); i < available; i++ {
		p[i] = pr.buffer[(readPos+i)%pr.bufferSize]
	}

	// Update read pointer
	pr.readPtr.Add(available)

	return int(available), nil
}

func (pr *PrefetchReader) prefetchWorker() {
	defer close(pr.workerDone)

	tmpBuf := make([]byte, 1024)

	for {
		select {
		case <-pr.done:
			return
		default:
			readPos := pr.readPtr.Load()
			writePos := pr.writePtr.Load()

			// Check if buffer is full
			if writePos-readPos >= pr.bufferSize {
				time.Sleep(defaultSleepDuration)
				continue
			}

			// Calculate space available
			spaceAvailable := pr.bufferSize - (writePos - readPos)
			if spaceAvailable > uint64(len(tmpBuf)) {
				spaceAvailable = uint64(len(tmpBuf))
			}

			// Read from source
			n, err := pr.source.Read(tmpBuf[:spaceAvailable])
			if n > 0 {
				// Copy to ring buffer
				for i := 0; i < n; i++ {
					pr.buffer[(writePos+uint64(i))%pr.bufferSize] = tmpBuf[i]
				}
				pr.writePtr.Add(uint64(n))
			}

			if err != nil {
				errCopy := new(error)
				*errCopy = err
				pr.err.Store(errCopy)
				return
			}
		}
	}
}

func (pr *PrefetchReader) Close() error {
	close(pr.done)
	<-pr.workerDone // Wait for worker to finish

	// Clean up buffer
	if pr.buffer != nil {
		pool.Put(pr.buffer)
		pr.buffer = nil
	}

	if closer, ok := pr.source.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
