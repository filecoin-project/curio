package pieceprovider

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-state-types/abi"
)

// mockReadCloser is a test helper that wraps bytes.Reader with Close functionality
type mockReadCloser struct {
	*bytes.Reader
	closed     bool
	closeMu    sync.Mutex
	closeErr   error
	closeCount int
}

func newMockReadCloser(data []byte) *mockReadCloser {
	return &mockReadCloser{
		Reader: bytes.NewReader(data),
	}
}

func (m *mockReadCloser) Close() error {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	m.closed = true
	m.closeCount++
	return m.closeErr
}

func (m *mockReadCloser) IsClosed() bool {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	return m.closed
}

func (m *mockReadCloser) CloseCount() int {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	return m.closeCount
}

// testPieceGetter creates a pieceGetter function for testing
func testPieceGetter(data []byte) pieceGetter {
	return func(offset, size uint64) (io.ReadCloser, error) {
		if offset >= uint64(len(data)) {
			return newMockReadCloser([]byte{}), nil
		}
		end := offset + size
		if end > uint64(len(data)) {
			end = uint64(len(data))
		}
		return newMockReadCloser(data[offset:end]), nil
	}
}

// trackingPieceGetter wraps pieceGetter and tracks calls
type trackingPieceGetter struct {
	data       []byte
	calls      []pieceGetterCall
	callsMu    sync.Mutex
	readerRefs []*mockReadCloser
	errOnCall  int // return error on Nth call (1-indexed), 0 means never
	err        error
}

type pieceGetterCall struct {
	Offset uint64
	Size   uint64
}

func newTrackingPieceGetter(data []byte) *trackingPieceGetter {
	return &trackingPieceGetter{
		data: data,
	}
}

func (t *trackingPieceGetter) get(offset, size uint64) (io.ReadCloser, error) {
	t.callsMu.Lock()
	defer t.callsMu.Unlock()

	t.calls = append(t.calls, pieceGetterCall{Offset: offset, Size: size})

	if t.errOnCall > 0 && len(t.calls) == t.errOnCall {
		return nil, t.err
	}

	if offset >= uint64(len(t.data)) {
		rc := newMockReadCloser([]byte{})
		t.readerRefs = append(t.readerRefs, rc)
		return rc, nil
	}
	end := offset + size
	if end > uint64(len(t.data)) {
		end = uint64(len(t.data))
	}
	rc := newMockReadCloser(t.data[offset:end])
	t.readerRefs = append(t.readerRefs, rc)
	return rc, nil
}

func (t *trackingPieceGetter) getCalls() []pieceGetterCall {
	t.callsMu.Lock()
	defer t.callsMu.Unlock()
	result := make([]pieceGetterCall, len(t.calls))
	copy(result, t.calls)
	return result
}

//func (t *trackingPieceGetter) setErrorOnCall(n int, err error) {
//	t.callsMu.Lock()
//	defer t.callsMu.Unlock()
//	t.errOnCall = n
//	t.err = err
//}

// createTestPieceReader creates a pieceReader for testing with given data
func createTestPieceReader(t *testing.T, data []byte) (*pieceReader, *trackingPieceGetter) {
	t.Helper()

	tracker := newTrackingPieceGetter(data)
	testCid, err := cid.Parse("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pr := &pieceReader{
		getReader: tracker.get,
		pieceCid:  testCid,
		len:       abi.UnpaddedPieceSize(len(data)),
		onClose:   cancel,
	}

	initialized, err := pr.init(ctx)
	require.NoError(t, err)
	require.NotNil(t, initialized)

	return initialized, tracker
}

// TestPieceReader_Init tests the initialization of pieceReader
func TestPieceReader_Init(t *testing.T) {
	t.Run("successful initialization", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, tracker := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		assert.False(t, pr.closed)
		assert.Equal(t, int64(0), pr.seqAt)
		assert.Equal(t, int64(0), pr.rAt)
		assert.NotNil(t, pr.r)
		assert.NotNil(t, pr.br)
		assert.NotNil(t, pr.remReads)

		// Should have made one call to get initial reader
		calls := tracker.getCalls()
		assert.Len(t, calls, 1)
		assert.Equal(t, uint64(0), calls[0].Offset)
		assert.Equal(t, uint64(len(data)), calls[0].Size)
	})

	t.Run("init with empty data", func(t *testing.T) {
		data := []byte{}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		assert.Equal(t, abi.UnpaddedPieceSize(0), pr.len)
	})

	t.Run("init error from getReader", func(t *testing.T) {
		testCid, _ := cid.Parse("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
		ctx := context.Background()

		expectedErr := errors.New("init error")
		pr := &pieceReader{
			getReader: func(offset, size uint64) (io.ReadCloser, error) {
				return nil, expectedErr
			},
			pieceCid: testCid,
			len:      100,
			onClose:  func() {},
		}

		result, err := pr.init(ctx)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("init returns nil when reader is nil", func(t *testing.T) {
		testCid, _ := cid.Parse("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
		ctx := context.Background()

		pr := &pieceReader{
			getReader: func(offset, size uint64) (io.ReadCloser, error) {
				return nil, nil
			},
			pieceCid: testCid,
			len:      100,
			onClose:  func() {},
		}

		result, err := pr.init(ctx)
		assert.NoError(t, err)
		assert.Nil(t, result)
	})
}

// TestPieceReader_Read tests sequential reading
func TestPieceReader_Read(t *testing.T) {
	t.Run("read all data in one call", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, len(data))
		n, err := pr.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, buf)
	})

	t.Run("read in multiple small chunks", func(t *testing.T) {
		data := []byte("hello world test data for chunked reading")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		result := make([]byte, 0, len(data))
		buf := make([]byte, 5)

		for {
			n, err := pr.Read(buf)
			if n > 0 {
				result = append(result, buf[:n]...)
			}
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}

		assert.Equal(t, data, result)
	})

	t.Run("read updates seqAt position", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		assert.Equal(t, int64(0), pr.seqAt)

		buf := make([]byte, 5)
		n, err := pr.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, int64(5), pr.seqAt)

		n, err = pr.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, int64(10), pr.seqAt)
	})

	t.Run("read from closed reader returns error", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		_ = pr.Close()

		buf := make([]byte, 5)
		_, err := pr.Read(buf)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "reader closed")
	})

	t.Run("read empty buffer", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 0)
		n, err := pr.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 0, n)
	})
}

