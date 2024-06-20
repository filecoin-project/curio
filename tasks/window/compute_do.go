package window

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/samber/lo"
	"go.uber.org/multierr"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	miner2 "github.com/filecoin-project/go-state-types/builtin/v9/miner"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/go-state-types/proof"
	proof7 "github.com/filecoin-project/specs-actors/v7/actors/runtime/proof"

	"github.com/filecoin-project/curio/lib/ffiselect"

	lbuild "github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sealer"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

const disablePreChecks = false // todo config

func (t *WdPostTask) DoPartition(ctx context.Context, ts *types.TipSet, maddr address.Address, di *dline.Info, partIdx uint64, test bool) (out *miner2.SubmitWindowedPoStParams, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("recover: %s", r)
			err = xerrors.Errorf("panic in doPartition: %s", r)
		}
	}()

	buf := new(bytes.Buffer)
	if err := maddr.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal address to cbor: %w", err)
	}

	headTs, err := t.api.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting current head: %w", err)
	}

	rand, err := t.api.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes(), headTs.Key())
	if err != nil {
		return nil, xerrors.Errorf("failed to get chain randomness from beacon for window post (ts=%d; deadline=%d): %w", ts.Height(), di, err)
	}

	parts, err := t.api.StateMinerPartitions(ctx, maddr, di.Index, ts.Key())
	if err != nil {
		return nil, xerrors.Errorf("getting partitions: %w", err)
	}

	if partIdx >= uint64(len(parts)) {
		return nil, xerrors.Errorf("invalid partIdx %d (deadline has %d partitions)", partIdx, len(parts))
	}

	partition := parts[partIdx]

	params := miner2.SubmitWindowedPoStParams{
		Deadline:   di.Index,
		Partitions: make([]miner2.PoStPartition, 0, 1),
		Proofs:     nil,
	}

	var postPartition miner2.PoStPartition
	var xsinfos []proof7.ExtendedSectorInfo

	{
		toProve, err := bitfield.SubtractBitField(partition.LiveSectors, partition.FaultySectors)
		if err != nil {
			return nil, xerrors.Errorf("removing faults from set of sectors to prove: %w", err)
		}
		if test {
			// this is a check run, we want to prove faulty sectors, even
			// if they are not declared as recovering.
			toProve = partition.LiveSectors
		}
		toProve, err = bitfield.MergeBitFields(toProve, partition.RecoveringSectors)
		if err != nil {
			return nil, xerrors.Errorf("adding recoveries to set of sectors to prove: %w", err)
		}

		good, err := toProve.Copy()
		if err != nil {
			return nil, xerrors.Errorf("copy toProve: %w", err)
		}
		if !disablePreChecks {
			good, err = checkSectors(ctx, t.api, t.faultTracker, maddr, toProve, ts.Key())
			if err != nil {
				return nil, xerrors.Errorf("checking sectors to skip: %w", err)
			}
		}

		/*good, err = bitfield.SubtractBitField(good, postSkipped)
		if err != nil {
			return nil, xerrors.Errorf("toProve - postSkipped: %w", err)
		}

		post skipped is legacy retry mechanism, shouldn't be needed anymore
		*/

		skipped, err := bitfield.SubtractBitField(toProve, good)
		if err != nil {
			return nil, xerrors.Errorf("toProve - good: %w", err)
		}

		sc, err := skipped.Count()
		if err != nil {
			return nil, xerrors.Errorf("getting skipped sector count: %w", err)
		}

		skipCount := sc

		ssi, err := t.sectorsForProof(ctx, maddr, good, partition.AllSectors, ts)
		if err != nil {
			return nil, xerrors.Errorf("getting sorted sector info: %w", err)
		}

		if len(ssi) == 0 {
			return nil, xerrors.Errorf("no sectors to prove")
		}

		xsinfos = append(xsinfos, ssi...)
		postPartition = miner2.PoStPartition{
			Index:   partIdx,
			Skipped: skipped,
		}

		log.Infow("running window post",
			"chain-random", rand,
			"deadline", di,
			"height", ts.Height(),
			"skipped", skipCount)

		tsStart := lbuild.Clock.Now()

		mid, err := address.IDFromAddress(maddr)
		if err != nil {
			return nil, err
		}

		nv, err := t.api.StateNetworkVersion(ctx, ts.Key())
		if err != nil {
			return nil, xerrors.Errorf("getting network version: %w", err)
		}

		ppt, err := xsinfos[0].SealProof.RegisteredWindowPoStProofByNetworkVersion(nv)
		if err != nil {
			return nil, xerrors.Errorf("failed to get window post type: %w", err)
		}

		postOut, computeSkipped, err := t.generateWindowPoSt(ctx, ppt, abi.ActorID(mid), xsinfos, append(abi.PoStRandomness{}, rand...))
		elapsed := time.Since(tsStart)
		log.Infow("computing window post", "partition", partIdx, "elapsed", elapsed, "skip", len(computeSkipped), "err", err)
		if err != nil {
			log.Errorf("error generating window post: %s", err)
		}

		if err == nil {
			// If we proved nothing, something is very wrong.
			if len(postOut) == 0 {
				log.Errorf("len(postOut) == 0")
				return nil, xerrors.Errorf("received no proofs back from generate window post")
			}

			headTs, err := t.api.ChainHead(ctx)
			if err != nil {
				return nil, xerrors.Errorf("getting current head: %w", err)
			}

			checkRand, err := t.api.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes(), headTs.Key())
			if err != nil {
				return nil, xerrors.Errorf("failed to get chain randomness from beacon for window post (ts=%d; deadline=%d): %w", ts.Height(), di, err)
			}

			if !bytes.Equal(checkRand, rand) {
				// this is a check from legacy code, there it would retry with new randomness.
				// here we don't retry because the current network version uses beacon randomness
				// which should never change. We do keep this check tho to detect potential issues.
				return nil, xerrors.Errorf("post generation randomness was different from random beacon")
			}

			// computeSkipped is a list of sector numbers that were skipped during PoSt computation
			for _, skippedNum := range computeSkipped {
				// set postPartition.Skipped bitfield entries to also contain sectors skipped during PoSt computation
				// (initially it contains a list of sectors skipped during pre-checks)
				postPartition.Skipped.Set(uint64(skippedNum.Number))
			}

			// Compute SectorsInfo list for proof verification, matching the logic in the miner actor
			sinfos := make([]proof7.SectorInfo, len(xsinfos))

			// skipped sector infos need to be filled with the first non-skipped sector info so that the offsets
			// of the non-skipped sectors are correct. This no longer matters for PoSt verification in proofs, but
			// in any case we want this logic to match the miner actor's PoSt verification logic as closely as possible.
			var firstStandIn proof7.SectorInfo // https://github.com/filecoin-project/builtin-actors/blob/ea7c45478751bd0fe12d0d374abc8fdc9341bfea/actors/miner/src/sectors.rs#L112

			for i, xsi := range xsinfos {
				if lo.Contains(computeSkipped, abi.SectorID{Miner: abi.ActorID(mid), Number: xsi.SectorNumber}) {
					// a stand-in will be added in the next loop. We don't do that here because in the first few iterations
					// we may not know which sector will be the first non-skipped one.
					continue
				}

				si := proof7.SectorInfo{
					SealProof:    xsi.SealProof,
					SectorNumber: xsi.SectorNumber,
					SealedCID:    xsi.SealedCID,
				}
				sinfos[i] = si
				if firstStandIn.SealedCID == cid.Undef {
					firstStandIn = si
				}
			}

			// fill in skipped sector infos with the first non-skipped sector info
			for i := range sinfos {
				if sinfos[i].SealedCID == cid.Undef {
					sinfos[i] = firstStandIn
				}
			}

			// Verify the PoSt proof!
			if correct, err := t.verifier.VerifyWindowPoSt(ctx, proof.WindowPoStVerifyInfo{
				Randomness:        abi.PoStRandomness(checkRand),
				Proofs:            postOut,
				ChallengedSectors: sinfos,
				Prover:            abi.ActorID(mid),
			}); err != nil { // revive:disable-line:empty-block
				return nil, xerrors.Errorf("failed to verify window post: %w", err)
			} else if !correct {
				return nil, xerrors.Errorf("window post verification failed: proof was invalid")
			}

			// Proof generation successful, stop retrying
			//somethingToProve = true
			params.Partitions = []miner2.PoStPartition{postPartition}
			params.Proofs = postOut
			//break

			return &params, nil
		}
	}

	return nil, xerrors.Errorf("failed to generate window post")
}

