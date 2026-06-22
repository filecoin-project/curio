//go:build !maxboom

package paths

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ipfs/go-cid"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/proof"

	cuproof "github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/supraffi"

	"github.com/filecoin-project/lotus/lib/result"
)

func (st *Local) GenerateSingleVanillaProof(ctx context.Context, minerID abi.ActorID, si storiface.PostSectorChallenge, ppt abi.RegisteredPoStProof) ([]byte, error) {
	sr := storiface.SectorRef{
		ID: abi.SectorID{
			Miner:  minerID,
			Number: si.SectorNumber,
		},
		ProofType: si.SealProof,
	}

	var cache, sealed, cacheID, sealedID string

	if si.Update {
		src, si, err := st.AcquireSector(ctx, sr, storiface.FTUpdate|storiface.FTUpdateCache, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
		if err != nil {
			// Record the error with tags
			ctx, _ = tag.New(ctx,
				tag.Upsert(updateTagKey, fmt.Sprintf("%t", si.Update != "")),
				tag.Upsert(cacheIDTagKey, ""),
				tag.Upsert(sealedIDTagKey, ""),
			)
			stats.Record(ctx, GenerateSingleVanillaProofErrors.M(1))
			return nil, xerrors.Errorf("acquire sector: %w", err)
		}
		cache, sealed = src.UpdateCache, src.Update
		cacheID, sealedID = si.UpdateCache, si.Update
	} else {
		src, si, err := st.AcquireSector(ctx, sr, storiface.FTSealed|storiface.FTCache, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
		if err != nil {
			// Record the error with tags
			ctx, _ = tag.New(ctx,
				tag.Upsert(updateTagKey, fmt.Sprintf("%t", si.Update != "")),
				tag.Upsert(cacheIDTagKey, ""),
				tag.Upsert(sealedIDTagKey, ""),
			)
			stats.Record(ctx, GenerateSingleVanillaProofErrors.M(1))
			return nil, xerrors.Errorf("acquire sector: %w", err)
		}
		cache, sealed = src.Cache, src.Sealed
		cacheID, sealedID = si.Cache, si.Sealed
	}

	if sealed == "" || cache == "" {
		// Record the error with tags
		ctx, _ = tag.New(ctx,
			tag.Upsert(updateTagKey, fmt.Sprintf("%t", si.Update)),
			tag.Upsert(cacheIDTagKey, cacheID),
			tag.Upsert(sealedIDTagKey, sealedID),
		)
		stats.Record(ctx, GenerateSingleVanillaProofErrors.M(1))
		return nil, errPathNotFound
	}

	// Add metrics context with tags
	ctx, err := tag.New(ctx,
		tag.Upsert(updateTagKey, fmt.Sprintf("%t", si.Update)),
		tag.Upsert(cacheIDTagKey, cacheID),
		tag.Upsert(sealedIDTagKey, sealedID),
	)
	if err != nil {
		log.Errorw("failed to create tagged context", "err", err)
	}

	// Record that the function was called
	stats.Record(ctx, GenerateSingleVanillaProofCalls.M(1))

	psi := ffi.PrivateSectorInfo{
		SectorInfo: proof.SectorInfo{
			SealProof:    si.SealProof,
			SectorNumber: si.SectorNumber,
			SealedCID:    si.SealedCID,
		},
		CacheDirPath:     cache,
		PoStProofType:    ppt,
		SealedSectorPath: sealed,
	}

	start := time.Now()

	resCh := make(chan result.Result[[]byte], 1)
	go func() {
		resCh <- result.Wrap(ffi.GenerateSingleVanillaProof(psi, si.Challenge))
	}()

	select {
	case r := <-resCh:
		// Record the duration upon successful completion
		duration := time.Since(start).Milliseconds()
		stats.Record(ctx, GenerateSingleVanillaProofDuration.M(duration))

		if duration > SlowPoStCheckThreshold.Milliseconds() {
			log.Warnw("slow GenerateSingleVanillaProof", "duration", duration, "cache-id", cacheID, "sealed-id", sealedID, "cache", cache, "sealed", sealed, "sector", si)
		}

		return r.Unwrap()
	case <-ctx.Done():
		// Record the duration and error if the context is canceled
		duration := time.Since(start).Milliseconds()
		stats.Record(ctx, GenerateSingleVanillaProofDuration.M(duration))
		stats.Record(ctx, GenerateSingleVanillaProofErrors.M(1))
		log.Errorw("failed to generate vanilla PoSt proof before context cancellation", "err", ctx.Err(), "duration", duration, "cache-id", cacheID, "sealed-id", sealedID, "cache", cache, "sealed", sealed)

		// This will leave the GenerateSingleVanillaProof goroutine hanging, but that's still less bad than failing PoSt
		return nil, xerrors.Errorf("failed to generate vanilla proof before context cancellation: %w", ctx.Err())
	}
}

func (st *Local) GeneratePoRepVanillaProof(ctx context.Context, sr storiface.SectorRef, sealed, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error) {
	src, _, err := st.AcquireSector(ctx, sr, storiface.FTSealed|storiface.FTCache, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
	if err != nil {
		return nil, xerrors.Errorf("acquire sector: %w", err)
	}

	if src.Sealed == "" || src.Cache == "" {
		return nil, errPathNotFound
	}

	ssize, err := sr.ProofType.SectorSize()
	if err != nil {
		return nil, xerrors.Errorf("getting sector size: %w", err)
	}

	{
		// check if the sector is part of a supraseal batch with data in raw block storage
		// does BatchMetaFile exist in cache?
		batchMetaPath := filepath.Join(src.Cache, BatchMetaFile)
		if _, err := os.Stat(batchMetaPath); err == nil {
			return st.supraPoRepVanillaProof(src, sr, sealed, unsealed, ticket, seed)
		}
	}

	secPiece := []abi.PieceInfo{{
		Size:     abi.PaddedPieceSize(ssize),
		PieceCID: unsealed,
	}}

	return ffi.SealCommitPhase1(sr.ProofType, sealed, unsealed, src.Cache, src.Sealed, sr.ID.Number, sr.ID.Miner, ticket, seed, secPiece)
}

func (st *Local) ReadSnapVanillaProof(ctx context.Context, sr storiface.SectorRef) ([]byte, error) {
	src, _, err := st.AcquireSector(ctx, sr, storiface.FTUpdateCache, storiface.FTNone, storiface.PathStorage, storiface.AcquireMove)
	if err != nil {
		return nil, xerrors.Errorf("acquire sector: %w", err)
	}

	if src.UpdateCache == "" {
		return nil, errPathNotFound
	}

	out, err := os.ReadFile(filepath.Join(src.UpdateCache, SnapVproofFile))
	if err != nil {
		return nil, xerrors.Errorf("read snap vanilla proof: %w", err)
	}

	return out, nil
}

var supraC1Token = make(chan struct{}, 1)

func (st *Local) supraPoRepVanillaProof(src storiface.SectorPaths, sr storiface.SectorRef, _, unsealed cid.Cid, ticket abi.SealRandomness, seed abi.InteractiveSealRandomness) ([]byte, error) {
	batchMetaPath := filepath.Join(src.Cache, BatchMetaFile)
	bmdata, err := os.ReadFile(batchMetaPath)
	if err != nil {
		return nil, xerrors.Errorf("read batch meta file: %w", err)
	}

	var bm BatchMeta
	if err := json.Unmarshal(bmdata, &bm); err != nil {
		return nil, xerrors.Errorf("unmarshal batch meta file: %w", err)
	}

	commd, err := commcid.CIDToDataCommitmentV1(unsealed)
	if err != nil {
		return nil, xerrors.Errorf("unsealed cid to data commitment: %w", err)
	}

	replicaID, err := sr.ProofType.ReplicaId(sr.ID.Miner, sr.ID.Number, ticket, commd)
	if err != nil {
		return nil, xerrors.Errorf("replica id: %w", err)
	}

	ssize, err := sr.ProofType.SectorSize()
	if err != nil {
		return nil, xerrors.Errorf("sector size: %w", err)
	}

	// C1 writes the output to a file, so we need to read it back.
	// Outputs to cachePath/commit-phase1-output
	// NOTE: that file is raw, and rust C1 returns a json repr, so we need to translate first

	// first see if commit-phase1-output is there
	commitPhase1OutputPath := filepath.Join(src.Cache, CommitPhase1OutputFileSupra)

	var retry bool

	for {
		if retry {
			if err := os.Remove(commitPhase1OutputPath); err != nil {
				return nil, xerrors.Errorf("remove bad commit phase 1 output file: %w", err)
			}
		}
		retry = true

		if _, err := os.Stat(commitPhase1OutputPath); err != nil {
			if !os.IsNotExist(err) {
				return nil, xerrors.Errorf("stat commit phase1 output: %w", err)
			}

			parentsPath, err := ParentsForProof(sr.ProofType)
			if err != nil {
				return nil, xerrors.Errorf("parents for proof: %w", err)
			}

			// not found, compute it
			supraC1Token <- struct{}{}
			res := supraffi.C1(bm.BlockOffset, bm.BatchSectors, bm.NumInPipeline, replicaID[:], seed, ticket, src.Cache, parentsPath, src.Sealed, uint64(ssize))
			<-supraC1Token

			if res != 0 {
				return nil, xerrors.Errorf("c1 failed: %d", res)
			}

			// check again
			if _, err := os.Stat(commitPhase1OutputPath); err != nil {
				return nil, xerrors.Errorf("stat commit phase1 output after compute: %w", err)
			}
		}

		// read the output
		rawOut, err := os.ReadFile(commitPhase1OutputPath)
		if err != nil {
			return nil, xerrors.Errorf("read commit phase1 output: %w", err)
		}

		// decode
		dec, err := cuproof.DecodeCommit1OutRaw(bytes.NewReader(rawOut))
		if err != nil {
			log.Errorw("failed to decode commit phase1 output, will retry", "err", err)
			time.Sleep(1 * time.Second)
			continue
		}

		log.Infow("supraPoRepVanillaProof", "sref", sr, "replicaID", replicaID, "seed", seed, "ticket", ticket, "decrepl", dec.ReplicaID, "decr", dec.CommR, "decd", dec.CommD)

		// out is json, so we need to marshal it back
		out, err := json.Marshal(dec)
		if err != nil {
			log.Errorw("failed to decode commit phase1 output", "err", err)
			time.Sleep(1 * time.Second)
		}

		return out, nil
	}
}
