package common

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	sproof "github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/curio/lib/proof"

	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("psvcommon")

var PriceResolution = types.NewInt(1_000_000_000) // 1nFIL
var MaxPrice = types.FromFil(1)

type WorkRequest struct {
	ID int64 `json:"id" db:"id"`

	RequestCid *string `json:"request_cid" db:"request_cid"` // CID of the ProofData
	Done       *bool   `json:"done" db:"done"`

	WorkAskID int64 `json:"work_ask_id" db:"work_ask_id"`
}

type ProofResponse struct {
	ID    string `json:"id"`
	Proof []byte `json:"proof"`
	Error string `json:"error"`
}

type ProofReward struct {
	Status string `json:"status"`

	Nonce            uint64          `json:"nonce"`
	Amount           abi.TokenAmount `json:"amount"`
	CumulativeAmount abi.TokenAmount `json:"cumulative_amount"`
	Signature        []byte          `json:"signature"`
}

type WorkResponse struct {
	Requests   []WorkRequest `json:"requests"`
	ActiveAsks []int64       `json:"active_asks"`
}

type WorkAsk struct {
	ID int64 `json:"id"`
}

type ProofData struct {
	SectorID *abi.SectorID

	// proof request enum
	PoRep *proof.Commit1OutRaw

	Snap *proof.Snap
}

type ProofRequest struct {
	Data cid.Cid `json:"data"`

	PriceEpoch int64 `json:"price_epoch"`

	PaymentClientID         int64           `json:"payment_client_id"`
	PaymentNonce            int64           `json:"payment_nonce"`
	PaymentCumulativeAmount abi.TokenAmount `json:"payment_cumulative_amount"`
	PaymentSignature        []byte          `json:"payment_signature"`
}

func (p *ProofData) Validate() error {
	if p.PoRep != nil {
		if p.SectorID == nil {
			return xerrors.Errorf("sector id is required for PoRep")
		}

		if err := p.validatePoRep(); err != nil {
			return xerrors.Errorf("failed to validate PoRep: %w", err)
		}

		return nil
	}

	if p.Snap != nil {
		if p.SectorID == nil {
			return xerrors.Errorf("sector id is required for Snap")
		}

		if err := p.validateSnap(); err != nil {
			return xerrors.Errorf("failed to validate Snap: %w", err)
		}

		return nil
	}
	return xerrors.Errorf("invalid proof request: no proof request")
}

func (p *ProofData) DeciCost() int {
	if p.PoRep != nil {
		return 10 // 10x 130M constraints
	}

	if p.Snap != nil {
		return 16 // 16x 130M constraints
	}

	return -1
}

func (p *ProofData) Type() string {
	if p.PoRep != nil {
		return "PoRep"
	}

	if p.Snap != nil {
		return "Snap"
	}

	return "!Unknown!"
}

func (p *ProofData) validatePoRep() error {
	// Make sure we actually have PoRep data
	if p.PoRep == nil {
		return xerrors.Errorf("no PoRep (Commit1OutRaw) data in request")
	}

	// 1) Bincode-encode the commit1 proof data
	var bincodeBuf bytes.Buffer
	if err := proof.EncodeCommit1OutRaw(&bincodeBuf, *p.PoRep); err != nil {
		return xerrors.Errorf("failed to bincode-encode PoRep: %w", err)
	}

	// 2) From PoRep.RegisteredProof, figure out sector size and PoRep ID
	//    (In a real system, you'd likely call something like sp.SectorSize(), sp.PoRepID(), etc.)
	//    For example:
	sp, err := p.PoRep.RegisteredProof.ToABI()
	if err != nil {
		return xerrors.Errorf("invalid RegisteredProof string: %w", err)
	}

	sectorSize, err := sp.SectorSize() // Available in Lotus/Epik for known RegisteredSealProof
	if err != nil {
		return xerrors.Errorf("failed to get sector size from seal proof: %w", err)
	}

	// In the Filecoin proofs code, the 32-byte PoRep ID is typically embedded in param files
	// or derived from the RegisteredSealProof policy.  For demonstration, we assume:
	porepID, err := sp.PoRepID()
	if err != nil {
		return xerrors.Errorf("failed to get PoRep ID: %w", err)
	}
	porepIDHex := fmt.Sprintf("%x", porepID)

	porepPartitions := 10

	// 3) Construct CLI arguments with only what's needed
	args := []string{
		"--proof-type=porep",
		"--sector-size=" + strconv.FormatUint(uint64(sectorSize), 10),
		"--porep-id-hex=" + porepIDHex,
		"--porep-partitions=" + strconv.Itoa(porepPartitions),
	}

	cmd := exec.Command("./proof-validate", args...)
	cmd.Stdin = &bincodeBuf // Pipe bincode-encoded data to STDIN

	// Capture combined output (stdout & stderr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return xerrors.Errorf("error running proof-validate: %w\nOutput:\n%s", err, output)
	}

	// 4) Parse the output to see if is_valid = true
	//    The Rust program prints something like:
	//      "Result: PoRep VANILLA (Commit1) is_valid = true"
	re := regexp.MustCompile(`is_valid\s*=\s*(true|false)`)
	match := re.FindStringSubmatch(string(output))
	if len(match) < 2 {
		return xerrors.Errorf("could not parse 'is_valid' from proof-validate output:\n%s", output)
	}

	log.Infow("proof-validate output", "output", string(output))

	if match[1] == "true" {
		return nil // success
	}

	return xerrors.Errorf("PoRep validation reported is_valid=false:\n%s", output)
}

