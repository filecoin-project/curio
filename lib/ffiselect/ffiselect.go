package ffiselect

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/storiface"
)

type logCtxKt struct{}

var logCtxKey = logCtxKt{}

func WithLogCtx(ctx context.Context, kvs ...any) context.Context {
	return context.WithValue(ctx, logCtxKey, kvs)
}

var logger = logging.Logger("ffiselect")

var IsTest = false
var IsCuda = build.IsOpencl != "1"

// Get all devices from ffi
type deviceOrdinalManager struct {
	releaseChan chan int
	acquireChan chan chan int
}

var deviceOrdinalMgr = newDeviceOrdinalManager(ffi.GetGPUDevices)

func newDeviceOrdinalManager(getGPUDevices func() ([]string, error)) *deviceOrdinalManager {
	d := &deviceOrdinalManager{
		releaseChan: make(chan int),
		acquireChan: make(chan chan int),
	}
	go func() {
		devices, err := getGPUDevices()
		if err != nil {
			panic(err)
		}

		// No GPUs: immediately respond with -1 to every acquire, ignore releases.
		if len(devices) == 0 {
			for {
				select {
				case <-d.releaseChan:
					// nothing to track
				case acquireChan := <-d.acquireChan:
					acquireChan <- -1
				}
			}
		}

		gpuSlots := make([]byte, len(devices))
		for i := range gpuSlots {
			gpuSlots[i] = byte(resources.GpuOverprovisionFactor)
		}

		waitList := []chan int{}
		for { // distribute loop
			select {
			case ordinal := <-d.releaseChan:
				if ordinal < 0 || ordinal >= len(gpuSlots) {
					logger.Errorf("release of invalid GPU ordinal %d (have %d GPUs), ignoring", ordinal, len(gpuSlots))
					continue
				}
				if gpuSlots[ordinal] >= byte(resources.GpuOverprovisionFactor) {
					logger.Errorf("double-release of GPU ordinal %d (slot already at capacity %d), ignoring", ordinal, resources.GpuOverprovisionFactor)
					continue
				}
				if len(waitList) > 0 { // unblock the delayed requests
					waitList[0] <- ordinal
					waitList = waitList[1:]
				} else {
					gpuSlots[ordinal]++
				}
			case acquireChan := <-d.acquireChan:
				max, maxIdx := byte(0), 0
				for i, w := range gpuSlots { // find the least used GPU
					if w > max {
						max, maxIdx = w, i
					}
				}
				if max == 0 { // no GPU available, add to the wait list
					waitList = append(waitList, acquireChan)
					continue
				}
				gpuSlots[maxIdx]--
				acquireChan <- maxIdx
			}
		}
	}()
	return d
}

func (d *deviceOrdinalManager) Release(ordinal int) {
	d.releaseChan <- ordinal
}

func (d *deviceOrdinalManager) Get() int {
	acquireChan := make(chan int)
	d.acquireChan <- acquireChan
	return <-acquireChan
}

type ValErr struct {
	Val []interface{}
	Err string
}

// This is not the one you're looking for.
type FFICall struct {
	Fn   string
	Args []interface{}
}

func subStrInSet(set []string, sub string) bool {
	return lo.Reduce(set, func(agg bool, item string, _ int) bool { return agg || strings.Contains(item, sub) }, false)
}