// TestPieceReader_Seek tests seeking functionality
func TestPieceReader_Seek(t *testing.T) {
	t.Run("seek from start", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		pos, err := pr.Seek(5, io.SeekStart)
		require.NoError(t, err)
		assert.Equal(t, int64(5), pos)
		assert.Equal(t, int64(5), pr.seqAt)
	})

	t.Run("seek from current position", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// First seek to position 5
		_, err := pr.Seek(5, io.SeekStart)
		require.NoError(t, err)

		// Then seek 3 more from current
		pos, err := pr.Seek(3, io.SeekCurrent)
		require.NoError(t, err)
		assert.Equal(t, int64(8), pos)
		assert.Equal(t, int64(8), pr.seqAt)
	})

	t.Run("seek from end", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		pos, err := pr.Seek(-5, io.SeekEnd)
		require.NoError(t, err)
		assert.Equal(t, int64(len(data)-5), pos)
	})

	t.Run("seek to beginning", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Read some data first
		buf := make([]byte, 10)
		_, err := pr.Read(buf)
		require.NoError(t, err)

		// Seek back to start
		pos, err := pr.Seek(0, io.SeekStart)
		require.NoError(t, err)
		assert.Equal(t, int64(0), pos)
	})

	t.Run("seek with negative offset from current", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Seek to position 10
		_, err := pr.Seek(10, io.SeekStart)
		require.NoError(t, err)

		// Seek back 3 from current
		pos, err := pr.Seek(-3, io.SeekCurrent)
		require.NoError(t, err)
		assert.Equal(t, int64(7), pos)
	})

	t.Run("seek with invalid whence", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		_, err := pr.Seek(5, 99) // invalid whence
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bad whence")
	})

	t.Run("seek on closed reader", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		_ = pr.Close()

		_, err := pr.Seek(0, io.SeekStart)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "reader closed")
	})

	t.Run("seek and read", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Seek to position 6 ("world")
		_, err := pr.Seek(6, io.SeekStart)
		require.NoError(t, err)

		buf := make([]byte, 5)
		n, err := pr.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "world", string(buf))
	})
}

// TestPieceReader_ReadAt tests random access reading
func TestPieceReader_ReadAt(t *testing.T) {
	t.Run("read at specific offset", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 5)
		n, err := pr.ReadAt(buf, 6)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "world", string(buf))
	})

	t.Run("read at beginning", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 5)
		n, err := pr.ReadAt(buf, 0)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", string(buf))
	})

	t.Run("read at end", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 4)
		n, err := pr.ReadAt(buf, int64(len(data)-4))
		require.NoError(t, err)
		assert.Equal(t, 4, n)
		assert.Equal(t, "data", string(buf))
	})

	t.Run("read at offset beyond data returns EOF", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 5)
		n, err := pr.ReadAt(buf, int64(len(data)+10))
		assert.Equal(t, io.EOF, err)
		assert.Equal(t, 0, n)
	})

	t.Run("ReadAt does not affect sequential Read position", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Read some data sequentially
		buf := make([]byte, 5)
		_, err := pr.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(buf))

		// Do a ReadAt at a different position
		buf2 := make([]byte, 4)
		_, err = pr.ReadAt(buf2, int64(len(data)-4))
		require.NoError(t, err)
		assert.Equal(t, "data", string(buf2))

		// Sequential read should continue from where it left off
		_, err = pr.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, " worl", string(buf))
	})

	t.Run("multiple ReadAt calls", func(t *testing.T) {
		data := []byte("0123456789ABCDEFGHIJ")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		tests := []struct {
			offset   int64
			size     int
			expected string
		}{
			{0, 5, "01234"},
			{10, 5, "ABCDE"},
			{5, 5, "56789"},
			{15, 5, "FGHIJ"},
		}

		for _, tc := range tests {
			buf := make([]byte, tc.size)
			n, err := pr.ReadAt(buf, tc.offset)
			require.NoError(t, err)
			assert.Equal(t, tc.size, n)
			assert.Equal(t, tc.expected, string(buf))
		}
	})
}

