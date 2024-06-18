package asyncwrite

import (
	"fmt"
	pool "github.com/libp2p/go-buffer-pool"
	"io"
)

type BackgroundWriter struct {
	dest io.Writer
	ch   chan []byte
	done chan error
}

func New(dest io.Writer, bufferDepth int) *BackgroundWriter {
	bfw := &BackgroundWriter{
		dest: dest,
		ch:   make(chan []byte, bufferDepth),
		done: make(chan error, 1),
	}

	go bfw.writeWorker()

	return bfw
}

func (bfw *BackgroundWriter) writeWorker() {
	var err error
	for data := range bfw.ch {
		_, writeErr := bfw.dest.Write(data)
		pool.Put(data)
		if writeErr != nil {
			err = writeErr
			break
		}
	}

	if flusher, ok := bfw.dest.(interface{ Flush() error }); ok {
		if flushErr := flusher.Flush(); flushErr != nil && err == nil {
			err = flushErr
		}
	}

	bfw.done <- err
}

func (bfw *BackgroundWriter) Write(p []byte) (n int, err error) {
	b := pool.Get(len(p))
	copy(b, p)

	select {
	case bfw.ch <- b:
		return len(b), nil
	case err := <-bfw.done:
		return 0, err
	}
}

func (bfw *BackgroundWriter) Finish() error {
	if bfw.ch == nil {
		return nil
	}

	close(bfw.ch)
	bfw.ch = nil
	if err := <-bfw.done; err != nil {
		return fmt.Errorf("error during close: %w", err)
	}
	return nil
}

func (bfw *BackgroundWriter) Close() error {
	if err := bfw.Finish(); err != nil {
		return err
	}

	if closer, ok := bfw.dest.(io.Closer); ok {
		return closer.Close()
	}

	return nil
}