type CheckSectorsAPI interface {
	StateMinerSectors(ctx context.Context, addr address.Address, bf *bitfield.BitField, tsk types.TipSetKey) ([]*miner.SectorOnChainInfo, error)
}

func checkSectors(ctx context.Context, api CheckSectorsAPI, ft sealer.FaultTracker,
	maddr address.Address, check bitfield.BitField, tsk types.TipSetKey) (bitfield.BitField, error) {
	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return bitfield.BitField{}, xerrors.Errorf("failed to convert to ID addr: %w", err)
	}

	sectorInfos, err := api.StateMinerSectors(ctx, maddr, &check, tsk)
	if err != nil {
		return bitfield.BitField{}, xerrors.Errorf("failed to get sector infos: %w", err)
	}

	type checkSector struct {
		sealed cid.Cid
		update bool
	}

	sectors := make(map[abi.SectorNumber]checkSector)
	var tocheck []storiface.SectorRef
	for _, info := range sectorInfos {
		sectors[info.SectorNumber] = checkSector{
			sealed: info.SealedCID,
			update: info.SectorKeyCID != nil,
		}
		tocheck = append(tocheck, storiface.SectorRef{
			ProofType: info.SealProof,
			ID: abi.SectorID{
				Miner:  abi.ActorID(mid),
				Number: info.SectorNumber,
			},
		})
	}

	if len(tocheck) == 0 {
		return bitfield.BitField{}, nil
	}

	pp, err := tocheck[0].ProofType.RegisteredWindowPoStProof()
	if err != nil {
		return bitfield.BitField{}, xerrors.Errorf("failed to get window PoSt proof: %w", err)
	}
	pp, err = pp.ToV1_1PostProof()
	if err != nil {
		return bitfield.BitField{}, xerrors.Errorf("failed to convert to v1_1 post proof: %w", err)
	}

	bad, err := ft.CheckProvable(ctx, pp, tocheck, func(ctx context.Context, id abi.SectorID) (cid.Cid, bool, error) {
		s, ok := sectors[id.Number]
		if !ok {
			return cid.Undef, false, xerrors.Errorf("sealed CID not found")
		}
		return s.sealed, s.update, nil
	})
	if err != nil {
		return bitfield.BitField{}, xerrors.Errorf("checking provable sectors: %w", err)
	}
	for id := range bad {
		delete(sectors, id.Number)
	}

	log.Warnw("Checked sectors", "checked", len(tocheck), "good", len(sectors))

	sbf := bitfield.New()
	for s := range sectors {
		sbf.Set(uint64(s))
	}

	return sbf, nil
}

