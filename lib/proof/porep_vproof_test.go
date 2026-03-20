package proof

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	cid "github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"

	ffi "github.com/filecoin-project/filecoin-ffi"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	sproof "github.com/filecoin-project/go-state-types/proof"
)

// sealTestSector seals a 2KiB sector and returns the C1 output along with
// all parameters needed for C2 and verification. This is reused by multiple
// test functions.
func sealTestSector(t *testing.T) (c1out []byte, spt abi.RegisteredSealProof, sealed, unsealed cid.Cid, num abi.SectorNumber, minerID abi.ActorID, ticket, seed [32]byte) {
	t.Helper()

	testDir := t.TempDir()

	paddedSize := abi.PaddedPieceSize(2048)
	unpaddedSize := paddedSize.Unpadded()

	sectorData := make([]byte, unpaddedSize)
	_, err := rand.Read(sectorData)
	require.NoError(t, err)

	unsFile := filepath.Join(testDir, "unsealed")
	cacheFile := filepath.Join(testDir, "cache")
	sealedFile := filepath.Join(testDir, "sealed")

	err = os.MkdirAll(cacheFile, 0755)
	require.NoError(t, err)
	f, err := os.Create(sealedFile)
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	commd, err := BuildTreeD(bytes.NewReader(sectorData), true, unsFile, 2048)
	require.NoError(t, err)

	err = os.Truncate(unsFile, int64(paddedSize))
	require.NoError(t, err)

	spt = abi.RegisteredSealProof_StackedDrg2KiBV1_1
	num = 234
	minerID = 123

	_, err = rand.Read(ticket[:])
	require.NoError(t, err)
	ticket[31] &= 0x3f // fr32

	pieces := []abi.PieceInfo{{
		Size:     paddedSize,
		PieceCID: commd,
	}}

	p1o, err := ffi.SealPreCommitPhase1(spt, cacheFile, unsFile, sealedFile, num, minerID, ticket[:], pieces)
	require.NoError(t, err)

	sealed, unsealed, err = ffi.SealPreCommitPhase2(p1o, cacheFile, sealedFile)
	require.NoError(t, err)

	_, err = rand.Read(seed[:])
	require.NoError(t, err)
	seed[31] &= 0x3f // fr32

	c1out, err = ffi.SealCommitPhase1(spt, sealed, unsealed, cacheFile, sealedFile, num, minerID, ticket[:], seed[:], pieces)
	require.NoError(t, err)

	return
}