// TestPieceReader_ReadAtCaching tests the LRU cache behavior in ReadAt
func TestPieceReader_ReadAtCaching(t *testing.T) {
	// We need to use data larger than MinRandomReadSize to test caching behavior
	// MinRandomReadSize is 1MB, so we'll test with smaller reads that trigger caching

	t.Run("small reads are cached", func(t *testing.T) {
		// Create data larger than MinRandomReadSize for meaningful testing
		dataSize := MinRandomReadSize + 1024
		data := make([]byte, dataSize)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, tracker := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// First small read (smaller than MinRandomReadSize)
		buf := make([]byte, 100)
		n, err := pr.ReadAt(buf, 0)
		require.NoError(t, err)
		assert.Equal(t, 100, n)

		// Check that we made calls (initial + ReadAt call)
		calls := tracker.getCalls()
		assert.GreaterOrEqual(t, len(calls), 1)

		// Second read at same offset should hit cache (if data was cached)
		buf2 := make([]byte, 50)
		_, err = pr.ReadAt(buf2, 0)
		require.NoError(t, err)

		// Verify the first part matches
		assert.Equal(t, buf[:50], buf2)
	})

	t.Run("cache stores read-ahead data", func(t *testing.T) {
		// Create deterministic test data
		dataSize := MinRandomReadSize * 2
		data := make([]byte, dataSize)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Small read at offset 0
		buf1 := make([]byte, 100)
		_, err := pr.ReadAt(buf1, 0)
		require.NoError(t, err)

		// Verify data correctness
		expected := make([]byte, 100)
		for i := range expected {
			expected[i] = byte(i % 256)
		}
		assert.Equal(t, expected, buf1)
	})
}

// TestPieceReader_Close tests close functionality
func TestPieceReader_Close(t *testing.T) {
	t.Run("close successfully", func(t *testing.T) {
		data := []byte("hello world")
		var onCloseCalled atomic.Bool

		testCid, _ := cid.Parse("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
		ctx := context.Background()

		tracker := newTrackingPieceGetter(data)
		pr := &pieceReader{
			getReader: tracker.get,
			pieceCid:  testCid,
			len:       abi.UnpaddedPieceSize(len(data)),
			onClose:   func() { onCloseCalled.Store(true) },
		}

		initialized, err := pr.init(ctx)
		require.NoError(t, err)
		require.NotNil(t, initialized)

		err = initialized.Close()
		require.NoError(t, err)

		assert.True(t, initialized.closed)
		assert.True(t, onCloseCalled.Load())
	})

	t.Run("double close returns error", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)

		err := pr.Close()
		require.NoError(t, err)

		err = pr.Close()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "reader closed")
	})

	t.Run("close with nil reader", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)

		// Manually set reader to nil
		pr.seqMu.Lock()
		if pr.r != nil {
			_ = pr.r.Close()
			pr.r = nil
		}
		pr.seqMu.Unlock()

		err := pr.Close()
		require.NoError(t, err)
		assert.True(t, pr.closed)
	})
}

// TestPieceReader_BurnBytes tests the "burn bytes" optimization
func TestPieceReader_BurnBytes(t *testing.T) {
	// MaxPieceReaderBurnBytes is 1MB - for small skips we burn bytes instead of creating new reader

	t.Run("small forward seek burns bytes instead of new reader", func(t *testing.T) {
		data := make([]byte, 2*1024*1024) // 2MB
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, tracker := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Initial reader creation
		initialCalls := len(tracker.getCalls())

		// Read 10 bytes
		buf := make([]byte, 10)
		_, err := pr.Read(buf)
		require.NoError(t, err)

		// Seek forward by less than MaxPieceReaderBurnBytes (1MB)
		_, err = pr.Seek(100, io.SeekStart) // Seek to position 100
		require.NoError(t, err)

		// Read again - should burn 90 bytes, not create new reader
		_, err = pr.Read(buf)
		require.NoError(t, err)

		// Should not have created additional readers (burn is within threshold)
		assert.Equal(t, initialCalls, len(tracker.getCalls()))
	})

	t.Run("large forward seek creates new reader", func(t *testing.T) {
		data := make([]byte, 4*1024*1024) // 4MB
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, tracker := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Read 10 bytes to establish position
		buf := make([]byte, 10)
		_, err := pr.Read(buf)
		require.NoError(t, err)

		initialCalls := len(tracker.getCalls())

		// Seek forward by more than MaxPieceReaderBurnBytes (1MB)
		_, err = pr.Seek(2*1024*1024, io.SeekStart) // Seek to 2MB
		require.NoError(t, err)

		// Read again - should create new reader
		_, err = pr.Read(buf)
		require.NoError(t, err)

		// Should have created a new reader
		assert.Greater(t, len(tracker.getCalls()), initialCalls)
	})

	t.Run("backward seek always creates new reader", func(t *testing.T) {
		data := make([]byte, 1024*1024) // 1MB
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, tracker := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Read some data to advance position
		buf := make([]byte, 1000)
		_, err := pr.Read(buf)
		require.NoError(t, err)

		initialCalls := len(tracker.getCalls())

		// Seek backward
		_, err = pr.Seek(0, io.SeekStart)
		require.NoError(t, err)

		// Read again - should create new reader for backward seek
		_, err = pr.Read(buf)
		require.NoError(t, err)

		// Should have created a new reader
		assert.Greater(t, len(tracker.getCalls()), initialCalls)
	})
}