func (t *WdPostTask) sectorsForProof(ctx context.Context, maddr address.Address, goodSectors, allSectors bitfield.BitField, ts *types.TipSet) ([]proof7.ExtendedSectorInfo, error) {
	sset, err := t.api.StateMinerSectors(ctx, maddr, &goodSectors, ts.Key())
	if err != nil {
		return nil, err
	}

	if len(sset) == 0 {
		return nil, nil
	}

	sectorByID := make(map[uint64]proof7.ExtendedSectorInfo, len(sset))
	for _, sector := range sset {
		sectorByID[uint64(sector.SectorNumber)] = proof7.ExtendedSectorInfo{
			SectorNumber: sector.SectorNumber,
			SealedCID:    sector.SealedCID,
			SealProof:    sector.SealProof,
			SectorKey:    sector.SectorKeyCID,
		}
	}

	proofSectors := make([]proof7.ExtendedSectorInfo, 0, len(sset))
	if err := allSectors.ForEach(func(sectorNo uint64) error {
		if info, found := sectorByID[sectorNo]; found {
			proofSectors = append(proofSectors, info)
		} //else {
		//skip
		// todo: testing: old logic used to put 'substitute' sectors here
		//  that probably isn't needed post nv19, but we do need to check that
		//}
		return nil
	}); err != nil {
		return nil, xerrors.Errorf("iterating partition sector bitmap: %w", err)
	}

	return proofSectors, nil
}