func TestRoundtripPorepVproof(t *testing.T) {
	c1out, spt, sealed, unsealed, num, miner, ticket, seed := sealTestSector(t)

	// deserialize the proof with Go types
	var realVProof Commit1OutRaw
	err := json.Unmarshal(c1out, &realVProof)
	require.NoError(t, err)

	t.Run("json-roundtrip-map-check", func(t *testing.T) {
		// serialize the proof to JSON
		proof1out, err := json.Marshal(realVProof)
		require.NoError(t, err)

		// check that the JSON is as expected (map-level comparison)
		var rustObj, goObj map[string]interface{}
		err = json.Unmarshal(c1out, &rustObj)
		require.NoError(t, err)
		err = json.Unmarshal(proof1out, &goObj)
		require.NoError(t, err)

		diff := cmp.Diff(rustObj, goObj)
		if !cmp.Equal(rustObj, goObj) {
			t.Errorf("proof mismatch: %s", diff)
		}

		require.True(t, cmp.Equal(rustObj, goObj))
	})

	t.Run("json-roundtrip-byte-level", func(t *testing.T) {
		// Re-serialize the Go struct and compare byte-for-byte with the
		// original Rust JSON. If they differ, log the first divergent byte
		// and surrounding context.
		goJSON, err := json.Marshal(realVProof)
		require.NoError(t, err)

		if !bytes.Equal(c1out, goJSON) {
			// Find first difference
			minLen := len(c1out)
			if len(goJSON) < minLen {
				minLen = len(goJSON)
			}

			for i := 0; i < minLen; i++ {
				if c1out[i] != goJSON[i] {
					start := i - 40
					if start < 0 {
						start = 0
					}
					end := i + 40
					if end > minLen {
						end = minLen
					}
					t.Logf("First byte difference at offset %d (rustLen=%d, goLen=%d)", i, len(c1out), len(goJSON))
					t.Logf("  Rust context: ...%s...", string(c1out[start:end]))
					t.Logf("  Go   context: ...%s...", string(goJSON[start:end]))
					break
				}
			}

			if len(c1out) != len(goJSON) {
				t.Logf("Length difference: rust=%d, go=%d", len(c1out), len(goJSON))
			}

			// This is a WARNING not a failure — field order can differ
			// legitimately. But note it for diagnostics.
			t.Logf("WARNING: Rust and Go JSON bytes differ (this may be benign field reordering)")
		} else {
			t.Logf("Rust and Go JSON are byte-identical (%d bytes)", len(c1out))
		}
	})

	t.Run("check-toplevel", func(t *testing.T) {
		rawCommD, err := commcid.CIDToDataCommitmentV1(unsealed)
		require.NoError(t, err)
		rawCommR, err := commcid.CIDToReplicaCommitmentV1(sealed)
		require.NoError(t, err)

		require.Equal(t, realVProof.CommD, Commitment(rawCommD))
		require.Equal(t, realVProof.CommR, Commitment(rawCommR))

		require.Equal(t, realVProof.RegisteredProof, StringRegisteredProofType("StackedDrg2KiBV1_1"))

		replicaID, err := spt.ReplicaId(miner, num, ticket[:], realVProof.CommD[:])
		require.NoError(t, err)
		require.Equal(t, realVProof.ReplicaID, Commitment(replicaID))

		require.Equal(t, realVProof.Seed, Ticket(seed))
		require.Equal(t, realVProof.Ticket, Ticket(ticket))
	})

	// Test the full PSProve-equivalent path: Go round-tripped JSON -> SealCommitPhase2 -> VerifySeal
	t.Run("c2-with-go-roundtripped-json", func(t *testing.T) {
		// This replicates the PSProve FFI path:
		// 1. Deserialize raw Rust C1 JSON into Go struct (already done above)
		// 2. Re-serialize Go struct to JSON
		goJSON, err := json.Marshal(realVProof)
		require.NoError(t, err)

		// 3. Feed Go-produced JSON to SealCommitPhase2
		snarkProof, err := ffi.SealCommitPhase2(goJSON, num, miner)
		require.NoError(t, err)
		require.NotEmpty(t, snarkProof, "SealCommitPhase2 returned empty proof")

		t.Logf("C2 proof generated: %d bytes", len(snarkProof))

		// 4. Verify the proof (same as computePoRep verification)
		ok, err := ffi.VerifySeal(sproof.SealVerifyInfo{
			SealProof: spt,
			SectorID: abi.SectorID{
				Miner:  miner,
				Number: num,
			},
			DealIDs:               nil,
			Randomness:            abi.SealRandomness(realVProof.Ticket[:]),
			InteractiveRandomness: abi.InteractiveSealRandomness(realVProof.Seed[:]),
			Proof:                 snarkProof,
			SealedCID:             sealed,
			UnsealedCID:           unsealed,
		})
		require.NoError(t, err)
		require.True(t, ok, "SNARK proof from Go-roundtripped JSON failed VerifySeal")
		t.Logf("VerifySeal PASSED for Go-roundtripped C1 JSON -> FFI C2")
	})

	// Test with raw Rust JSON for comparison
	t.Run("c2-with-raw-rust-json", func(t *testing.T) {
		// Same but with original Rust bytes
		snarkProof, err := ffi.SealCommitPhase2(c1out, num, miner)
		require.NoError(t, err)
		require.NotEmpty(t, snarkProof, "SealCommitPhase2 returned empty proof")

		ok, err := ffi.VerifySeal(sproof.SealVerifyInfo{
			SealProof: spt,
			SectorID: abi.SectorID{
				Miner:  miner,
				Number: num,
			},
			DealIDs:               nil,
			Randomness:            abi.SealRandomness(ticket[:]),
			InteractiveRandomness: abi.InteractiveSealRandomness(seed[:]),
			Proof:                 snarkProof,
			SealedCID:             sealed,
			UnsealedCID:           unsealed,
		})
		require.NoError(t, err)
		require.True(t, ok, "SNARK proof from raw Rust JSON failed VerifySeal")
		t.Logf("VerifySeal PASSED for raw Rust C1 JSON -> FFI C2")
	})
}

