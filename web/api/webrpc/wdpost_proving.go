package webrpc

import (
	"bytes"
	"context"
	"runtime"
	"sync"
	"time"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/go-state-types/proof"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	cuproof "github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"
)

// VanillaTestResult contains the result of a single sector vanilla proof test
type VanillaTestResult struct {
	SectorNumber  abi.SectorNumber `json:"sector_number"`
	GenerateOK    bool             `json:"generate_ok"`
	VerifyOK      bool             `json:"verify_ok"`
	GenerateError string           `json:"generate_error,omitempty"`
	VerifyError   string           `json:"verify_error,omitempty"`
	GenerateTime  string           `json:"generate_time"`
	VerifyTime    string           `json:"verify_time,omitempty"`
	Slow          bool             `json:"slow"` // > 2 seconds
}

// VanillaTestReport contains the full report for a vanilla proof test
type VanillaTestReport struct {
	Miner       string              `json:"miner"`
	Deadline    uint64              `json:"deadline,omitempty"`
	Partition   uint64              `json:"partition,omitempty"`
	SectorCount int                 `json:"sector_count"`
	TestedCount int                 `json:"tested_count"`
	PassedCount int                 `json:"passed_count"`
	FailedCount int                 `json:"failed_count"`
	SlowCount   int                 `json:"slow_count"`
	TotalTime   string              `json:"total_time"`
	Results     []VanillaTestResult `json:"results"`
	Error       string              `json:"error,omitempty"`
}

// SectorVanillaTest tests vanilla proof generation and verification for a single sector
func (a *WebRPC) SectorVanillaTest(ctx context.Context, spStr string, sectorNum int64) (*VanillaTestReport, error) {
	start := time.Now()

	maddr, err := address.NewFromString(spStr)
	if err != nil {
		return nil, xerrors.Errorf("invalid miner address: %w", err)
	}

	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner ID: %w", err)
	}

	report := &VanillaTestReport{
		Miner:       maddr.String(),
		SectorCount: 1,
		Results:     []VanillaTestResult{},
	}

	// Get chain head and sector info
	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		report.Error = xerrors.Errorf("getting chain head: %w", err).Error()
		return report, nil
	}

	// Get sector on-chain info
	sinfo, err := a.deps.Chain.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(sectorNum), head.Key())
	if err != nil {
		report.Error = xerrors.Errorf("getting sector info: %w", err).Error()
		return report, nil
	}
	if sinfo == nil {
		report.Error = "sector not found on chain"
		return report, nil
	}

	// Get network version for proof type
	nv, err := a.deps.Chain.StateNetworkVersion(ctx, head.Key())
	if err != nil {
		report.Error = xerrors.Errorf("getting network version: %w", err).Error()
		return report, nil
	}

	ppt, err := sinfo.SealProof.RegisteredWindowPoStProofByNetworkVersion(nv)
	if err != nil {
		report.Error = xerrors.Errorf("getting window post proof type: %w", err).Error()
		return report, nil
	}

	// Generate random challenge for test
	buf := new(bytes.Buffer)
	if err := maddr.MarshalCBOR(buf); err != nil {
		report.Error = xerrors.Errorf("marshaling address: %w", err).Error()
		return report, nil
	}

	// Use current head for randomness (this is just a test, not actual proving)
	rand, err := a.deps.Chain.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_WindowedPoStChallengeSeed, head.Height(), buf.Bytes(), head.Key())
	if err != nil {
		report.Error = xerrors.Errorf("getting randomness: %w", err).Error()
		return report, nil
	}
	rand[31] &= 0x3f

	// Generate challenge for this sector
	postChallenges, err := ffi.GeneratePoStFallbackSectorChallenges(ppt, abi.ActorID(mid), abi.PoStRandomness(rand), []abi.SectorNumber{abi.SectorNumber(sectorNum)})
	if err != nil {
		report.Error = xerrors.Errorf("generating challenges: %w", err).Error()
		return report, nil
	}

	challenge := storiface.PostSectorChallenge{
		SealProof:    sinfo.SealProof,
		SectorNumber: abi.SectorNumber(sectorNum),
		SealedCID:    sinfo.SealedCID,
		Challenge:    postChallenges.Challenges[abi.SectorNumber(sectorNum)],
		Update:       sinfo.SectorKeyCID != nil,
	}

	result := VanillaTestResult{
		SectorNumber: abi.SectorNumber(sectorNum),
	}

	// Generate vanilla proof
	genStart := time.Now()
	vanilla, genErr := a.deps.Stor.GenerateSingleVanillaProof(ctx, abi.ActorID(mid), challenge, ppt)
	genElapsed := time.Since(genStart)
	result.GenerateTime = genElapsed.Round(time.Millisecond).String()
	result.Slow = genElapsed.Seconds() > 2

	if genErr != nil || vanilla == nil {
		result.GenerateOK = false
		if genErr != nil {
			result.GenerateError = genErr.Error()
		} else {
			result.GenerateError = "nil proof returned"
		}
		report.Results = append(report.Results, result)
		report.TestedCount = 1
		report.FailedCount = 1
		report.TotalTime = time.Since(start).Round(time.Millisecond).String()
		return report, nil
	}

	result.GenerateOK = true

	// Verify the proof
	pvi := proof.WindowPoStVerifyInfo{
		Randomness: abi.PoStRandomness(rand),
		Proofs: []proof.PoStProof{{
			PoStProof:  ppt,
			ProofBytes: vanilla,
		}},
		ChallengedSectors: []proof.SectorInfo{{
			SealProof:    sinfo.SealProof,
			SectorNumber: abi.SectorNumber(sectorNum),
			SealedCID:    sinfo.SealedCID,
		}},
	}

	verifyStart := time.Now()
	ok, verifyErr := cuproof.VerifyWindowPoStVanilla(pvi)
	verifyElapsed := time.Since(verifyStart)
	result.VerifyTime = verifyElapsed.Round(time.Millisecond).String()

	if verifyErr != nil {
		result.VerifyOK = false
		result.VerifyError = verifyErr.Error()
	} else {
		result.VerifyOK = ok
		if !ok {
			result.VerifyError = "verification returned false"
		}
	}

	report.Results = append(report.Results, result)
	report.TestedCount = 1
	if result.GenerateOK && result.VerifyOK {
		report.PassedCount = 1
	} else {
		report.FailedCount = 1
	}
	if result.Slow {
		report.SlowCount = 1
	}
	report.TotalTime = time.Since(start).Round(time.Millisecond).String()

	return report, nil
}

