package cachedreader

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

// mockReader implements io.Reader with controllable behavior
type mockReader struct {
	data    []byte
	pos     int
	delay   time.Duration
	errAt   int
	closed  bool
	readErr error
}

func (m *mockReader) Read(p []byte) (n int, err error) {
	if m.closed {
		return 0, errors.New("read from closed reader")
	}
	if m.readErr != nil {
		return 0, m.readErr
	}
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	if m.errAt > 0 && m.pos >= m.errAt {
		return 0, errors.New("planned error")
	}

	n = copy(p, m.data[m.pos:])
	m.pos += n
	return n, nil
}

func (m *mockReader) Close() error {
	m.closed = true
	return nil
}

func TestPrefetchReader_BasicRead(t *testing.T) {
	testData := []byte("Hello, World!")
	source := &mockReader{data: testData}
	reader := New(source, 1024)
	defer reader.Close()

	buf := make([]byte, len(testData))
	n, err := reader.Read(buf)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != len(testData) {
		t.Errorf("expected to read %d bytes, got %d", len(testData), n)
	}
	if !bytes.Equal(buf, testData) {
		t.Errorf("expected %q, got %q", testData, buf)
	}
}

func TestPrefetchReader_ReadEmpty(t *testing.T) {
	reader := New(&mockReader{data: []byte{}}, 1024)
	defer reader.Close()

	buf := make([]byte, 10)
	n, err := reader.Read(buf)

	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
	if n != 0 {
		t.Errorf("expected to read 0 bytes, got %d", n)
	}
}

func TestPrefetchReader_ReadZeroLength(t *testing.T) {
	reader := New(&mockReader{data: []byte("data")}, 1024)
	defer reader.Close()

	n, err := reader.Read([]byte{})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected to read 0 bytes, got %d", n)
	}
}

func TestPrefetchReader_LargeRead(t *testing.T) {
	// Create large test data
	testData := make([]byte, 1024*1024) // 1MB
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	source := &mockReader{data: testData}
	reader := New(source, 4096) // Smaller buffer to test multiple reads
	defer reader.Close()

	// Read in chunks
	buf := make([]byte, len(testData))
	totalRead := 0
	for totalRead < len(testData) {
		n, err := reader.Read(buf[totalRead:])
		if n > 0 {
			totalRead += n
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if totalRead != len(testData) {
		t.Errorf("expected to read %d bytes, got %d", len(testData), totalRead)
		return // Prevent array bounds panic in next check
	}
	if !bytes.Equal(buf[:totalRead], testData) {
		t.Error("read data doesn't match expected data")
		// Add more debug info
		for i := 0; i < totalRead; i++ {
			if buf[i] != testData[i] {
				t.Errorf("first mismatch at position %d: got %d, want %d", i, buf[i], testData[i])
				break
			}
		}
	}
}

func TestPrefetchReader_SlowSource(t *testing.T) {
	testData := []byte("Slow data source test")
	source := &mockReader{
		data:  testData,
		delay: 50 * time.Millisecond,
	}

	reader := New(source, 1024)
	defer reader.Close()

	buf := make([]byte, len(testData))
	n, err := reader.Read(buf)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != len(testData) {
		t.Errorf("expected to read %d bytes, got %d", len(testData), n)
	}
	if !bytes.Equal(buf, testData) {
		t.Errorf("expected %q, got %q", testData, buf)
	}
}

func TestPrefetchReader_ErrorHandling(t *testing.T) {
	testData := []byte("Error test data")
	source := &mockReader{
		data:    testData,
		readErr: errors.New("planned error"),
	}

	reader := New(source, 1024)
	defer reader.Close()

	buf := make([]byte, len(testData))
	_, firstErr := reader.Read(buf)

	for firstErr == nil {
		_, firstErr = reader.Read(buf)
	}

	if firstErr == nil || firstErr.Error() != "planned error" {
		t.Errorf("expected 'planned error', got %v", firstErr)
	}

	_, secondErr := reader.Read(buf)
	if secondErr == nil || secondErr != firstErr {
		t.Errorf("expected same error on second read, got %v", secondErr)
	}
}

func TestPrefetchReader_Close(t *testing.T) {
	source := &mockReader{data: []byte("test")}
	reader := New(source, 1024)

	if err := reader.Close(); err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	if !source.closed {
		t.Error("source was not closed")
	}

	buf := make([]byte, 4)
	_, err := reader.Read(buf)
	if err == nil {
		t.Error("expected error reading from closed reader")
	}
}

func TestPrefetchReader_MinBufferSize(t *testing.T) {
	reader := New(&mockReader{}, 100) // Less than minBufferDepth
	defer reader.Close()

	if cap(reader.buffer) < minBufferDepth {
		t.Errorf("buffer size %d is less than minimum %d", cap(reader.buffer), minBufferDepth)
	}
}

func TestPrefetchReader_ReadAfterError(t *testing.T) {
	source := &mockReader{
		data:    []byte("test data"),
		readErr: errors.New("read error"),
	}

	reader := New(source, 1024)
	defer reader.Close()

	buf := make([]byte, 4)
	_, firstErr := reader.Read(buf)
	if firstErr == nil {
		t.Error("expected error on first read")
	}

	_, secondErr := reader.Read(buf)
	if secondErr == nil || secondErr != firstErr {
		t.Error("expected same error on second read")
	}
}
