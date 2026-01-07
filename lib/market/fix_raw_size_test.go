package market

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetRawSizeFromCarReader1_TestData(t *testing.T) {
	testdataDir := "testdata"

	testCases := []struct {
		fileName      string
		expectError   bool
		errorContains string
		checkSize     bool // if true, verify calculated size against file size
	}{
		{fileName: "sample-v1.car", checkSize: true},
		{fileName: "sample-v1-noidentity.car", checkSize: true},
		{fileName: "sample-unixfs-v2.car", checkSize: true},
		{fileName: "sample-rw-bs-v2.car", checkSize: true},
		{fileName: "sample-v2-indexless.car", checkSize: true},
		{fileName: "sample-wrapped-v2.car", checkSize: true},
		{fileName: "sample-v1-with-zero-len-section.car", checkSize: true},
		{fileName: "sample-v1-with-zero-len-section2.car", checkSize: true},

		{fileName: "sample-corrupt-pragma.car", expectError: true, errorContains: "failed to read car version"},
		{fileName: "sample-rootless-v42.car", expectError: true, errorContains: "unsupported car version: 42"},
		{fileName: "sample-v1-tailing-corrupt-section.car", expectError: true, errorContains: "failed to read next block"},
		{fileName: "sample-v2-corrupt-data-and-index.car", expectError: true, errorContains: "invalid data payload offset"},
	}

	for _, tc := range testCases {
		t.Run(tc.fileName, func(t *testing.T) {
			filePath := filepath.Join(testdataDir, tc.fileName)
			f, err := os.Open(filePath)
			require.NoError(t, err)
			defer func() {
				_ = f.Close()
			}()

			size, err := GetRawSizeFromCarReader(f)

			if tc.expectError {
				require.Error(t, err)
				if tc.errorContains != "" {
					require.Contains(t, err.Error(), tc.errorContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotZero(t, size)
			fmt.Println("Size is", size)

			if tc.checkSize {
				info, err := f.Stat()
				require.NoError(t, err)

				if tc.fileName == "sample-v1-with-zero-len-section.car" || tc.fileName == "sample-v1-with-zero-len-section2.car" {
					// These might stop early due to zero-len section acting as EOF
					require.Less(t, size, uint64(info.Size()))
				} else {
					require.Equal(t, uint64(info.Size()), size, "size mismatch expected %d got %d", info.Size(), size)
				}
			}
		})
	}
}

func TestGetRawSizeFromCarReader_NonExistent(t *testing.T) {
	_, err := GetRawSizeFromCarReader(nil)
	require.Error(t, err)
}