// TestPieceReader_ReadSeqReader tests the internal readSeqReader function
func TestPieceReader_ReadSeqReader(t *testing.T) {
	t.Run("sequential reads work correctly", func(t *testing.T) {
		data := []byte("hello world test data for sequential reading")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		result := make([]byte, 0, len(data))
		buf := make([]byte, 10)

		for {
			n, err := pr.Read(buf)
			if n > 0 {
				result = append(result, buf[:n]...)
			}
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}

		assert.Equal(t, data, result)
	})

	t.Run("handles partial reads at end of data", func(t *testing.T) {
		data := []byte("hello")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Try to read more than available
		buf := make([]byte, 100)
		n, err := pr.Read(buf)

		assert.Equal(t, len(data), n)
		assert.Equal(t, data, buf[:n])
		// May get EOF immediately or on next read
		if err != nil {
			assert.Equal(t, io.EOF, err)
		}
	})
}

// TestPieceReader_ConcurrentAccess tests thread safety
func TestPieceReader_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent ReadAt calls", func(t *testing.T) {
		dataSize := 10 * 1024 // 10KB
		data := make([]byte, dataSize)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		var wg sync.WaitGroup
		numGoroutines := 10
		readsPerGoroutine := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				buf := make([]byte, 100)
				for j := 0; j < readsPerGoroutine; j++ {
					offset := int64((id*readsPerGoroutine + j) % (dataSize - 100))
					_, err := pr.ReadAt(buf, offset)
					if err != nil && err != io.EOF {
						t.Errorf("unexpected error: %v", err)
					}
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("concurrent Read and Seek", func(t *testing.T) {
		data := make([]byte, 10*1024)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		var wg sync.WaitGroup

		// Reader goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 100)
			for i := 0; i < 100; i++ {
				_, _ = pr.Read(buf)
			}
		}()

		// Seeker goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, _ = pr.Seek(int64(i*100%len(data)), io.SeekStart)
			}
		}()

		wg.Wait()
	})
}

// TestPieceReader_EdgeCases tests various edge cases
func TestPieceReader_EdgeCases(t *testing.T) {
	t.Run("seek to exact end", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		pos, err := pr.Seek(0, io.SeekEnd)
		require.NoError(t, err)
		assert.Equal(t, int64(len(data)), pos)

		// Read at end should return EOF
		buf := make([]byte, 1)
		n, err := pr.Read(buf)
		assert.Equal(t, 0, n)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("seek beyond end", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		pos, err := pr.Seek(100, io.SeekStart)
		require.NoError(t, err)
		assert.Equal(t, int64(100), pos)

		// Read should return an error (either EOF or wrapped EOF)
		buf := make([]byte, 1)
		n, err := pr.Read(buf)
		assert.Equal(t, 0, n)
		assert.Error(t, err) // Error could be wrapped, so just check it's an error
	})

	t.Run("seek to negative position via SeekEnd", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Seek past the beginning
		pos, err := pr.Seek(-100, io.SeekEnd)
		require.NoError(t, err)
		// Position will be negative
		assert.Equal(t, int64(len(data))-100, pos)
	})

	t.Run("read exactly all data", func(t *testing.T) {
		data := []byte("hello world test")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, len(data))
		n, err := pr.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, buf)

		// Next read should return EOF
		n, err = pr.Read(buf)
		assert.Equal(t, 0, n)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("ReadAt with exact buffer size matching data", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, len(data))
		n, err := pr.ReadAt(buf, 0)
		require.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, buf)
	})

	t.Run("ReadAt requesting more than available", func(t *testing.T) {
		data := []byte("hello")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 100)
		n, err := pr.ReadAt(buf, 0)
		assert.Equal(t, io.EOF, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, buf[:n])
	})
}

// TestPieceReader_ReaderReset tests reader reset scenarios
func TestPieceReader_ReaderReset(t *testing.T) {
	t.Run("reader reset on nil reader", func(t *testing.T) {
		data := make([]byte, 2*1024*1024)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, tracker := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Force reader to nil by seeking beyond burn threshold and reading
		buf := make([]byte, 100)
		_, err := pr.Read(buf)
		require.NoError(t, err)

		initialCalls := len(tracker.getCalls())

		// Manually close the internal reader to simulate nil state
		pr.seqMu.Lock()
		if pr.r != nil {
			_ = pr.r.Close()
			pr.r = nil
			pr.br = nil
		}
		pr.seqMu.Unlock()

		// Read should recreate reader
		_, err = pr.Read(buf)
		require.NoError(t, err)

		assert.Greater(t, len(tracker.getCalls()), initialCalls)
	})
}

