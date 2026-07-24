//! Compressed Groth16 proof bytes (`cuzk_vk::write_groth16_proof_compressed`) round-trip through bellperson.

mod common;

use bellperson::groth16::{create_random_proof, generate_random_parameters, Proof};
use blstrs::{Bls12, Scalar};
use ff::Field;
use cuzk_vk::{write_groth16_proof_compressed, GROTH16_PROOF_COMPRESSED_BYTES};
use rand::thread_rng;
use std::io::Cursor;

use common::MulCircuit;

#[test]
fn groth16_compressed_proof_matches_bellperson_read() {
    let mut rng = thread_rng();
    let params = generate_random_parameters::<Bls12, _, _>(
        MulCircuit {
            a: Some(Scalar::ONE),
            b: Some(Scalar::from(3u64)),
        },
        &mut rng,
    )
    .expect("paramgen");
    let proof = create_random_proof(
        MulCircuit {
            a: Some(Scalar::ONE),
            b: Some(Scalar::from(3u64)),
        },
        &params,
        &mut rng,
    )
    .expect("prove");

    let mut buf = [0u8; GROTH16_PROOF_COMPRESSED_BYTES];
    write_groth16_proof_compressed(&proof.a, &proof.b, &proof.c, &mut buf);
    let round = Proof::<Bls12>::read(Cursor::new(buf.as_slice())).expect("read compressed");
    assert_eq!(round, proof);
}
