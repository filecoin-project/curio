package types

import (
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

type PdpIpniFlags uint8

const (
	// Bit 0 preserves the legacy 0/1 payload encoding.
	PDP_IPNI_FLAG_PAYLOAD   PdpIpniFlags = 1 << 0
	PDP_IPNI_FLAG_ENCRYPTED PdpIpniFlags = 1 << 1
)

// PdpIpniContext is used to generate the context bytes for PDP IPNI ads
type PdpIpniContext struct {
	// PieceCID is piece CID V2
	PieceCID cid.Cid

	Flags PdpIpniFlags
}

// Payload reports whether the ad indexes payload blocks rather than the piece itself.
func (p *PdpIpniContext) Payload() bool {
	return p.Flags&PDP_IPNI_FLAG_PAYLOAD != 0
}

func (p *PdpIpniContext) Encrypted() bool {
	return p.Flags&PDP_IPNI_FLAG_ENCRYPTED != 0
}

// Marshal encodes the PdpIpniContext as one flags byte followed by the PieceCID bytes.
func (p *PdpIpniContext) Marshal() ([]byte, error) {
	pBytes := p.PieceCID.Bytes()
	if len(pBytes) > 63 {
		return nil, xerrors.Errorf("piece CID byte length exceeds 63")
	}
	return append([]byte{byte(p.Flags)}, pBytes...), nil
}

// Unmarshal decodes the PieceCID and preserves all flags, including unknown bits.
func (p *PdpIpniContext) Unmarshal(b []byte) error {
	if len(b) > 64 {
		return xerrors.Errorf("byte length exceeds 64")
	}
	if len(b) < 2 {
		return xerrors.Errorf("byte length is less than 2")
	}
	pcid, err := cid.Cast(b[1:])
	if err != nil {
		return err
	}

	p.PieceCID = pcid
	p.Flags = PdpIpniFlags(b[0])

	return nil
}