// TestPieceReader_GetReaderErrors tests error handling from getReader
func TestPieceReader_GetReaderErrors(t *testing.T) {
	t.Run("getReader error during readInto", func(t *testing.T) {
		data := make([]byte, 1024)
		for i := range data {
			data[i] = byte(i % 256)
		}

		expectedErr := errors.New("simulated getReader error")
		callCount := 0
		testCid, _ := cid.Parse("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		pr := &pieceReader{
			getReader: func(offset, size uint64) (io.ReadCloser, error) {
				callCount++
				// First call is for init(), let it succeed
				// Subsequent calls should fail
				if callCount > 1 {
					return nil, expectedErr
				}
				if offset >= uint64(len(data)) {
					return newMockReadCloser([]byte{}), nil
				}
				end := offset + size
				if end > uint64(len(data)) {
					end = uint64(len(data))
				}
				return newMockReadCloser(data[offset:end]), nil
			},
			pieceCid: testCid,
			len:      abi.UnpaddedPieceSize(len(data)),
			onClose:  cancel,
		}

		initialized, err := pr.init(ctx)
		require.NoError(t, err)
		defer func() {
			_ = initialized.Close()
		}()

		// ReadAt uses readInto which creates a new reader
		buf := make([]byte, 100)
		_, err = initialized.ReadAt(buf, 500) // ReadAt at non-zero offset
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "simulated getReader error")
	})

	t.Run("getReader error during sequential read after nil reader", func(t *testing.T) {
		// This tests the scenario where the internal reader is nil and getReader fails
		data := make([]byte, 1024)
		for i := range data {
			data[i] = byte(i % 256)
		}

		callCount := 0
		expectedErr := errors.New("reset reader error")
		testCid, _ := cid.Parse("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		pr := &pieceReader{
			getReader: func(offset, size uint64) (io.ReadCloser, error) {
				callCount++
				// First call is for init(), let it succeed
				// Second call (after manually nilling reader) should fail
				// Call sequence: init (1), then read after nil (2)
				if callCount > 1 {
					return nil, expectedErr
				}
				if offset >= uint64(len(data)) {
					return newMockReadCloser([]byte{}), nil
				}
				end := offset + size
				if end > uint64(len(data)) {
					end = uint64(len(data))
				}
				return newMockReadCloser(data[offset:end]), nil
			},
			pieceCid: testCid,
			len:      abi.UnpaddedPieceSize(len(data)),
			onClose:  cancel,
		}

		initialized, err := pr.init(ctx)
		require.NoError(t, err)
		require.NotNil(t, initialized)

		// Read some data first - this uses the existing reader, no new getReader call
		buf := make([]byte, 100)
		_, err = initialized.Read(buf)
		require.NoError(t, err)

		// Manually nil the reader to simulate state requiring reset
		initialized.seqMu.Lock()
		if initialized.r != nil {
			_ = initialized.r.Close()
			initialized.r = nil
			initialized.br = nil
		}
		initialized.seqMu.Unlock()

		// Next read should trigger getReader which will fail (second call)
		_, err = initialized.Read(buf)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "reset reader error")

		// Don't close since state is corrupted - set closed to prevent double close attempt
		initialized.seqMu.Lock()
		initialized.closed = true
		initialized.seqMu.Unlock()
	})
}

// TestPieceReader_SmallVsLargeReads tests the behavior difference between small and large reads in ReadAt
func TestPieceReader_SmallVsLargeReads(t *testing.T) {
	t.Run("small read triggers caching behavior", func(t *testing.T) {
		dataSize := int(MinRandomReadSize * 2)
		data := make([]byte, dataSize)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Small read (less than MinRandomReadSize)
		smallBuf := make([]byte, MinRandomReadSize/2)
		n, err := pr.ReadAt(smallBuf, 0)
		require.NoError(t, err)
		assert.Equal(t, int(MinRandomReadSize/2), n)

		// Verify data
		for i := 0; i < len(smallBuf); i++ {
			assert.Equal(t, byte(i%256), smallBuf[i], "mismatch at position %d", i)
		}
	})

	t.Run("large read goes directly to user buffer", func(t *testing.T) {
		dataSize := int(MinRandomReadSize * 3)
		data := make([]byte, dataSize)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Large read (greater than MinRandomReadSize)
		largeBuf := make([]byte, MinRandomReadSize*2)
		n, err := pr.ReadAt(largeBuf, 0)
		require.NoError(t, err)
		assert.Equal(t, int(MinRandomReadSize*2), n)

		// Verify data
		for i := 0; i < len(largeBuf); i++ {
			assert.Equal(t, byte(i%256), largeBuf[i], "mismatch at position %d", i)
		}
	})
}

// TestPieceReader_HeaderCaching tests that offset 0 data is specially cached
func TestPieceReader_HeaderCaching(t *testing.T) {
	t.Run("header data kept in cache", func(t *testing.T) {
		dataSize := int(MinRandomReadSize * 2)
		data := make([]byte, dataSize)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Read header (offset 0)
		headerBuf := make([]byte, 100)
		n, err := pr.ReadAt(headerBuf, 0)
		require.NoError(t, err)
		assert.Equal(t, 100, n)

		// Read header again - should use cache
		headerBuf2 := make([]byte, 50)
		n, err = pr.ReadAt(headerBuf2, 0)
		require.NoError(t, err)
		assert.Equal(t, 50, n)

		// Data should match
		assert.Equal(t, headerBuf[:50], headerBuf2)
	})
}

