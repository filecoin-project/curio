package FWSS

import (
	"bytes"
	"math/big"
	"testing"
)

func TestVersionStringAtLeast(t *testing.T) {
	tests := []struct {
		name    string
		version string
		minimum string
		want    bool
	}{
		{name: "below", version: "1.1.9", minimum: "1.2.0", want: false},
		{name: "equal", version: "1.2.0", minimum: "1.2.0", want: true},
		{name: "patch above", version: "1.2.1", minimum: "1.2.0", want: true},
		{name: "minor above", version: "1.3.0", minimum: "1.2.0", want: true},
		{name: "major above", version: "2.0.0", minimum: "1.2.0", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := versionStringAtLeast(tt.version, tt.minimum)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("versionAtLeast(%q, %q) = %t, want %t", tt.version, tt.minimum, got, tt.want)
			}
		})
	}
}

func TestVersionStringAtLeastInvalid(t *testing.T) {
	if _, err := versionStringAtLeast("1.2", "1.2.0"); err == nil {
		t.Fatal("expected invalid version error")
	}
	if _, err := versionStringAtLeast("1.2.0", "1.2"); err == nil {
		t.Fatal("expected invalid minimum version error")
	}
}

func TestVersionStringAfter(t *testing.T) {
	tests := []struct {
		name    string
		version string
		maximum string
		want    bool
	}{
		{name: "below", version: "1.1.9", maximum: "1.2.0", want: false},
		{name: "equal", version: "1.2.0", maximum: "1.2.0", want: false},
		{name: "patch above", version: "1.2.1", maximum: "1.2.0", want: true},
		{name: "minor above", version: "1.3.0", maximum: "1.2.0", want: true},
		{name: "major above", version: "2.0.0", maximum: "1.2.0", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := versionStringAfter(tt.version, tt.maximum)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("versionStringAfter(%q, %q) = %t, want %t", tt.version, tt.maximum, got, tt.want)
			}
		})
	}
}

func TestVersionStringAfterInvalid(t *testing.T) {
	if _, err := versionStringAfter("1.2", "1.2.0"); err == nil {
		t.Fatal("expected invalid version error")
	}
	if _, err := versionStringAfter("1.2.0", "1.2"); err == nil {
		t.Fatal("expected invalid maximum version error")
	}
}

func TestTerminateServiceOverloadPacking(t *testing.T) {
	fwssABI, err := FilecoinWarmStorageServiceMetaData.GetAbi()
	if err != nil {
		t.Fatal(err)
	}

	providerData, err := fwssABI.Pack("terminateService", big.NewInt(10))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := providerData[:4], []byte{0xb9, 0x97, 0xa7, 0x1e}; !bytes.Equal(got, want) {
		t.Fatalf("terminateService selector = 0x%x, want 0x%x", got, want)
	}

	clientData, err := fwssABI.Pack("terminateService0", big.NewInt(10), []byte{0x01, 0x02})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := clientData[:4], []byte{0xd0, 0xe3, 0xc9, 0x54}; !bytes.Equal(got, want) {
		t.Fatalf("terminateService0 selector = 0x%x, want 0x%x", got, want)
	}

	if _, err := fwssABI.Pack("terminateService", big.NewInt(10), []byte{0x01}); err == nil {
		t.Fatal("expected two-arg terminateService packing to fail without overload suffix")
	}
}
