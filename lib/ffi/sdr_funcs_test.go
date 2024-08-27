package ffi

import (
	"path/filepath"
	"testing"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

func TestChangePathType(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		newType     storiface.SectorFileType
		expected    string
		expectError bool
	}{
		{
			name:        "Valid relative path change",
			path:        filepath.Join("some", "parent", "sealed", "filename"),
			newType:     storiface.FTCache,
			expected:    filepath.Join("some", "parent", "cache", "filename"),
			expectError: false,
		},
		{
			name:        "Valid absolute path change",
			path:        filepath.Join("/", "some", "parent", "sealed", "filename"),
			newType:     storiface.FTCache,
			expected:    filepath.Join("/", "some", "parent", "cache", "filename"),
			expectError: false,
		},
		{
			name:        "Same type, no change (relative)",
			path:        filepath.Join("some", "parent", "sealed", "filename"),
			newType:     storiface.FTSealed,
			expected:    filepath.Join("some", "parent", "sealed", "filename"),
			expectError: false,
		},
		{
			name:        "Same type, no change (absolute)",
			path:        filepath.Join("/", "some", "parent", "sealed", "filename"),
			newType:     storiface.FTSealed,
			expected:    filepath.Join("/", "some", "parent", "sealed", "filename"),
			expectError: false,
		},
		{
			name:        "Too few components",
			path:        "filename",
			newType:     storiface.FTCache,
			expected:    "",
			expectError: true,
		},
		{
			name:        "Empty path",
			path:        "",
			newType:     storiface.FTCache,
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := changePathType(tt.path, tt.newType)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected an error, but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("Expected %s, but got %s", tt.expected, result)
				}

				// Check if the absolute/relative nature of the path is preserved
				if filepath.IsAbs(tt.path) != filepath.IsAbs(result) {
					t.Errorf("Absolute/relative nature of path not preserved. Original: %s, Result: %s", tt.path, result)
				}
			}
		})
	}
}