// TestPieceReader_ReadInto tests the readInto internal function
func TestPieceReader_ReadInto(t *testing.T) {
	t.Run("readInto basic functionality", func(t *testing.T) {
		data := []byte("hello world test data")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 5)
		n, err := pr.readInto(buf, 6)
		require.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "world", string(buf))
	})

	t.Run("readInto at end of data", func(t *testing.T) {
		data := []byte("hello")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 10)
		n, err := pr.readInto(buf, 0)
		assert.Equal(t, io.EOF, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", string(buf[:n]))
	})
}

// TestPieceReader_UnpaddedPieceSize tests with various piece sizes
func TestPieceReader_UnpaddedPieceSize(t *testing.T) {
	sizes := []int{
		0,
		127,
		254,
		1016,
		1024,
		32768,
		65536,
	}

	for _, size := range sizes {
		t.Run(string(rune(size)), func(t *testing.T) {
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i % 256)
			}

			if size == 0 {
				// Empty data case
				pr, _ := createTestPieceReader(t, data)
				defer func() {
					_ = pr.Close()
				}()
				assert.Equal(t, abi.UnpaddedPieceSize(0), pr.len)
				return
			}

			pr, _ := createTestPieceReader(t, data)
			defer func() {
				_ = pr.Close()
			}()

			// Read all and verify
			result := make([]byte, 0, size)
			buf := make([]byte, 127) // Odd-sized buffer

			for {
				n, err := pr.Read(buf)
				if n > 0 {
					result = append(result, buf[:n]...)
				}
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
			}

			assert.Equal(t, data, result)
		})
	}
}