// c1OutputWrapper mirrors the wrapper in lib/ffi/cuzk_funcs.go and
// tasks/proofshare/task_prove.go. The cuzk Rust server deserializes this;
// Phase1Out is base64-encoded by Go's json.Marshal since it's []byte.
type c1OutputWrapper struct {
	SectorNum  int64  `json:"SectorNum"`
	Phase1Out  []byte `json:"Phase1Out"`
	SectorSize uint64 `json:"SectorSize"`
}

// TestCuzkWrapperRoundtrip tests that wrapping C1 JSON in the c1OutputWrapper
// envelope and then unwrapping (simulating what cuzk Rust does) produces
// identical inner JSON bytes.
func TestCuzkWrapperRoundtrip(t *testing.T) {
	c1out, spt, _, _, num, miner, _, _ := sealTestSector(t)

	ssize, err := spt.SectorSize()
	require.NoError(t, err)

	t.Run("raw-rust-json-wrapper-roundtrip", func(t *testing.T) {
		// Wrap raw Rust C1 JSON (this is what PoRepSnarkCuzk in cuzk_funcs.go does)
		wrapped, err := json.Marshal(c1OutputWrapper{
			SectorNum:  int64(num),
			Phase1Out:  c1out,
			SectorSize: uint64(ssize),
		})
		require.NoError(t, err)

		// Simulate Rust-side unwrapping
		var unwrapped c1OutputWrapper
		err = json.Unmarshal(wrapped, &unwrapped)
		require.NoError(t, err)

		// Phase1Out should be the original bytes
		require.True(t, bytes.Equal(c1out, unwrapped.Phase1Out),
			"wrapper roundtrip changed Phase1Out bytes (rust path)")
	})

	t.Run("go-roundtripped-json-wrapper-roundtrip", func(t *testing.T) {
		// This is what PSProve computePoRep does:
		// 1. Deserialize raw C1 into Go struct
		var goStruct Commit1OutRaw
		err := json.Unmarshal(c1out, &goStruct)
		require.NoError(t, err)

		// 2. Re-serialize to JSON
		goJSON, err := json.Marshal(goStruct)
		require.NoError(t, err)

		// 3. Wrap in c1OutputWrapper
		wrapped, err := json.Marshal(c1OutputWrapper{
			SectorNum:  int64(num),
			Phase1Out:  goJSON,
			SectorSize: uint64(ssize),
		})
		require.NoError(t, err)

		// 4. Simulate Rust unwrapping
		var unwrapped c1OutputWrapper
		err = json.Unmarshal(wrapped, &unwrapped)
		require.NoError(t, err)

		// Phase1Out should be the Go-produced JSON
		require.True(t, bytes.Equal(goJSON, unwrapped.Phase1Out),
			"wrapper roundtrip changed Phase1Out bytes (go-roundtripped path)")

		// Now simulate what the cuzk Rust server does: parse the inner JSON
		// as SealCommitPhase1Output (we approximate by parsing into Commit1OutRaw again)
		var innerStruct Commit1OutRaw
		err = json.Unmarshal(unwrapped.Phase1Out, &innerStruct)
		require.NoError(t, err)

		// Compare the struct fields
		require.Equal(t, goStruct.CommD, innerStruct.CommD, "CommD mismatch after wrapper roundtrip")
		require.Equal(t, goStruct.CommR, innerStruct.CommR, "CommR mismatch after wrapper roundtrip")
		require.Equal(t, goStruct.RegisteredProof, innerStruct.RegisteredProof, "RegisteredProof mismatch")
		require.Equal(t, goStruct.ReplicaID, innerStruct.ReplicaID, "ReplicaID mismatch")
		require.Equal(t, goStruct.Seed, innerStruct.Seed, "Seed mismatch")
		require.Equal(t, goStruct.Ticket, innerStruct.Ticket, "Ticket mismatch")
	})

	// Test that wrapping Go-roundtripped JSON and then using it for C2+verify works
	t.Run("go-roundtripped-wrapper-c2-verify", func(t *testing.T) {
		// Replicate the full PSProve cuzk path but using FFI for C2
		// (since we don't have a cuzk server in tests).
		// The key insight: if this works, the JSON content is correct
		// and the bug must be in cuzk's processing; if this fails,
		// the JSON round-trip is the problem.

		var goStruct Commit1OutRaw
		err := json.Unmarshal(c1out, &goStruct)
		require.NoError(t, err)

		goJSON, err := json.Marshal(goStruct)
		require.NoError(t, err)

		// Wrap in c1OutputWrapper (as PSProve does)
		wrapped, err := json.Marshal(c1OutputWrapper{
			SectorNum:  int64(num),
			Phase1Out:  goJSON,
			SectorSize: uint64(ssize),
		})
		require.NoError(t, err)

		// Simulate cuzk unwrapping: parse outer wrapper, base64-decode Phase1Out
		var wrapper c1OutputWrapper
		err = json.Unmarshal(wrapped, &wrapper)
		require.NoError(t, err)

		// wrapper.Phase1Out is already decoded from base64 by Go's json.Unmarshal
		innerJSON := wrapper.Phase1Out

		// Now simulate what Rust does with the inner JSON: pass to SealCommitPhase2
		// (In the real cuzk path, Rust deserializes this into SealCommitPhase1Output
		// and then calls seal_commit_phase2. We test with FFI which does the same.)
		snarkProof, err := ffi.SealCommitPhase2(innerJSON, num, miner)
		require.NoError(t, err)
		require.NotEmpty(t, snarkProof)

		// Verify using the Go struct values (as computePoRep does)
		commR, err := commcid.ReplicaCommitmentV1ToCID(goStruct.CommR[:])
		require.NoError(t, err)
		commD, err := commcid.DataCommitmentV1ToCID(goStruct.CommD[:])
		require.NoError(t, err)

		abiSpt, err := goStruct.RegisteredProof.ToABI()
		require.NoError(t, err)

		ok, err := ffi.VerifySeal(sproof.SealVerifyInfo{
			SealProof: abiSpt,
			SectorID: abi.SectorID{
				Miner:  miner,
				Number: num,
			},
			DealIDs:               nil,
			Randomness:            abi.SealRandomness(goStruct.Ticket[:]),
			InteractiveRandomness: abi.InteractiveSealRandomness(goStruct.Seed[:]),
			Proof:                 snarkProof,
			SealedCID:             commR,
			UnsealedCID:           commD,
		})
		require.NoError(t, err)
		require.True(t, ok, "SNARK proof from Go-roundtripped + wrapped JSON failed VerifySeal")
		t.Logf("VerifySeal PASSED for full PSProve-equivalent path (Go roundtrip + wrapper)")
	})

	// Test the base64 encoding explicitly to make sure Go and Rust agree
	t.Run("base64-encoding-check", func(t *testing.T) {
		// Verify that Go's json.Marshal of []byte matches what Rust
		// base64::STANDARD.decode expects.
		testData := []byte(`{"test": "data", "num": 42}`)

		// Go json.Marshal wraps []byte as base64 string
		wrapper := c1OutputWrapper{Phase1Out: testData}
		wrapped, err := json.Marshal(wrapper)
		require.NoError(t, err)

		// Extract the base64 string from the JSON
		var rawMap map[string]json.RawMessage
		err = json.Unmarshal(wrapped, &rawMap)
		require.NoError(t, err)

		var b64str string
		err = json.Unmarshal(rawMap["Phase1Out"], &b64str)
		require.NoError(t, err)

		// Decode with standard base64 (what Rust STANDARD engine uses)
		decoded, err := base64.StdEncoding.DecodeString(b64str)
		require.NoError(t, err)

		require.True(t, bytes.Equal(testData, decoded),
			"base64 roundtrip failed: expected %q, got %q", testData, decoded)
	})
}

