package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/lib/ffi/cunative"
	"github.com/filecoin-project/curio/lib/supraffi"
)

var testSupraCmd = &cli.Command{
	Name:  "supra",
	Usage: translations.T("Supra consensus testing utilities"),
	Subcommands: []*cli.Command{
		testSupraSystemInfoCmd,
		testSupraTreeRFileCmd,
		testSnapEncodeCmd,
	},
}

var testSupraSystemInfoCmd = &cli.Command{
	Name:  "system-info",
	Usage: "Display CPU and CUDA information relevant for supraseal",
	Action: func(cctx *cli.Context) error {
		err := logging.SetLogLevel("supraffi", "WARN")
		if err != nil {
			return xerrors.Errorf("set log level: %w", err)
		}
		features := supraffi.GetCPUFeatures()

		fmt.Println("=== Supraseal System Information ===")
		fmt.Println()

		fmt.Println("CPU Features:")
		fmt.Printf("  %s\n", supraffi.CPUFeaturesSummary())
		fmt.Println()

		fmt.Println("Feature Details:")
		fmt.Printf("  SHA-NI (SHA Extensions):  %s\n", yesNo(features.HasSHAExt))
		fmt.Printf("  SSE2:                     %s\n", yesNo(features.HasSSE2))
		fmt.Printf("  SSSE3:                    %s\n", yesNo(features.HasSSSE3))
		fmt.Printf("  SSE4.1:                   %s\n", yesNo(features.HasSSE4))
		fmt.Printf("  AVX:                      %s\n", yesNo(features.HasAVX))
		fmt.Printf("  AVX2:                     %s\n", yesNo(features.HasAVX2))
		fmt.Printf("  AVX512 (AMD64v4):         %s\n", yesNo(features.HasAMD64v4))
		fmt.Println()

		fmt.Println("Supraseal Capability:")
		fmt.Printf("  Can run PC1 (sha_ext_mbx2):  %s\n", yesNo(supraffi.CanRunSupraSealPC1()))
		fmt.Printf("  Can run full Supraseal:      %s\n", yesNo(supraffi.CanRunSupraSeal()))
		fmt.Printf("  Can run fast TreeR:          %s\n", yesNo(supraffi.HasAMD64v4() && supraffi.HasUsableCUDAGPU()))
		fmt.Println()

		fmt.Println("CUDA:")
		fmt.Printf("  Usable CUDA GPU detected:    %s\n", yesNo(supraffi.HasUsableCUDAGPU()))
		fmt.Println()

		fmt.Println("GPU Devices (ffi):")
		gpuMode := "OpenCL"
		if build.IsOpencl != "1" {
			gpuMode = "CUDA"
		}
		fmt.Printf("  Mode:                        %s\n", gpuMode)
		fmt.Printf("  Overprovision factor:        %d\n", resources.GpuOverprovisionFactor)
		gpus, err := ffi.GetGPUDevices()
		if err != nil {
			fmt.Printf("  Error listing GPUs:          %s\n", err)
		} else if len(gpus) == 0 {
			fmt.Println("  No GPU devices found")
		} else {
			fmt.Printf("  Devices (%d):\n", len(gpus))
			for i, name := range gpus {
				fmt.Printf("    [%d] %s\n", i, name)
			}
		}

		return nil
	},
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

var testSupraTreeRFileCmd = &cli.Command{
	Name:  "tree-r-file",
	Usage: "Test tree-r-file",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "last-layer-filename",
			Usage:    "Last layer filename",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "data-filename",
			Usage:    "Data filename",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "output-dir",
			Usage:    "Output directory",
			Required: true,
		},
		&cli.Uint64Flag{
			Name:     "sector-size",
			Usage:    "Sector size",
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		res := supraffi.TreeRFile(cctx.String("last-layer-filename"), cctx.String("data-filename"), cctx.String("output-dir"), cctx.Uint64("sector-size"))
		if res != 0 {
			return xerrors.Errorf("tree-r-file failed: %d", res)
		}
		return nil
	},
}