func (t *WdPostTask) generateWindowPoSt(ctx context.Context, ppt abi.RegisteredPoStProof, minerID abi.ActorID, sectorInfo []proof.ExtendedSectorInfo, randomness abi.PoStRandomness) ([]proof.PoStProof, []abi.SectorID, error) {
	var retErr error
	randomness[31] &= 0x3f

	out := make([]proof.PoStProof, 0)

	if len(sectorInfo) == 0 {
		return nil, nil, xerrors.New("generate window post len(sectorInfo)=0")
	}

	maxPartitionSize, err := builtin.PoStProofWindowPoStPartitionSectors(ppt) // todo proxy through chain/actors
	if err != nil {
		return nil, nil, xerrors.Errorf("get sectors count of partition failed:%+v", err)
	}

	// The partitions number of this batch
	// ceil(sectorInfos / maxPartitionSize)
	partitionCount := uint64((len(sectorInfo) + int(maxPartitionSize) - 1) / int(maxPartitionSize))
	if partitionCount > 1 {
		return nil, nil, xerrors.Errorf("generateWindowPoSt partitionCount:%d, only support 1", partitionCount)
	}

	log.Infof("generateWindowPoSt maxPartitionSize:%d partitionCount:%d", maxPartitionSize, partitionCount)

	var skipped []abi.SectorID
	var flk sync.Mutex
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sort.Slice(sectorInfo, func(i, j int) bool {
		return sectorInfo[i].SectorNumber < sectorInfo[j].SectorNumber
	})

	sectorNums := make([]abi.SectorNumber, len(sectorInfo))
	sectorMap := make(map[abi.SectorNumber]proof.ExtendedSectorInfo)
	for i, s := range sectorInfo {
		sectorNums[i] = s.SectorNumber
		sectorMap[s.SectorNumber] = s
	}

	postChallenges, err := ffi.GeneratePoStFallbackSectorChallenges(ppt, minerID, randomness, sectorNums)
	if err != nil {
		return nil, nil, xerrors.Errorf("generating fallback challenges: %v", err)
	}

	proofList := make([]ffi.PartitionProof, partitionCount)
	var wg sync.WaitGroup
	wg.Add(int(partitionCount))

	for partIdx := uint64(0); partIdx < partitionCount; partIdx++ {
		go func(partIdx uint64) {
			defer wg.Done()

			sectors := make([]storiface.PostSectorChallenge, 0)
			for i := uint64(0); i < maxPartitionSize; i++ {
				si := i + partIdx*maxPartitionSize
				if si >= uint64(len(postChallenges.Sectors)) {
					break
				}

				snum := postChallenges.Sectors[si]
				sinfo := sectorMap[snum]

				sectors = append(sectors, storiface.PostSectorChallenge{
					SealProof:    sinfo.SealProof,
					SectorNumber: snum,
					SealedCID:    sinfo.SealedCID,
					Challenge:    postChallenges.Challenges[snum],
					Update:       sinfo.SectorKey != nil,
				})
			}

			pr, err := t.GenerateWindowPoStAdv(cctx, ppt, minerID, sectors, int(partIdx), randomness, true)
			sk := pr.Skipped

			if err != nil || len(sk) > 0 {
				log.Errorf("generateWindowPost part:%d, skipped:%d, sectors: %d, err: %+v", partIdx, len(sk), len(sectors), err)
				flk.Lock()
				skipped = append(skipped, sk...)

				if err != nil {
					retErr = multierr.Append(retErr, xerrors.Errorf("partitionIndex:%d err:%+v", partIdx, err))
				}
				flk.Unlock()
			}

			proofList[partIdx] = ffi.PartitionProof(pr.PoStProofs)
		}(partIdx)
	}

	wg.Wait()

	if len(skipped) > 0 {
		log.Warnw("generateWindowPoSt skipped sectors", "skipped", len(skipped))
	}

	postProofs, err := ffi.MergeWindowPoStPartitionProofs(ppt, proofList)
	if err != nil {
		return nil, skipped, xerrors.Errorf("merge windowPoSt partition proofs: %v", err)
	}

	out = append(out, *postProofs)
	return out, skipped, retErr
}