func (p *ProofData) validateSnap() error {
	if p.Snap == nil {
		return xerrors.Errorf("no Snap data in request")
	}

	oldR, err := commcid.ReplicaCommitmentV1ToCID(p.Snap.OldR[:])
	if err != nil {
		return xerrors.Errorf("failed to convert OldR to CID: %w", err)
	}

	newR, err := commcid.ReplicaCommitmentV1ToCID(p.Snap.NewR[:])
	if err != nil {
		return xerrors.Errorf("failed to convert NewR to CID: %w", err)
	}

	newD, err := commcid.DataCommitmentV1ToCID(p.Snap.NewD[:])
	if err != nil {
		return xerrors.Errorf("failed to convert NewD to CID: %w", err)
	}

	ok, err := ffi.SectorUpdate.VerifyVanillaProofs(p.Snap.ProofType, oldR, newR, newD, p.Snap.Proofs)
	if err != nil {
		return xerrors.Errorf("failed to verify Snap: %w", err)
	}

	if !ok {
		return xerrors.Errorf("invalid Snap proof")
	}

	return nil
}

func (p *ProofData) CheckOutput(pb []byte) error {
	if p.PoRep != nil {
		spt, err := p.PoRep.RegisteredProof.ToABI()
		if err != nil {
			return xerrors.Errorf("failed to convert RegisteredProof to ABI: %w", err)
		}

		sealed, err := commcid.ReplicaCommitmentV1ToCID(p.PoRep.CommR[:])
		if err != nil {
			return xerrors.Errorf("failed to convert CommR to CID: %w", err)
		}

		unsealed, err := commcid.DataCommitmentV1ToCID(p.PoRep.CommD[:])
		if err != nil {
			return xerrors.Errorf("failed to convert CommD to CID: %w", err)
		}

		svi := sproof.SealVerifyInfo{
			SealProof:             spt,
			SectorID:              *p.SectorID,
			Proof:                 pb,
			Randomness:            p.PoRep.Ticket[:],
			InteractiveRandomness: p.PoRep.Seed[:],
			SealedCID:             sealed,
			UnsealedCID:           unsealed,
			DealIDs:               []abi.DealID{},
		}

		valid, err := ffi.VerifySeal(svi)
		if err != nil {
			return xerrors.Errorf("failed to verify seal: %w", err)
		}

		if !valid {
			return xerrors.Errorf("invalid proof")
		}
	}

	if p.Snap != nil {
		uinfo := sproof.ReplicaUpdateInfo{
			UpdateProofType:       p.Snap.ProofType,
			Proof:                 pb,
		}

		var err error
		uinfo.OldSealedSectorCID, err = commcid.ReplicaCommitmentV1ToCID(p.Snap.OldR[:])
		if err != nil {
			return xerrors.Errorf("failed to convert OldR to CID: %w", err)
		}

		uinfo.NewSealedSectorCID, err = commcid.ReplicaCommitmentV1ToCID(p.Snap.NewR[:])
		if err != nil {
			return xerrors.Errorf("failed to convert NewR to CID: %w", err)
		}

		uinfo.NewUnsealedSectorCID, err = commcid.DataCommitmentV1ToCID(p.Snap.NewD[:])
		if err != nil {
			return xerrors.Errorf("failed to convert NewD to CID: %w", err)
		}

		ok, err := ffi.SectorUpdate.VerifyUpdateProof(uinfo)
		if err != nil {
			return xerrors.Errorf("failed to verify UpdateProof: %w", err)
		}

		if !ok {
			return xerrors.Errorf("invalid UpdateProof")
		}
	}

	return nil
}