// PartitionVanillaTest tests vanilla proof generation for all sectors in a partition
func (a *WebRPC) PartitionVanillaTest(ctx context.Context, spStr string, deadlineIdx, partitionIdx uint64) (*VanillaTestReport, error) {
	start := time.Now()

	maddr, err := address.NewFromString(spStr)
	if err != nil {
		return nil, xerrors.Errorf("invalid miner address: %w", err)
	}

	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner ID: %w", err)
	}

	report := &VanillaTestReport{
		Miner:     maddr.String(),
		Deadline:  deadlineIdx,
		Partition: partitionIdx,
		Results:   []VanillaTestResult{},
	}

	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		report.Error = xerrors.Errorf("getting chain head: %w", err).Error()
		return report, nil
	}

	// Get partitions
	parts, err := a.deps.Chain.StateMinerPartitions(ctx, maddr, deadlineIdx, head.Key())
	if err != nil {
		report.Error = xerrors.Errorf("getting partitions: %w", err).Error()
		return report, nil
	}

	if partitionIdx >= uint64(len(parts)) {
		report.Error = xerrors.Errorf("partition %d does not exist (have %d)", partitionIdx, len(parts)).Error()
		return report, nil
	}

	partition := parts[partitionIdx]

	// Get live sectors
	liveSectors, err := partition.LiveSectors.All(abi.MaxSectorNumber)
	if err != nil {
		report.Error = xerrors.Errorf("getting live sectors: %w", err).Error()
		return report, nil
	}

	if len(liveSectors) == 0 {
		report.Error = "no live sectors in partition"
		return report, nil
	}

	report.SectorCount = len(liveSectors)

	// Get randomness
	buf := new(bytes.Buffer)
	if err := maddr.MarshalCBOR(buf); err != nil {
		report.Error = xerrors.Errorf("marshaling address: %w", err).Error()
		return report, nil
	}

	di := dline.NewInfo(head.Height(), deadlineIdx, 0, 0, 0, 10, 0, 0)
	rand, err := a.deps.Chain.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes(), head.Key())
	if err != nil {
		report.Error = xerrors.Errorf("getting randomness: %w", err).Error()
		return report, nil
	}
	rand[31] &= 0x3f

	// Get sector infos
	sectorNums := make([]abi.SectorNumber, len(liveSectors))
	for i, s := range liveSectors {
		sectorNums[i] = abi.SectorNumber(s)
	}

	// Get proof type from first sector
	sinfo, err := a.deps.Chain.StateSectorGetInfo(ctx, maddr, sectorNums[0], head.Key())
	if err != nil {
		report.Error = xerrors.Errorf("getting sector info: %w", err).Error()
		return report, nil
	}

	nv, err := a.deps.Chain.StateNetworkVersion(ctx, head.Key())
	if err != nil {
		report.Error = xerrors.Errorf("getting network version: %w", err).Error()
		return report, nil
	}

	ppt, err := sinfo.SealProof.RegisteredWindowPoStProofByNetworkVersion(nv)
	if err != nil {
		report.Error = xerrors.Errorf("getting window post proof type: %w", err).Error()
		return report, nil
	}

	// Generate challenges
	postChallenges, err := ffi.GeneratePoStFallbackSectorChallenges(ppt, abi.ActorID(mid), abi.PoStRandomness(rand), sectorNums)
	if err != nil {
		report.Error = xerrors.Errorf("generating challenges: %w", err).Error()
		return report, nil
	}

	// Build sector challenges
	type sectorChallenge struct {
		challenge storiface.PostSectorChallenge
		sinfo     *proof.SectorInfo
	}
	challenges := make([]sectorChallenge, 0, len(sectorNums))

	for _, snum := range postChallenges.Sectors {
		si, err := a.deps.Chain.StateSectorGetInfo(ctx, maddr, snum, head.Key())
		if err != nil {
			log.Warnw("failed to get sector info", "sector", snum, "error", err)
			continue
		}

		challenges = append(challenges, sectorChallenge{
			challenge: storiface.PostSectorChallenge{
				SealProof:    si.SealProof,
				SectorNumber: snum,
				SealedCID:    si.SealedCID,
				Challenge:    postChallenges.Challenges[snum],
				Update:       si.SectorKeyCID != nil,
			},
			sinfo: &proof.SectorInfo{
				SealProof:    si.SealProof,
				SectorNumber: snum,
				SealedCID:    si.SealedCID,
			},
		})
	}

	// Run tests in parallel
	var resultsLk sync.Mutex
	var wg sync.WaitGroup
	throttle := make(chan struct{}, runtime.NumCPU())

	for _, sc := range challenges {
		wg.Add(1)
		go func(sc sectorChallenge) {
			defer wg.Done()
			throttle <- struct{}{}
			defer func() { <-throttle }()

			result := VanillaTestResult{
				SectorNumber: sc.challenge.SectorNumber,
			}

			// Generate
			genStart := time.Now()
			vanilla, genErr := a.deps.Stor.GenerateSingleVanillaProof(ctx, abi.ActorID(mid), sc.challenge, ppt)
			genElapsed := time.Since(genStart)
			result.GenerateTime = genElapsed.Round(time.Millisecond).String()
			result.Slow = genElapsed.Seconds() > 2

			if genErr != nil || vanilla == nil {
				result.GenerateOK = false
				if genErr != nil {
					result.GenerateError = genErr.Error()
				} else {
					result.GenerateError = "nil proof returned"
				}
				resultsLk.Lock()
				report.Results = append(report.Results, result)
				resultsLk.Unlock()
				return
			}
			result.GenerateOK = true

			// Verify
			pvi := proof.WindowPoStVerifyInfo{
				Randomness: abi.PoStRandomness(rand),
				Proofs: []proof.PoStProof{{
					PoStProof:  ppt,
					ProofBytes: vanilla,
				}},
				ChallengedSectors: []proof.SectorInfo{*sc.sinfo},
			}

			verifyStart := time.Now()
			ok, verifyErr := cuproof.VerifyWindowPoStVanilla(pvi)
			verifyElapsed := time.Since(verifyStart)
			result.VerifyTime = verifyElapsed.Round(time.Millisecond).String()

			if verifyErr != nil {
				result.VerifyOK = false
				result.VerifyError = verifyErr.Error()
			} else {
				result.VerifyOK = ok
				if !ok {
					result.VerifyError = "verification returned false"
				}
			}

			resultsLk.Lock()
			report.Results = append(report.Results, result)
			resultsLk.Unlock()
		}(sc)
	}

	wg.Wait()

	// Tally results
	report.TestedCount = len(report.Results)
	for _, r := range report.Results {
		if r.GenerateOK && r.VerifyOK {
			report.PassedCount++
		} else {
			report.FailedCount++
		}
		if r.Slow {
			report.SlowCount++
		}
	}
	report.TotalTime = time.Since(start).Round(time.Millisecond).String()

	return report, nil
}