func (t *WdPostTask) GenerateWindowPoStAdv(ctx context.Context, ppt abi.RegisteredPoStProof, mid abi.ActorID, sectors []storiface.PostSectorChallenge, partitionIdx int, randomness abi.PoStRandomness, allowSkip bool) (storiface.WindowPoStResult, error) {

	var slk sync.Mutex
	var skipped []abi.SectorID

	var wg sync.WaitGroup
	wg.Add(len(sectors))

	vproofs := make([][]byte, len(sectors))

	for i, s := range sectors {
		if t.parallel != nil {
			select {
			case t.parallel <- struct{}{}:
			case <-ctx.Done():
				return storiface.WindowPoStResult{}, xerrors.Errorf("context error waiting on challengeThrottle")
			}
		}

		go func(i int, s storiface.PostSectorChallenge) {
			defer wg.Done()
			ctx := ctx

			if t.parallel != nil {
				defer func() {
					<-t.parallel
				}()
			}

			if t.challengeReadTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, t.challengeReadTimeout)
				defer cancel()
			}

			vanilla, err := t.storage.GenerateSingleVanillaProof(ctx, mid, s, ppt)
			slk.Lock()
			defer slk.Unlock()

			if err != nil || vanilla == nil {
				skipped = append(skipped, abi.SectorID{
					Miner:  mid,
					Number: s.SectorNumber,
				})
				log.Errorf("reading PoSt challenge for sector %d, vlen:%d, err: %s", s.SectorNumber, len(vanilla), err)
				return
			}

			vproofs[i] = vanilla
		}(i, s)
	}
	wg.Wait()

	if len(skipped) > 0 && !allowSkip {
		// This should happen rarely because before entering GenerateWindowPoSt we check all sectors by reading challenges.
		// When it does happen, window post runner logic will just re-check sectors, and retry with newly-discovered-bad sectors skipped
		log.Errorf("couldn't read some challenges (skipped %d)", len(skipped))

		// note: can't return an error as this in an jsonrpc call
		return storiface.WindowPoStResult{Skipped: skipped}, nil
	}

	// compact skipped sectors
	var skippedSoFar int
	for i := range vproofs {
		if len(vproofs[i]) == 0 {
			skippedSoFar++
			continue
		}

		if skippedSoFar > 0 {
			vproofs[i-skippedSoFar] = vproofs[i]
		}
	}

	vproofs = vproofs[:len(vproofs)-skippedSoFar]

	// compute the PoSt!
	res, err := t.GenerateWindowPoStWithVanilla(ctx, ppt, mid, randomness, vproofs, partitionIdx)
	r := storiface.WindowPoStResult{
		PoStProofs: res,
		Skipped:    skipped,
	}
	if err != nil {
		log.Errorw("generating window PoSt failed", "error", err)
		return r, xerrors.Errorf("generate window PoSt with vanilla proofs: %w", err)
	}
	return r, nil
}

func (t *WdPostTask) GenerateWindowPoStWithVanilla(ctx context.Context, proofType abi.RegisteredPoStProof, minerID abi.ActorID, randomness abi.PoStRandomness, proofs [][]byte, partitionIdx int) (proof.PoStProof, error) {
	ctx = ffiselect.WithLogCtx(ctx, "miner", minerID, "proofType", proofType, "randomness", randomness, "partitionIdx", partitionIdx)
	pp, err := ffiselect.FFISelect.GenerateSinglePartitionWindowPoStWithVanilla(ctx, proofType, minerID, randomness, proofs, uint(partitionIdx))
	if err != nil {
		return proof.PoStProof{}, err
	}
	if pp == nil {
		// should be impossible, but just in case do not panic
		return proof.PoStProof{}, xerrors.New("postproof was nil")
	}

	return proof.PoStProof{
		PoStProof:  pp.PoStProof,
		ProofBytes: pp.ProofBytes,
	}, nil
}