func call(ctx context.Context, body []byte) (io.ReadCloser, error) {
	if IsTest {
		return callTest(ctx, body)
	}

	// get dOrdinal
	dOrdinal := deviceOrdinalMgr.Get()

	if dOrdinal == -1 {
		return nil, xerrors.Errorf("no GPUs available. Something went wrong in the scheduler.")
	}

	defer func() {
		deviceOrdinalMgr.Release(dOrdinal)
	}()

	p, err := os.Executable()
	if err != nil {
		return nil, err
	}

	commandAry := []string{"ffi"}
	cmd := exec.CommandContext(ctx, p, commandAry...)

	// Set Visible Devices for CUDA and OpenCL
	cmd.Env = append(os.Environ(),
		func(isCuda bool) string {
			ordinal := strconv.Itoa(dOrdinal)
			if isCuda {
				return "CUDA_VISIBLE_DEVICES=" + ordinal
			}
			return "GPU_DEVICE_ORDINAL=" + ordinal
		}(IsCuda))
	tmpDir, err := os.MkdirTemp("", "rust-fil-proofs")
	if err != nil {
		return nil, err
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+tmpDir)

	if !subStrInSet(cmd.Env, "RUST_LOG") {
		cmd.Env = append(cmd.Env, "RUST_LOG=debug")
	}
	if !subStrInSet(cmd.Env, "FIL_PROOFS_USE_GPU_COLUMN_BUILDER") {
		cmd.Env = append(cmd.Env, "FIL_PROOFS_USE_GPU_COLUMN_BUILDER=1")
	}
	if !subStrInSet(cmd.Env, "FIL_PROOFS_USE_GPU_TREE_BUILDER") {
		cmd.Env = append(cmd.Env, "FIL_PROOFS_USE_GPU_TREE_BUILDER=1")
	}

	defer func() { _ = os.RemoveAll(tmpDir) }()

	logKvs, _ := ctx.Value(logCtxKey).([]any)
	lw := NewLogWriter(logKvs, os.Stderr)

	cmd.Stderr = lw
	cmd.Stdout = lw
	outFile, err := os.CreateTemp("", "out")
	if err != nil {
		return nil, err
	}
	cmd.ExtraFiles = []*os.File{outFile}

	cmd.Stdin = bytes.NewReader(body)
	err = cmd.Run()
	if err != nil {
		_ = outFile.Close()
		_ = os.Remove(outFile.Name())
		return nil, err
	}

	// seek to start
	if _, err := outFile.Seek(0, io.SeekStart); err != nil {
		_ = outFile.Close()
		_ = os.Remove(outFile.Name())
		return nil, xerrors.Errorf("failed to seek to beginning of output file: %w", err)
	}

	return outFile, nil
}

///////////Funcs reachable by the GPU selector.///////////
// NOTE: Changes here MUST also change ffi-direct.go

var FFISelect struct {
	GenerateSinglePartitionWindowPoStWithVanilla func(
		ctx context.Context,
		proofType abi.RegisteredPoStProof,
		minerID abi.ActorID,
		randomness abi.PoStRandomness,
		proofs [][]byte,
		partitionIndex uint,
	) (*ffi.PartitionProof, error)

	SealPreCommitPhase2 func(
		ctx context.Context,
		phase1Output []byte,
		cacheDirPath string,
		sealedSectorPath string,
	) (out storiface.SectorCids, err error)

	SealCommitPhase2 func(
		ctx context.Context,
		phase1Output []byte,
		sectorNum abi.SectorNumber,
		minerID abi.ActorID,
	) ([]byte, error)

	GenerateWinningPoStWithVanilla func(
		ctx context.Context,
		proofType abi.RegisteredPoStProof,
		minerID abi.ActorID,
		randomness abi.PoStRandomness,
		proofs [][]byte,
	) ([]proof.PoStProof, error)

	EncodeInto func(
		ctx context.Context,
		proofType abi.RegisteredUpdateProof,
		newReplicaPath string,
		newReplicaCachePath string,
		sectorKeyPath string,
		sectorKeyCachePath string,
		stagedDataPath string,
		pieces []abi.PieceInfo,
	) (out storiface.SectorCids, err error)

	GenerateUpdateProofWithVanilla func(
		ctx context.Context,
		proofType abi.RegisteredUpdateProof,
		key, sealed, unsealed cid.Cid,
		vproofs [][]byte,
	) ([]byte, error)

	TreeRFile func(ctx context.Context, lastLayerFilename, dataFilename, outputDir string, sectorSize uint64) error

	SelfTest func(ctx context.Context, val1 int, val2 cid.Cid) (cid.Cid, error)
}

// //////////////////////////

func init() {
	_, err := jsonrpc.NewCustomClient("FFI", []interface{}{&FFISelect}, call)
	if err != nil {
		panic(err)
	}
}
