package main

import (
	"encoding/binary"
	"fmt"
	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/filecoin-ffi/cgo"
	"github.com/filecoin-project/go-commp-utils/zerocomm"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/minio/sha256-simd"
	"golang.org/x/xerrors"
	"math/bits"
	"os"
	"path/filepath"
)

func ReplicaId(sector abi.SectorNumber, ticket []byte, commd []byte) ([32]byte, error) {
	// https://github.com/filecoin-project/rust-fil-proofs/blob/5b46d4ac88e19003416bb110e2b2871523cc2892/storage-proofs-porep/src/stacked/vanilla/params.rs#L758-L775

	pi := [32]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
	porepID, err := abi.RegisteredSealProof_StackedDrg32GiBV1_1.PoRepID()
	if err != nil {
		return [32]byte{}, err
	}

	if len(ticket) != 32 {
		return [32]byte{}, xerrors.Errorf("invalid ticket length %d", len(ticket))
	}
	if len(commd) != 32 {
		return [32]byte{}, xerrors.Errorf("invalid commd length %d", len(commd))
	}

	var sectorID [8]byte
	binary.BigEndian.PutUint64(sectorID[:], uint64(sector))

	s := sha256.New()

	// sha256 writes never error
	_, _ = s.Write(pi[:])
	_, _ = s.Write(sectorID[:])
	_, _ = s.Write(ticket)
	_, _ = s.Write(commd)
	_, _ = s.Write(porepID[:])

	return bytesIntoFr32Safe(s.Sum(nil)), nil
}

func bytesIntoFr32Safe(in []byte) [32]byte {
	var out [32]byte
	copy(out[:], in)

	out[31] &= 0b0011_1111

	return out
}

func main() {
	outPath := os.Args[1]

	/*const ssize = 32 << 30
	const profTyp = cgo.RegisteredSealProofStackedDrg32GiBV11*/

	const ssize = 32 << 30
	const profTyp = cgo.RegisteredSealProofStackedDrg32GiBV11

	cacheDirPath := filepath.Join(outPath, "cache")
	stagedSectorPath := filepath.Join(outPath, "unseal")
	sealedSectorPath := filepath.Join(outPath, "seal")

	_ = os.MkdirAll(cacheDirPath, 0755)

	{
		file, err := os.Create(stagedSectorPath)
		if err != nil {
			fmt.Printf("Failed to create file: %v\n", err)
			return
		}

		size := int64(ssize)
		zero := make([]byte, max(1024*1024, ssize))
		var written int64
		for written < size {
			n, err := file.Write(zero)
			if err != nil {
				fmt.Printf("Failed to write to file: %v\n", err)
				return
			}
			written += int64(n)
		}

		if err := file.Close(); err != nil {
			panic(err)
		}
	}

	{
		file, err := os.Create(sealedSectorPath)
		if err != nil {
			fmt.Printf("Failed to create file: %v\n", err)
			return
		}

		size := int64(ssize)
		zero := make([]byte, max(1024*1024, ssize))
		var written int64
		for written < size {
			n, err := file.Write(zero)
			if err != nil {
				fmt.Printf("Failed to write to file: %v\n", err)
				return
			}
			written += int64(n)
		}

		if err := file.Close(); err != nil {
			panic(err)
		}
	}

	sectorNum := uint64(0)
	var proverID = [32]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
	var ticket = [32]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}

	/*var proverID [32]byte
	proverID[2] = 4
	var ticket [32]byte
	rand.Read(ticket[:])
	ticket[31] &= 0x3c*/

	cprover := cgo.AsByteArray32(proverID[:])
	cticket := cgo.AsByteArray32(ticket[:])

	level := bits.TrailingZeros64(uint64(ssize)) - zerocomm.Skip - 5

	ppi := []cgo.PublicPieceInfo{
		cgo.NewPublicPieceInfo(uint64(abi.PaddedPieceSize(ssize).Unpadded()), cgo.AsByteArray32(zerocomm.PieceComms[level][:])),
	}

	replid, _ := ReplicaId(abi.SectorNumber(sectorNum), ticket[:], zerocomm.PieceComms[level][:])
	fmt.Printf("replid: %x\n", replid)
	for i := range replid {
		fmt.Printf("%d, ", replid[i])
	}
	fmt.Println()

	p1o, err := cgo.SealPreCommitPhase1(profTyp,
		cgo.AsSliceRefUint8([]byte(cacheDirPath)),
		cgo.AsSliceRefUint8([]byte(stagedSectorPath)),
		cgo.AsSliceRefUint8([]byte(sealedSectorPath)), sectorNum, &cprover, &cticket, cgo.AsSliceRefPublicPieceInfo(ppi))
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(filepath.Join(cacheDirPath, "pc1out.json"), p1o, 0666)
	if err != nil {
		panic(err)
	}

	scid, ucid, err := ffi.SealPreCommitPhase2(p1o, cacheDirPath, sealedSectorPath)
	if err != nil {
		panic(err)
	}

	fmt.Println("sealed:", scid)
	fmt.Println("unsealed:", ucid)

	commr, err := commcid.CIDToReplicaCommitmentV1(scid)
	if err != nil {
		panic(err)
	}
	ccommr := cgo.AsByteArray32(commr)

	commd, err := commcid.CIDToDataCommitmentV1(ucid)
	if err != nil {
		panic(err)
	}
	ccommd := cgo.AsByteArray32(commd)

	c1o, err := cgo.SealCommitPhase1(profTyp, &ccommr, &ccommd,
		cgo.AsSliceRefUint8([]byte(cacheDirPath)),
		cgo.AsSliceRefUint8([]byte(sealedSectorPath)),
		sectorNum,
		&cprover,
		&cticket,
		&cticket,
		cgo.AsSliceRefPublicPieceInfo(ppi))
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(filepath.Join(cacheDirPath, "commit1-out.json"), c1o, 0666)
	if err != nil {
		panic(err)
	}

	proof, err := cgo.SealCommitPhase2(cgo.AsSliceRefUint8(c1o), sectorNum, &cprover)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(filepath.Join(cacheDirPath, "proof.json"), proof, 0666)
	if err != nil {
		panic(err)
	}

	fmt.Println("done!")

}
