//! Milestone B — **§3.2 step 11 slice:** native bellperson Groth16 **prove + verify** (uses the BLS12-381
//! **pairing** engine), then [`cuzk_vk::prove_groth16_partition`] on a live ICD (`CUZK_VK_SKIP_SMOKE=0`).
//!
//! This does **not** route bellperson’s R1CS witness through Vulkan kernels yet; it gates that the
//! optional `bellperson` stack and `cuzk-vk` smoke coexist on the same machine for future E2E work.

mod common;

use std::sync::Arc;

use bellperson::groth16::{
    create_random_proof, generate_random_parameters, prepare_verifying_key, verify_proof,
};
use blstrs::{Bls12, Scalar};
use ff::Field;
use rand::thread_rng;

use common::MulCircuit;
use cuzk_vk::{
    prove_groth16_partition, VkGroth16Job, VkProofKind, VkProverContext, VulkanDevice,
};

fn skip_vulkan_smoke() -> bool {
    !matches!(std::env::var("CUZK_VK_SKIP_SMOKE").as_deref(), Ok("0"))
}

#[test]
fn bellperson_groth16_verify_then_vulkan_partition_smoke() {
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
        "Groth16 verify (pairing) failed before Vulkan smoke"
    );

    if skip_vulkan_smoke() {
        return;
    }
    let dev = match VulkanDevice::new() {
        Ok(d) => Arc::new(d),
        Err(_) => return,
    };
    let ctx = VkProverContext::new(dev);
    let job = VkGroth16Job {
        kind: VkProofKind::PoRepC2,
        circuit_log_n: 4,
        partition_index: None,
    };
    let t = prove_groth16_partition(&ctx, &job).expect("partition smoke after bellperson");
    assert!(t.total.as_nanos() > 0);
}