// WdPostTaskResult contains the result of a WdPost task test
type WdPostTaskResult struct {
	TaskID    int64  `json:"task_id"`
	SpID      int64  `json:"sp_id"`
	Deadline  uint64 `json:"deadline"`
	Partition uint64 `json:"partition"`
	Submitted bool   `json:"submitted"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

// WdPostTaskStart creates a test WdPost task for a partition and returns the task ID
func (a *WebRPC) WdPostTaskStart(ctx context.Context, spStr string, deadlineIdx uint64, partitionIdx uint64) (*WdPostTaskResult, error) {
	maddr, err := address.NewFromString(spStr)
	if err != nil {
		return nil, xerrors.Errorf("invalid miner address: %w", err)
	}

	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner ID: %w", err)
	}

	// Get current chain height for proving period
	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting chain head: %w", err)
	}

	result := &WdPostTaskResult{
		SpID:      int64(mid),
		Deadline:  deadlineIdx,
		Partition: partitionIdx,
	}

	var taskID int64

	_, err = a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		err = tx.QueryRow(`INSERT INTO harmony_task (name, posted_time, added_by) VALUES ('WdPost', CURRENT_TIMESTAMP, $1) RETURNING id`, a.deps.MachineID).Scan(&taskID)
		if err != nil {
			return false, xerrors.Errorf("inserting harmony_task: %w", err)
		}

		_, err = tx.Exec(`INSERT INTO wdpost_partition_tasks 
			(task_id, sp_id, proving_period_start, deadline_index, partition_index) VALUES ($1, $2, $3, $4, $5)`,
			taskID, mid, head.Height(), deadlineIdx, partitionIdx)
		if err != nil {
			return false, xerrors.Errorf("inserting wdpost_partition_tasks: %w", err)
		}

		_, err = tx.Exec("INSERT INTO harmony_test (task_id) VALUES ($1)", taskID)
		if err != nil {
			return false, xerrors.Errorf("inserting into harmony_test: %w", err)
		}

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return nil, xerrors.Errorf("creating test task: %w", err)
	}

	result.TaskID = taskID
	result.Submitted = true

	return result, nil
}

// WdPostTaskCheck checks the status of a WdPost test task
func (a *WebRPC) WdPostTaskCheck(ctx context.Context, taskID int64) (*WdPostTaskResult, error) {
	result := &WdPostTaskResult{
		TaskID: taskID,
	}

	// Check harmony_test for result
	var testResult NullString
	err := a.deps.DB.QueryRow(ctx, `SELECT result FROM harmony_test WHERE task_id = $1`, taskID).Scan(&testResult)
	if err != nil {
		return nil, xerrors.Errorf("checking harmony_test: %w", err)
	}

	if testResult.Valid {
		result.Result = testResult.String
		return result, nil
	}

	// Check task history for errors - use existing (task_id, result) index, filter in Go
	var history []struct {
		ID     int64  `db:"id"`
		Result bool   `db:"result"`
		Err    string `db:"err"`
	}
	err = a.deps.DB.Select(ctx, &history, `SELECT id, result, err FROM harmony_task_history WHERE task_id = $1 AND result = false`, taskID)
	if err == nil && len(history) > 0 {
		// Find the most recent failed entry (highest ID)
		var latest *struct {
			ID     int64  `db:"id"`
			Result bool   `db:"result"`
			Err    string `db:"err"`
		}
		for i := range history {
			if latest == nil || history[i].ID > latest.ID {
				latest = &history[i]
			}
		}
		if latest != nil && latest.Err != "" {
			result.Error = latest.Err
		}
	}

	return result, nil
}