// TestPieceReader_DataCorrectness tests that all read operations return correct data
func TestPieceReader_DataCorrectness(t *testing.T) {
	t.Run("sequential read returns exact bytes", func(t *testing.T) {
		// Create deterministic test data with known pattern
		data := make([]byte, 10000)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Read all data and verify byte-by-byte
		result := make([]byte, len(data))
		totalRead := 0
		buf := make([]byte, 137) // Odd size to test boundary handling

		for totalRead < len(data) {
			n, err := pr.Read(buf)
			if n > 0 {
				copy(result[totalRead:], buf[:n])
				totalRead += n
			}
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}

		// Verify every single byte
		require.Equal(t, len(data), totalRead, "should read all data")
		for i := 0; i < len(data); i++ {
			assert.Equal(t, data[i], result[i], "mismatch at position %d: expected %d, got %d", i, data[i], result[i])
		}
	})

	t.Run("ReadAt returns exact bytes at any offset", func(t *testing.T) {
		// Create deterministic test data
		data := make([]byte, 5000)
		for i := range data {
			data[i] = byte((i * 7) % 256) // Different pattern
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Test various offsets and sizes
		testCases := []struct {
			offset int64
			size   int
		}{
			{0, 100},
			{0, 1},
			{1, 1},
			{50, 200},
			{100, 500},
			{4900, 100},
			{4999, 1},
			{1000, 1000},
			{0, len(data)}, // Read entire piece
		}

		for _, tc := range testCases {
			buf := make([]byte, tc.size)
			n, err := pr.ReadAt(buf, tc.offset)
			if tc.offset+int64(tc.size) > int64(len(data)) {
				// Expected partial read
				expectedN := int(int64(len(data)) - tc.offset)
				assert.Equal(t, expectedN, n, "offset=%d size=%d", tc.offset, tc.size)
			} else {
				require.NoError(t, err, "offset=%d size=%d", tc.offset, tc.size)
				assert.Equal(t, tc.size, n, "offset=%d size=%d", tc.offset, tc.size)
			}

			// Verify each byte
			for i := 0; i < n; i++ {
				expectedByte := byte((int(tc.offset) + i) * 7 % 256)
				assert.Equal(t, expectedByte, buf[i],
					"mismatch at ReadAt offset=%d, buf[%d]: expected %d, got %d",
					tc.offset, i, expectedByte, buf[i])
			}
		}
	})

	t.Run("seek then read returns correct data", func(t *testing.T) {
		data := make([]byte, 2000)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Seek to various positions and verify data
		positions := []int64{0, 100, 500, 1000, 1500, 1999}
		for _, pos := range positions {
			_, err := pr.Seek(pos, io.SeekStart)
			require.NoError(t, err)

			buf := make([]byte, 50)
			expectedLen := 50
			if pos+50 > int64(len(data)) {
				expectedLen = int(int64(len(data)) - pos)
			}

			n, err := pr.Read(buf)
			if expectedLen < 50 {
				assert.Equal(t, io.EOF, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, expectedLen, n, "at position %d", pos)

			// Verify data
			for i := 0; i < n; i++ {
				expected := byte((int(pos) + i) % 256)
				assert.Equal(t, expected, buf[i],
					"mismatch after seek to %d, buf[%d]: expected %d, got %d",
					pos, i, expected, buf[i])
			}
		}
	})

	t.Run("interleaved Read and ReadAt return correct data", func(t *testing.T) {
		data := make([]byte, 1000)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Sequential read
		buf1 := make([]byte, 100)
		n1, err := pr.Read(buf1)
		require.NoError(t, err)
		assert.Equal(t, 100, n1)
		for i := 0; i < n1; i++ {
			assert.Equal(t, byte(i%256), buf1[i], "Read buf[%d]", i)
		}

		// ReadAt at different position
		buf2 := make([]byte, 50)
		n2, err := pr.ReadAt(buf2, 500)
		require.NoError(t, err)
		assert.Equal(t, 50, n2)
		for i := 0; i < n2; i++ {
			assert.Equal(t, byte((500+i)%256), buf2[i], "ReadAt buf[%d]", i)
		}

		// Continue sequential read - should be at position 100
		buf3 := make([]byte, 100)
		n3, err := pr.Read(buf3)
		require.NoError(t, err)
		assert.Equal(t, 100, n3)
		for i := 0; i < n3; i++ {
			assert.Equal(t, byte((100+i)%256), buf3[i], "Second Read buf[%d]", i)
		}
	})
}

// TestPieceReader_ReadAtSemantics tests that ReadAt follows io.ReaderAt contract
func TestPieceReader_ReadAtSemantics(t *testing.T) {
	t.Run("ReadAt does not modify sequential read position", func(t *testing.T) {
		data := make([]byte, 1000)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Read 100 bytes sequentially
		buf := make([]byte, 100)
		_, err := pr.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, int64(100), pr.seqAt, "seqAt should be 100 after first Read")

		// Do multiple ReadAt calls at various positions
		for _, offset := range []int64{0, 200, 500, 999} {
			_, err := pr.ReadAt(make([]byte, 1), offset)
			if offset < int64(len(data)) {
				require.NoError(t, err)
			}
		}

		// seqAt should still be 100
		assert.Equal(t, int64(100), pr.seqAt, "seqAt should still be 100 after ReadAt calls")

		// Next Read should continue from position 100
		n, err := pr.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, 100, n)
		// Verify data starts at byte 100
		for i := 0; i < n; i++ {
			assert.Equal(t, byte((100+i)%256), buf[i])
		}
		assert.Equal(t, int64(200), pr.seqAt)
	})

	t.Run("ReadAt can read same offset multiple times with same result", func(t *testing.T) {
		data := make([]byte, 500)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		offset := int64(100)
		size := 50

		// Read multiple times
		results := make([][]byte, 5)
		for i := 0; i < 5; i++ {
			results[i] = make([]byte, size)
			n, err := pr.ReadAt(results[i], offset)
			require.NoError(t, err)
			assert.Equal(t, size, n)
		}

		// All results should be identical
		for i := 1; i < len(results); i++ {
			assert.Equal(t, results[0], results[i], "ReadAt result %d differs from first", i)
		}

		// Verify correctness
		for i := 0; i < size; i++ {
			assert.Equal(t, byte((int(offset)+i)%256), results[0][i])
		}
	})

	t.Run("ReadAt at end of data returns correct partial read", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// Try to read 10 bytes starting 3 bytes from end
		buf := make([]byte, 10)
		n, err := pr.ReadAt(buf, int64(len(data)-3))

		assert.Equal(t, 3, n)
		assert.Equal(t, io.EOF, err)
		assert.Equal(t, "rld", string(buf[:n]))
	})

	t.Run("ReadAt with zero-length buffer", func(t *testing.T) {
		data := []byte("hello world")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 0)
		n, err := pr.ReadAt(buf, 5)
		assert.NoError(t, err)
		assert.Equal(t, 0, n)
	})

	t.Run("ReadAt beyond EOF returns EOF and zero bytes", func(t *testing.T) {
		data := []byte("hello")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		buf := make([]byte, 10)
		n, err := pr.ReadAt(buf, 100) // Way past end
		assert.Equal(t, 0, n)
		assert.Equal(t, io.EOF, err)
	})

	t.Run("concurrent ReadAt calls return correct data", func(t *testing.T) {
		data := make([]byte, 10000)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		var wg sync.WaitGroup
		errCh := make(chan error, 100)

		// Spawn multiple goroutines doing ReadAt
		for g := 0; g < 10; g++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for i := 0; i < 100; i++ {
					offset := int64((goroutineID*1000 + i*10) % len(data))
					size := 50
					if offset+int64(size) > int64(len(data)) {
						size = int(int64(len(data)) - offset)
					}

					buf := make([]byte, size)
					n, err := pr.ReadAt(buf, offset)
					if err != nil && err != io.EOF {
						errCh <- err
						return
					}

					// Verify data
					for j := 0; j < n; j++ {
						expected := byte((int(offset) + j) % 256)
						if buf[j] != expected {
							errCh <- errors.New("data mismatch in concurrent ReadAt")
							return
						}
					}
				}
			}(g)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("concurrent ReadAt error: %v", err)
		}
	})

	t.Run("ReadAt is idempotent - calling twice gives same result", func(t *testing.T) {
		data := make([]byte, 500)
		for i := range data {
			data[i] = byte((i * 13) % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		// First read
		buf1 := make([]byte, 100)
		n1, err1 := pr.ReadAt(buf1, 200)
		require.NoError(t, err1)

		// Second read at same position
		buf2 := make([]byte, 100)
		n2, err2 := pr.ReadAt(buf2, 200)
		require.NoError(t, err2)

		assert.Equal(t, n1, n2)
		assert.Equal(t, buf1, buf2)
	})
}

// TestPieceReader_ReadSemantics tests that Read follows io.Reader contract
func TestPieceReader_ReadSemantics(t *testing.T) {
	t.Run("Read advances position correctly", func(t *testing.T) {
		data := make([]byte, 500)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		positions := []int64{0}
		buf := make([]byte, 73) // Odd size

		for {
			n, err := pr.Read(buf)
			if n > 0 {
				positions = append(positions, positions[len(positions)-1]+int64(n))
			}
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}

		// Verify positions advance correctly
		assert.Equal(t, int64(0), positions[0])
		assert.Equal(t, int64(len(data)), positions[len(positions)-1])
	})

	t.Run("Read after Seek returns correct data", func(t *testing.T) {
		data := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		testCases := []struct {
			seekPos  int64
			expected string
			readLen  int
		}{
			{0, "01234", 5},
			{10, "ABCDE", 5},
			{26, "QRSTU", 5},
			{35, "Z", 5}, // Only 1 byte left
		}

		for _, tc := range testCases {
			_, err := pr.Seek(tc.seekPos, io.SeekStart)
			require.NoError(t, err)

			buf := make([]byte, tc.readLen)
			n, err := pr.Read(buf)
			if tc.seekPos+int64(tc.readLen) > int64(len(data)) {
				// Expect partial read
				expectedN := int(int64(len(data)) - tc.seekPos)
				assert.Equal(t, expectedN, n)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.readLen, n)
			}
			assert.Equal(t, tc.expected, string(buf[:n]), "at seekPos %d", tc.seekPos)
		}
	})

	t.Run("multiple short reads accumulate correctly", func(t *testing.T) {
		data := []byte("The quick brown fox jumps over the lazy dog")
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		var accumulated []byte
		buf := make([]byte, 7) // Small buffer

		for {
			n, err := pr.Read(buf)
			accumulated = append(accumulated, buf[:n]...)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}

		assert.Equal(t, data, accumulated)
	})
}