// TestDoubleRoundtripPorepVproof simulates the PSProve data flow:
// Rust C1 -> Go unmarshal -> Go marshal (ProofData) -> Go unmarshal -> Go marshal (vproof)
// This is TWO Go JSON round-trips, matching the actual PSProve pipeline.
func TestDoubleRoundtripPorepVproof(t *testing.T) {
	c1out, spt, sealed, unsealed, num, miner, _, _ := sealTestSector(t)

	// Round-trip 1: upload side (task_client_upload_porep.go)
	var uploadStruct Commit1OutRaw
	err := json.Unmarshal(c1out, &uploadStruct)
	require.NoError(t, err)

	// Wrap in ProofData-like envelope
	type proofDataLike struct {
		SectorID *abi.SectorID
		PoRep    *Commit1OutRaw
	}
	envelope := proofDataLike{
		SectorID: &abi.SectorID{Miner: miner, Number: num},
		PoRep:    &uploadStruct,
	}
	envelopeBytes, err := json.Marshal(envelope)
	require.NoError(t, err)

	// Round-trip 2: provider side (task_prove.go Do())
	var receivedEnvelope proofDataLike
	err = json.Unmarshal(envelopeBytes, &receivedEnvelope)
	require.NoError(t, err)

	// This is what computePoRep gets as 'request'
	request := receivedEnvelope.PoRep

	// json.Marshal(request) - this is the vproof in computePoRep
	vproof, err := json.Marshal(request)
	require.NoError(t, err)

	// Verify the double-roundtripped JSON still produces valid C2
	snarkProof, err := ffi.SealCommitPhase2(vproof, num, miner)
	require.NoError(t, err)
	require.NotEmpty(t, snarkProof)

	abiSpt, err := request.RegisteredProof.ToABI()
	require.NoError(t, err)
	require.Equal(t, spt, abiSpt, "RegisteredProof mismatch after double roundtrip")

	commR, err := commcid.ReplicaCommitmentV1ToCID(request.CommR[:])
	require.NoError(t, err)
	commD, err := commcid.DataCommitmentV1ToCID(request.CommD[:])
	require.NoError(t, err)

	// Verify with the in-memory struct values (exactly as computePoRep does)
	ok, err := ffi.VerifySeal(sproof.SealVerifyInfo{
		SealProof: abiSpt,
		SectorID: abi.SectorID{
			Miner:  miner,
			Number: num,
		},
		DealIDs:               nil,
		Randomness:            abi.SealRandomness(request.Ticket[:]),
		InteractiveRandomness: abi.InteractiveSealRandomness(request.Seed[:]),
		Proof:                 snarkProof,
		SealedCID:             commR,
		UnsealedCID:           commD,
	})
	require.NoError(t, err)
	require.True(t, ok, "SNARK proof from double-roundtripped JSON failed VerifySeal")
	t.Logf("VerifySeal PASSED for double-roundtripped PSProve pipeline (2x Go JSON roundtrip)")

	// Also verify against the original CIDs for belt-and-suspenders
	ok2, err := ffi.VerifySeal(sproof.SealVerifyInfo{
		SealProof: spt,
		SectorID: abi.SectorID{
			Miner:  miner,
			Number: num,
		},
		DealIDs:               nil,
		Randomness:            abi.SealRandomness(request.Ticket[:]),
		InteractiveRandomness: abi.InteractiveSealRandomness(request.Seed[:]),
		Proof:                 snarkProof,
		SealedCID:             sealed,
		UnsealedCID:           unsealed,
	})
	require.NoError(t, err)
	require.True(t, ok2, "SNARK proof failed VerifySeal with original CIDs")
}

