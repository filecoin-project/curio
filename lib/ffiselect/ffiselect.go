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
	"github.com/samber/lo"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/curio/build"

	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type logCtxKt struct{}

var logCtxKey = logCtxKt{}

func WithLogCtx(ctx context.Context, kvs ...any) context.Context {
	return context.WithValue(ctx, logCtxKey, kvs)
}

var IsTest = false
var IsCuda = build.IsOpencl != "1"

// Get all devices from ffi
var ch chan string

func init() {
	devices, err := ffi.GetGPUDevices()
	if err != nil {
		panic(err)
	}
	if len(devices) == 0 {
		ch = make(chan string, 1)
		ch <- "0"
	} else {
		ch = make(chan string, len(devices))
		for i := 0; i < len(devices); i++ {
			ch <- strconv.Itoa(i)
		}
	}
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
	dOrdinal := <-ch
	defer func() {
		ch <- dOrdinal
	}()

	p, err := os.Executable()
	if err != nil {
		return nil, err
	}

	commandAry := []string{"ffi"}
	cmd := exec.Command(p, commandAry...)

	// Set Visible Devices for CUDA and OpenCL
	cmd.Env = append(os.Environ(),
		func(isCuda bool) string {
			if isCuda {
				return "CUDA_VISIBLE_DEVICES=" + dOrdinal
			}
			return "GPU_DEVICE_ORDINAL=" + dOrdinal
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

	lw := NewLogWriter(ctx.Value(logCtxKey).([]any), os.Stderr)

	cmd.Stderr = lw
	cmd.Stdout = os.Stdout
	outFile, err := os.CreateTemp("", "out")
	if err != nil {
		return nil, err
	}
	cmd.ExtraFiles = []*os.File{outFile}

	cmd.Stdin = bytes.NewReader(body)
	err = cmd.Run()
	if err != nil {
		return nil, err
	}

	// seek to start
	if _, err := outFile.Seek(0, io.SeekStart); err != nil {
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

	SelfTest func(ctx context.Context, val1 int, val2 cid.Cid) (cid.Cid, error)
}

// //////////////////////////

func init() {
	_, err := jsonrpc.NewCustomClient("FFI", []interface{}{&FFISelect}, call)
	if err != nil {
		panic(err)
	}
}