// TestPieceReader_SeekSemantics tests Seek behavior
func TestPieceReader_SeekSemantics(t *testing.T) {
	t.Run("Seek returns new absolute position", func(t *testing.T) {
		data := make([]byte, 1000)
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		pos, err := pr.Seek(100, io.SeekStart)
		require.NoError(t, err)
		assert.Equal(t, int64(100), pos)

		pos, err = pr.Seek(50, io.SeekCurrent)
		require.NoError(t, err)
		assert.Equal(t, int64(150), pos)

		pos, err = pr.Seek(-10, io.SeekEnd)
		require.NoError(t, err)
		assert.Equal(t, int64(990), pos)
	})

	t.Run("Seek to same position multiple times", func(t *testing.T) {
		data := make([]byte, 500)
		for i := range data {
			data[i] = byte(i % 256)
		}
		pr, _ := createTestPieceReader(t, data)
		defer func() {
			_ = pr.Close()
		}()

		for i := 0; i < 5; i++ {
			_, err := pr.Seek(100, io.SeekStart)
			require.NoError(t, err)

			buf := make([]byte, 10)
			n, err := pr.Read(buf)
			require.NoError(t, err)
			assert.Equal(t, 10, n)

			// Verify same data each time
			for j := 0; j < 10; j++ {
				assert.Equal(t, byte((100+j)%256), buf[j])
			}
		}
	})
}

// BenchmarkPieceReader_SequentialRead benchmarks sequential reading
func BenchmarkPieceReader_SequentialRead(b *testing.B) {
	sizes := []int{
		1024,
		64 * 1024,
		1024 * 1024,
	}

	for _, dataSize := range sizes {
		b.Run(string(rune(dataSize)), func(b *testing.B) {
			data := make([]byte, dataSize)
			for i := range data {
				data[i] = byte(i % 256)
			}

			testCid, _ := cid.Parse("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
			buf := make([]byte, 4096)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ctx, cancel := context.WithCancel(context.Background())
				pr := &pieceReader{
					getReader: testPieceGetter(data),
					pieceCid:  testCid,
					len:       abi.UnpaddedPieceSize(len(data)),
					onClose:   cancel,
				}
				initialized, _ := pr.init(ctx)
				if initialized == nil {
					continue
				}

				for {
					_, err := initialized.Read(buf)
					if err == io.EOF {
						break
					}
				}
				_ = initialized.Close()
			}
		})
	}
}

// BenchmarkPieceReader_RandomRead benchmarks random access reading
func BenchmarkPieceReader_RandomRead(b *testing.B) {
	dataSize := 1024 * 1024
	data := make([]byte, dataSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	testCid, _ := cid.Parse("bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr := &pieceReader{
		getReader: testPieceGetter(data),
		pieceCid:  testCid,
		len:       abi.UnpaddedPieceSize(len(data)),
		onClose:   cancel,
	}
	initialized, _ := pr.init(ctx)
	defer func() {
		_ = initialized.Close()
	}()

	buf := make([]byte, 4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offset := int64((i * 12345) % (dataSize - 4096))
		_, _ = initialized.ReadAt(buf, offset)
	}
}
