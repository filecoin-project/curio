package FWSS

import "testing"

func TestFwssSupportsClientTerminationVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{
			name:    "older version is unsupported",
			version: "1.2.0",
			want:    false,
		},
		{
			name:    "threshold version is unsupported",
			version: "1.2.1",
			want:    false,
		},
		{
			name:    "newer bare version is supported",
			version: "1.2.2",
			want:    true,
		},
		{
			name:    "newer prefixed version is supported",
			version: "v1.2.2",
			want:    true,
		},
		{
			name:    "empty version is unsupported",
			version: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fwssSupportsClientTerminationVersion(tt.version)
			if got != tt.want {
				t.Fatalf("fwssSupportsClientTerminationVersion(%q) = %t, want %t", tt.version, got, tt.want)
			}
		})
	}
}
