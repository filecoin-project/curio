package common

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"

	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/types"

	"github.com/filecoin-project/curio/lib/proof"
)

//go:generate cbor-gen-for --map-encoding ProofRequest

var log = logging.Logger("psvcommon")

var PriceResolution = types.NewInt(1_000_000_000) // 1nFIL
var MaxPriceNfil = types.NewInt(1_000_000_000) // 1 FIL

type WorkRequest struct {
	ID int64 `json:"id" db:"id"`

	Data *string `json:"data" db:"request_data"`
	Done *bool   `json:"done" db:"done"`

	WorkAskID int64 `json:"work_ask_id" db:"work_ask_id"`
}

type ProofResponse struct {
	ID    string `json:"id"`
	Proof []byte `json:"proof"`
	Error string `json:"error"`
}

type WorkResponse struct {
	Requests   []WorkRequest `json:"requests"`
	ActiveAsks []int64       `json:"active_asks"`
}

type WorkAsk struct {
	ID int64 `json:"id"`
}

type ProofRequest struct {
	SectorID *abi.SectorID

	// proof request enum
	PoRep *proof.Commit1OutRaw

	MaxPriceNfil abi.TokenAmount
}

func (p *ProofRequest) Validate() error {
	if err := p.validatePrice(); err != nil {
		return xerrors.Errorf("failed to validate price: %w", err)
	}

	if p.PoRep != nil {
		if p.SectorID == nil {
			return xerrors.Errorf("sector id is required for PoRep")
		}

		if err := p.validatePoRep(); err != nil {
			return xerrors.Errorf("failed to validate PoRep: %w", err)
		}

		return nil
	}
	return xerrors.Errorf("invalid proof request: no proof request")
}

func (p *ProofRequest) validatePrice() error {
	if p.MaxPriceNfil.IsZero() {
		return xerrors.Errorf("max price is required")
	}

	if p.MaxPriceNfil.Sign() <= 0 {
		return xerrors.Errorf("max price must be positive")
	}

	if big.Mod(p.MaxPriceNfil, PriceResolution).Sign() != 0 {
		return xerrors.Errorf("max price must be a multiple of 1nFIL")
	}

	if big.Cmp(p.MaxPriceNfil, MaxPriceNfil) > 0 {
		return xerrors.Errorf("max price must be less than 1 FIL")
	}

	return nil
}

func (p *ProofRequest) validatePoRep() error {
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
