package types

import (
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
)

// PdpIpniContext is used to generate the context bytes for PDP IPNI ads
type PdpIpniContext struct {
	// PieceCID is piece CID V2
	PieceCID cid.Cid

	// Payload determines if the IPNI ad is TransportFilecoinPieceHttp or TransportIpfsGatewayHttp
	Payload bool
}

// Marshal encodes the PdpIpniContext into a byte slice containing a single byte for Payload and the byte representation of PieceCID.
func (p *PdpIpniContext) Marshal() ([]byte, error) {
	pBytes := p.PieceCID.Bytes()
	if len(pBytes) > 63 {
		return nil, xerrors.Errorf("piece CID byte length exceeds 63")
	}
	payloadByte := make([]byte, 1)
	if p.Payload {
		payloadByte[0] = 1
	} else {
		payloadByte[0] = 0
	}
	return append(payloadByte, pBytes...), nil
}

// Unmarshal decodes the provided byte slice into the PdpIpniContext struct, validating its length and extracting the PieceCID and Payload values.
func (p *PdpIpniContext) Unmarshal(b []byte) error {
	if len(b) > 64 {
		return xerrors.Errorf("byte length exceeds 64")
	}
	if len(b) < 2 {
		return xerrors.Errorf("byte length is less than 2")
	}
	payload := b[0] == 1
	pcid, err := cid.Cast(b[1:])
	if err != nil {
		return err
	}

	p.PieceCID = pcid
	p.Payload = payload

	return nil
}