// TestRepeatedC2SameSector seals one sector and calls SealCommitPhase2
// multiple times with both raw Rust JSON and Go-roundtripped JSON.
// This isolates whether the intermittent "post seal aggregation verifies"
// failure is:
//   - Data-dependent (different sector data each time) -> would not reproduce here
//   - A process-level FFI issue (e.g., repeated calls) -> would reproduce here
//   - Specific to Go-roundtripped JSON -> would only fail on Go path
func TestRepeatedC2SameSector(t *testing.T) {
	c1out, _, sealed, unsealed, num, miner, ticket, seed := sealTestSector(t)

	// Prepare Go-roundtripped JSON
	var goStruct Commit1OutRaw
	err := json.Unmarshal(c1out, &goStruct)
	require.NoError(t, err)
	goJSON, err := json.Marshal(goStruct)
	require.NoError(t, err)

	const iterations = 10

	t.Run("raw-rust-json-repeated", func(t *testing.T) {
		for i := 0; i < iterations; i++ {
			snarkProof, err := ffi.SealCommitPhase2(c1out, num, miner)
			if err != nil {
				t.Fatalf("iteration %d: SealCommitPhase2 with raw Rust JSON failed: %v", i, err)
			}
			ok, err := ffi.VerifySeal(sproof.SealVerifyInfo{
				SealProof:             abi.RegisteredSealProof_StackedDrg2KiBV1_1,
				SectorID:              abi.SectorID{Miner: miner, Number: num},
				Randomness:            abi.SealRandomness(ticket[:]),
				InteractiveRandomness: abi.InteractiveSealRandomness(seed[:]),
				Proof:                 snarkProof,
				SealedCID:             sealed,
				UnsealedCID:           unsealed,
			})
			if err != nil {
				t.Fatalf("iteration %d: VerifySeal with raw Rust JSON failed: %v", i, err)
			}
			if !ok {
				t.Fatalf("iteration %d: VerifySeal returned false for raw Rust JSON", i)
			}
			t.Logf("iteration %d: raw Rust JSON C2+verify PASSED", i)
		}
	})

	t.Run("go-roundtripped-json-repeated", func(t *testing.T) {
		for i := 0; i < iterations; i++ {
			snarkProof, err := ffi.SealCommitPhase2(goJSON, num, miner)
			if err != nil {
				t.Fatalf("iteration %d: SealCommitPhase2 with Go JSON failed: %v", i, err)
			}
			ok, err := ffi.VerifySeal(sproof.SealVerifyInfo{
				SealProof:             abi.RegisteredSealProof_StackedDrg2KiBV1_1,
				SectorID:              abi.SectorID{Miner: miner, Number: num},
				Randomness:            abi.SealRandomness(ticket[:]),
				InteractiveRandomness: abi.InteractiveSealRandomness(seed[:]),
				Proof:                 snarkProof,
				SealedCID:             sealed,
				UnsealedCID:           unsealed,
			})
			if err != nil {
				t.Fatalf("iteration %d: VerifySeal with Go JSON failed: %v", i, err)
			}
			if !ok {
				t.Fatalf("iteration %d: VerifySeal returned false for Go JSON", i)
			}
			t.Logf("iteration %d: Go-roundtripped JSON C2+verify PASSED", i)
		}
	})
}

