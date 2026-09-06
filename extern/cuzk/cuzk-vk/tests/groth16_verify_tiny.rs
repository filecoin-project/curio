//! Phase K: native Groth16 **prove + verify** on BLS12-381 (bellperson), independent of Vulkan.
//!
//! Establishes that the bellperson fork used by `cuzk-vk` tests is sound for a minimal R1CS; the
//! Vulkan backend (`prove_groth16_partition`) remains compute smoke until wired to this path.

mod common;

use bellperson::groth16::{
    create_random_proof, generate_random_parameters, prepare_verifying_key, verify_proof,
};
use blstrs::{Bls12, Scalar};
use ff::Field;
use rand::thread_rng;

use common::MulCircuit;

#[test]
fn groth16_mul_circuit_prove_and_verify() {
    let mut rng = thread_rng();
    let params = generate_random_parameters::<Bls12, _, _>(
        MulCircuit {
            a: Some(Scalar::ONE),
            b: Some(Scalar::ONE),
        },
        &mut rng,
    )
    .expect("paramgen");
    let pvk = prepare_verifying_key(&params.vk);

    let a = Scalar::from(7u64);
    let b = Scalar::from(11u64);
    let proof = create_random_proof(
        MulCircuit {
            a: Some(a),
            b: Some(b),
        },
        &params,
        &mut rng,
    )
    .expect("prove");

    assert!(
        verify_proof(&pvk, &proof, &[a * b]).expect("verify"),
        "Groth16 verify failed"
    );
}
