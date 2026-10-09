package webrpc

import (
	"testing"

	"github.com/filecoin-project/curio/pdp/contract"
)

func TestCapabilitiesToOfferingCapacity(t *testing.T) {
	cases := []struct {
		name   string
		keys   []string
		values [][]byte
		want   int64
	}{
		{"gib", []string{contract.CapCapacityGiB}, [][]byte{{0x64}}, 100},
		{"legacy tib", []string{contract.CapCapacityTiB}, [][]byte{{0x02}}, 2048},
		{"gib wins, tib first", []string{contract.CapCapacityTiB, contract.CapCapacityGiB}, [][]byte{{0x02}, {0x64}}, 100},
		{"gib wins, gib first", []string{contract.CapCapacityGiB, contract.CapCapacityTiB}, [][]byte{{0x64}, {0x02}}, 100},
	}
	for _, tc := range cases {
		got, custom := capabilitiesToOffering(tc.keys, tc.values)
		if got.CapacityGiB != tc.want {
			t.Errorf("%s: CapacityGiB = %d, want %d", tc.name, got.CapacityGiB, tc.want)
		}
		if len(custom) != 0 {
			t.Errorf("%s: capacity keys leaked into custom capabilities: %v", tc.name, custom)
		}
	}
}