var testSnapEncodeCmd = &cli.Command{
	Name:  "snap-encode",
	Usage: "Test snap-encode",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "sealed-filename",
			Usage:    "Sealed filename",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "unsealed-filename",
			Usage:    "Unsealed filename",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "update-filename",
			Usage:    "Update filename",
			Required: true,
		},
		&cli.Uint64Flag{
			Name:     "sector-size",
			Usage:    "Sector size (bytes). Supported: 2048, 8388608, 549755813888, 34359738368, 68719476736",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "commd",
			Usage:    "Unsealed CommD CID (v1)",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "commk",
			Usage:    "SectorKey CommR (commK) CID (v1)",
			Required: true,
		},
		&cli.BoolFlag{
			Name:  "membuffer",
			Usage: "Use memory buffer instead of disk (load and store)",
			Value: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		sealedPath := cctx.String("sealed-filename")
		unsealedPath := cctx.String("unsealed-filename")
		updatePath := cctx.String("update-filename")
		useMem := cctx.Bool("membuffer")

		commD, err := cid.Parse(cctx.String("commd"))
		if err != nil {
			return xerrors.Errorf("parse commD: %w", err)
		}
		commK, err := cid.Parse(cctx.String("commk"))
		if err != nil {
			return xerrors.Errorf("parse commK: %w", err)
		}

		spt, err := proofFromSectorSize(cctx.Uint64("sector-size"))
		if err != nil {
			return err
		}
		ssize, err := spt.SectorSize()
		if err != nil {
			return err
		}

		start := time.Now()
		if useMem {
			sealedBytes, err := os.ReadFile(sealedPath)
			if err != nil {
				return xerrors.Errorf("read sealed: %w", err)
			}
			unsealedBytes, err := os.ReadFile(unsealedPath)
			if err != nil {
				return xerrors.Errorf("read unsealed: %w", err)
			}

			elapsed := time.Since(start)
			mbps := float64(ssize) / elapsed.Seconds() / 1024.0 / 1024.0
			fmt.Printf("Load time: %s\n", elapsed)
			fmt.Printf("Load throughput: %.2f MB/s\n", mbps)

			var outBuf bytes.Buffer
			outBuf.Grow(int(ssize))
			start = time.Now() //nolint:staticcheck // false positive: used on line 181
			if err := cunative.EncodeSnap(spt, commD, commK, bytes.NewReader(sealedBytes), bytes.NewReader(unsealedBytes), &outBuf); err != nil {
				return xerrors.Errorf("EncodeSnap: %w", err)
			}
		} else {
			keyF, err := os.Open(sealedPath)
			if err != nil {
				return xerrors.Errorf("open sealed: %w", err)
			}
			defer func() { _ = keyF.Close() }()

			dataF, err := os.Open(unsealedPath)
			if err != nil {
				return xerrors.Errorf("open unsealed: %w", err)
			}
			defer func() { _ = dataF.Close() }()

			outF, err := os.OpenFile(updatePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return xerrors.Errorf("create update: %w", err)
			}
			defer func() { _ = outF.Close() }()

			if err := cunative.EncodeSnap(spt, commD, commK, keyF, dataF, outF); err != nil {
				return xerrors.Errorf("EncodeSnap: %w", err)
			}

			if err := outF.Sync(); err != nil {
				return xerrors.Errorf("sync update: %w", err)
			}

			_, _ = io.Copy(io.Discard, keyF)
			_, _ = io.Copy(io.Discard, dataF)
			start = time.Now()
		}
		elapsed := time.Since(start)
		mbps := float64(ssize) / elapsed.Seconds() / 1024.0 / 1024.0
		fmt.Printf("EncodeSnap time: %s\n", elapsed)
		fmt.Printf("EncodeSnap throughput: %.2f MB/s\n", mbps)

		return nil
	},
}

func proofFromSectorSize(size uint64) (abi.RegisteredSealProof, error) {
	switch size {
	case 2 << 10:
		return abi.RegisteredSealProof_StackedDrg2KiBV1_1, nil
	case 8 << 20:
		return abi.RegisteredSealProof_StackedDrg8MiBV1_1, nil
	case 512 << 20:
		return abi.RegisteredSealProof_StackedDrg512MiBV1_1, nil
	case 32 << 30:
		return abi.RegisteredSealProof_StackedDrg32GiBV1_1, nil
	case 64 << 30:
		return abi.RegisteredSealProof_StackedDrg64GiBV1_1, nil
	default:
		return 0, xerrors.Errorf("unsupported sector size: %d", size)
	}
}
