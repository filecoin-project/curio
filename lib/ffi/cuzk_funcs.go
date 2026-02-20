package ffi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"
	proof2 "github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/curio/lib/cuzk"
	"github.com/filecoin-project/curio/lib/storiface"
)

// PoRepSnarkCuzk generates the vanilla proof locally (needs sector data on disk),
// then delegates the SNARK computation to the cuzk daemon via gRPC.
// The proof is verified locally after cuzk returns.
func (sb *SealCalls) PoRepSnarkCuzk(ctx context.Context, cuzkClient *cuzk.Client, sn storiface.SectorRef, sealed, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error) {
	// Step 1: Generate vanilla proof locally (requires sector cache on disk)
	vproof, err := sb.Sectors.storage.GeneratePoRepVanillaProof(ctx, sn, sealed, unsealed, ticket, seed)
	if err != nil {
		return nil, xerrors.Errorf("failed to generate vanilla proof: %w", err)
	}

	// Step 2: Submit vanilla proof to cuzk for SNARK computation
	ssize, err := sn.ProofType.SectorSize()
	if err != nil {
		return nil, xerrors.Errorf("getting sector size: %w", err)
	}

	resp, err := cuzkClient.Prove(ctx, &cuzk.SubmitProofRequest{
		RequestId:       fmt.Sprintf("porep-%d-%d", sn.ID.Miner, sn.ID.Number),
		ProofKind:       cuzk.ProofKind_POREP_SEAL_COMMIT,
		SectorSize:      uint64(ssize),
		RegisteredProof: uint64(sn.ProofType),
		Priority:        cuzk.Priority_NORMAL,
		VanillaProof:    vproof,
		SectorNumber:    uint64(sn.ID.Number),
		MinerId:         uint64(sn.ID.Miner),
	})
	if err != nil {
		return nil, xerrors.Errorf("cuzk porep prove failed: %w", err)
	}

	proof := resp.Proof

	// Step 3: Verify proof locally
	ok, err := ffi.VerifySeal(proof2.SealVerifyInfo{
		SealProof:             sn.ProofType,
		SectorID:              sn.ID,
		DealIDs:               nil,
		Randomness:            ticket,
		InteractiveRandomness: seed,
		Proof:                 proof,
		SealedCID:             sealed,
		UnsealedCID:           unsealed,
	})
	if err != nil {
		return nil, xerrors.Errorf("failed to verify cuzk proof: %w", err)
	}
	if !ok {
		return nil, xerrors.Errorf("cuzk porep proof failed to validate")
	}

	return proof, nil
}

// ProveUpdateCuzk reads the snap vanilla proof from storage, then delegates
// the SNARK computation to the cuzk daemon via gRPC.
func (sb *SealCalls) ProveUpdateCuzk(ctx context.Context, cuzkClient *cuzk.Client, proofType abi.RegisteredUpdateProof, sector storiface.SectorRef, key, sealed, unsealed cid.Cid) ([]byte, error) {
	// Step 1: Read snap vanilla proofs from storage
	jsonb, err := sb.Sectors.storage.ReadSnapVanillaProof(ctx, sector)
	if err != nil {
		return nil, xerrors.Errorf("read snap vanilla proof: %w", err)
	}

	var vproofs [][]byte
	if err := json.Unmarshal(jsonb, &vproofs); err != nil {
		return nil, xerrors.Errorf("unmarshal snap vanilla proof: %w", err)
	}

	// Step 2: Pack vanilla proofs for cuzk
	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return nil, xerrors.Errorf("getting sector size: %w", err)
	}

	resp, err := cuzkClient.Prove(ctx, &cuzk.SubmitProofRequest{
		RequestId:       fmt.Sprintf("snap-%d-%d", sector.ID.Miner, sector.ID.Number),
		ProofKind:       cuzk.ProofKind_SNAP_DEALS_UPDATE,
		SectorSize:      uint64(ssize),
		RegisteredProof: uint64(proofType),
		Priority:        cuzk.Priority_NORMAL,
		VanillaProofs:   vproofs,
		SectorNumber:    uint64(sector.ID.Number),
		MinerId:         uint64(sector.ID.Miner),
		CommROld:        key.Bytes(),
		CommRNew:        sealed.Bytes(),
		CommDNew:        unsealed.Bytes(),
	})
	if err != nil {
		return nil, xerrors.Errorf("cuzk snap prove failed: %w", err)
	}

	return resp.Proof, nil
}
