package cunative

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	UnsealedB64 = `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
AAAA`

	SealedB64 = `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`

	KeyB64 = `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`
)

func TestDecode(t *testing.T) {
	unsealed, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(UnsealedB64, "\n", ""))
	require.NoError(t, err)
	sealed, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(SealedB64, "\n", ""))
	require.NoError(t, err)
	key, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(KeyB64, "\n", ""))
	require.NoError(t, err)

	// unsealed has a trailer, so trim to 2k
	unsealed = unsealed[:2<<10]

	require.Len(t, unsealed, 2<<10)
	require.Len(t, sealed, 2<<10)
	require.Len(t, key, 2<<10)

	// extend to 8M to test parallel decoding
	repeats := (8 << 20) / len(unsealed)
	unsealed = bytes.Repeat(unsealed, repeats)
	sealed = bytes.Repeat(sealed, repeats)
	key = bytes.Repeat(key, repeats)

	// Create readers for sealed and key data
	sealedReader := bytes.NewReader(sealed)
	keyReader := bytes.NewReader(key)

	// Create a buffer to store the decoded output
	var decodedBuf bytes.Buffer

	// Call the Decode function
	err = Decode(sealedReader, keyReader, &decodedBuf)
	require.NoError(t, err)

	// Compare the decoded output with the expected unsealed data
	decodedData, err := io.ReadAll(&decodedBuf)
	require.NoError(t, err)

	// Debug: Print the first few bytes of each slice
	t.Logf("First 32 bytes of unsealed: %s", hex.EncodeToString(unsealed[:32]))
	t.Logf("First 32 bytes of sealed: %s", hex.EncodeToString(sealed[:32]))
	t.Logf("First 32 bytes of key: %s", hex.EncodeToString(key[:32]))
	t.Logf("First 32 bytes of decoded: %s", hex.EncodeToString(decodedData[:32]))

	// Find the first differing byte
	for i := 0; i < len(unsealed) && i < len(decodedData); i++ {
		if unsealed[i] != decodedData[i] {
			t.Logf("First difference at index %d: expected %02x, got %02x", i, unsealed[i], decodedData[i])
			break
		}
	}

	// Compare the full slices
	if !bytes.Equal(unsealed, decodedData) {
		t.Errorf("Decoded data does not match expected unsealed data")
		// Print more detailed diff information
		for i := 0; i < len(unsealed) && i < len(decodedData); i += 32 {
			end := i + 32
			if end > len(unsealed) {
				end = len(unsealed)
			}
			t.Logf("unsealed[%d:%d]: %s", i, end, hex.EncodeToString(unsealed[i:end]))
			t.Logf("decoded[%d:%d]: %s", i, end, hex.EncodeToString(decodedData[i:end]))
			t.Logf("")
		}
	} else {
		t.Logf("Decoded data matches expected unsealed data")
	}

	require.Equal(t, unsealed, decodedData, "Decoded data does not match expected unsealed data")
}

/*
2024-09-02:

goos: linux
goarch: amd64
pkg: github.com/filecoin-project/curio/lib/ffi/cunative
cpu: AMD Ryzen Threadripper PRO 7995WX 96-Cores
BenchmarkDecode4G
BenchmarkDecode4G-192     	       1	1201906605 ns/op	3573.46 MB/s

goos: linux
goarch: amd64
pkg: github.com/filecoin-project/curio/lib/ffi/cunative
cpu: AMD Ryzen 7 7840HS w/ Radeon 780M Graphics
BenchmarkDecode4G-16                   1        23175794389 ns/op        185.32 MB/s

goos: linux
goarch: amd64
pkg: github.com/filecoin-project/curio/lib/ffi/cunative
cpu: Intel(R) Xeon(R) Silver 4310T CPU @ 2.30GHz
BenchmarkDecode4G-40    	       1	6854494251 ns/op	 626.59 MB/s
*/
func BenchmarkDecode4G(b *testing.B) {
	// Size of the data to decode (4 GiB)
	const dataSize = 4 << 30

	// Decode the base64 strings for sealed and key data
	sealed, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(SealedB64, "\n", ""))
	if err != nil {
		b.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(KeyB64, "\n", ""))
	if err != nil {
		b.Fatal(err)
	}

	// Create 1 GiB buffers by repeating the sealed and key data
	sealedRepeated := bytes.Repeat(sealed, dataSize/len(sealed)+1)[:dataSize]
	keyRepeated := bytes.Repeat(key, dataSize/len(key)+1)[:dataSize]

	// Create readers for the data
	sealedReader := bytes.NewReader(sealedRepeated)
	keyReader := bytes.NewReader(keyRepeated)

	// Create a buffer for the output
	var outputBuffer bytes.Buffer

	// Reset the timer before the benchmark loop
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Reset the readers and output buffer for each iteration
		_, _ = sealedReader.Seek(0, io.SeekStart)
		_, _ = keyReader.Seek(0, io.SeekStart)
		outputBuffer.Reset()

		// Call the Decode function
		err := Decode(sealedReader, keyReader, &outputBuffer)
		if err != nil {
			b.Fatal(err)
		}

		// Ensure the output size is correct
		if outputBuffer.Len() != dataSize {
			b.Fatalf("Expected output size %d, got %d", dataSize, outputBuffer.Len())
		}
	}

	// Add custom metrics
	b.SetBytes(dataSize) // This will report bytes/sec in the benchmark output
}