// TestMultipleSectorsC2 seals multiple independent sectors and tests C2
// for each one. This checks whether the intermittent failure is related
// to sealing multiple sectors in the same process.
func TestMultipleSectorsC2(t *testing.T) {
	const numSectors = 5

	for i := 0; i < numSectors; i++ {
		i := i
		t.Run(fmt.Sprintf("sector-%d-raw", i), func(t *testing.T) {
			c1out, _, sealed, unsealed, num, miner, ticket, seed := sealTestSector(t)

			snarkProof, err := ffi.SealCommitPhase2(c1out, num, miner)
			require.NoError(t, err, "sector %d: SealCommitPhase2 raw failed", i)

			ok, err := ffi.VerifySeal(sproof.SealVerifyInfo{
				SealProof:             abi.RegisteredSealProof_StackedDrg2KiBV1_1,
				SectorID:              abi.SectorID{Miner: miner, Number: num},
				Randomness:            abi.SealRandomness(ticket[:]),
				InteractiveRandomness: abi.InteractiveSealRandomness(seed[:]),
				Proof:                 snarkProof,
				SealedCID:             sealed,
				UnsealedCID:           unsealed,
			})
			require.NoError(t, err, "sector %d: VerifySeal raw failed", i)
			require.True(t, ok, "sector %d: VerifySeal returned false for raw", i)
			t.Logf("sector %d: raw C2+verify PASSED", i)
		})

		t.Run(fmt.Sprintf("sector-%d-go-roundtrip", i), func(t *testing.T) {
			c1out, _, sealed, unsealed, num, miner, ticket, seed := sealTestSector(t)

			var goStruct Commit1OutRaw
			err := json.Unmarshal(c1out, &goStruct)
			require.NoError(t, err)
			goJSON, err := json.Marshal(goStruct)
			require.NoError(t, err)

			snarkProof, err := ffi.SealCommitPhase2(goJSON, num, miner)
			require.NoError(t, err, "sector %d: SealCommitPhase2 go-roundtrip failed", i)

			ok, err := ffi.VerifySeal(sproof.SealVerifyInfo{
				SealProof:             abi.RegisteredSealProof_StackedDrg2KiBV1_1,
				SectorID:              abi.SectorID{Miner: miner, Number: num},
				Randomness:            abi.SealRandomness(ticket[:]),
				InteractiveRandomness: abi.InteractiveSealRandomness(seed[:]),
				Proof:                 snarkProof,
				SealedCID:             sealed,
				UnsealedCID:           unsealed,
			})
			require.NoError(t, err, "sector %d: VerifySeal go-roundtrip failed", i)
			require.True(t, ok, "sector %d: VerifySeal returned false for go-roundtrip", i)
			t.Logf("sector %d: go-roundtrip C2+verify PASSED", i)
		})
	}
}
