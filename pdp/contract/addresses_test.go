package contract

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestKnownFWSSViewAddressIgnoresBuildDefaultsWhenAddressesAreConfigured(t *testing.T) {
	service := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	SetAddresses(Addresses{FWSService: service})
	t.Cleanup(func() { configured.Store(nil) })

	if view, ok := knownFWSSViewAddress(service); ok {
		t.Fatalf("got known view %s for a configured service; want none, so the view is read from the service contract", view)
	}
}
