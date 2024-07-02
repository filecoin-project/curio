package proof

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/filecoin-project/filecoin-ffi/cgo"
)

func TestDecode(t *testing.T) {
	//binFile := "../../extern/supra_seal/demos/c2-test/resources/test/commit-phase1-output"
	binFile := "../../commit-phase1-output"

	rawData, err := os.ReadFile(binFile)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := DecodeCommit1OutRaw(bytes.NewReader(rawData))
	if err != nil {
		t.Fatal(err)
	}

	p1o, err := json.Marshal(dec)
	if err != nil {
		t.Fatal(err)
	}

	var proverID = [32]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
	pba := cgo.AsByteArray32(proverID[:])

	_, err = cgo.SealCommitPhase2(cgo.AsSliceRefUint8(p1o), uint64(0), &pba)
	if err != nil {
		t.Fatal(err)
	}
}

/*
// bin/main.rs
use storage_proofs_core::settings::SETTINGS;
use anyhow::{Context, Result};
use std::fs::{read, write};
use std::path::PathBuf;
use bincode::deserialize;
use serde_json;
use filecoin_proofs_v1::{SealCommitPhase1Output, SectorShape32GiB};
use filecoin_proofs_api::RegisteredSealProof::StackedDrg32GiBV1_1;
use filecoin_proofs_api::RegisteredSealProof::StackedDrg32GiBV1;

use filecoin_proofs_api::seal::{SealCommitPhase1Output as SealCommitPhase1OutputOut, VanillaSealProof};

fn main() -> Result<()> {
    println!("{:#?}", *SETTINGS);

    let commit_phase1_output_path = PathBuf::from("/tmp/c1o");

    let commit_phase1_output_bytes = read(&commit_phase1_output_path)
        .with_context(|| {
            format!(
                "couldn't read file commit_phase1_output_path={:?}",
                commit_phase1_output_path
            )
        })?;
    println!(
        "commit_phase1_output_bytes len {}",
        commit_phase1_output_bytes.len()
    );

    let res: SealCommitPhase1Output<SectorShape32GiB> =
        deserialize(&commit_phase1_output_bytes)?;

    let SealCommitPhase1Output::<SectorShape32GiB> {
        vanilla_proofs,
        comm_r,
        comm_d,
        replica_id,
        seed,
        ticket,
    } = res;

    let registered_proof= StackedDrg32GiBV1_1;

    let res2: SealCommitPhase1OutputOut = SealCommitPhase1OutputOut{
        registered_proof,
        vanilla_proofs: VanillaSealProof::from_raw::<SectorShape32GiB>(StackedDrg32GiBV1, &vanilla_proofs)?,
        comm_r,
        comm_d,
        replica_id: replica_id.into(),
        seed,
        ticket,
    };

    let result = serde_json::to_vec(&res2)?;

    let output_path = PathBuf::from("/tmp/c1o.json");
    write(&output_path, result)
        .with_context(|| format!("couldn't write to file output_path={:?}", output_path))?;

    Ok(())
}

func TestDecodeSNRustDec(t *testing.T) {
	//binFile := "../../extern/supra_seal/demos/c2-test/resources/test/commit-phase1-output"
	binFile := "../../commit-phase1-output.json"

	rawData, err := os.ReadFile(binFile)
	if err != nil {
		t.Fatal(err)
	}

	var dec Commit1OutRaw
	err = json.Unmarshal(rawData, &dec)
	if err != nil {
		t.Fatal(err)
	}

	commr := dec.CommR
	commd := dec.CommD

	sl, _ := commcid.ReplicaCommitmentV1ToCID(commr[:])
	us, _ := commcid.DataCommitmentV1ToCID(commd[:])

	fmt.Println("sealed:", sl)
	fmt.Println("unsealed:", us)
	fmt.Printf("replicaid: %x\n", dec.ReplicaID)

	var proverID = [32]byte{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9}
	pba := cgo.AsByteArray32(proverID[:])

	_, err = cgo.SealCommitPhase2(cgo.AsSliceRefUint8(rawData), uint64(0), &pba)
	if err != nil {
		t.Fatal(err)
	}
}
*/
